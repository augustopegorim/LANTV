# LANTV

Emissora própria de propaganda na rede: um PC com Windows gera um canal e as TVs assistem pelo cabo de rede. Um aparelho HDMI (TV box, notebook…) ligado numa placa de captura entra no ar sozinho quando está ligado; quando desliga, o canal volta para as propagandas. O canal nunca fica preto.

Plano e decisões: [PLANO.md](PLANO.md).

## Instalar no PC (Windows 10/11)

1. Baixe o **lantv-windows** (artefato do GitHub Actions, aba *Actions* → *Windows*) ou compile: `go build -ldflags "-H=windowsgui" -o lantv.exe ./cmd/lantv`.
2. Baixe o **ffmpeg** (versão *full* ou *GPL*, ex.: gyan.dev ou BtbN) e copie `ffmpeg.exe` e `ffprobe.exe` para a mesma pasta do `lantv.exe`.
3. Execute `instalar-windows.ps1` como administrador. Ele abre a porta 8080, impede o PC de dormir e inicia o LANTV junto com o Windows.
4. Abra o painel em `http://IP-DO-PC:8080/`. Para testar sem placa de captura: `lantv.exe -simular`.

Senha do painel (opcional): variável de ambiente `LANTV_SENHA` ou `lantv.exe -senha MINHASENHA`. O canal das TVs nunca pede senha.

## Nas TVs

- Android/Google TV: app de IPTV ou VLC com `http://IP-DO-PC:8080/canal/canal.m3u8`.
- Samsung/LG: app de IPTV da loja com o mesmo endereço, ou o navegador da TV em `http://IP-DO-PC:8080/tv`.

## Arquivos

Tudo fica em `dados/` ao lado do `lantv.exe` (propagandas, configurações, `lantv.log`). Para fazer backup, copie essa pasta.
