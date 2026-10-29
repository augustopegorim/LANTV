// lanscan: varredura da rede (LAN/Wi-Fi) com interface no navegador.
//
// Descobre os aparelhos por ARP, ping e portas TCP; descobre nomes por DNS,
// NetBIOS e mDNS; identifica o fabricante pelo MAC e coleta serviços abertos.
// Começa pela rede em que o PC está conectado e aceita outras faixas de IP.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

func main() {
	porta := flag.Int("porta", 8787, "porta do painel (só aceita acesso deste PC)")
	semNavegador := flag.Bool("sem-navegador", false, "não abrir o navegador sozinho")
	flag.Parse()

	var ln net.Listener
	var err error
	for p := *porta; p < *porta+20; p++ { // se a porta estiver ocupada, tenta a seguinte
		if ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			break
		}
	}
	if err != nil {
		log.Fatalf("não foi possível abrir uma porta: %v", err)
	}
	s := &Servidor{v: &Varredura{estado: "ociosa", hosts: map[string]*Host{}}, addr: ln.Addr().String()}
	url := "http://" + s.addr + "/"
	fmt.Println("Varredura de rede aberta em", url, "(feche esta janela para encerrar)")
	if !*semNavegador {
		abrirNavegador(url)
	}
	log.Fatal(http.Serve(ln, s.handler()))
}
