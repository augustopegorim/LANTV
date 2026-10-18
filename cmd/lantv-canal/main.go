// lantv-canal: protótipo do motor do canal LANTV (fase 0).
//
// Gera um canal contínuo em HLS a partir de uma pasta de propagandas e,
// quando há imagem na entrada HDMI (placa de captura), troca para ela
// sozinho. Sem imagem (aparelho desligado, conteúdo protegido por HDCP ou
// placa ausente) o canal volta para as propagandas.
package main

import (
	"bufio"
	"context"
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-zeromq/zmq4"
)

//go:embed index.html
var paginaTeste []byte

const (
	enderecoZMQ = "tcp://127.0.0.1:5555"
	// Tempo com imagem na HDMI antes de trocar para ela.
	esperaSinal = 2 * time.Second
	// Tempo com tela preta na HDMI antes de voltar para a propaganda.
	// Evita trocar em fades e cenas escuras.
	esperaPreto = 3 * time.Second
)

type config struct {
	propagandas string
	hdmi        string
	hdmiAudio   string
	hdmiTamanho string
	codificador string
	vaapi       string
	saida       string
	cache       string
	porta       int
	largura     int
	altura      int
	fps         int
	bitrate     string
	imagemSeg   int
}

type motor struct {
	cfg config

	mu         sync.Mutex
	modo       string // auto, propaganda ou hdmi
	fonte      int    // 0 = propaganda, 1 = hdmi (o que está no ar)
	preto      bool   // última leitura da HDMI
	desde      time.Time
	hdmiAtiva  bool // ffmpeg rodando com a placa (ou simulação)
	reiniciar  chan struct{}
	extensoes  map[string]bool
	extImagens map[string]bool
}

