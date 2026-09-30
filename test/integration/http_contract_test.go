//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/config"
)

func TestWalletOpening(t *testing.T) {
	admin := token(t, "wallet-admin")
	playerID := uuid.NewString()

	r := apiRequest(t, "POST", "/wallets", admin, map[string]any{
		"playerId": playerID, "initialBalance": map[string]string{"amount": "1000.00", "currency": "BRL"},
	}, nil)
	if r.Status != 201 || r.money("balance") != "1000.00" || r.Body["version"].(float64) != 1 {
		t.Fatalf("open: %d %s", r.Status, r.Raw)
	}
	walletID := r.str("id")

	if n := queryInt64(t, `SELECT COUNT(*) FROM wager_transactions WHERE wallet_id = $1 AND kind = 'OPENING' AND status = 'PROCESSED' AND origin = 'INTERNAL'`, walletID); n != 1 {
		t.Errorf("opening tx count %d", n)
	}
	if ledgerCount(t, walletID) != 1 {
		t.Errorf("ledger count %d", ledgerCount(t, walletID))
	}
	if n := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type IN ('WagerTransactionProcessed','WalletBalanceChanged')`, walletID); n != 2 {
		t.Errorf("outbox events %d", n)
	}

	r = apiRequest(t, "POST", "/wallets", admin, map[string]any{
		"playerId": playerID, "initialBalance": map[string]string{"amount": "5.00", "currency": "BRL"},
	}, nil)
	if r.Status != 409 || r.str("code") != "WALLET_ALREADY_EXISTS" {
		t.Errorf("duplicate: %d %s", r.Status, r.Raw)
	}

	r = apiRequest(t, "POST", "/wallets", admin, map[string]any{
		"playerId": uuid.NewString(), "initialBalance": map[string]string{"amount": "0.00", "currency": "BRL"},
	}, nil)
	if r.Status != 201 {
		t.Fatalf("zero open: %d %s", r.Status, r.Raw)
	}
	zeroID := r.str("id")
	if ledgerCount(t, zeroID) != 0 || queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1`, zeroID) != 0 {
		t.Error("zero opening must not create ledger or events")
	}

	for _, amt := range []string{"1", "1.0", "1.000", "-1.00", "1e2", "NaN", ""} {
		r = apiRequest(t, "POST", "/wallets", admin, map[string]any{
			"playerId": uuid.NewString(), "initialBalance": map[string]string{"amount": amt, "currency": "BRL"},
		}, nil)
		if r.Status != 400 {
			t.Errorf("amount %q: %d %s", amt, r.Status, r.Raw)
		}
	}

	r = apiRequest(t, "GET", "/wallets/"+walletID, admin, nil, nil)
	if r.Status != 200 || r.money("balance") != "1000.00" {
		t.Errorf("get: %d %s", r.Status, r.Raw)
	}
	r = apiRequest(t, "GET", "/wallets/"+uuid.NewString(), admin, nil, nil)
	if r.Status != 404 {
		t.Errorf("get missing: %d", r.Status)
	}
}

