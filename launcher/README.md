# Launcher Brasil

Um único executável para Windows que deixa o jogador pronto para entrar nas
salas da base brasileira de Assetto Corsa (Av. Brasil, Bandeirantes, Washington
Luís, Interlagos…), e um painel para o administrador montar essa base.

O launcher não substitui o Content Manager: ele confere e instala o conteúdo
que falta, e o Content Manager continua fazendo a conexão com o servidor.

## Como funciona

```
Painel do admin ──gera──▶ hub.json ──────────────▶ Launcher do jogador
      │                   (salas, modos, carros,         │
      │                    pista, versão, link)          │ compara com a pasta
      │                                                  │ do Assetto e instala
      └──gera──▶ cfg/cm_content/content.json             │ o que falta
                 (um por servidor AssettoServer)         ▼
                         │                     Entrar pelo Content Manager
                         └──── /api/details ────▶ (acmanager://…)
```

Para cada carro e pista de uma sala, o launcher classifica:

| Estado | Quando |
| --- | --- |
| **Falta** | a pasta não existe em `content/cars` ou `content/tracks` |
| **Danificado** | falta `data.acd`, `ui/ui_car.json`, `.kn5`, `collider.kn5` (carros) ou `models.ini`, `data/surfaces.ini`, `ui_track.json` (pistas) |
| **Desatualizado** | o campo `version` do `ui_car.json`/`ui_track.json` local é menor que o da base |

A instalação baixa até 3 pacotes ao mesmo tempo, confere o `sha256`, extrai
numa pasta temporária e só então substitui a pasta antiga — uma instalação que
falha não deixa um mod pela metade. Downloads só são aceitos via HTTPS dos
hosts permitidos pela base.

## Uso

- **Jogador:** abra `LauncherBrasil.exe`. Em *Ajustes*, confirme a pasta do
  Assetto Corsa (detectada pela Steam) e cole o link do `hub.json` da base. Um
  `hub.json` colocado ao lado do `.exe` é usado automaticamente.
- **Administrador:** abra `LauncherBrasil.exe -admin` (ou *Ajustes → Painel do
  administrador*).
  1. *Pacotes → Empacotar do meu Assetto Corsa*: gera o `.zip` de cada carro ou
     pista já instalado no seu PC, gravando a versão dentro do pacote.
  2. *Salas*: cadastre servidor, pista e carros escolhendo os pacotes.
  3. *Publicar*: gera `hub.json` e `servidores/<sala>/cfg/cm_content/content.json`.
  4. Suba os `.zip` para o endereço de downloads, o `hub.json` para o site e o
     `cfg/cm_content/content.json` para cada AssettoServer.

Sempre que alterar um mod, empacote de novo com uma versão maior. É isso que
faz os jogadores receberem a atualização e evita o erro de checksum no
handshake.

Os dados do launcher ficam em `%APPDATA%\LauncherBrasil` (ajustes, `base.json`,
pacotes e publicação).

## Compilar

Requer Go 1.24+. Não há dependências externas.

```sh
cd launcher
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=0.1.0" -o dist/LauncherBrasil.exe .
```

No Windows (PowerShell): `./build.ps1`.
