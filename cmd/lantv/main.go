// lantv: emissora própria de propaganda na rede.
//
// Gera um canal HLS contínuo com as propagandas cadastradas no painel e, se
// houver uma placa de captura HDMI com imagem, troca para ela sozinho. Sem
// imagem (aparelho desligado, conteúdo protegido por HDCP ou placa ausente) o
// canal volta para as propagandas, então nunca fica preto.
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
)

// registro recebe o log do programa e dos ffmpeg: arquivo dados/lantv.log
// (o .exe do Windows não tem janela) e a tela, quando houver.
var registro io.Writer = os.Stderr

type ignorarErro struct{ w io.Writer }

func (i ignorarErro) Write(b []byte) (int, error) { i.w.Write(b); return len(b), nil }

func abrirLog(dir string) {
	arq := filepath.Join(dir, "lantv.log")
	if st, err := os.Stat(arq); err == nil && st.Size() > 5<<20 {
		os.Rename(arq, arq+".antigo")
	}
	if f, err := os.OpenFile(arq, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		registro = io.MultiWriter(f, ignorarErro{os.Stderr})
		log.SetOutput(registro)
	}
}

func main() {
	exe, _ := os.Executable()
	padrao := filepath.Join(filepath.Dir(exe), "dados")
	dados := flag.String("dados", padrao, "pasta onde ficam propagandas, configurações e o canal")
	porta := flag.Int("porta", 8080, "porta do painel e do canal")
	senha := flag.String("senha", os.Getenv("LANTV_SENHA"), "senha do painel (opcional; também vale a variável LANTV_SENHA)")
	simular := flag.Bool("simular", false, "usa uma HDMI de mentira (20 s imagem, 20 s preto) para testes")
	flag.Parse()

	os.MkdirAll(*dados, 0o755)
	abrirLog(*dados)
	if !temFiltroZMQ() {
		log.Fatal("o ffmpeg não foi encontrado ou não tem o filtro ZMQ. Coloque o ffmpeg.exe e o ffprobe.exe (versão \"full\" ou \"GPL\") na mesma pasta do lantv.exe.")
	}
	if err := os.MkdirAll(filepath.Join(*dados, "saida"), 0o755); err != nil {
		log.Fatal(err)
	}

	cfg := abrirConfig(*dados)
	lib, err := abrirBiblioteca(*dados, cfg)
	if err != nil {
		log.Fatal(err)
	}
	motor := novoMotor(cfg, lib, *dados, *simular)
	motor.hardware = codificadoresDisponiveis()
	log.Printf("codificadores por hardware: %v", motor.hardware)

	go motor.Rodar()
	go motor.VigiarHDMI()
	go motor.DecidirFonte()
	(&Servidor{motor: motor, lib: lib, cfg: cfg, dados: *dados, porta: *porta, senha: *senha}).Servir()
}
