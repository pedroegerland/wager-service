//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	tokenMu    sync.Mutex
	tokenCache = map[string]string{}
)

func token(t testing.TB, client string) string {
	t.Helper()
	tokenMu.Lock()
	defer tokenMu.Unlock()
	if tok, ok := tokenCache[client]; ok {
		return tok
	}
	form := url.Values{
		"grant_type": {"client_credentials"}, "client_id": {client}, "client_secret": {client + "-secret"},
	}
	resp, err := http.PostForm(keycloakURL+"/realms/wager/protocol/openid-connect/token", form)
	if err != nil {
		t.Fatalf("keycloak: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("keycloak %d: %s", resp.StatusCode, body)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(body, &out)
	tokenCache[client] = out.AccessToken
	return out.AccessToken
}

type resp struct {
	Status int
	Body   map[string]any
	Raw    []byte
	Header http.Header
}

func (r resp) str(k string) string {
	v, _ := r.Body[k].(string)
	return v
}

func (r resp) money(k string) string {
	m, _ := r.Body[k].(map[string]any)
	s, _ := m["amount"].(string)
	return s
}

func (r resp) bool(k string) bool {
	b, _ := r.Body[k].(bool)
	return b
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func httpRequest(t testing.TB, base, method, path, tok string, body any, headers map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Raw: raw, Header: res.Header}
	if len(raw) > 0 && strings.Contains(res.Header.Get("Content-Type"), "json") {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

func apiRequest(t testing.TB, method, path, tok string, body any, headers map[string]string) resp {
	return httpRequest(t, apiURL, method, path, tok, body, headers)
}

func openWallet(t testing.TB, amount string) (walletID, playerID string) {
	t.Helper()
	playerID = uuid.NewString()
	r := apiRequest(t, "POST", "/wallets", token(t, "wallet-admin"), map[string]any{
		"playerId": playerID, "initialBalance": map[string]string{"amount": amount, "currency": "BRL"},
	}, nil)
	if r.Status != 201 {
		t.Fatalf("open wallet: %d %s", r.Status, r.Raw)
	}
	return r.str("id"), playerID
}

type operation struct {
	provider, ext, kind, amount, ref, round string
	walletID, playerID                      string
	key                                     string
}

func (o operation) body() map[string]any {
	b := map[string]any{
		"providerId": o.provider, "externalTransactionId": o.ext, "playerId": o.playerID, "walletId": o.walletID,
		"roundId": o.round, "gameId": "fortune-chimp", "kind": o.kind,
		"money": map[string]string{"amount": o.amount, "currency": "BRL"},
	}
	if o.ref != "" {
		b["referenceExternalTransactionId"] = o.ref
	}
	return b
}

func (o operation) idemKey() string {
	if o.key != "" {
		return o.key
	}
	return o.provider + ":" + o.ext
}

func submit(t testing.TB, base string, o operation) resp {
	t.Helper()
	if o.provider == "" {
		o.provider = "provider-a"
	}
	if o.round == "" {
		o.round = "round-1"
	}
	return httpRequest(t, base, "POST", "/wagering/transactions", token(t, o.provider), o.body(), map[string]string{"Idempotency-Key": o.idemKey()})
}

func submitAPI(t testing.TB, o operation) resp { return submit(t, apiURL, o) }

func waitUntil(t testing.TB, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func queryInt64(t testing.TB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func queryString(t testing.TB, q string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s
}

func ledgerCount(t testing.TB, walletID string) int64 {
	return queryInt64(t, `SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID)
}

func storedBalance(t testing.TB, walletID string) int64 {
	return queryInt64(t, `SELECT balance FROM wallets WHERE id = $1`, walletID)
}

func ledgerNet(t testing.TB, walletID string) int64 {
	return queryInt64(t, `SELECT COALESCE(SUM(CASE direction WHEN 'CREDIT' THEN amount ELSE -amount END),0) FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID)
}

func assertReconciled(t testing.TB, walletID string) {
	t.Helper()
	if storedBalance(t, walletID) != ledgerNet(t, walletID) {
		t.Errorf("wallet %s: stored %d != ledger %d", walletID, storedBalance(t, walletID), ledgerNet(t, walletID))
	}
	r := apiRequest(t, "POST", "/wallets/"+walletID+"/reconciliation", token(t, "wallet-admin"), nil, nil)
	if r.Status != 200 || !r.bool("consistent") {
		t.Errorf("reconciliation: %d %s", r.Status, r.Raw)
	}
}

func uid() string { return uuid.NewString()[:8] }

func operationMessage(o operation, messageID string) string {
	b := map[string]any{
		"messageId":  messageID,
		"type":       "WagerTransactionRequested",
		"occurredAt": time.Now().UTC().Format(time.RFC3339Nano),
		"data": map[string]any{
			"providerId": o.provider, "externalTransactionId": o.ext, "idempotencyKey": o.idemKey(),
			"playerId": o.playerID, "walletId": o.walletID, "roundId": o.round, "gameId": "fortune-chimp",
			"kind": o.kind, "money": map[string]string{"amount": o.amount, "currency": "BRL"},
		},
	}
	if o.ref != "" {
		b["data"].(map[string]any)["referenceExternalTransactionId"] = o.ref
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
