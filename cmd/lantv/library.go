package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	estadoPreparando = "preparando"
	estadoPronto     = "pronto"
	estadoErro       = "erro"
)

var (
	extVideos  = map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".avi": true, ".webm": true, ".m4v": true, ".wmv": true, ".ts": true}
	extImagens = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true}
)

type Item struct {
	ID       string  `json:"id"`
	Nome     string  `json:"nome"`
	Ext      string  `json:"ext"`
	Imagem   bool    `json:"imagem"`
	Segundos int     `json:"segundos"` // tempo na tela (só imagens)
	Ativo    bool    `json:"ativo"`
	Duracao  float64 `json:"duracao"`
	Estado   string  `json:"estado"`
	Erro     string  `json:"erro,omitempty"`
}

// Biblioteca guarda a lista de propagandas e converte cada uma para o
// formato do canal em segundo plano.
type Biblioteca struct {
	mu    sync.Mutex
	dir   string
	cfg   *ConfigStore
	itens []*Item
	fila  chan string
}

func abrirBiblioteca(dir string, cfg *ConfigStore) (*Biblioteca, error) {
	for _, d := range []string{"midia", "cache", "miniaturas"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	l := &Biblioteca{dir: dir, cfg: cfg, fila: make(chan string, 4096)}
	if b, err := os.ReadFile(filepath.Join(dir, "playlist.json")); err == nil {
		json.Unmarshal(b, &l.itens)
	}
	for _, it := range l.itens {
		if it.Estado != estadoPronto || !l.existe(l.arqCache(it)) {
			it.Estado = estadoPreparando
			l.fila <- it.ID
		}
	}
	go l.trabalhar()
	return l, nil
}

func (l *Biblioteca) existe(p string) bool { _, err := os.Stat(p); return err == nil }

func (l *Biblioteca) original(it *Item) string {
	return filepath.Join(l.dir, "midia", it.ID+it.Ext)
}

func (l *Biblioteca) arqCache(it *Item) string {
	return filepath.Join(l.dir, "cache", fmt.Sprintf("%s-%s-%d.mp4", it.ID, l.cfg.Get().Qualidade, it.Segundos))
}

func (l *Biblioteca) Miniatura(id string) string {
	return filepath.Join(l.dir, "miniaturas", id+".jpg")
}

func (l *Biblioteca) salvar() {
	b, _ := json.MarshalIndent(l.itens, "", "  ")
	tmp := filepath.Join(l.dir, "playlist.json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(l.dir, "playlist.json"))
	}
}

func novoID() string {
	b := make([]byte, 5)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (l *Biblioteca) Listar() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Item, len(l.itens))
	for i, it := range l.itens {
		out[i] = *it
	}
	return out
}

func extensaoPermitida(ext string) bool { return extVideos[ext] || extImagens[ext] }

// Adicionar registra um arquivo já gravado em midia/<id><ext>.
func (l *Biblioteca) Adicionar(id, nome, ext string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.itens = append(l.itens, &Item{ID: id, Nome: nome, Ext: ext, Imagem: extImagens[ext],
		Segundos: 10, Ativo: true, Estado: estadoPreparando})
	l.salvar()
	l.fila <- id
}

type Alteracao struct {
	Nome     *string `json:"nome"`
	Ativo    *bool   `json:"ativo"`
	Segundos *int    `json:"segundos"`
}

func (l *Biblioteca) Atualizar(id string, a Alteracao) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.itens {
		if it.ID != id {
			continue
		}
		if a.Nome != nil && strings.TrimSpace(*a.Nome) != "" {
			it.Nome = strings.TrimSpace(*a.Nome)
		}
		if a.Ativo != nil {
			it.Ativo = *a.Ativo
		}
		if a.Segundos != nil && it.Imagem && *a.Segundos != it.Segundos {
			if *a.Segundos < 3 || *a.Segundos > 120 {
				return fmt.Errorf("o tempo da imagem deve ficar entre 3 e 120 segundos")
			}
			it.Segundos, it.Estado = *a.Segundos, estadoPreparando
			l.fila <- it.ID
		}
		l.salvar()
		return nil
	}
	return fmt.Errorf("propaganda não encontrada")
}

func (l *Biblioteca) Reordenar(ids []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	por := map[string]*Item{}
	for _, it := range l.itens {
		por[it.ID] = it
	}
	var novos []*Item
	for _, id := range ids {
		if it, ok := por[id]; ok {
			novos = append(novos, it)
			delete(por, id)
		}
	}
	for _, it := range l.itens { // o que não veio na lista vai para o fim
		if _, ok := por[it.ID]; ok {
			novos = append(novos, it)
		}
	}
	l.itens = novos
	l.salvar()
}

func (l *Biblioteca) Remover(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var resto []*Item
	for _, it := range l.itens {
		if it.ID == id {
			os.Remove(l.original(it))
			os.Remove(l.Miniatura(id))
			if ms, _ := filepath.Glob(filepath.Join(l.dir, "cache", id+"-*")); ms != nil {
				for _, m := range ms {
					os.Remove(m)
				}
			}
			continue
		}
		resto = append(resto, it)
	}
	l.itens = resto
	l.salvar()
}

// Reconverter refaz todas as propagandas (troca de qualidade do canal).
func (l *Biblioteca) Reconverter() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, it := range l.itens {
		if !l.existe(l.arqCache(it)) {
			it.Estado = estadoPreparando
			l.fila <- it.ID
		}
	}
	l.salvar()
}