func main() {
	var cfg config
	flag.StringVar(&cfg.propagandas, "propagandas", "propagandas", "pasta com vídeos e imagens (tocados em ordem alfabética)")
	flag.StringVar(&cfg.hdmi, "hdmi", "", "placa de captura (ex.: /dev/video0), \"simular\" ou vazio para não usar")
	flag.StringVar(&cfg.hdmiAudio, "hdmi-audio", "", "áudio da placa no ALSA (ex.: hw:CARD=MS2109,DEV=0); vazio = mudo")
	flag.StringVar(&cfg.hdmiTamanho, "hdmi-tamanho", "1920x1080", "resolução de captura da placa (ex.: 3840x2160 se a placa capturar 4K)")
	flag.StringVar(&cfg.codificador, "codificador", "x264", "x264 (processador) ou vaapi (Intel Quick Sync, necessário para 4K em mini PC)")
	flag.StringVar(&cfg.vaapi, "vaapi-dispositivo", "/dev/dri/renderD128", "dispositivo da GPU Intel para -codificador vaapi")
	flag.StringVar(&cfg.saida, "saida", "saida", "pasta onde o HLS é gravado")
	flag.StringVar(&cfg.cache, "cache", "cache", "pasta das propagandas convertidas")
	flag.IntVar(&cfg.porta, "porta", 8080, "porta HTTP")
	flag.IntVar(&cfg.largura, "largura", 1920, "largura do canal")
	flag.IntVar(&cfg.altura, "altura", 1080, "altura do canal")
	flag.IntVar(&cfg.fps, "fps", 30, "quadros por segundo")
	flag.StringVar(&cfg.bitrate, "bitrate", "6M", "taxa de vídeo")
	flag.IntVar(&cfg.imagemSeg, "imagem-seg", 10, "segundos de cada imagem na playlist")
	flag.Parse()
	if cfg.codificador != "x264" && cfg.codificador != "vaapi" {
		log.Fatalf("codificador inválido: %q (use x264 ou vaapi)", cfg.codificador)
	}

	m := &motor{
		cfg:        cfg,
		modo:       "auto",
		desde:      time.Now(),
		reiniciar:  make(chan struct{}, 1),
		extensoes:  map[string]bool{".mp4": true, ".mov": true, ".mkv": true, ".avi": true, ".webm": true, ".ts": true},
		extImagens: map[string]bool{".jpg": true, ".jpeg": true, ".png": true},
	}
	for _, d := range []string{cfg.saida, cfg.cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	lista, err := m.prepararPlaylist()
	if err != nil {
		log.Fatal(err)
	}
	go m.servirHTTP()
	go m.vigiarHDMI()
	go m.decidirFonte()
	m.rodar(lista)
}

// prepararPlaylist converte cada propaganda para o mesmo formato do canal
// (resolução, fps, áudio) e grava a lista do concat. Sem arquivos, gera uma
// vinheta para o canal nunca ficar preto.
func (m *motor) prepararPlaylist() (string, error) {
	c := m.cfg
	entradas, _ := os.ReadDir(c.propagandas)
	var arquivos []string
	for _, e := range entradas {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (m.extensoes[ext] || m.extImagens[ext]) {
			arquivos = append(arquivos, filepath.Join(c.propagandas, e.Name()))
		}
	}
	sort.Strings(arquivos)

	var convertidos []string
	for _, a := range arquivos {
		destino, err := m.normalizar(a)
		if err != nil {
			log.Printf("ignorando %s: %v", a, err)
			continue
		}
		convertidos = append(convertidos, destino)
	}
	if len(convertidos) == 0 {
		log.Printf("nenhuma propaganda em %q; usando vinheta padrão", c.propagandas)
		v, err := m.vinheta()
		if err != nil {
			return "", err
		}
		convertidos = []string{v}
	}

	var b strings.Builder
	for _, a := range convertidos {
		abs, _ := filepath.Abs(a)
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	lista := filepath.Join(c.cache, "playlist.txt")
	return lista, os.WriteFile(lista, []byte(b.String()), 0o644)
}

func (m *motor) escala() string {
	c := m.cfg
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p",
		c.largura, c.altura, c.largura, c.altura, c.fps)
}

func (m *motor) argsCodificacao() []string {
	c := m.cfg
	return []string{
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-g", fmt.Sprint(c.fps * 2),
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2",
	}
}

func (m *motor) normalizar(origem string) (string, error) {
	c := m.cfg
	info, err := os.Stat(origem)
	if err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%dx%d@%d|%d", origem, info.Size(), info.ModTime().UnixNano(), c.largura, c.altura, c.fps, c.imagemSeg)))
	destino := filepath.Join(c.cache, hex.EncodeToString(h[:8])+".mp4")
	if _, err := os.Stat(destino); err == nil {
		return destino, nil
	}

	var args []string
	if m.extImagens[strings.ToLower(filepath.Ext(origem))] {
		args = []string{"-loop", "1", "-t", fmt.Sprint(c.imagemSeg), "-i", origem,
			"-f", "lavfi", "-t", fmt.Sprint(c.imagemSeg), "-i", "anullsrc=r=48000:cl=stereo",
			"-map", "0:v", "-map", "1:a"}
	} else if temAudio(origem) {
		args = []string{"-i", origem, "-map", "0:v:0", "-map", "0:a:0"}
	} else {
		args = []string{"-i", origem, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
			"-map", "0:v:0", "-map", "1:a", "-shortest"}
	}
	args = append(args, "-vf", m.escala())
	args = append(args, m.argsCodificacao()...)
	log.Printf("convertendo %s", origem)
	return destino, ffmpegUmaVez(append(args, destino))
}

func (m *motor) vinheta() (string, error) {
	c := m.cfg
	destino := filepath.Join(c.cache, fmt.Sprintf("vinheta-%dx%d@%d.mp4", c.largura, c.altura, c.fps))
	if _, err := os.Stat(destino); err == nil {
		return destino, nil
	}
	args := []string{
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=0x102040:s=%dx%d:r=%d:d=10", c.largura, c.altura, c.fps),
		"-f", "lavfi", "-t", "10", "-i", "anullsrc=r=48000:cl=stereo",
		"-vf", "drawtext=text='LANTV':fontsize=h/6:fontcolor=white:x=(w-tw)/2:y=(h-th)/2",
	}
	args = append(args, m.argsCodificacao()...)
	return destino, ffmpegUmaVez(append(args, destino))
}

func temAudio(arquivo string) bool {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=index", "-of", "csv=p=0", arquivo).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func ffmpegUmaVez(args []string) error {
	cmd := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *motor) hdmiDisponivel() bool {
	switch m.cfg.hdmi {
	case "":
		return false
	case "simular":
		return true
	}
	_, err := os.Stat(m.cfg.hdmi)
	return err == nil
}

// argsCanal monta o ffmpeg do canal. Entrada 0 = playlist em loop,
// 1 = vídeo da HDMI, 2 = áudio da HDMI. A troca de fonte é feita pelos
// filtros streamselect/astreamselect, comandados via ZMQ, sem reiniciar o
// codificador; por isso as TVs não perdem o canal na troca.
func (m *motor) argsCanal(lista string, comHDMI bool) []string {
	c := m.cfg
	tam := fmt.Sprintf("%dx%d", c.largura, c.altura)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "warning"}
	if c.codificador == "vaapi" {
		args = append(args, "-vaapi_device", c.vaapi)
	}
	args = append(args, "-re", "-stream_loop", "-1", "-f", "concat", "-safe", "0", "-i", lista)

	switch {
	case !comHDMI:
		// Sem placa: entrada preta, o detector mantém a propaganda no ar.
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%s:r=%d", tam, c.fps))
	case c.hdmi == "simular":
		// 20 s com imagem, 20 s preto, para testar a troca automática.
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf(
			"smptebars=s=%s:r=%d,drawtext=text='HDMI SIMULADO %%{localtime\\:%%X}':fontsize=h/12:fontcolor=white:box=1:boxcolor=black:x=(w-tw)/2:y=h/3,drawbox=c=black:t=fill:enable='gte(mod(t\\,40)\\,20)'",
			tam, c.fps))
	default:
		args = append(args, "-thread_queue_size", "512", "-f", "v4l2", "-input_format", "mjpeg",
			"-video_size", c.hdmiTamanho, "-framerate", fmt.Sprint(c.fps), "-i", c.hdmi)
	}

	switch {
	case comHDMI && c.hdmi == "simular":
		args = append(args, "-re", "-f", "lavfi", "-i", "sine=f=440:r=48000")
	case comHDMI && c.hdmiAudio != "":
		args = append(args, "-thread_queue_size", "512", "-f", "alsa", "-i", c.hdmiAudio)
	default:
		args = append(args, "-re", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo")
	}

	esc := m.escala()
	fps := c.fps
	saidaVideo := "[vout]"
	codVideo := []string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-pix_fmt", "yuv420p",
		"-keyint_min", fmt.Sprint(fps * 2), "-sc_threshold", "0"}
	if c.codificador == "vaapi" {
		// O quadro escolhido sobe para a GPU só na saída; a troca continua no processador.
		saidaVideo = ",format=nv12,hwupload[vout]"
		codVideo = []string{"-c:v", "h264_vaapi", "-profile:v", "high"}
	}
	grafo := strings.Join([]string{
		fmt.Sprintf("[0:v]%s,setpts=N/%d/TB[v0]", esc, fps),
		"[1:v]split[v1in][v1d]",
		fmt.Sprintf("[v1in]%s,setpts=N/%d/TB[v1]", esc, fps),
		// O detector olha uma cópia pequena: mesmo resultado, bem menos CPU em 4K.
		`[v1d]scale=320:180,blackdetect=d=0:pix_th=0.10,metadata=mode=print:file=pipe\\:1:direct=1,nullsink`,
		`[v0][v1]streamselect@v=inputs=2:map=0,zmq=bind_address=tcp\\://127.0.0.1\\:5555` + saidaVideo,
		"[0:a]aresample=48000,aformat=channel_layouts=stereo,asetpts=N/SR/TB[a0]",
		"[2:a]aresample=48000,aformat=channel_layouts=stereo,asetpts=N/SR/TB[a1]",
		"[a0][a1]astreamselect@a=inputs=2:map=0[aout]",
	}, ";")

	args = append(args, "-filter_complex", grafo, "-map", "[vout]", "-map", "[aout]")
	args = append(args, codVideo...)
	args = append(args,
		"-b:v", c.bitrate, "-maxrate", c.bitrate, "-bufsize", "2*"+c.bitrate, "-g", fmt.Sprint(fps*2),
		"-c:a", "aac", "-b:a", "128k",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "6",
		"-hls_flags", "delete_segments+append_list+discont_start+omit_endlist+independent_segments",
		"-hls_start_number_source", "epoch",
		"-hls_segment_filename", filepath.Join(c.saida, "canal-%d.ts"),
		filepath.Join(c.saida, "canal.m3u8"))
	return args
}

