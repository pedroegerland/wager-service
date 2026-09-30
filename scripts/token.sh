#!/usr/bin/env bash
# usage: scripts/token.sh [provider-a|provider-b|wallet-admin|outsider]
# prints an access token from the local keycloak
set -euo pipefail
client="${1:-provider-a}"
kc="${KEYCLOAK_URL:-http://localhost:8180}"
curl -sf -X POST "$kc/realms/wager/protocol/openid-connect/token" \
  -d grant_type=client_credentials \
  -d client_id="$client" \
  -d client_secret="$client-secret" | sed -E 's/.*"access_token":"([^"]+)".*/\1/'
