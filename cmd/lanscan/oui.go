package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
)

//go:embed oui.tsv.gz
var ouiGz []byte

var (
	ouiOnce sync.Once
	ouiMapa map[string]string
)

// Fabricante devolve o fabricante pelo prefixo do MAC (base IEEE, 6 dígitos).
func Fabricante(mac string) string {
	ouiOnce.Do(func() {
		ouiMapa = map[string]string{}
		zr, err := gzip.NewReader(bytes.NewReader(ouiGz))
		if err != nil {
			return
		}
		sc := bufio.NewScanner(zr)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), "\t"); ok {
				ouiMapa[k] = v
			}
		}
	})
	h := strings.ToUpper(strings.NewReplacer(":", "", "-", "", ".", "").Replace(mac))
	if len(h) < 6 {
		return ""
	}
	// Bit 2 do primeiro byte = endereço aleatório (privacidade de celulares).
	if b := h[1]; b == '2' || b == '6' || b == 'A' || b == 'E' {
		if v, ok := ouiMapa[h[:6]]; ok {
			return v
		}
		return "Endereço aleatório (privacidade do celular/PC)"
	}
	return ouiMapa[h[:6]]
}
