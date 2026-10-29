package main

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"
)

type Porta struct {
	Porta   int    `json:"porta"`
	Servico string `json:"servico"`
	Info    string `json:"info,omitempty"`
}

type Host struct {
	IP          string  `json:"ip"`
	Nome        string  `json:"nome"`
	NomeDNS     string  `json:"nome_dns,omitempty"`
	NomeMDNS    string  `json:"nome_mdns,omitempty"`
	NomeNetBIOS string  `json:"nome_netbios,omitempty"`
	Grupo       string  `json:"grupo,omitempty"`
	MAC         string  `json:"mac,omitempty"`
	Fabricante  string  `json:"fabricante,omitempty"`
	Tipo        string  `json:"tipo,omitempty"`
	SO          string  `json:"so,omitempty"`
	TTL         int     `json:"ttl,omitempty"`
	RTTms       float64 `json:"rtt_ms,omitempty"`
	Portas      []Porta `json:"portas"`
	MesmaRede   bool    `json:"mesma_rede"`
	Descoberto  string  `json:"descoberto_por"`
	Pronto      bool    `json:"pronto"`
	ip          netip.Addr
}

type Opcoes struct {
	Faixas []string `json:"faixas"`
	Portas string   `json:"portas"` // nenhuma, comuns ou ampla
}

type Varredura struct {
	mu     sync.Mutex
	opcoes Opcoes
	estado string // rodando, concluida ou cancelada
	total  int
	feitos int
	inicio time.Time
	fim    time.Time
	hosts  map[string]*Host
	cancel context.CancelFunc
	redes  []RedeLocal
}

type Resumo struct {
	Estado   string   `json:"estado"`
	Faixas   []string `json:"faixas"`
	Total    int      `json:"total"`
	Feitos   int      `json:"feitos"`
	Segundos float64  `json:"segundos"`
	Hosts    []*Host  `json:"hosts"`
}

func (v *Varredura) Resumo() Resumo {
	v.mu.Lock()
	defer v.mu.Unlock()
	fim := v.fim
	if v.estado == "rodando" {
		fim = time.Now()
	}
	r := Resumo{Estado: v.estado, Faixas: v.opcoes.Faixas, Total: v.total, Feitos: v.feitos, Segundos: fim.Sub(v.inicio).Seconds(), Hosts: []*Host{}}
	for _, h := range v.hosts {
		c := *h
		c.Portas = append([]Porta{}, h.Portas...)
		r.Hosts = append(r.Hosts, &c)
	}
	sort.Slice(r.Hosts, func(i, j int) bool { return r.Hosts[i].ip.Less(r.Hosts[j].ip) })
	return r
}

func (v *Varredura) Rodando() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.estado == "rodando"
}

func (v *Varredura) Cancelar() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.estado == "rodando" && v.cancel != nil {
		v.cancel()
	}
}

// Iniciar começa uma varredura em segundo plano.
func (v *Varredura) Iniciar(o Opcoes) error {
	ips, err := ExpandirFaixas(o.Faixas)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("informe ao menos uma faixa")
	}
	if o.Portas != "nenhuma" && o.Portas != "ampla" {
		o.Portas = "comuns"
	}
	v.mu.Lock()
	if v.estado == "rodando" {
		v.mu.Unlock()
		return fmt.Errorf("já existe uma varredura em andamento")
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.opcoes, v.estado, v.total, v.feitos = o, "rodando", len(ips), 0
	v.inicio, v.fim, v.hosts, v.cancel = time.Now(), time.Time{}, map[string]*Host{}, cancel
	v.redes = RedesLocais()
	v.mu.Unlock()

	go func() {
		fila := make(chan netip.Addr)
		var wg sync.WaitGroup
		for i := 0; i < 96; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ip := range fila {
					v.verificar(ctx, ip, o.Portas)
					v.mu.Lock()
					v.feitos++
					v.mu.Unlock()
				}
			}()
		}
	enviar:
		for _, ip := range ips {
			select {
			case fila <- ip:
			case <-ctx.Done():
				break enviar
			}
		}
		close(fila)
		wg.Wait()
		v.mu.Lock()
		v.fim = time.Now()
		v.estado = "concluida"
		if ctx.Err() != nil {
			v.estado = "cancelada"
		}
		v.mu.Unlock()
		cancel()
	}()
	return nil
}

