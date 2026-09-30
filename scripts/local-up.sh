#!/usr/bin/env bash
# Sobe o ambiente (se preciso) e roda um fluxo de exemplo no final.
# Uso: scripts/local-up.sh [--no-demo]   (ou: make demo)
set -euo pipefail
cd "$(dirname "$0")/.."

scripts/ensure-infra.sh
[ "${1:-}" = "--no-demo" ] && exit 0

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
new_uuid() {
  uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null \
    || python3 -c 'import uuid; print(uuid.uuid4())' 2>/dev/null \
    || powershell.exe -NoProfile -Command '[guid]::NewGuid().ToString()' 2>/dev/null | tr -d '\r'
}

step "fluxo de exemplo pela nginx (localhost:8080)"
ADMIN=$(scripts/token.sh wallet-admin)
PROV=$(scripts/token.sh provider-a)
PLAYER=$(new_uuid | tr 'A-F' 'a-f')
RUN=${PLAYER:0:8}

echo "# abrindo carteira com 1000.00 BRL"
WALLET_JSON=$(curl -s localhost:8080/wallets -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d "{\"playerId\":\"$PLAYER\",\"initialBalance\":{\"amount\":\"1000.00\",\"currency\":\"BRL\"}}")
echo "$WALLET_JSON"
WALLET=$(echo "$WALLET_JSON" | sed -E 's/.*"id":"([^"]+)".*/\1/')

BET="{\"providerId\":\"provider-a\",\"externalTransactionId\":\"bet-$RUN\",\"playerId\":\"$PLAYER\",\"walletId\":\"$WALLET\",\"roundId\":\"round-1\",\"gameId\":\"fortune-chimp\",\"kind\":\"BET\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"
echo "# aposta de 25.00"
curl -s localhost:8080/wagering/transactions -H "Authorization: Bearer $PROV" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: provider-a:bet-$RUN" -d "$BET"; echo
echo "# mesma aposta de novo (replay)"
curl -s localhost:8080/wagering/transactions -H "Authorization: Bearer $PROV" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: provider-a:bet-$RUN" -d "$BET"; echo
echo "# refund da aposta"
curl -s localhost:8080/wagering/transactions -H "Authorization: Bearer $PROV" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: provider-a:refund-$RUN" \
  -d "{\"providerId\":\"provider-a\",\"externalTransactionId\":\"refund-$RUN\",\"referenceExternalTransactionId\":\"bet-$RUN\",\"playerId\":\"$PLAYER\",\"walletId\":\"$WALLET\",\"roundId\":\"round-1\",\"gameId\":\"fortune-chimp\",\"kind\":\"REFUND\",\"money\":{\"amount\":\"25.00\",\"currency\":\"BRL\"}}"; echo
echo "# sem token -> 401; provider lendo carteira -> 403"
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/wallets/$WALLET
curl -s -o /dev/null -w '%{http_code}\n' localhost:8080/wallets/$WALLET -H "Authorization: Bearer $PROV"
echo "# reconciliação"
curl -s -X POST localhost:8080/wallets/$WALLET/reconciliation -H "Authorization: Bearer $ADMIN"; echo

cat <<MSG

Pronto.
  api (nginx, 3 instâncias): http://localhost:8080   instâncias: :8081 :8082 :8083
  keycloak:                  http://localhost:8180   (admin / admin)
  tokens:                    scripts/token.sh provider-a | provider-b | wallet-admin
  testar na mão:             make play
  testes:                    make verify
  derrubar:                  docker compose down -v
MSG