// rodar mantém o ffmpeg do canal sempre no ar, reiniciando se ele cair ou
// se a placa de captura for conectada/desconectada.
func (m *motor) rodar(lista string) {
	for {
		comHDMI := m.hdmiDisponivel()
		m.mu.Lock()
		m.hdmiAtiva = comHDMI
		m.fonte = 0
		// O blackdetect só avisa mudanças: se a HDMI já começa preta, o
		// aviso vem no primeiro quadro; se começa com imagem, não vem nada.
		m.preto = false
		m.desde = time.Now()
		m.mu.Unlock()

		cmd := exec.Command("ffmpeg", m.argsCanal(lista, comHDMI)...)
		// Se o lantv-canal morrer, o ffmpeg morre junto (senão segura a porta do ZMQ).
		cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
		cmd.Stderr = os.Stderr
		saida, _ := cmd.StdoutPipe()
		inicio := time.Now()
		if err := cmd.Start(); err != nil {
			log.Fatalf("ffmpeg: %v", err)
		}
		log.Printf("canal no ar (HDMI: %v)", comHDMI)

		feito := make(chan struct{})
		go m.lerDetector(saida)
		go func() {
			select {
			case <-m.reiniciar:
				log.Printf("mudança na placa de captura; reiniciando o canal")
				cmd.Process.Kill()
			case <-feito:
			}
		}()
		err := cmd.Wait()
		close(feito)
		log.Printf("ffmpeg terminou: %v (rodou %s)", err, time.Since(inicio).Round(time.Second))
		time.Sleep(2 * time.Second)
	}
}

