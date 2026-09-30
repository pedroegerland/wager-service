#!/usr/bin/env bash
# Abre o playground; sobe a infra antes se ela não estiver respondendo.
set -euo pipefail
cd "$(dirname "$0")/.."
scripts/ensure-infra.sh
exec go run ./cmd/playground
