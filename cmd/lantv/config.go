package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

const fps = 30

// Config é o que o painel edita; fica em dados/config.json.
type Config struct {
	Qualidade   string `json:"qualidade"`   // 720p, 1080p ou 4k
	HDMI        string `json:"hdmi"`        // nome da placa de captura; vazio = não usar
	HDMIAudio   string `json:"hdmi_audio"`  // nome do áudio da placa; vazio = mudo
	HDMI4K      bool   `json:"hdmi_4k"`     // a placa captura em 4K
	Codificador string `json:"codificador"` // auto, x264, qsv, nvenc ou amf
}

// Dim devolve largura, altura e taxa de vídeo do canal.
func (c Config) Dim() (int, int, string) {
	switch c.Qualidade {
	case "720p":
		return 1280, 720, "3M"
	case "4k":
		return 3840, 2160, "25M"
	}
	return 1920, 1080, "6M"
}

func (c Config) tamanhoCaptura() string {
	if c.HDMI4K {
		return "3840x2160"
	}
	return "1920x1080"
}

var reNomeDispositivo = regexp.MustCompile(`^[^\x00-\x1f"]{0,200}$`)

func (c Config) validar() error {
	switch c.Qualidade {
	case "720p", "1080p", "4k":
	default:
		return fmt.Errorf("qualidade inválida")
	}
	switch c.Codificador {
	case "auto", "x264", "qsv", "nvenc", "amf":
	default:
		return fmt.Errorf("codificador inválido")
	}
	if !reNomeDispositivo.MatchString(c.HDMI) || !reNomeDispositivo.MatchString(c.HDMIAudio) {
		return fmt.Errorf("nome de dispositivo inválido")
	}
	return nil
}

type ConfigStore struct {
	mu  sync.Mutex
	arq string
	c   Config
}

func abrirConfig(dir string) *ConfigStore {
	s := &ConfigStore{arq: filepath.Join(dir, "config.json"),
		c: Config{Qualidade: "1080p", Codificador: "auto"}}
	if b, err := os.ReadFile(s.arq); err == nil {
		var c Config
		if json.Unmarshal(b, &c) == nil && c.validar() == nil {
			s.c = c
		}
	}
	return s
}

func (s *ConfigStore) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c
}

func (s *ConfigStore) Set(c Config) error {
	if err := c.validar(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(s.arq, b, 0o644); err != nil {
		return err
	}
	s.c = c
	return nil
}