// Proximo devolve a propaganda que toca depois de `depoisDe` (ordem atual da
// lista, só as ativas e prontas). Sem nenhuma, devolve a vinheta, para o
// canal nunca ficar vazio.
func (l *Biblioteca) Proximo(depoisDe string) (id, nome, caminho string) {
	l.mu.Lock()
	n := len(l.itens)
	inicio := 0
	for i, it := range l.itens {
		if it.ID == depoisDe {
			inicio = i + 1
		}
	}
	for k := 0; k < n; k++ {
		it := l.itens[(inicio+k)%n]
		if it.Ativo && it.Estado == estadoPronto {
			if p := l.arqCache(it); l.existe(p) {
				l.mu.Unlock()
				return it.ID, it.Nome, p
			}
		}
	}
	l.mu.Unlock()
	return "", "LANTV", l.vinheta()
}

func (l *Biblioteca) vinheta() string {
	w, h, _ := l.cfg.Get().Dim()
	p := filepath.Join(l.dir, "cache", fmt.Sprintf("vinheta-%dx%d.mp4", w, h))
	if l.existe(p) {
		return p
	}
	err := ffmpegUmaVez(append([]string{
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=0x102040:s=%dx%d:r=%d:d=10", w, h, fps),
		"-f", "lavfi", "-t", "10", "-i", "anullsrc=r=48000:cl=stereo",
		"-vf", "drawtext=text='LANTV':fontsize=h/6:fontcolor=white:x=(w-tw)/2:y=(h-th)/2" + fonteTexto()},
		append(codecArquivo(), "-t", "10", p)...))
	if err != nil {
		log.Printf("não foi possível criar a vinheta: %v", err)
	}
	return p
}

func codecArquivo() []string {
	return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "21", "-maxrate", "30M", "-bufsize", "60M",
		"-g", strconv.Itoa(fps * 2), "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2"}
}

func (l *Biblioteca) trabalhar() {
	for id := range l.fila {
		l.converter(id)
	}
}

func (l *Biblioteca) converter(id string) {
	l.mu.Lock()
	var it Item
	achou := false
	for _, x := range l.itens {
		if x.ID == id {
			it, achou = *x, true
		}
	}
	l.mu.Unlock()
	if !achou {
		return
	}
	destino := l.arqCache(&it)
	if !l.existe(destino) {
		log.Printf("preparando %q", it.Nome)
	}
	err := l.gerar(&it, destino)
	dur := 0.0
	if err == nil {
		dur = duracao(destino)
		l.miniatura(&it)
	} else {
		log.Printf("erro ao preparar %q: %v", it.Nome, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, x := range l.itens {
		if x.ID != id || x.Segundos != it.Segundos {
			continue // removida ou alterada de novo: outra conversão já está na fila
		}
		if err != nil {
			x.Estado, x.Erro = estadoErro, "formato não suportado"
		} else {
			x.Estado, x.Erro, x.Duracao = estadoPronto, "", dur
		}
	}
	l.salvar()
	if ms, _ := filepath.Glob(filepath.Join(l.dir, "cache", id+"-*")); ms != nil {
		for _, m := range ms { // apaga versões antigas (outra qualidade ou tempo)
			if m != destino {
				os.Remove(m)
			}
		}
	}
}

func (l *Biblioteca) escala() string {
	w, h, _ := l.cfg.Get().Dim()
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p", w, h, w, h, fps)
}

func (l *Biblioteca) gerar(it *Item, destino string) error {
	if l.existe(destino) {
		return nil
	}
	orig := l.original(it)
	var args []string
	switch {
	case it.Imagem:
		s := strconv.Itoa(it.Segundos)
		args = []string{"-loop", "1", "-t", s, "-i", orig, "-f", "lavfi", "-t", s, "-i", "anullsrc=r=48000:cl=stereo", "-map", "0:v", "-map", "1:a"}
	case temAudio(orig):
		args = []string{"-i", orig, "-map", "0:v:0", "-map", "0:a:0"}
	default:
		args = []string{"-i", orig, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-map", "0:v:0", "-map", "1:a", "-shortest"}
	}
	tmp := destino + ".tmp.mp4"
	args = append(args, "-vf", l.escala())
	args = append(args, codecArquivo()...)
	if err := ffmpegUmaVez(append(args, tmp)); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destino)
}

func (l *Biblioteca) miniatura(it *Item) {
	if l.existe(l.Miniatura(it.ID)) {
		return
	}
	tempos := []string{"1", "0"} // vídeo: 1 s para fugir de tela preta de abertura
	if it.Imagem {
		tempos = []string{"0"}
	}
	for _, ss := range tempos {
		ffmpegUmaVez([]string{"-ss", ss, "-i", l.original(it), "-frames:v", "1", "-vf", "scale=320:-2", l.Miniatura(it.ID)})
		if l.existe(l.Miniatura(it.ID)) {
			return
		}
	}
}

func temAudio(arquivo string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := comando(ctx, "ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=index", "-of", "csv=p=0", arquivo).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func duracao(arquivo string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := comando(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", arquivo).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

// ffmpegUmaVez roda o ffmpeg até o fim (conversões).
func ffmpegUmaVez(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	out, err := comando(ctx, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
