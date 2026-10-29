//go:build !windows

package main

import (
	"bufio"
	"context"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// No Linux (usado só em desenvolvimento) o MAC vem da tabela ARP do sistema,
// que é preenchida quando o aparelho responde a qualquer tráfego.
func arpMAC(ip netip.Addr) (string, bool) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		c := strings.Fields(sc.Text())
		if len(c) >= 4 && c[0] == ip.String() && c[3] != "00:00:00:00:00:00" {
			return strings.ToUpper(c[3]), true
		}
	}
	return "", false
}

var reLinuxPing = regexp.MustCompile(`ttl=(\d+).*time=([\d.]+)`)

func pingICMP(ip netip.Addr, limite time.Duration) (time.Duration, int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), limite+time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ping", "-c1", "-W1", ip.String()).Output()
	if err != nil {
		return 0, 0, false
	}
	m := reLinuxPing.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, false
	}
	ttl, _ := strconv.Atoi(m[1])
	ms, _ := strconv.ParseFloat(m[2], 64)
	return time.Duration(ms * float64(time.Millisecond)), ttl, true
}

func abrirNavegador(url string) {}
