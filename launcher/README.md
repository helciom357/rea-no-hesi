# Launcher Brasil

Um único executável para Windows que transforma o race invite de um servidor
de Assetto Corsa num **evento**: o jogador vê o evento na home, baixa
exatamente os arquivos que aquele servidor roda e entra pelo Content Manager.

## Quem faz o quê

| Quem | O que faz |
| --- | --- |
| **Dono da base** | Hospeda a base num site com PHP (ex.: Hostinger) e gera códigos para os hosts. |
| **Host** | Tem um servidor (Content Manager / AssettoServer). Cola o código de host uma vez e publica eventos. |
| **Jogador** | Cola o link da base, escolhe um evento, instala o que falta e joga. |

## Fluxo do host (painel → Novo evento)

1. **Evento** — nome, descrição, público ou privado (privado pede senha).
2. **Conexão** — Internet ou LAN via Radmin VPN (nome e senha da rede).
3. **Race invite** — cole o convite do servidor; *Ler servidor* busca pista e
   carros no `/INFO` do servidor.
4. **Pista** e 5. **Carros** — o que estiver no Assetto do host vira pacote
   com um clique (*Usar os arquivos do meu Assetto*); o que não estiver,
   arraste o `.zip` para a janela.

*Publicar* envia os pacotes que faltam e grava o evento na base.

## Como o launcher sabe se o jogador tem os arquivos certos

Cada pacote é nomeado pelo seu hash (`car-<id>-<hash>.zip`), então dois hosts
com versões diferentes do mesmo carro não se sobrescrevem: cada evento aponta
para os arquivos do seu servidor. O evento guarda o tamanho e o CRC dos
arquivos que o handshake do Assetto confere (`data.acd`, `.kn5`,
`surfaces.ini`, `models*.ini`). No PC do jogador cada item fica:

| Estado | Quando |
| --- | --- |
| **Instalado** | os arquivos conferidos são iguais aos do servidor |
| **Falta baixar** | a pasta não existe |
| **Danificado** | falta `data.acd`, `ui_car.json`, `.kn5`, `surfaces.ini`… |
| **Versão diferente** | o jogador tem o mod, mas não os mesmos arquivos do servidor |

Skins, `ui/` e extras de CSP não entram na comparação. A instalação baixa até
3 pacotes ao mesmo tempo, confere o sha256, extrai numa pasta temporária e só
então troca a pasta — uma instalação que falha não deixa mod pela metade.

## A base no site (`receiver/upload.php`)

- `files/` — pacotes e imagens (público).
- `data/` — eventos, hosts e nome da base. São arquivos `.php` que encerram
  se acessados direto, além do `.htaccess`.
- A chave do dono fica só como hash no `upload.php`; cada host tem a sua.
  O dono remove um host e os eventos dele saem junto.
- Evento privado: a home mostra nome, host, pista e carros; o race invite, a
  rede Radmin e os links de download só saem com a senha (guardada como hash).
  Os pacotes em `files/` têm nome imprevisível, mas são públicos para quem
  tiver o link.

Os dados do launcher ficam em `%APPDATA%\LauncherBrasil`.

## Compilar

Requer Go 1.24+. Sem dependências externas. Os testes usam `php` se estiver
instalado (pulam o teste do `upload.php` se não estiver).

```sh
cd launcher
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=0.3.0" -o dist/LauncherBrasil.exe .
```

No Windows (PowerShell): `./build.ps1`.