func TestSubmitContract(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	bet := operation{ext: "bet-" + uid(), kind: "BET", amount: "25.00", walletID: walletID, playerID: playerID}

	r := submitAPI(t, bet)
	if r.Status != 200 || r.str("status") != "PROCESSED" || r.money("balance") != "75.00" || r.bool("idempotentReplay") {
		t.Fatalf("bet: %d %s", r.Status, r.Raw)
	}
	txID := r.str("transactionId")

	win := operation{ext: "win-" + uid(), kind: "WIN", amount: "10.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, win); r.Status != 200 || r.money("balance") != "85.00" {
		t.Fatalf("win: %d %s", r.Status, r.Raw)
	}
	r = submitAPI(t, bet)
	if r.Status != 200 || !r.bool("idempotentReplay") || r.money("balance") != "75.00" || r.str("transactionId") != txID {
		t.Errorf("replay: %d %s", r.Status, r.Raw)
	}

	changed := bet
	changed.amount = "26.00"
	if r := submitAPI(t, changed); r.Status != 409 || r.str("code") != "IDEMPOTENCY_KEY_CONFLICT" {
		t.Errorf("conflict: %d %s", r.Status, r.Raw)
	}

	rekeyed := bet
	rekeyed.key = "another-key-" + uid()
	if r := submitAPI(t, rekeyed); r.Status != 409 || r.str("code") != "EXTERNAL_TRANSACTION_ID_REUSED" {
		t.Errorf("ext reuse: %d %s", r.Status, r.Raw)
	}

	r = httpRequest(t, apiURL, "POST", "/wagering/transactions", token(t, "provider-a"), bet.body(), nil)
	if r.Status != 400 || r.str("field") != "Idempotency-Key" {
		t.Errorf("no key: %d %s", r.Status, r.Raw)
	}

	big := operation{ext: "bet-" + uid(), kind: "BET", amount: "500.00", walletID: walletID, playerID: playerID}
	r = submitAPI(t, big)
	if r.Status != 422 || r.str("failureCode") != "INSUFFICIENT_FUNDS" || r.money("balance") != "85.00" {
		t.Errorf("insufficient: %d %s", r.Status, r.Raw)
	}
	r = submitAPI(t, big)
	if r.Status != 422 || !r.bool("idempotentReplay") {
		t.Errorf("insufficient replay: %d %s", r.Status, r.Raw)
	}

	loss := operation{ext: "loss-" + uid(), kind: "LOSS", amount: "0.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, loss); r.Status != 200 || r.money("balance") != "85.00" {
		t.Errorf("loss: %d %s", r.Status, r.Raw)
	}
	loss.ext, loss.amount = "loss-"+uid(), "1.00"
	if r := submitAPI(t, loss); r.Status != 400 {
		t.Errorf("loss with amount: %d %s", r.Status, r.Raw)
	}

	opening := operation{ext: "o-" + uid(), kind: "OPENING", amount: "1.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, opening); r.Status != 400 {
		t.Errorf("opening: %d %s", r.Status, r.Raw)
	}

	ghost := operation{ext: "g-" + uid(), kind: "BET", amount: "1.00", walletID: uuid.NewString(), playerID: playerID}
	if r := submitAPI(t, ghost); r.Status != 404 {
		t.Errorf("ghost wallet: %d %s", r.Status, r.Raw)
	}

	r = httpRequest(t, apiURL, "POST", "/wagering/transactions", token(t, "provider-a"),
		map[string]any{"foo": "bar"}, map[string]string{"Idempotency-Key": "k-" + uid()})
	if r.Status != 400 {
		t.Errorf("unknown field: %d %s", r.Status, r.Raw)
	}

	r = apiRequest(t, "GET", "/wallets/"+walletID, token(t, "wallet-admin"), nil, nil)
	if r.Body["version"].(float64) != 3 {
		t.Errorf("version %v", r.Body["version"])
	}
	assertReconciled(t, walletID)
}

func TestReversals(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	bet := operation{ext: "bet-" + uid(), kind: "BET", amount: "30.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, bet); r.Status != 200 {
		t.Fatalf("bet: %s", r.Raw)
	}
	refund := operation{ext: "ref-" + uid(), kind: "REFUND", amount: "30.00", ref: bet.ext, walletID: walletID, playerID: playerID}
	if r := submitAPI(t, refund); r.Status != 200 || r.money("balance") != "100.00" {
		t.Fatalf("refund: %d %s", r.Status, r.Raw)
	}
	rollback := operation{ext: "rb-" + uid(), kind: "ROLLBACK", amount: "30.00", ref: bet.ext, walletID: walletID, playerID: playerID}
	if r := submitAPI(t, rollback); r.Status != 422 || r.str("failureCode") != "REFERENCE_ALREADY_REVERSED" {
		t.Errorf("second reversal: %d %s", r.Status, r.Raw)
	}

	rb2 := operation{ext: "rb-" + uid(), kind: "ROLLBACK", amount: "30.00", ref: refund.ext, walletID: walletID, playerID: playerID}
	if r := submitAPI(t, rb2); r.Status != 200 || r.money("balance") != "70.00" {
		t.Errorf("rollback refund: %d %s", r.Status, r.Raw)
	}

	win := operation{ext: "win-" + uid(), kind: "WIN", amount: "100.00", walletID: walletID, playerID: playerID}
	submitAPI(t, win)
	drain := operation{ext: "bet-" + uid(), kind: "BET", amount: "170.00", walletID: walletID, playerID: playerID}
	if r := submitAPI(t, drain); r.Status != 200 {
		t.Fatalf("drain: %s", r.Raw)
	}
	rbWin := operation{ext: "rb-" + uid(), kind: "ROLLBACK", amount: "100.00", ref: win.ext, walletID: walletID, playerID: playerID}
	if r := submitAPI(t, rbWin); r.Status != 422 || r.str("failureCode") != "REVERSAL_INSUFFICIENT_FUNDS" {
		t.Errorf("overdraw rollback: %d %s", r.Status, r.Raw)
	}

	partial := operation{ext: "ref-" + uid(), kind: "REFUND", amount: "10.00", ref: drain.ext, walletID: walletID, playerID: playerID}
	if r := submitAPI(t, partial); r.Status != 422 || r.str("failureCode") != "REFERENCE_MISMATCH" {
		t.Errorf("partial: %d %s", r.Status, r.Raw)
	}

	if n := queryInt64(t, `SELECT COUNT(*) FROM wager_transactions WHERE reference_external_transaction_id = $1 AND status = 'PROCESSED'`, bet.ext); n != 1 {
		t.Errorf("processed reversals of bet: %d", n)
	}
	assertReconciled(t, walletID)
}

