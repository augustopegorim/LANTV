package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// binario procura o ffmpeg/ffprobe ao lado do programa e, se não achar, no PATH.
func binario(nome string) string {
	if runtime.GOOS == "windows" {
		nome += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), nome)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return nome
}

func comando(ctx context.Context, nome string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binario(nome), args...)
	configurarProcesso(cmd)
	return cmd
}

type Dispositivos struct {
	Video []string `json:"video"`
	Audio []string `json:"audio"`
}

var reDshow = regexp.MustCompile(`"([^"]+)"\s+\((video|audio)\)`)

// listarDispositivos devolve as placas de captura e entradas de áudio do PC.
func listarDispositivos() Dispositivos {
	d := Dispositivos{Video: []string{}, Audio: []string{}}
	if runtime.GOOS != "windows" {
		ms, _ := filepath.Glob("/dev/video*")
		d.Video = append(d.Video, ms...)
		return d
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var saida bytes.Buffer
	cmd := comando(ctx, "ffmpeg", "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	cmd.Stderr = &saida
	cmd.Run() // sai com erro de propósito; a lista vem no stderr
	secao := ""
	for _, linha := range strings.Split(saida.String(), "\n") {
		if m := reDshow.FindStringSubmatch(linha); m != nil { // ffmpeg novo: "Nome" (video)
			if m[2] == "video" {
				d.Video = append(d.Video, m[1])
			} else {
				d.Audio = append(d.Audio, m[1])
			}
			continue
		}
		switch { // ffmpeg antigo: seções "DirectShow video devices"
		case strings.Contains(linha, "DirectShow video devices"):
			secao = "video"
		case strings.Contains(linha, "DirectShow audio devices"):
			secao = "audio"
		case secao != "" && strings.Contains(linha, `"`) && !strings.Contains(linha, "Alternative name"):
			nome := linha[strings.Index(linha, `"`)+1:]
			if i := strings.Index(nome, `"`); i >= 0 {
				if secao == "video" {
					d.Video = append(d.Video, nome[:i])
				} else {
					d.Audio = append(d.Audio, nome[:i])
				}
			}
		}
	}
	return d
}

// placaResponde abre a placa por um instante para saber se ela está conectada e utilizável.
func placaResponde(c Config) bool {
	if c.HDMI == "simular" {
		return true
	}
	if c.HDMI == "" {
		return false
	}
	if runtime.GOOS != "windows" {
		_, err := os.Stat(c.HDMI)
		return err == nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return comando(ctx, "ffmpeg", entradaVideoHDMI(c, "-hide_banner", "-loglevel", "error", "-nostdin", "-t", "1", "-f", "null", "-")...).Run() == nil
}

// entradaVideoHDMI monta os argumentos de entrada da placa (extra vai antes, ex.: -t).
func entradaVideoHDMI(c Config, extra ...string) []string {
	var in []string
	if runtime.GOOS == "windows" {
		in = []string{"-f", "dshow", "-rtbufsize", "512M", "-video_size", c.tamanhoCaptura(),
			"-framerate", "30", "-vcodec", "mjpeg", "-i", "video=" + c.HDMI}
	} else {
		in = []string{"-thread_queue_size", "512", "-f", "v4l2", "-input_format", "mjpeg",
			"-video_size", c.tamanhoCaptura(), "-framerate", "30", "-i", c.HDMI}
	}
	if len(extra) == 0 {
		return in
	}
	// extra = opções globais + saída: ficam depois da entrada
	return append(in, extra...)
}

func entradaAudioHDMI(c Config) []string {
	if runtime.GOOS == "windows" {
		return []string{"-thread_queue_size", "1024", "-f", "dshow", "-audio_buffer_size", "80", "-i", "audio=" + c.HDMIAudio}
	}
	return nil
}

// codificadoresDisponiveis testa quais codificadores de vídeo por hardware funcionam neste PC.
func codificadoresDisponiveis() map[string]bool {
	ok := map[string]bool{"x264": true}
	for cod, enc := range map[string]string{"qsv": "h264_qsv", "nvenc": "h264_nvenc", "amf": "h264_amf"} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := comando(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
			"-f", "lavfi", "-i", "color=s=1280x720:r=30:d=0.5", "-pix_fmt", "nv12",
			"-c:v", enc, "-f", "null", "-").Run()
		cancel()
		ok[cod] = err == nil
	}
	return ok
}

func temFiltroZMQ() bool {
	out, err := exec.Command(binario("ffmpeg"), "-hide_banner", "-filters").Output()
	return err == nil && bytes.Contains(out, []byte(" zmq "))
}

// fonteTexto é o argumento de fonte do drawtext (no Windows o ffmpeg não acha fontes sozinho).
func fonteTexto() string {
	if runtime.GOOS == "windows" {
		return ":fontfile='C\\:/Windows/Fonts/arialbd.ttf'"
	}
	return ""
}
