# LANTV — Plano

Canal próprio de propaganda transmitido pela rede (LAN) para até 10 TVs (50–75"), substituindo os receptores Sky que falham com a maresia.

## Decisões

| Decisão | Motivo |
|---|---|
| Emissora própria na LAN: um PC central gera um canal e as TVs assistem pela rede | Nenhum aparelho atrás da TV; tudo concentrado em um local só |
| Mesmo conteúdo em todas as TVs; atraso entre TVs é aceitável | TVs ficam separadas |
| Fontes: playlist de propagandas (vídeos e imagens) + placa de captura HDMI (TV box, notebook etc.) | Pedido do cliente |
| Sinal da Sky **não** será redistribuído | HDCP; burlar é proibido (Lei 9.610/98, art. 107) e viola o contrato. Canais pagos só via plano comercial licenciado da operadora |
| Conteúdo protegido (Netflix, Globoplay etc.) via TV box aparece preto na captura → o canal fica na propaganda | Mesmo motivo; é o comportamento esperado |
| Nunca tela preta: sem imagem na HDMI, o canal volta sozinho para a propaganda; o app da TV guarda propagandas para tocar se o PC/rede cair | Requisito: "abriu o local, está passando" |
| Qualidade: canal em 1080p ou até 4K, sempre H.264 | H.264 é o formato que todas as TVs e apps tocam |
| 4K exige codificação por hardware (Intel Quick Sync) | Teste: só com processador, 4K rodou a 35% do tempo real |
| Rede: switch gigabit exclusivo das TVs | Já existe; 10 TVs × 25 Mbps (4K) ≈ 250 Mbps, cabe com folga |
| PC central com Ubuntu 24.04 LTS, em sala fechada com ar-condicionado | Estabilidade 24/7; ar protege contra maresia |
| Tecnologia: Go (controle e painel), FFmpeg (vídeo), HLS (entrega), Kotlin/Android TV (app) | HLS toca em qualquer TV/app; Go gera um executável só |

## Arquitetura

```
SALA CENTRAL                                                        PONTOS (só a TV)
┌──────────────────────────────────────────────┐
│ Pasta de propagandas ─┐                      │
│                       ├─► Motor do canal ─► HLS ──► Switch ──► TV 1 … TV 10
│ TV box ─HDMI─► Placa ─┘   (troca automática) │        exclusivo   (app LANTV abre o canal)
│                Painel web (subir propaganda) │
└──────────────────────────────────────────────┘
```

## Compras (valores estimados, a confirmar)

| Item | Faixa |
|---|---|
| Mini PC Intel i5 12ª geração ou mais nova (Quick Sync), SSD, 16 GB — N100 só se o canal ficar em 1080p | R$ 2.500–4.000 |
| Placa de captura HDMI USB 3.x, MJPEG (UVC): 1080p (TV box vira 4K por ampliação) ou 4K30 real | R$ 150–600 / R$ 800–2.000 |
| Nobreak 1 kVA | R$ 600–1.000 |

## Fases

| Fase | Entrega | Pronta quando |
|---|---|---|
| **0. Teste de bancada** | 1 TV + PC + placa, com o protótipo `lantv-canal` | Roteiro abaixo aprovado |
| 1. Motor do canal | Playlist 24h, troca HDMI/propaganda, atualizar playlist sem derrubar o canal, reinício automático | 7 dias sem intervenção |
| 2. App Android/Google TV | Abre sozinho ao ligar, tela cheia, reconecta, toca propagandas guardadas se o canal cair | TV religada / cabo puxado → volta sozinha |
| 3. Painel web | Subir, ordenar e remover propagandas; ver o que está no ar; forçar HDMI/propaganda | Operador usa sem ajuda |
| 4. TVs Samsung/LG | App IPTV da loja apontando para o canal; avaliar app próprio | Todas as TVs da casa abrindo o canal |
| 5. Instalação | PC na sala com ar, nobreak, BIOS "ligar ao voltar energia", TVs configuradas | 7 dias em produção sem chamado |

Antes da fase 3, avaliar o [ffplayout](https://github.com/ffplayout/ffplayout) (playout 24/7 pronto, com painel web e entrada ao vivo) como alternativa a construir o painel do zero.

## Fase 0 — roteiro de teste de bancada

### Preparar o PC

```bash
sudo apt install ffmpeg golang-go v4l-utils alsa-utils git intel-media-va-driver-non-free vainfo
git clone <repo> lantv && cd lantv
go build -o lantv-canal ./cmd/lantv-canal
vainfo | grep -i h264 | grep -i enc     # precisa listar VAProfileH264High ... VAEntrypointEncSlice(LP)
```

Identificar a placa de captura:

```bash
v4l2-ctl --list-devices                               # vídeo, ex.: /dev/video0
v4l2-ctl -d /dev/video0 --list-formats-ext | grep -A3 MJPG   # precisa ter 1920x1080 30fps
arecord -L | grep -i hw:                               # áudio, ex.: hw:CARD=MS2109,DEV=0
```

### Rodar

```bash
mkdir propagandas   # copiar vídeos/imagens; tocam em ordem alfabética (01-..., 02-...)

# 1) sem placa, HDMI simulada (20 s imagem / 20 s preto)
./lantv-canal -hdmi simular

# 2) com a placa
./lantv-canal -hdmi /dev/video0 -hdmi-audio hw:CARD=MS2109,DEV=0

# 3) 4K com Quick Sync (placa capturando em 1080p; use -hdmi-tamanho 3840x2160 se ela capturar 4K)
./lantv-canal -hdmi /dev/video0 -largura 3840 -altura 2160 -bitrate 25M -codificador vaapi
```

- Canal: `http://IP-DO-PC:8080/canal/canal.m3u8`
- Página de teste (navegador da TV): `http://IP-DO-PC:8080/`
- Estado / forçar fonte: `http://IP-DO-PC:8080/status?modo=auto|propaganda|hdmi`

### Checklist (anotar resultado por marca/modelo de TV)

| # | Teste | Esperado |
|---|---|---|
| 1 | Abrir o canal na TV (Android: VLC com a URL; Samsung/LG: navegador na página de teste e um app IPTV da loja) | Toca |
| 2 | Qualidade da imagem na TV de 75" a 2–3 m | Sem blocos/borrões visíveis |
| 3 | Atraso: relógio na tela do PC/TV box vs. TV | Anotar (esperado 6–12 s) |
| 4 | O que a placa mostra **sem nada conectado** | Preto. Se mostrar azul/"No Signal", o detector precisa de ajuste |
| 5 | Ligar TV box (tela inicial) | Canal troca para HDMI em ~2 s |
| 6 | Abrir Netflix/Globoplay/YouTube no TV box | Anotar: aparece ou fica preto (se preto, canal volta para propaganda) |
| 7 | Desligar o TV box | Canal volta para propaganda em ~3 s |
| 8 | Desplugar o USB da placa / replugar | Canal continua na propaganda; volta a aceitar HDMI ao replugar |
| 9 | CPU do PC com o canal no ar (`top`) | Abaixo de 60% |
| 10 | Puxar o cabo de rede da TV e recolocar | Anotar se volta sozinha |
| 11 | Desligar e ligar a TV | Anotar se abre sozinha no canal (por marca) |
| 12 | Deixar rodando 24 h | Sem travar; memória estável |
| 13 | Canal em 4K com `-codificador vaapi` (comando 3) | `ls saida` ganha 1 segmento a cada 2 s; CPU abaixo de 60% |
| 14 | Canal 4K em cada TV | Toca liso; anotar TVs que não abrem 4K |

## Riscos

| Risco | Mitigação |
|---|---|
| Samsung/LG comuns não abrem sozinhas app de fora da loja; na LG o modo desenvolvedor expira | App IPTV da loja nessas TVs; padronizar Google TV nas próximas compras |
| TVs com app da loja mostram erro (não propaganda) se o PC cair | Só o app próprio (fase 2) tem propagandas guardadas |
| PC central é ponto único de falha | Nobreak, reinício automático, app com propagandas guardadas |
| Placa mostra tela "No Signal" colorida em vez de preto | Teste 4; ajustar o detector |
| Trocar a playlist hoje reinicia o canal (TVs piscam alguns segundos) | Resolver na fase 1 |
| Deriva de relógio entre placa e playlist em execução longa | Teste 12 |
| Caminho 4K com Quick Sync ainda não testado (ambiente de desenvolvimento sem GPU Intel) | Testes 13–14 na bancada |
| Em 4K, ampliar a imagem e decodificar as propagandas ainda usa o processador | Medir no teste 13; se pesar, mover para a GPU na fase 1 |
| Quando a HDMI escurece, o preto fica ~3 s no ar antes de voltar à propaganda (espera para não trocar em cena escura) | Ajustar o tempo no teste 7 |

## Resultados já obtidos (ambiente de desenvolvimento, HDMI simulada)

| Teste | Resultado |
|---|---|
| Troca automática HDMI ↔ propaganda (5 ciclos + troca forçada), gravando o canal por 100 s como uma TV | 3.002 quadros contínuos, sem lacuna de vídeo ou áudio; troca em ~2 s (sinal) e ~3 s (preto) |
| Placa ausente | Canal no ar só com propaganda |
| Pasta de propagandas vazia | Vinheta "LANTV" no ar (nunca preto) |
| Motor de vídeo derrubado à força | Volta sozinho em ~2 s; TVs recebem aviso de descontinuidade |
| 4K só com processador (4 núcleos) | 35% do tempo real — inviável sem Quick Sync |
