# LANTV — varredura de rede

Programa para Windows que varre a rede Wi-Fi/LAN e lista os aparelhos com **IP, nome, MAC, fabricante, tipo/sistema provável, portas abertas e serviços**. A interface abre no navegador; é um único `lanscan.exe`, sem instalação.

## Como usar

1. Baixe o `lanscan.exe` (GitHub → aba **Actions** → execução mais recente → artefato **lanscan-windows**) ou compile: `go build -o lanscan.exe ./cmd/lanscan`.
2. Dê dois cliques. O navegador abre em `http://127.0.0.1:8787/`. Feche a janela preta para encerrar.
3. A rede em que o PC está conectado já vem marcada. Para varrer outras faixas, digite e clique em **Adicionar**: `10.0.0.0/24`, `192.168.2.10-50`, `192.168.2.10-192.168.2.80` ou um IP. Pode juntar várias faixas (até 65.536 endereços por varredura).
4. Escolha a profundidade (Rápida, Padrão ou Completa) e clique em **Iniciar varredura**. Clique em um aparelho para ver todos os detalhes. **Exportar CSV/JSON** guarda o resultado.

## O que ele descobre e como

| Informação | Fonte |
|---|---|
| Quem está ligado | ARP (mesma rede), ping e portas TCP |
| MAC | ARP; ou NetBIOS (vale também em outras faixas) |
| Fabricante | Base IEEE (OUI) embutida no programa |
| Nome | DNS reverso, NetBIOS e mDNS/Bonjour |
| Serviços | Portas abertas, título e servidor de páginas web, certificado HTTPS, banner SSH |
| Tipo e sistema | Dedução por TTL, portas e fabricante (é uma estimativa) |

## Limites

- Em **outras faixas** (atrás de um roteador) o MAC não é visível; só aparece se o próprio aparelho informar por NetBIOS.
- Celulares modernos usam **MAC aleatório** por rede; o fabricante não pode ser identificado.
- Aparelhos com firewall que bloqueia ping e portas podem não aparecer.
- O painel só aceita acesso do próprio PC (`127.0.0.1`).
- Use apenas em redes que você administra ou tem autorização para testar.