var portasSonda = []int{445, 80, 443, 22, 135, 139, 3389, 8080, 62078, 5357, 53, 554, 9100}

// verificar descobre se o IP responde e, se sim, coleta tudo o que der.
func (v *Varredura) verificar(ctx context.Context, ip netip.Addr, nivel string) {
	if ctx.Err() != nil {
		return
	}
	v.mu.Lock()
	local := mesmaRede(v.redes, ip)
	v.mu.Unlock()

	h := &Host{IP: ip.String(), ip: ip, MesmaRede: local, Portas: []Porta{}}
	var (
		mu    sync.Mutex
		via   []string
		vivo  bool
		wg    sync.WaitGroup
		abert = map[int]bool{}
	)
	achou := func(motivo string) { mu.Lock(); vivo = true; via = append(via, motivo); mu.Unlock() }

	if local {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if mac, ok := arpMAC(ip); ok {
				mu.Lock()
				h.MAC = mac
				mu.Unlock()
				achou("ARP")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if rtt, ttl, ok := pingICMP(ip, 1200*time.Millisecond); ok {
			mu.Lock()
			h.RTTms, h.TTL = float64(rtt.Microseconds())/1000, ttl
			mu.Unlock()
			achou("ping")
		}
	}()
	for _, p := range portasSonda {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			switch sondaTCP(ip, p, 900*time.Millisecond) {
			case "aberta":
				mu.Lock()
				abert[p] = true
				mu.Unlock()
				achou(fmt.Sprintf("porta %d", p))
			case "fechada":
				achou("TCP")
			}
		}(p)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if nb, ok := consultaNetBIOS(ip); ok {
			mu.Lock()
			h.NomeNetBIOS, h.Grupo = nb.Nome, nb.Grupo
			if h.MAC == "" {
				h.MAC = nb.MAC
			}
			mu.Unlock()
			achou("NetBIOS")
		}
	}()
	wg.Wait()
	if !vivo || ctx.Err() != nil {
		return
	}
	h.Descoberto = strings.Join(unicos(via), ", ")
	h.Nome = melhorNome(h)
	completar(h)
	v.mu.Lock()
	v.hosts[h.IP] = h
	v.mu.Unlock()

	// Segunda etapa: portas, banners e nomes. O host já aparece na tela.
	var ab []int
	for p := range abert {
		ab = append(ab, p)
	}
	extra := portasAlvo(nivel)
	if nivel != "nenhuma" {
		var wg2 sync.WaitGroup
		sem := make(chan struct{}, 48)
		for _, p := range extra {
			if abert[p] || ctx.Err() != nil {
				continue
			}
			wg2.Add(1)
			sem <- struct{}{}
			go func(p int) {
				defer wg2.Done()
				defer func() { <-sem }()
				if sondaTCP(ip, p, 900*time.Millisecond) == "aberta" {
					mu.Lock()
					abert[p] = true
					mu.Unlock()
				}
			}(p)
		}
		wg2.Wait()
		ab = ab[:0]
		for p := range abert {
			ab = append(ab, p)
		}
	}
	sort.Ints(ab)
	var wg3 sync.WaitGroup
	portas := make([]Porta, len(ab))
	for i, p := range ab {
		portas[i] = Porta{Porta: p, Servico: nomeServico(p)}
		wg3.Add(1)
		go func(i, p int) {
			defer wg3.Done()
			portas[i].Info = banner(ip, p)
		}(i, p)
	}
	wg3.Add(2)
	go func() { defer wg3.Done(); n := dnsReverso(ip); mu.Lock(); h.NomeDNS = n; mu.Unlock() }()
	go func() { defer wg3.Done(); n := consultaMDNS(ip); mu.Lock(); h.NomeMDNS = n; mu.Unlock() }()
	wg3.Wait()

	v.mu.Lock()
	h.Portas = portas
	h.Nome = melhorNome(h)
	completar(h)
	h.Pronto = true
	v.mu.Unlock()
}

