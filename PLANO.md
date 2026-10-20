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
| 4K exige codificação por hardware (Intel Quick Sync) | Teste (4 núcleos): só com processador, 4K rodou a 35% do tempo real |
| Rede: switch gigabit exclusivo das TVs | Já existe; 10 TVs × 25 Mbps (4K) ≈ 250 Mbps, cabe com folga |
| PC central com **Windows 10/11**, em sala fechada com ar-condicionado | Decisão do cliente; ar protege contra maresia |
| Tecnologia: Go (`lantv.exe`: motor + painel web), FFmpeg (vídeo), HLS (entrega); app Kotlin/Android TV só na fase 2 | HLS toca em qualquer TV/app; um executável só |
| 4K por hardware no Windows: Intel Quick Sync, NVIDIA ou AMD (escolha automática) | Mini PC com Intel i5 12ª geração+ já resolve |

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
| Mini PC com Windows 11, Intel i5 12ª geração ou mais nova (Quick Sync), SSD, 16 GB — N100 só se o canal ficar em 1080p | R$ 2.500–4.000 |
| Placa de captura HDMI USB 3.x, MJPEG (UVC): 1080p (TV box vira 4K por ampliação) ou 4K30 real | R$ 150–600 / R$ 800–2.000 |
| Nobreak 1 kVA | R$ 600–1.000 |

## Fases

| Fase | Entrega | Pronta quando |
|---|---|---|
| **0. Teste de bancada** | 1 TV + PC + placa, com o `lantv.exe` | Roteiro abaixo aprovado |
| 1. Motor do canal e painel — **feito (falta validar no Windows)** | Playlist 24h, troca HDMI/propaganda, painel web (enviar, ordenar, pausar, remover, modo, configurações), mudanças sem derrubar o canal, reinício automático | 7 dias sem intervenção |
| 2. App Android/Google TV | Abre sozinho ao ligar, tela cheia, reconecta, toca propagandas guardadas se o canal cair | TV religada / cabo puxado → volta sozinha |
| 4. TVs Samsung/LG | App IPTV da loja apontando para o canal; avaliar app próprio | Todas as TVs da casa abrindo o canal |
| 5. Instalação | PC na sala com ar, nobreak, BIOS "ligar ao voltar energia", TVs configuradas | 7 dias em produção sem chamado |


## Fase 0 — roteiro de teste de bancada

### Preparar o PC (Windows)

Siga o [LEIA-ME.md](LEIA-ME.md): baixar o `lantv.exe` (GitHub Actions → *Windows*), colocar `ffmpeg.exe`/`ffprobe.exe` (versão *full*) na mesma pasta e rodar `instalar-windows.ps1`.

### Rodar o teste

1. Sem placa: `lantv.exe -simular` (HDMI de mentira: 20 s imagem, 20 s preto). Abra o painel `http://IP-DO-PC:8080/`.
2. Envie 2 ou 3 propagandas pelo painel e confira que tocam em sequência.
3. Com a placa: painel → *Configurações* → escolher a placa de captura e o áudio dela → *Salvar e aplicar*.
4. 4K: *Configurações* → Qualidade 4K (codificador *Automático* escolhe Quick Sync/NVIDIA/AMD). Se a placa de captura for 4K, marque "A placa de captura é 4K".
5. Nas TVs: app de IPTV/VLC com `http://IP-DO-PC:8080/canal/canal.m3u8`, ou o navegador da TV em `http://IP-DO-PC:8080/tv`.

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
| 13 | Qualidade 4K no painel | Painel mostra o codificador de hardware em uso; CPU abaixo de 60% |
| 14 | Canal 4K em cada TV | Toca liso; anotar TVs que não abrem 4K |

## Riscos

| Risco | Mitigação |
|---|---|
| Samsung/LG comuns não abrem sozinhas app de fora da loja; na LG o modo desenvolvedor expira | App IPTV da loja nessas TVs; padronizar Google TV nas próximas compras |
| TVs com app da loja mostram erro (não propaganda) se o PC cair | Só o app próprio (fase 2) tem propagandas guardadas |
| PC central é ponto único de falha | Nobreak, reinício automático, app com propagandas guardadas |
| Placa mostra tela "No Signal" colorida em vez de preto | Teste 4; ajustar o detector |
| Se a placa de captura falhar, o canal reinicia uma vez (~3 s) e segue só com propagandas; o motor tenta a placa de novo em 30 s, 1, 2, 5 e 10 min | Teste 8 |
| Deriva de relógio entre placa e playlist em execução longa | Teste 12 |
| Windows: captura (DirectShow) e codificadores por hardware ainda não testados em PC real (desenvolvido em Linux, compilado para Windows) | Testes 4–8 e 13–14 na bancada |
| Em 4K, ampliar a imagem e decodificar as propagandas ainda usa o processador | Medir no teste 13; se pesar, mover para a GPU na fase 1 |
| Quando a HDMI escurece, o preto fica ~3 s no ar antes de voltar à propaganda (espera para não trocar em cena escura) | Ajustar o tempo no teste 7 |

## Resultados já obtidos (ambiente de desenvolvimento, HDMI simulada)

| Teste | Resultado |
|---|---|
| Troca automática HDMI ↔ propaganda (5 ciclos + troca forçada), gravando o canal por 100 s como uma TV | 3.002 quadros contínuos, sem lacuna de vídeo ou áudio; troca em ~2 s (sinal) e ~3 s (preto) |
| Painel/API: enviar 3 vídeos + 1 imagem (arquivo .exe recusado), reordenar, pausar, mudar tempo da imagem, remover, trocar modo — com o canal gravado por 70 s | 2.101 quadros, nenhuma lacuna de vídeo ou áudio; miniaturas e estados corretos |
| Placa ausente | Canal no ar só com propaganda |
| Pasta de propagandas vazia | Vinheta "LANTV" no ar (nunca preto) |
| Motor de vídeo derrubado à força | Volta sozinho em ~2 s; TVs recebem aviso de descontinuidade |
| 4K só com processador (4 núcleos) | 35% do tempo real — inviável sem Quick Sync |