// lerDetector lê a saída do blackdetect (metadata) e atualiza o estado da HDMI.
func (m *motor) lerDetector(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		linha := sc.Text()
		var preto bool
		switch {
		case strings.HasPrefix(linha, "lavfi.black_start="):
			preto = true
		case strings.HasPrefix(linha, "lavfi.black_end="):
			preto = false
		default:
			continue
		}
		m.mu.Lock()
		if m.preto != preto {
			m.preto, m.desde = preto, time.Now()
		}
		m.mu.Unlock()
	}
}

// vigiarHDMI reinicia o canal quando a placa aparece (ex.: USB reconectado).
// Quando ela some, o próprio ffmpeg cai e o canal volta sem ela.
func (m *motor) vigiarHDMI() {
	for range time.Tick(5 * time.Second) {
		m.mu.Lock()
		ativa := m.hdmiAtiva
		m.mu.Unlock()
		if !ativa && m.hdmiDisponivel() {
			select {
			case m.reiniciar <- struct{}{}:
			default:
			}
		}
	}
}

func (m *motor) decidirFonte() {
	for range time.Tick(250 * time.Millisecond) {
		m.mu.Lock()
		desejada := m.fonte
		switch m.modo {
		case "propaganda":
			desejada = 0
		case "hdmi":
			desejada = 1
		default:
			if !m.hdmiAtiva {
				desejada = 0
			} else if m.preto && time.Since(m.desde) >= esperaPreto {
				desejada = 0
			} else if !m.preto && time.Since(m.desde) >= esperaSinal {
				desejada = 1
			}
		}
		atual := m.fonte
		m.mu.Unlock()
		if desejada == atual {
			continue
		}
		if err := trocarFonte(desejada); err != nil {
			log.Printf("troca de fonte falhou: %v", err)
			continue
		}
		m.mu.Lock()
		m.fonte = desejada
		m.mu.Unlock()
		log.Printf("no ar: %s", nomeFonte(desejada))
	}
}

func nomeFonte(f int) string {
	if f == 1 {
		return "hdmi"
	}
	return "propaganda"
}

func trocarFonte(f int) error {
	for _, alvo := range []string{"streamselect@v", "astreamselect@a"} {
		if err := comandoZMQ(fmt.Sprintf("%s map %d", alvo, f)); err != nil {
			return err
		}
	}
	return nil
}

func comandoZMQ(cmd string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s := zmq4.NewReq(ctx)
	defer s.Close()
	if err := s.Dial(enderecoZMQ); err != nil {
		return err
	}
	if err := s.Send(zmq4.NewMsgString(cmd)); err != nil {
		return err
	}
	resp, err := s.Recv()
	if err != nil {
		return err
	}
	if r := string(resp.Bytes()); !strings.HasPrefix(r, "0 ") {
		return fmt.Errorf("ffmpeg respondeu %q", r)
	}
	return nil
}

func (m *motor) servirHTTP() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(paginaTeste)
	})
	arquivos := http.StripPrefix("/canal/", http.FileServer(http.Dir(m.cfg.saida)))
	mux.HandleFunc("/canal/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		}
		arquivos.ServeHTTP(w, r)
	})
	// /status mostra o que está no ar; /status?modo=auto|propaganda|hdmi força a fonte (teste).
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		if modo := r.URL.Query().Get("modo"); modo == "auto" || modo == "propaganda" || modo == "hdmi" {
			m.modo = modo
		}
		st := map[string]any{
			"modo":           m.modo,
			"no_ar":          nomeFonte(m.fonte),
			"hdmi_ativa":     m.hdmiAtiva,
			"hdmi_com_sinal": !m.preto,
		}
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
	})
	endereco := fmt.Sprintf(":%d", m.cfg.porta)
	log.Printf("canal em http://<ip-do-pc>%s/canal/canal.m3u8 | página de teste em http://<ip-do-pc>%s/", endereco, endereco)
	log.Fatal(http.ListenAndServe(endereco, mux))
}
