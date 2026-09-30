#!/usr/bin/env bash
# Roda todas as verificações: formatação, vet, unitários com -race e integração
# contra os containers (precisa do ambiente no ar: scripts/local-up.sh).
set -euo pipefail
cd "$(dirname "$0")/.."
export CGO_ENABLED=0

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

step "gofmt"
unformatted=$(gofmt -l .)
[ -z "$unformatted" ] || { echo "arquivos fora do gofmt:"; echo "$unformatted"; exit 1; }
echo ok

step "go vet"
go vet ./...
go vet -tags integration ./...

step "go test -race ./... (unitários, sem infraestrutura)"
go test -race -count=1 ./...

step "integração in-process (uma instância dentro do processo de teste)"
go test -race -tags integration -count=1 -timeout 15m ./test/integration/... ./internal/adapter/sqs/...

step "integração contra as três instâncias reais (:8081 :8082 :8083)"
API_URL=http://localhost:8080 API_INSTANCES=http://localhost:8081,http://localhost:8082,http://localhost:8083 \
  go test -race -tags integration -count=1 -timeout 15m ./test/integration/...

step "tudo verde"
