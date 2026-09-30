#!/usr/bin/env bash
# Abre o playground; se a infra não estiver respondendo, sobe tudo antes.
set -euo pipefail
cd "$(dirname "$0")/.."

ready() {
  for port in 8080 8081 8082 8083; do
    curl -sf "localhost:$port/health/ready" >/dev/null 2>&1 || return 1
  done
}

if ready; then
  echo "ambiente no ar (nginx :8080, instâncias :8081 :8082 :8083)"
else
  echo "ambiente fora do ar ou incompleto; subindo tudo (leva uns minutos na primeira vez)..."
  scripts/local-up.sh --no-demo
fi

exec go run ./cmd/playground
