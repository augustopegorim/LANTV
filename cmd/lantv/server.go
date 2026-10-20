package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed web
var webFS embed.FS

type Servidor struct {
	motor *Motor
	lib   *Biblioteca
	cfg   *ConfigStore
	dados string
	porta int
	senha string
}

func (s *Servidor) Servir() {
	web, _ := fs.Sub(webFS, "web")
	arquivo := func(nome string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := fs.ReadFile(web, nome)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}
	mux := http.NewServeMux()

	// Públicas: o que as TVs precisam.
	mux.HandleFunc("GET /tv", arquivo("tv.html"))
	mux.Handle("GET /web/", http.StripPrefix("/web/", http.FileServerFS(web)))
	canal := http.StripPrefix("/canal/", http.FileServer(http.Dir(s.motor.saida)))
	mux.HandleFunc("GET /canal/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		}
		canal.ServeHTTP(w, r)
	})

	// Painel (protegido por senha, se houver).
	adm := http.NewServeMux()
	adm.HandleFunc("GET /{$}", arquivo("index.html"))
	adm.HandleFunc("GET /api/estado", s.estado)
	adm.HandleFunc("POST /api/modo", s.modo)
	adm.HandleFunc("POST /api/upload", s.upload)
	adm.HandleFunc("POST /api/ordem", s.ordem)
	adm.HandleFunc("PATCH /api/item/{id}", s.alterar)
	adm.HandleFunc("DELETE /api/item/{id}", s.remover)
	adm.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { s.json(w, s.cfg.Get()) })
	adm.HandleFunc("PUT /api/config", s.salvarConfig)
	adm.HandleFunc("GET /api/dispositivos", func(w http.ResponseWriter, r *http.Request) { s.json(w, listarDispositivos()) })
	adm.HandleFunc("GET /miniaturas/{arq}", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(filepath.Base(r.PathValue("arq")), ".jpg")
		w.Header().Set("Cache-Control", "max-age=3600")
		http.ServeFile(w, r, s.lib.Miniatura(id))
	})
	mux.Handle("/", s.proteger(adm))

	log.Printf("painel:  http://%s:%d/", ipLocal(), s.porta)
	log.Printf("canal:   http://%s:%d/canal/canal.m3u8   (página para TV: /tv)", ipLocal(), s.porta)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", s.porta), mux))
}

func (s *Servidor) proteger(h http.Handler) http.Handler {
	if s.senha == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, senha, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(senha), []byte(s.senha)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="LANTV (usuário: qualquer; senha: a senha do painel)"`)
			http.Error(w, "senha necessária", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Servidor) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *Servidor) erro(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	s.json(w, map[string]string{"erro": msg})
}

type itemJSON struct {
	Item
	Miniatura string `json:"miniatura"`
}

func (s *Servidor) itens() []itemJSON {
	var out []itemJSON
	for _, it := range s.lib.Listar() {
		j := itemJSON{Item: it}
		if _, err := os.Stat(s.lib.Miniatura(it.ID)); err == nil {
			j.Miniatura = "/miniaturas/" + it.ID + ".jpg"
		}
		out = append(out, j)
	}
	if out == nil {
		out = []itemJSON{}
	}
	return out
}

func (s *Servidor) estado(w http.ResponseWriter, r *http.Request) {
	s.json(w, map[string]any{"motor": s.motor.Estado(), "itens": s.itens(), "config": s.cfg.Get()})
}

func (s *Servidor) modo(w http.ResponseWriter, r *http.Request) {
	var b struct{ Modo string }
	json.NewDecoder(r.Body).Decode(&b)
	if !s.motor.SetModo(b.Modo) {
		s.erro(w, 400, "modo inválido")
		return
	}
	s.json(w, s.motor.Estado())
}

func (s *Servidor) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		s.erro(w, 400, "envio inválido")
		return
	}
	var recusados []string
	enviados := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.erro(w, 400, "o envio foi interrompido")
			return
		}
		nome := filepath.Base(p.FileName())
		ext := strings.ToLower(filepath.Ext(nome))
		if p.FileName() == "" {
			continue
		}
		if !extensaoPermitida(ext) {
			recusados = append(recusados, nome)
			io.Copy(io.Discard, p)
			continue
		}
		id := novoID()
		destino := filepath.Join(s.dados, "midia", id+ext)
		f, err := os.Create(destino)
		if err != nil {
			s.erro(w, 500, "não foi possível gravar o arquivo")
			return
		}
		_, err = io.Copy(f, p)
		f.Close()
		if err != nil {
			os.Remove(destino)
			s.erro(w, 400, "o envio foi interrompido")
			return
		}
		s.lib.Adicionar(id, strings.TrimSuffix(nome, filepath.Ext(nome)), ext)
		enviados++
	}
	s.json(w, map[string]any{"enviados": enviados, "recusados": recusados})
}

func (s *Servidor) ordem(w http.ResponseWriter, r *http.Request) {
	var b struct{ Ids []string }
	json.NewDecoder(r.Body).Decode(&b)
	s.lib.Reordenar(b.Ids)
	s.json(w, s.itens())
}

func (s *Servidor) alterar(w http.ResponseWriter, r *http.Request) {
	var a Alteracao
	if json.NewDecoder(r.Body).Decode(&a) != nil {
		s.erro(w, 400, "pedido inválido")
		return
	}
	if err := s.lib.Atualizar(r.PathValue("id"), a); err != nil {
		s.erro(w, 400, err.Error())
		return
	}
	s.json(w, s.itens())
}

func (s *Servidor) remover(w http.ResponseWriter, r *http.Request) {
	s.lib.Remover(r.PathValue("id"))
	s.json(w, s.itens())
}

func (s *Servidor) salvarConfig(w http.ResponseWriter, r *http.Request) {
	var c Config
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		s.erro(w, 400, "pedido inválido")
		return
	}
	antiga := s.cfg.Get()
	if err := s.cfg.Set(c); err != nil {
		s.erro(w, 400, err.Error())
		return
	}
	if c != antiga {
		if c.Qualidade != antiga.Qualidade {
			s.lib.Reconverter()
		}
		s.motor.Reiniciar()
	}
	s.json(w, s.cfg.Get())
}

func ipLocal() string {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return "IP-DO-PC"
}
