package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
	"github.com/pedroegerland/wager-service/internal/app/port/porttest"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
)

type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, raw string) (auth.Principal, error) {
	switch raw {
	case "admin":
		return auth.Principal{Subject: "admin", ClientID: "wallet-admin", Roles: []string{auth.RoleInternal}}, nil
	case "provider-a", "provider-b":
		return auth.Principal{Subject: raw, ClientID: raw, ProviderID: raw, Roles: []string{auth.RoleProvider}}, nil
	case "nobody":
		return auth.Principal{Subject: "nobody"}, nil
	}
	return auth.Principal{}, auth.ErrInvalidToken
}

type alwaysReady struct{}

func (alwaysReady) Ready(context.Context) map[string]error { return map[string]error{"postgres": nil} }

func newTestRouter() http.Handler {
	store := porttest.NewInMemoryStore()
	clock := &porttest.SteppingClock{Time: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(Config{RateLimitRPS: 1000, RateLimitBurst: 1000}, Deps{
		Wallets:  wallets.NewService(store, clock, log, nil),
		Wagers:   wagering.NewProcessor(store, clock, wagering.Config{}, log, nil),
		Verifier: fakeVerifier{},
		Ready:    alwaysReady{},
		Log:      log,
	})
}

type response struct {
	code int
	body map[string]any
}

func (r response) str(k string) string { s, _ := r.body[k].(string); return s }

func do(t *testing.T, h http.Handler, method, path, tok string, body any, headers map[string]string) response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := response{code: rec.Code}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.body)
	return out
}

func openWallet(t *testing.T, h http.Handler, amount string) (walletID, playerID string) {
	t.Helper()
	playerID = uuid.NewString()
	r := do(t, h, "POST", "/wallets", "admin", map[string]any{
		"playerId": playerID, "initialBalance": map[string]string{"amount": amount, "currency": "BRL"},
	}, nil)
	if r.code != 201 {
		t.Fatalf("open wallet: %d %v", r.code, r.body)
	}
	return r.str("id"), playerID
}

func betBody(walletID, playerID, ext, amount string) map[string]any {
	return map[string]any{
		"providerId": "provider-a", "externalTransactionId": ext, "playerId": playerID, "walletId": walletID,
		"roundId": "r", "gameId": "g", "kind": "BET", "money": map[string]string{"amount": amount, "currency": "BRL"},
	}
}

func TestRouterAuthorization(t *testing.T) {
	h := newTestRouter()
	walletID, playerID := openWallet(t, h, "10.00")
	bet := betBody(walletID, playerID, "tx-1", "1.00")
	key := map[string]string{"Idempotency-Key": "k-1"}

	cases := []struct {
		name         string
		method, path string
		tok          string
		body         any
		want         int
	}{
		{"health is public", "GET", "/health/live", "", nil, 200},
		{"ready is public", "GET", "/health/ready", "", nil, 200},
		{"no token", "GET", "/wallets/" + walletID, "", nil, 401},
		{"bad token", "GET", "/wallets/" + walletID, "garbage", nil, 401},
		{"provider cannot read wallets", "GET", "/wallets/" + walletID, "provider-a", nil, 403},
		{"provider cannot reconcile", "POST", "/wallets/" + walletID + "/reconciliation", "provider-a", nil, 403},
		{"no role cannot read wallets", "GET", "/wallets/" + walletID, "nobody", nil, 403},
		{"no role cannot submit", "POST", "/wagering/transactions", "nobody", bet, 403},
		{"other provider cannot impersonate", "POST", "/wagering/transactions", "provider-b", bet, 403},
		{"other provider cannot read", "GET", "/providers/provider-a/wagering/transactions/tx-1", "provider-b", nil, 403},
		{"internal may act for a provider", "POST", "/wagering/transactions", "admin", bet, 200},
		{"owner may read", "GET", "/providers/provider-a/wagering/transactions/tx-1", "provider-a", nil, 200},
		{"admin may read wallet", "GET", "/wallets/" + walletID, "admin", nil, 200},
	}
	for _, c := range cases {
		r := do(t, h, c.method, c.path, c.tok, c.body, key)
		if r.code != c.want {
			t.Errorf("%s: got %d want %d (%v)", c.name, r.code, c.want, r.body)
		}
	}
}

