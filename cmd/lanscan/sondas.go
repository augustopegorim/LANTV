package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

var servicos = map[int]string{
	21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP", 53: "DNS", 80: "HTTP", 110: "POP3", 111: "RPC",
	135: "RPC Windows", 139: "NetBIOS", 143: "IMAP", 161: "SNMP", 443: "HTTPS", 445: "SMB (arquivos)",
	515: "Impressão LPD", 548: "AFP (Mac)", 554: "RTSP (câmera)", 631: "Impressão IPP", 993: "IMAPS", 1433: "SQL Server",
	1883: "MQTT", 1900: "UPnP", 3000: "Web (3000)", 3306: "MySQL", 3389: "Área de Trabalho Remota", 5000: "Web/UPnP (5000)",
	5357: "WSD", 5432: "PostgreSQL", 5900: "VNC", 5985: "WinRM", 7000: "AirPlay", 8000: "Web (8000)", 8008: "Chromecast",
	8009: "Chromecast", 8080: "Web (8080)", 8081: "Web (8081)", 8443: "HTTPS (8443)", 8888: "Web (8888)", 9100: "Impressora (RAW)",
	37777: "Câmera Dahua", 62078: "iPhone/iPad", 8554: "RTSP", 34567: "DVR", 1723: "VPN PPTP", 2049: "NFS", 5353: "mDNS", 49152: "UPnP",
}

var portasComuns = []int{21, 22, 23, 25, 53, 80, 110, 135, 139, 143, 443, 445, 515, 548, 554, 631, 1433, 1883, 3306, 3389,
	5000, 5357, 5432, 5900, 5985, 7000, 8000, 8008, 8009, 8080, 8081, 8443, 8554, 8888, 9100, 34567, 37777, 62078}

// portasAlvo devolve as portas conforme o nível escolhido na tela.
func portasAlvo(nivel string) []int {
	switch nivel {
	case "nenhuma":
		return nil
	case "ampla":
		var p []int
		for i := 1; i <= 1024; i++ {
			p = append(p, i)
		}
		seen := map[int]bool{}
		for _, x := range p {
			seen[x] = true
		}
		for _, x := range portasComuns {
			if !seen[x] {
				p = append(p, x)
			}
		}
		return p
	}
	return portasComuns
}

// sondaTCP diz se a porta está aberta ("aberta"), recusou ("fechada": o
// aparelho existe e respondeu) ou não respondeu ("").
func sondaTCP(ip netip.Addr, porta int, limite time.Duration) string {
	c, err := net.DialTimeout("tcp4", net.JoinHostPort(ip.String(), fmt.Sprint(porta)), limite)
	if err == nil {
		c.Close()
		return "aberta"
	}
	if strings.Contains(strings.ToLower(err.Error()), "refused") || strings.Contains(err.Error(), "actively refused") {
		return "fechada"
	}
	return ""
}

var reTitulo = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

type InfoWeb struct {
	Titulo   string `json:"titulo,omitempty"`
	Servidor string `json:"servidor,omitempty"`
	Cert     string `json:"certificado,omitempty"`
}

func bannerWeb(ip netip.Addr, porta int, tls_ bool) InfoWeb {
	var info InfoWeb
	cli := &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls_Config, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	esquema := "http"
	if tls_ {
		esquema = "https"
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s://%s:%d/", esquema, ip, porta), nil)
	req.Header.Set("User-Agent", "lanscan")
	resp, err := cli.Do(req)
	if err != nil {
		return info
	}
	defer resp.Body.Close()
	info.Servidor = resp.Header.Get("Server")
	corpo, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if m := reTitulo.FindSubmatch(corpo); m != nil {
		info.Titulo = strings.Join(strings.Fields(string(m[1])), " ")
		if len(info.Titulo) > 120 {
			info.Titulo = info.Titulo[:120]
		}
	}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		c := resp.TLS.PeerCertificates[0]
		info.Cert = c.Subject.CommonName
		if info.Cert == "" && len(c.DNSNames) > 0 {
			info.Cert = c.DNSNames[0]
		}
		if o := strings.Join(c.Issuer.Organization, ", "); o != "" {
			info.Cert += " (emitido por " + o + ")"
		}
	}
	return info
}

var tls_Config = tls.Config{InsecureSkipVerify: true}

