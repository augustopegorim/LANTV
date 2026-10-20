package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-zeromq/zmq4"
)

const (
	enderecoZMQ = "tcp://127.0.0.1:5555"
	// Tempo com imagem na HDMI antes de trocar para ela.
	esperaSinal = 2 * time.Second
	// Tempo com tela preta na HDMI antes de voltar para a propaganda
	// (evita trocar em fades e cenas escuras).
	esperaPreto = 3 * time.Second
)

type Motor struct {
	cfg      *ConfigStore
	lib      *Biblioteca
	saida    string // pasta do HLS
	simular  bool
	hardware map[string]bool

	mu        sync.Mutex
	modo      string // auto, propaganda ou hdmi
	fonte     int    // 0 = propaganda, 1 = hdmi (o que está no ar)
	preto     bool   // última leitura da HDMI
	desde     time.Time
	hdmiAtiva bool   // o canal está rodando com a placa
	hdmiErro  string // motivo, se a placa falhou
	pausaAte  time.Time
	pausa     time.Duration
	tocando   string
	semHW     atomic.Bool // codificador por hardware falhou: usa o processador
	reiniciar chan struct{}
}

func novoMotor(cfg *ConfigStore, lib *Biblioteca, dados string, simular bool) *Motor {
	return &Motor{cfg: cfg, lib: lib, saida: filepath.Join(dados, "saida"), simular: simular,
		modo: "auto", reiniciar: make(chan struct{}, 1)}
}

func (m *Motor) config() Config {
	c := m.cfg.Get()
	if m.simular {
		c.HDMI = "simular"
	}
	return c
}

// Reiniciar derruba o canal por alguns segundos para aplicar novas configurações.
func (m *Motor) Reiniciar() {
	select {
	case m.reiniciar <- struct{}{}:
	default:
	}
}

func (m *Motor) codificador(c Config) string {
	if m.semHW.Load() {
		return "x264"
	}
	cod := c.Codificador
	if cod == "auto" {
		cod = "x264"
		if c.Qualidade == "4k" { // 4K só com processador não acompanha o tempo real
			for _, h := range []string{"qsv", "nvenc", "amf"} {
				if m.hardware[h] {
					return h
				}
			}
		}
	}
	if !m.hardware[cod] {
		return "x264"
	}
	return cod
}

func (m *Motor) escala(c Config) string {
	w, h, _ := c.Dim()
	return fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=%d,format=yuv420p", w, h, w, h, fps)
}

func argsCodificador(cod, bitrate string) []string {
	var a []string
	switch cod {
	case "qsv":
		a = []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "high", "-pix_fmt", "nv12"}
	case "nvenc":
		a = []string{"-c:v", "h264_nvenc", "-preset", "p4", "-profile:v", "high", "-pix_fmt", "yuv420p"}
	case "amf":
		a = []string{"-c:v", "h264_amf", "-quality", "speed", "-profile:v", "high", "-pix_fmt", "nv12"}
	default:
		a = []string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-pix_fmt", "yuv420p", "-sc_threshold", "0"}
	}
	return append(a, "-b:v", bitrate, "-maxrate", bitrate, "-bufsize", "2*"+bitrate,
		"-g", fmt.Sprint(fps*2), "-force_key_frames", "expr:gte(t,n_forced*2)")
}