func TestRouterSubmitStatusCodes(t *testing.T) {
	h := newTestRouter()
	walletID, playerID := openWallet(t, h, "10.00")
	submit := func(body map[string]any, key string) response {
		return do(t, h, "POST", "/wagering/transactions", "provider-a", body, map[string]string{"Idempotency-Key": key})
	}

	r := submit(betBody(walletID, playerID, "tx-1", "4.00"), "k1")
	if r.code != 200 || r.str("status") != "PROCESSED" || r.body["balance"].(map[string]any)["amount"] != "6.00" {
		t.Fatalf("processed: %d %v", r.code, r.body)
	}
	if r = submit(betBody(walletID, playerID, "tx-1", "4.00"), "k1"); r.code != 200 || r.body["idempotentReplay"] != true {
		t.Errorf("replay: %d %v", r.code, r.body)
	}
	if r = submit(betBody(walletID, playerID, "tx-1", "5.00"), "k1"); r.code != 409 || r.str("code") != "IDEMPOTENCY_KEY_CONFLICT" {
		t.Errorf("key conflict: %d %v", r.code, r.body)
	}
	if r = submit(betBody(walletID, playerID, "tx-1", "4.00"), "k2"); r.code != 409 || r.str("code") != "EXTERNAL_TRANSACTION_ID_REUSED" {
		t.Errorf("external id reuse: %d %v", r.code, r.body)
	}
	if r = submit(betBody(walletID, playerID, "tx-2", "50.00"), "k3"); r.code != 422 || r.str("failureCode") != "INSUFFICIENT_FUNDS" {
		t.Errorf("rejected: %d %v", r.code, r.body)
	}
	refund := betBody(walletID, playerID, "tx-3", "4.00")
	refund["kind"], refund["referenceExternalTransactionId"] = "REFUND", "missing"
	if r = submit(refund, "k4"); r.code != 202 || r.str("status") != "PENDING_REFERENCE" || r.body["balance"] != nil {
		t.Errorf("pending: %d %v", r.code, r.body)
	}
	if r = submit(betBody(uuid.NewString(), playerID, "tx-4", "1.00"), "k5"); r.code != 404 {
		t.Errorf("unknown wallet: %d %v", r.code, r.body)
	}
	if r = do(t, h, "POST", "/wagering/transactions", "provider-a", betBody(walletID, playerID, "tx-5", "1.00"), nil); r.code != 400 || r.str("field") != "Idempotency-Key" {
		t.Errorf("missing key: %d %v", r.code, r.body)
	}
	bad := betBody(walletID, playerID, "tx-6", "1.0")
	if r = submit(bad, "k6"); r.code != 400 || r.str("code") != "VALIDATION_ERROR" {
		t.Errorf("bad amount: %d %v", r.code, r.body)
	}
	bad = betBody(walletID, playerID, "tx-7", "1.00")
	bad["kind"] = "OPENING"
	if r = submit(bad, "k7"); r.code != 400 || r.str("field") != "kind" {
		t.Errorf("opening: %d %v", r.code, r.body)
	}
	bad = betBody(walletID, playerID, "tx-8", "1.00")
	bad["extra"] = true
	if r = submit(bad, "k8"); r.code != 400 {
		t.Errorf("unknown field: %d %v", r.code, r.body)
	}
	if r = do(t, h, "GET", "/wagering/transactions/not-a-uuid", "provider-a", nil, nil); r.code != 400 {
		t.Errorf("bad path uuid: %d", r.code)
	}
}

func TestRouterWalletEndpoints(t *testing.T) {
	h := newTestRouter()
	walletID, playerID := openWallet(t, h, "10.00")

	r := do(t, h, "POST", "/wallets", "admin", map[string]any{
		"playerId": playerID, "initialBalance": map[string]string{"amount": "1.00", "currency": "BRL"},
	}, nil)
	if r.code != 409 || r.str("code") != "WALLET_ALREADY_EXISTS" {
		t.Errorf("duplicate wallet: %d %v", r.code, r.body)
	}
	if r = do(t, h, "GET", "/wallets/"+uuid.NewString(), "admin", nil, nil); r.code != 404 {
		t.Errorf("missing wallet: %d", r.code)
	}
	if r = do(t, h, "POST", "/wallets/"+walletID+"/reconciliation", "admin", nil, nil); r.code != 200 || r.body["consistent"] != true {
		t.Errorf("reconciliation: %d %v", r.code, r.body)
	}
	if r = do(t, h, "GET", "/wallets/"+walletID+"/ledger", "admin", nil, nil); r.code != 200 {
		t.Errorf("ledger: %d %v", r.code, r.body)
	}
}

func TestWriteErrorResponseMapsUnavailable(t *testing.T) {
	h := &handlers{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	h.writeErrorResponse(rec, httptest.NewRequest("GET", "/", nil), errors.New("boom"))
	if rec.Code != 500 {
		t.Errorf("unknown error: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.writeErrorResponse(rec, httptest.NewRequest("GET", "/", nil), context.DeadlineExceeded)
	if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("timeout: %d", rec.Code)
	}
}
