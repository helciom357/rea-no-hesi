param([string]$Version = "0.2.0")
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
go test ./...
$env:GOOS = "windows"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w -X main.Version=$Version" -o dist/LauncherBrasil.exe .
Write-Host "Gerado: dist/LauncherBrasil.exe"
