#!/usr/bin/env bash
# Garante que o ambiente local está de pé: detecta o sistema, inicia o Docker se
# preciso, confere as ferramentas e sobe o compose quando algo não responde.
# Chamado por make demo / play / verify / test-integration / test-multi.
set -euo pipefail
cd "$(dirname "$0")/.."

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux)
    if grep -qi microsoft /proc/version 2>/dev/null; then OS=wsl; else OS=linux; fi ;;
  MINGW*|MSYS*|CYGWIN*) OS=windows ;;
  *) OS=unknown ;;
esac

install_hint() {
  case "$OS" in
    macos)   echo "  macOS:   brew install --cask docker    (e brew install go curl)" ;;
    windows) echo "  Windows: winget install Docker.DockerDesktop GoLang.Go   (rode este script no Git Bash ou no WSL)" ;;
    wsl)     echo "  WSL:     instale o Docker Desktop no Windows e ative a integração com esta distro; sudo apt install golang-go curl" ;;
    linux)   echo "  Linux:   https://docs.docker.com/engine/install/  e  sudo apt install golang-go curl (ou o equivalente da sua distro)" ;;
    *)       echo "  instale Docker, Go 1.27+ e curl" ;;
  esac
}

step "sistema: $OS"

missing=0
for tool in docker curl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "falta: $tool"; missing=1; }
done
if ! command -v go >/dev/null 2>&1; then
  echo "aviso: go não encontrado; a api roda em container, mas testes e playground precisam de Go 1.27+"
fi
if [ "$missing" = 1 ]; then
  echo "instale o que falta e rode de novo:"; install_hint; exit 1
fi

start_docker() {
  case "$OS" in
    macos)   open -a Docker ;;
    windows) (cmd.exe /c start "" "C:\\Program Files\\Docker\\Docker\\Docker Desktop.exe" >/dev/null 2>&1) || true ;;
    wsl)     ("/mnt/c/Program Files/Docker/Docker/Docker Desktop.exe" >/dev/null 2>&1 &) || true ;;
    linux)   if command -v systemctl >/dev/null 2>&1; then sudo systemctl start docker || true; fi ;;
  esac
}

if ! docker info >/dev/null 2>&1; then
  step "docker parado; tentando iniciar"
  start_docker
  for i in $(seq 1 90); do
    docker info >/dev/null 2>&1 && break
    sleep 2
    if [ "$i" -eq 90 ]; then
      echo "o docker não respondeu em 3 minutos. Abra o Docker Desktop (ou 'sudo systemctl start docker') e rode de novo."
      install_hint; exit 1
    fi
  done
  echo "docker no ar"
fi
docker compose version >/dev/null 2>&1 || { echo "docker compose v2 não encontrado (vem com o Docker Desktop; no Linux instale docker-compose-plugin)"; exit 1; }

[ -f .env ] || { cp .env.example .env; echo ".env criado a partir de .env.example"; }

ready() {
  for port in 8080 8081 8082 8083; do
    curl -sf "localhost:$port/health/ready" >/dev/null 2>&1 || return 1
  done
}

if ready; then
  echo "ambiente no ar (nginx :8080, instâncias :8081 :8082 :8083)"
  exit 0
fi

step "subindo postgres, keycloak, localstack, migrations, 3 instâncias da api e nginx"
docker compose up --build -d

step "aguardando as instâncias ficarem prontas"
for i in $(seq 1 120); do
  ready && break
  sleep 2
  [ "$i" -eq 120 ] && { echo "timeout esperando a api; veja: docker compose logs api-1"; exit 1; }
done
docker compose ps --format 'table {{.Service}}\t{{.Status}}'
