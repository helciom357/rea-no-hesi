# Launcher Brasil

Um único executável para Windows com duas partes:

- **Jogador** — servidores agrupados por pista com abas de modo, página da sala
  com os carros exigidos e as vagas, instalação do conteúdo faltante em um
  clique e "Meus mods" (o que está instalado × o que a base precisa). A
  entrada no servidor continua pelo Content Manager.
- **Administrador** (`LauncherBrasil.exe -admin`) — arraste os `.zip` de carros
  e pistas, monte as salas e pronto: o envio para o site e a publicação são
  automáticos.

## Como o administrador usa

1. **Conexão** (uma vez): baixe o `upload.php`, envie para uma pasta do site
   (na Hostinger: Gerenciador de Arquivos → `public_html/ac/`), cole o
   endereço `https://seusite.com.br/ac/upload.php` e clique em Conectar.
   Passe aos jogadores o link mostrado (`…/ac/files/hub.json`).
2. **Conteúdo**: arraste os `.zip`. Um pack com vários carros vira um item por
   carro. Arrastar de novo um mod alterado cria a versão seguinte (1.0 → 1.1)
   e os jogadores recebem a atualização. Mods em `.7z`/`.rar`: instale no seu
   Assetto e use "Adicionar do meu Assetto".
3. **Salas**: nome, modo, `IP:porta HTTP` do servidor, pista e carros. Salvar
   já publica. Salas incompletas ficam fora do ar até serem completadas.

Opcional: em cada sala há o `content.json` para `cfg/cm_content/` do
AssettoServer, para quem entra pelo Content Manager sem o launcher.

## Como funciona

```
Painel ──zip──▶ biblioteca local (pacote normalizado + versão + sha256 + imagem)
   │                        │ upload em partes de 4 MB, com retomada
   │                        ▼
   └──hub.json──▶  site/ac/files/  ◀── launcher do jogador
                                        compara com content/cars|tracks,
                                        baixa, confere sha256, instala,
                                        abre a sala no Content Manager
```

- Versão por conteúdo: o painel calcula uma impressão digital dos arquivos do
  mod; reenviar o mesmo mod não muda nada, mod alterado ganha versão nova.
- O `upload.php` guarda só o hash da chave, aceita apenas `.zip`, `.json` e
  imagens, verifica o sha256 antes de publicar o arquivo e bloqueia scripts na
  pasta `files/`.
- O jogador só baixa por HTTPS do domínio da base; a instalação extrai numa
  pasta temporária e troca a pasta inteira, sem deixar mod pela metade.
- Estados de cada item: Falta baixar, Danificado (faltam `data.acd`, `.kn5`,
  `collider.kn5`, `models.ini`, `surfaces.ini`…) e Atualizar (versão menor).

Dados locais em `%APPDATA%\LauncherBrasil` (ajustes, `base.json`, `conteudo/`).

## Compilar

Go 1.24+, sem dependências externas.

```sh
cd launcher
go test ./...            # inclui teste de upload real se houver `php` no PATH
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=0.2.0" -o dist/LauncherBrasil.exe .
```

No Windows: `./build.ps1`.