func TestLedgerPagingAndTransactionReads(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	var exts []string
	for i := 0; i < 5; i++ {
		o := operation{ext: "bet-" + uid(), kind: "BET", amount: "1.00", walletID: walletID, playerID: playerID}
		if r := submitAPI(t, o); r.Status != 200 {
			t.Fatalf("bet %d: %s", i, r.Raw)
		}
		exts = append(exts, o.ext)
	}
	admin := token(t, "wallet-admin")
	var seen []string
	cursor := ""
	for page := 0; page < 10; page++ {
		r := apiRequest(t, "GET", "/wallets/"+walletID+"/ledger?limit=2&cursor="+cursor, admin, nil, nil)
		if r.Status != 200 {
			t.Fatalf("ledger: %d %s", r.Status, r.Raw)
		}
		for _, e := range r.Body["entries"].([]any) {
			seen = append(seen, e.(map[string]any)["transactionId"].(string))
		}
		cursor = r.str("nextCursor")
		if cursor == "" {
			break
		}
	}
	if len(seen) != 6 {
		t.Errorf("paged %d entries: %v", len(seen), seen)
	}

	r := apiRequest(t, "GET", "/wallets/"+walletID+"/ledger?limit=1", admin, nil, nil)
	first := r.Body["entries"].([]any)[0].(map[string]any)
	if first["direction"] != "CREDIT" || first["balanceBefore"].(map[string]any)["amount"] != "0.00" {
		t.Errorf("first entry: %v", first)
	}
	if r := apiRequest(t, "GET", "/wallets/"+walletID+"/ledger?cursor=abc*def", admin, nil, nil); r.Status != 400 {
		t.Errorf("bad cursor: %d", r.Status)
	}

	r = apiRequest(t, "GET", "/providers/provider-a/wagering/transactions/"+exts[0], token(t, "provider-a"), nil, nil)
	if r.Status != 200 || r.str("status") != "PROCESSED" || r.str("kind") != "BET" {
		t.Errorf("provider read: %d %s", r.Status, r.Raw)
	}
	txID := r.str("id")
	r = apiRequest(t, "GET", "/wagering/transactions/"+txID, token(t, "provider-a"), nil, nil)
	if r.Status != 200 || r.str("externalTransactionId") != exts[0] {
		t.Errorf("tx read: %d %s", r.Status, r.Raw)
	}
	r = apiRequest(t, "GET", "/wagering/transactions/"+uuid.NewString(), token(t, "provider-a"), nil, nil)
	if r.Status != 404 {
		t.Errorf("missing tx: %d", r.Status)
	}
}