// argsCanal monta o ffmpeg do canal. Entrada 0 = propagandas (chegam pelo
// stdin), 1 = vídeo da HDMI, 2 = áudio da HDMI. A troca de fonte é feita
// pelos filtros streamselect/astreamselect, comandados via ZMQ, sem reiniciar
// o codificador; por isso as TVs não perdem o canal na troca.
func (m *Motor) argsCanal(c Config, comHDMI bool, cod string) []string {
	w, h, bitrate := c.Dim()
	tam := fmt.Sprintf("%dx%d", w, h)
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error",
		"-probesize", "2M", "-analyzeduration", "1M", "-f", "mpegts", "-i", "pipe:0"}

	switch {
	case !comHDMI:
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%s:r=%d", tam, fps))
	case c.HDMI == "simular":
		// 20 s com imagem, 20 s preto, para testar a troca automática.
		args = append(args, "-re", "-f", "lavfi", "-i", fmt.Sprintf(
			"smptebars=s=%s:r=%d,drawtext=text='HDMI SIMULADO %%{localtime\\:%%X}':fontsize=h/12:fontcolor=white:box=1:boxcolor=black:x=(w-tw)/2:y=h/3%s,drawbox=c=black:t=fill:enable='gte(mod(t\\,40)\\,20)'",
			tam, fps, fonteTexto()))
	default:
		args = append(args, entradaVideoHDMI(c)...)
	}

	switch {
	case comHDMI && c.HDMI == "simular":
		args = append(args, "-re", "-f", "lavfi", "-i", "sine=f=440:r=48000")
	case comHDMI && c.HDMIAudio != "" && len(entradaAudioHDMI(c)) > 0:
		args = append(args, entradaAudioHDMI(c)...)
	default:
		args = append(args, "-re", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo")
	}

	esc := m.escala(c)
	grafo := strings.Join([]string{
		fmt.Sprintf("[0:v]%s,setpts=N/%d/TB[v0]", esc, fps),
		"[1:v]split[v1in][v1d]",
		fmt.Sprintf("[v1in]%s,setpts=N/%d/TB[v1]", esc, fps),
		// O detector olha uma cópia pequena: mesmo resultado, bem menos CPU em 4K.
		`[v1d]scale=320:180,blackdetect=d=0:pix_th=0.10,metadata=mode=print:file=pipe\\:1:direct=1,nullsink`,
		`[v0][v1]streamselect@v=inputs=2:map=0,zmq=bind_address=tcp\\://127.0.0.1\\:5555[vout]`,
		"[0:a]aresample=48000,aformat=channel_layouts=stereo,asetpts=N/SR/TB[a0]",
		"[2:a]aresample=48000,aformat=channel_layouts=stereo,asetpts=N/SR/TB[a1]",
		"[a0][a1]astreamselect@a=inputs=2:map=0[aout]",
	}, ";")

	args = append(args, "-filter_complex", grafo, "-map", "[vout]", "-map", "[aout]")
	args = append(args, argsCodificador(cod, bitrate)...)
	return append(args, "-c:a", "aac", "-b:a", "128k",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "6",
		"-hls_flags", "delete_segments+append_list+discont_start+omit_endlist+independent_segments",
		"-hls_start_number_source", "epoch",
		"-hls_segment_filename", filepath.Join(m.saida, "canal-%d.ts"),
		filepath.Join(m.saida, "canal.m3u8"))
}

