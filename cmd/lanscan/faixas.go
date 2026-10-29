package main

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// maxHosts evita varreduras acidentais gigantes (um /16 inteiro já é 65 mil).
const maxHosts = 1 << 16

// Faixa é um intervalo contínuo de IPv4.
type Faixa struct {
	Texto string
	de    uint32
	ate   uint32
}

func (f Faixa) Total() int { return int(f.ate-f.de) + 1 }

func ipParaUint(a netip.Addr) uint32 {
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func uintParaIP(u uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(u >> 24), byte(u >> 16), byte(u >> 8), byte(u)})
}

// ParseFaixa aceita "192.168.0.0/24", "192.168.0.10-192.168.0.50",
// "192.168.0.10-50" ou um IP único.
func ParseFaixa(s string) (Faixa, error) {
	s = strings.TrimSpace(s)
	f := Faixa{Texto: s}
	switch {
	case s == "":
		return f, fmt.Errorf("faixa vazia")
	case strings.Contains(s, "/"):
		p, err := netip.ParsePrefix(s)
		if err != nil || !p.Addr().Is4() {
			return f, fmt.Errorf("%q não é uma faixa CIDR válida (exemplo: 192.168.1.0/24)", s)
		}
		p = p.Masked()
		f.de = ipParaUint(p.Addr())
		f.ate = f.de + (1 << (32 - p.Bits())) - 1
		if p.Bits() <= 30 { // tira endereço da rede e de broadcast
			f.de++
			f.ate--
		}
	case strings.Contains(s, "-"):
		ini, fim, _ := strings.Cut(s, "-")
		a, err := netip.ParseAddr(strings.TrimSpace(ini))
		if err != nil || !a.Is4() {
			return f, fmt.Errorf("%q: IP inicial inválido", s)
		}
		f.de = ipParaUint(a)
		fim = strings.TrimSpace(fim)
		if b, err := netip.ParseAddr(fim); err == nil && b.Is4() {
			f.ate = ipParaUint(b)
		} else if n, err := strconv.Atoi(fim); err == nil && n >= 0 && n <= 255 { // "10.0.0.5-50"
			f.ate = f.de&^0xFF | uint32(n)
		} else {
			return f, fmt.Errorf("%q: IP final inválido", s)
		}
		if f.ate < f.de {
			return f, fmt.Errorf("%q: o IP final vem antes do inicial", s)
		}
	default:
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() {
			return f, fmt.Errorf("%q não é um IPv4 válido", s)
		}
		f.de = ipParaUint(a)
		f.ate = f.de
	}
	if f.Total() > maxHosts {
		return f, fmt.Errorf("%q tem %d endereços; o máximo por varredura é %d (um /16)", s, f.Total(), maxHosts)
	}
	return f, nil
}

// ExpandirFaixas junta as faixas sem repetir IP, na ordem numérica.
func ExpandirFaixas(textos []string) ([]netip.Addr, error) {
	vistos := map[uint32]bool{}
	var ips []netip.Addr
	for _, t := range textos {
		f, err := ParseFaixa(t)
		if err != nil {
			return nil, err
		}
		for u := f.de; ; u++ {
			if !vistos[u] {
				vistos[u] = true
				ips = append(ips, uintParaIP(u))
			}
			if u == f.ate {
				break
			}
		}
		if len(ips) > maxHosts {
			return nil, fmt.Errorf("a soma das faixas passa de %d endereços", maxHosts)
		}
	}
	return ips, nil
}

// RedeLocal é uma rede em que este PC está conectado.
type RedeLocal struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	Faixa     string `json:"faixa"`
	Gateway   bool   `json:"principal"`
	prefixo   netip.Prefix
}

// RedesLocais lista as redes IPv4 das placas ativas (a primeira é a sugerida).
func RedesLocais() []RedeLocal {
	var out []RedeLocal
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ip, _ := netip.AddrFromSlice(n.IP.To4())
			bits, _ := n.Mask.Size()
			if ip.IsLinkLocalUnicast() || bits == 32 || bits < 16 {
				continue
			}
			p := netip.PrefixFrom(ip, bits)
			out = append(out, RedeLocal{Interface: ifc.Name, IP: ip.String(), Faixa: p.Masked().String(), prefixo: p.Masked()})
		}
	}
	// A rede "principal" é a que o sistema usa para sair para a internet.
	if c, err := net.Dial("udp4", "198.51.100.1:9"); err == nil {
		sai := c.LocalAddr().(*net.UDPAddr).IP.String()
		c.Close()
		for i := range out {
			if out[i].IP == sai {
				out[i].Gateway = true
				out[0], out[i] = out[i], out[0]
				break
			}
		}
	}
	return out
}

// mesmaRede diz se ip está numa rede local (onde ARP funciona).
func mesmaRede(redes []RedeLocal, ip netip.Addr) bool {
	for _, r := range redes {
		if r.prefixo.Contains(ip) {
			return true
		}
	}
	return false
}