func TestAuthAndIsolation(t *testing.T) {
	walletID, playerID := openWallet(t, "50.00")
	bet := operation{ext: "bet-" + uid(), kind: "BET", amount: "5.00", walletID: walletID, playerID: playerID}
	r := submitAPI(t, bet)
	if r.Status != 200 {
		t.Fatalf("bet: %s", r.Raw)
	}
	txID := r.str("transactionId")

	for name, tok := range map[string]string{"none": "", "garbage": "not-a-jwt", "tampered": token(t, "provider-a")[:len(token(t, "provider-a"))-3] + "abc"} {
		r := httpRequest(t, apiURL, "POST", "/wagering/transactions", tok, bet.body(), map[string]string{"Idempotency-Key": "x-" + uid()})
		if r.Status != 401 {
			t.Errorf("%s token: %d %s", name, r.Status, r.Raw)
		}
	}

	other := bet
	other.ext, other.provider = "bet-"+uid(), "provider-b"
	other.key = "provider-b:" + other.ext
	body := other.body()
	body["providerId"] = "provider-a"
	r = httpRequest(t, apiURL, "POST", "/wagering/transactions", token(t, "provider-b"), body, map[string]string{"Idempotency-Key": other.key})
	if r.Status != 403 {
		t.Errorf("impersonation: %d %s", r.Status, r.Raw)
	}
	if r := apiRequest(t, "GET", "/providers/provider-a/wagering/transactions/"+bet.ext, token(t, "provider-b"), nil, nil); r.Status != 403 {
		t.Errorf("cross provider read: %d %s", r.Status, r.Raw)
	}
	if r := apiRequest(t, "GET", "/wagering/transactions/"+txID, token(t, "provider-b"), nil, nil); r.Status != 404 {
		t.Errorf("cross provider tx read: %d %s", r.Status, r.Raw)
	}

	same := bet
	same.provider = "provider-b"
	same.key = "provider-b:" + bet.ext
	r = submitAPI(t, same)
	if r.Status != 200 || r.bool("idempotentReplay") {
		t.Errorf("provider namespace: %d %s", r.Status, r.Raw)
	}

	for _, path := range []string{"/wallets/" + walletID, "/wallets/" + walletID + "/ledger"} {
		if r := apiRequest(t, "GET", path, token(t, "provider-a"), nil, nil); r.Status != 403 {
			t.Errorf("provider on %s: %d", path, r.Status)
		}
	}
	if r := apiRequest(t, "POST", "/wallets/"+walletID+"/reconciliation", token(t, "provider-a"), nil, nil); r.Status != 403 {
		t.Errorf("provider reconciliation: %d", r.Status)
	}

	if r := apiRequest(t, "GET", "/wallets/"+walletID, token(t, "outsider"), nil, nil); r.Status != 403 {
		t.Errorf("outsider wallet: %d", r.Status)
	}
	r = httpRequest(t, apiURL, "POST", "/wagering/transactions", token(t, "outsider"), bet.body(), map[string]string{"Idempotency-Key": "o-" + uid()})
	if r.Status != 403 {
		t.Errorf("outsider submit: %d %s", r.Status, r.Raw)
	}

	if r := apiRequest(t, "GET", "/wagering/transactions/"+txID, token(t, "wallet-admin"), nil, nil); r.Status != 200 {
		t.Errorf("internal tx read: %d", r.Status)
	}

	if storedBalance(t, walletID) != 4000 {
		t.Errorf("balance changed by unauthorized calls: %d", storedBalance(t, walletID))
	}

	if r := apiRequest(t, "GET", "/health/live", "", nil, nil); r.Status != 200 {
		t.Errorf("live: %d", r.Status)
	}
	if r := apiRequest(t, "GET", "/health/ready", "", nil, nil); r.Status != 200 {
		t.Errorf("ready: %d %s", r.Status, r.Raw)
	}
}

func TestRateLimit(t *testing.T) {
	if !inProcess {
		t.Skip("rate limit is per instance; only deterministic in-process")
	}
	port := freePort()
	app := newApp(port, func(c *config.Config) {
		c.RateLimitRPS, c.RateLimitBurst = 1, 3
		c.ConsumerEnabled, c.OutboxEnabled = false, false
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	must(t, app.Start(ctx))
	defer app.Stop(ctx)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	got429 := false
	for i := 0; i < 6; i++ {
		r := httpRequest(t, base, "GET", "/wagering/transactions/"+uuid.NewString(), token(t, "provider-a"), nil, nil)
		if r.Status == 429 {
			got429 = true
			if r.Header.Get("Retry-After") == "" {
				t.Error("429 without Retry-After")
			}
			break
		}
	}
	if !got429 {
		t.Error("expected a 429 after the burst")
	}

	if r := httpRequest(t, base, "GET", "/wagering/transactions/"+uuid.NewString(), token(t, "provider-b"), nil, nil); r.Status == 429 {
		t.Error("provider-b should not be throttled by provider-a")
	}
}