func unicos(s []string) []string {
	sort.Strings(s)
	var out []string
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func nomeServico(p int) string {
	if s, ok := servicos[p]; ok {
		return s
	}
	return ""
}

func banner(ip netip.Addr, p int) string {
	switch p {
	case 22:
		return bannerSSH(ip, p)
	case 80, 8000, 8008, 8080, 8081, 8888, 3000, 5000:
		return formatarWeb(bannerWeb(ip, p, false))
	case 443, 8443:
		return formatarWeb(bannerWeb(ip, p, true))
	}
	return ""
}

func formatarWeb(w InfoWeb) string {
	var p []string
	if w.Titulo != "" {
		p = append(p, "Título: "+w.Titulo)
	}
	if w.Servidor != "" {
		p = append(p, "Servidor: "+w.Servidor)
	}
	if w.Cert != "" {
		p = append(p, "Certificado: "+w.Cert)
	}
	return strings.Join(p, " · ")
}

func melhorNome(h *Host) string {
	for _, n := range []string{h.NomeDNS, h.NomeMDNS, h.NomeNetBIOS} {
		if n != "" {
			return n
		}
	}
	return ""
}

// completar preenche fabricante, sistema e tipo a partir do que já se sabe.
func completar(h *Host) {
	if h.MAC != "" {
		h.Fabricante = Fabricante(h.MAC)
	}
	h.SO, h.Tipo = adivinhar(h)
}

func adivinhar(h *Host) (so, tipo string) {
	tem := map[int]bool{}
	for _, p := range h.Portas {
		tem[p.Porta] = true
	}
	fab := strings.ToLower(h.Fabricante)
	tem9100 := tem[9100] || tem[631] || tem[515]
	windows := tem[3389] || tem[135] || tem[5985] || (tem[445] && tem[139]) || h.NomeNetBIOS != ""
	apple := strings.Contains(fab, "apple") || tem[62078] || tem[548] || tem[7000]
	rede := false
	for _, f := range []string{"tp-link", "d-link", "mikrotik", "ubiquiti", "huawei", "zte", "intelbras", "netgear", "cisco", "juniper", "aruba", "ruckus", "zyxel", "linksys", "routerboard", "fortinet", "sagemcom", "arris", "technicolor"} {
		if strings.Contains(fab, f) {
			rede = true
		}
	}
	camera := tem[554] || tem[8554] || tem[37777] || tem[34567] || strings.Contains(fab, "hikvision") || strings.Contains(fab, "dahua")

	switch {
	case tem[3389] || tem[135] || tem[5985] || (tem[445] && tem[139]):
		so = "Windows"
	case windows:
		so = "Windows ou Samba (NetBIOS)"
	case apple:
		so = "Apple (macOS/iOS)"
	case h.TTL > 128:
		so = "Equipamento de rede (TTL 255)"
	case h.TTL > 64:
		so = "Windows provável (TTL 128)"
	case h.TTL > 0:
		so = "Linux/Android/Unix (TTL 64)"
	}
	switch {
	case tem9100 || strings.Contains(fab, "epson") || strings.Contains(fab, "brother") || strings.Contains(fab, "canon") || strings.Contains(fab, "xerox") || strings.Contains(fab, "kyocera") || strings.Contains(fab, "lexmark") || strings.Contains(fab, "ricoh"):
		tipo = "Impressora"
	case camera:
		tipo = "Câmera / DVR"
	case tem[8008] || tem[8009]:
		tipo = "Chromecast / TV"
	case tem[62078]:
		tipo = "iPhone / iPad"
	case rede || (tem[53] && (tem[80] || tem[443])):
		tipo = "Roteador / rede"
	case windows:
		tipo = "Computador Windows"
	case apple:
		tipo = "Dispositivo Apple"
	case strings.Contains(fab, "raspberry"):
		tipo = "Raspberry Pi"
	case strings.HasPrefix(h.Fabricante, "Endereço aleatório"):
		tipo = "Celular / notebook (MAC aleatório)"
	case strings.Contains(fab, "samsung") || strings.Contains(fab, "xiaomi") || strings.Contains(fab, "lg elec") || strings.Contains(fab, "motorola") || strings.Contains(fab, "oneplus") || strings.Contains(fab, "oppo"):
		tipo = "Celular / TV"
	case tem[22] || tem[3306] || tem[5432]:
		tipo = "Servidor"
	}
	return
}