// Rodar mantém o canal sempre no ar: se o ffmpeg cair, volta sozinho.
func (m *Motor) Rodar() {
	for {
		select { // sinal antigo de reinício não vale para esta rodada
		case <-m.reiniciar:
		default:
		}
		c := m.config()
		m.mu.Lock()
		comHDMI := c.HDMI != "" && time.Now().After(m.pausaAte)
		m.mu.Unlock()
		if comHDMI && c.HDMI != "simular" && !placaResponde(c) {
			comHDMI = false
			m.definirErro("placa não encontrada")
		}
		cod := m.codificador(c)

		m.mu.Lock()
		m.hdmiAtiva, m.fonte, m.preto, m.desde = comHDMI, 0, false, time.Now()
		if comHDMI {
			m.hdmiErro = ""
		}
		m.mu.Unlock()

		cmd := comando(context.Background(), "ffmpeg", m.argsCanal(c, comHDMI, cod)...)
		pr, pw, err := os.Pipe()
		if err != nil {
			log.Fatal(err)
		}
		cmd.Stdin = pr
		cmd.Stderr = registro
		saida, _ := cmd.StdoutPipe()
		inicio := time.Now()
		if err := cmd.Start(); err != nil {
			log.Printf("ffmpeg não iniciou: %v", err)
			pr.Close()
			pw.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		aposIniciar(cmd)
		pr.Close()
		log.Printf("canal no ar (HDMI: %v, codificador: %s)", comHDMI, cod)

		ctx, cancel := context.WithCancel(context.Background())
		go m.alimentar(ctx, pw)
		go m.lerDetector(saida)
		go func() {
			select {
			case <-m.reiniciar:
				log.Printf("reiniciando o canal")
				cmd.Process.Kill()
			case <-ctx.Done():
			}
		}()
		err = cmd.Wait()
		cancel()
		dur := time.Since(inicio)
		log.Printf("ffmpeg terminou: %v (rodou %s)", err, dur.Round(time.Second))

		if comHDMI && dur >= 15*time.Second {
			m.mu.Lock()
			m.pausa = 0
			m.mu.Unlock()
		}
		if dur < 15*time.Second { // caiu logo ao iniciar: descobrir o culpado
			switch {
			case comHDMI:
				m.pausarHDMI()
			case cod != "x264":
				log.Printf("codificador %s falhou; usando o processador", cod)
				m.semHW.Store(true)
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func (m *Motor) definirErro(msg string) {
	m.mu.Lock()
	m.hdmiErro = msg
	m.mu.Unlock()
}

// pausarHDMI tira a placa do canal por um tempo crescente (30 s até 10 min),
// para uma placa com defeito não derrubar o canal a toda hora.
func (m *Motor) pausarHDMI() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pausa == 0 {
		m.pausa = 30 * time.Second
	} else if m.pausa < 10*time.Minute {
		m.pausa *= 2
	}
	m.pausaAte = time.Now().Add(m.pausa)
	m.hdmiErro = "a placa de captura falhou; tentando de novo em breve"
	log.Printf("placa HDMI falhou; canal segue só com propagandas por %s", m.pausa)
}

// alimentar envia as propagandas, uma a uma, para o stdin do canal. A lista
// é relida a cada propaganda: o que o painel muda entra no ar sem reiniciar.
func (m *Motor) alimentar(ctx context.Context, pw *os.File) {
	defer pw.Close()
	ultimo := ""
	for ctx.Err() == nil {
		id, nome, caminho := m.lib.Proximo(ultimo)
		ultimo = id
		m.mu.Lock()
		m.tocando = nome
		m.mu.Unlock()

		cmd := comando(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
			"-re", "-i", caminho, "-c", "copy", "-f", "mpegts", "pipe:1")
		cmd.Stdout = pw
		inicio := time.Now()
		if err := cmd.Start(); err == nil {
			aposIniciar(cmd)
			cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(inicio) < time.Second { // arquivo ruim: não girar em falso
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

// lerDetector lê a saída do blackdetect e atualiza o estado da HDMI.
func (m *Motor) lerDetector(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var preto bool
		switch l := sc.Text(); {
		case strings.HasPrefix(l, "lavfi.black_start="):
			preto = true
		case strings.HasPrefix(l, "lavfi.black_end="):
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

// VigiarHDMI reinicia o canal quando a placa passa a responder (ex.: USB conectado).
func (m *Motor) VigiarHDMI() {
	for range time.Tick(10 * time.Second) {
		c := m.config()
		m.mu.Lock()
		tentar := !m.hdmiAtiva && c.HDMI != "" && time.Now().After(m.pausaAte)
		m.mu.Unlock()
		if tentar && placaResponde(c) {
			m.Reiniciar()
		}
	}
}

func (m *Motor) DecidirFonte() {
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

func (m *Motor) SetModo(modo string) bool {
	if modo != "auto" && modo != "propaganda" && modo != "hdmi" {
		return false
	}
	m.mu.Lock()
	m.modo = modo
	m.mu.Unlock()
	return true
}

type EstadoMotor struct {
	Modo        string `json:"modo"`
	NoAr        string `json:"no_ar"`
	Tocando     string `json:"tocando"`
	HDMI        bool   `json:"hdmi_configurada"`
	HDMIAtiva   bool   `json:"hdmi_ativa"`
	ComSinal    bool   `json:"hdmi_com_sinal"`
	HDMIErro    string `json:"hdmi_erro"`
	Codificador string `json:"codificador"`
}

func (m *Motor) Estado() EstadoMotor {
	c := m.config()
	cod := m.codificador(c)
	m.mu.Lock()
	defer m.mu.Unlock()
	return EstadoMotor{Modo: m.modo, NoAr: nomeFonte(m.fonte), Tocando: m.tocando, HDMI: c.HDMI != "",
		HDMIAtiva: m.hdmiAtiva, ComSinal: m.hdmiAtiva && !m.preto, HDMIErro: m.hdmiErro, Codificador: cod}
}
