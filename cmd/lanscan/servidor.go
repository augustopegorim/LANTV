package main

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

type Servidor struct {
	v    *Varredura
	addr string
}

func (s *Servidor) handler() http.Handler {
	web, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(web))
	mux.HandleFunc("GET /api/redes", func(w http.ResponseWriter, r *http.Request) {
		nome, _ := os.Hostname()
		json2(w, map[string]any{"computador": nome, "redes": RedesLocais()})
	})
	mux.HandleFunc("GET /api/varredura", func(w http.ResponseWriter, r *http.Request) { json2(w, s.v.Resumo()) })
	mux.HandleFunc("POST /api/varredura", func(w http.ResponseWriter, r *http.Request) {
		var o Opcoes
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&o) != nil {
			erro(w, 400, "pedido inválido")
			return
		}
		if err := s.v.Iniciar(o); err != nil {
			erro(w, 400, err.Error())
			return
		}
		json2(w, s.v.Resumo())
	})
	mux.HandleFunc("DELETE /api/varredura", func(w http.ResponseWriter, r *http.Request) { s.v.Cancelar(); json2(w, s.v.Resumo()) })
	mux.HandleFunc("GET /api/exportar", s.exportar)
	return protegerLocal(mux, s)
}

// protegerLocal impede que outra página aberta no navegador dispare varreduras
// (ataque por "localhost"): só aceita o Host e a Origem deste próprio programa.
func protegerLocal(h http.Handler, s *Servidor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, porta, _ := net.SplitHostPort(r.Host)
		_, minha, _ := net.SplitHostPort(s.addr)
		if (host != "127.0.0.1" && host != "localhost") || porta != minha {
			http.Error(w, "acesso negado", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "acesso negado", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func json2(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func erro(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	json2(w, map[string]string{"erro": msg})
}

func (s *Servidor) exportar(w http.ResponseWriter, r *http.Request) {
	res := s.v.Resumo()
	nome := "varredura-" + time.Now().Format("2006-01-02-1504")
	if r.URL.Query().Get("formato") == "json" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+nome+`.json"`)
		json2(w, res)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nome+`.csv"`)
	w.Write([]byte("\xEF\xBB\xBF")) // o Excel só abre acentos corretamente com isto
	c := csv.NewWriter(w)
	c.Comma = ';'
	c.Write([]string{"IP", "Nome", "Nome DNS", "Nome mDNS", "Nome NetBIOS", "Grupo", "MAC", "Fabricante", "Tipo", "Sistema", "TTL", "Tempo (ms)", "Portas abertas", "Detalhes das portas", "Mesma rede", "Descoberto por"})
	for _, h := range res.Hosts {
		var ps, det []string
		for _, p := range h.Portas {
			ps = append(ps, fmt.Sprint(p.Porta))
			if p.Info != "" {
				det = append(det, fmt.Sprintf("%d: %s", p.Porta, p.Info))
			}
		}
		rede := "não"
		if h.MesmaRede {
			rede = "sim"
		}
		c.Write([]string{h.IP, h.Nome, h.NomeDNS, h.NomeMDNS, h.NomeNetBIOS, h.Grupo, h.MAC, h.Fabricante, h.Tipo, h.SO,
			fmt.Sprint(h.TTL), fmt.Sprintf("%.1f", h.RTTms), strings.Join(ps, " "), strings.Join(det, " | "), rede, h.Descoberto})
	}
	c.Flush()
}