// bannerSSH lê a linha de apresentação do servidor SSH.
func bannerSSH(ip netip.Addr, porta int) string {
	c, err := net.DialTimeout("tcp4", net.JoinHostPort(ip.String(), fmt.Sprint(porta)), 2*time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, 200)
	n, _ := c.Read(b)
	return strings.TrimSpace(strings.SplitN(string(b[:n]), "\n", 2)[0])
}

// DNS reverso pelo servidor configurado no PC.
func dnsReverso(ip netip.Addr) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	nomes, err := net.DefaultResolver.LookupAddr(ctx, ip.String())
	if err != nil || len(nomes) == 0 {
		return ""
	}
	return strings.TrimSuffix(nomes[0], ".")
}

type NetBIOS struct {
	Nome  string
	Grupo string
	MAC   string
}

// consultaNetBIOS pergunta o nome do aparelho pela porta UDP 137 (Windows, Samba,
// impressoras). A resposta traz até o MAC, mesmo fora da rede local.
func consultaNetBIOS(ip netip.Addr) (NetBIOS, bool) {
	var r NetBIOS
	c, err := net.DialTimeout("udp4", net.JoinHostPort(ip.String(), "137"), time.Second)
	if err != nil {
		return r, false
	}
	defer c.Close()
	req := []byte{0x13, 0x37, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20}
	req = append(req, []byte("CKAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")...)
	req = append(req, 0, 0, 0x21, 0, 1)
	for i := 0; i < 2; i++ { // UDP pode se perder: tenta duas vezes
		c.SetDeadline(time.Now().Add(900 * time.Millisecond))
		if _, err := c.Write(req); err != nil {
			return r, false
		}
		buf := make([]byte, 1500)
		n, err := c.Read(buf)
		if err != nil {
			continue
		}
		return parseNBSTAT(buf[:n])
	}
	return r, false
}

func parseNBSTAT(b []byte) (NetBIOS, bool) {
	var r NetBIOS
	if len(b) < 12+2+10+1 {
		return r, false
	}
	i := 12
	for i < len(b) && b[i] != 0 { // pula o nome da resposta (comprimido ou não)
		if b[i]&0xC0 == 0xC0 {
			i++
			break
		}
		i += int(b[i]) + 1
	}
	i++
	i += 10 // tipo, classe, ttl, tamanho
	if i >= len(b) {
		return r, false
	}
	n := int(b[i])
	i++
	for k := 0; k < n && i+18 <= len(b); k++ {
		nome := strings.TrimSpace(string(b[i : i+15]))
		suf := b[i+15]
		grupo := b[i+16]&0x80 != 0
		i += 18
		if suf == 0 && nome != "" {
			if grupo && r.Grupo == "" {
				r.Grupo = nome
			} else if !grupo && r.Nome == "" {
				r.Nome = nome
			}
		}
	}
	if i+6 <= len(b) {
		m := b[i : i+6]
		if m[0]|m[1]|m[2]|m[3]|m[4]|m[5] != 0 {
			r.MAC = fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", m[0], m[1], m[2], m[3], m[4], m[5])
		}
	}
	return r, r.Nome != "" || r.MAC != ""
}

// consultaMDNS pergunta o nome (Apple, Android, impressoras, Chromecast) direto ao aparelho.
func consultaMDNS(ip netip.Addr) string {
	a := ip.As4()
	nome := fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", a[3], a[2], a[1], a[0])
	n, err := dnsmessage.NewName(nome)
	if err != nil {
		return ""
	}
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{},
		Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}},
	}
	pkt, err := msg.Pack()
	if err != nil {
		return ""
	}
	c, err := net.DialTimeout("udp4", net.JoinHostPort(ip.String(), "5353"), time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(900 * time.Millisecond))
	if _, err := c.Write(pkt); err != nil {
		return ""
	}
	buf := make([]byte, 1500)
	k, err := c.Read(buf)
	if err != nil {
		return ""
	}
	var resp dnsmessage.Message
	if resp.Unpack(buf[:k]) != nil {
		return ""
	}
	for _, an := range resp.Answers {
		if p, ok := an.Body.(*dnsmessage.PTRResource); ok {
			return strings.TrimSuffix(p.PTR.String(), ".")
		}
	}
	return ""
}
