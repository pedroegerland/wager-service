//go:build integration

package integration

import (
	"testing"
	"time"
)

func TestReversalBeforeReference(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	betExt := "bet-" + uid()
	refund := operation{ext: "ref-" + uid(), kind: "REFUND", amount: "30.00", ref: betExt, walletID: walletID, playerID: playerID}

	r := submitAPI(t, refund)
	if r.Status != 202 || r.str("status") != "PENDING_REFERENCE" || r.Body["balance"] != nil {
		t.Fatalf("early refund: %d %s", r.Status, r.Raw)
	}
	txID := r.str("transactionId")

	if r := submitAPI(t, refund); r.Status != 202 || !r.bool("idempotentReplay") {
		t.Errorf("pending replay: %d %s", r.Status, r.Raw)
	}
	if n := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'WagerTransactionPendingReference' AND payload->'data'->>'transactionId' = $1`, txID); n != 1 {
		t.Errorf("pending event count %d", n)
	}

	rr := apiRequest(t, "GET", "/wagering/transactions/"+txID, token(t, "provider-a"), nil, nil)
	if rr.str("status") != "PENDING_REFERENCE" || rr.Body["nextAttemptAt"] == nil {
		t.Errorf("pending lookup: %s", rr.Raw)
	}

	if r := submitAPI(t, operation{ext: betExt, kind: "BET", amount: "30.00", walletID: walletID, playerID: playerID}); r.Status != 200 {
		t.Fatalf("bet: %s", r.Raw)
	}
	waitUntil(t, 20*time.Second, "refund to be resolved", func() bool {
		return queryString(t, `SELECT status::text FROM wager_transactions WHERE id = $1`, txID) == "PROCESSED"
	})
	if storedBalance(t, walletID) != 10000 || ledgerCount(t, walletID) != 3 {
		t.Errorf("after resolution: balance %d ledger %d", storedBalance(t, walletID), ledgerCount(t, walletID))
	}
	if queryInt64(t, `SELECT COUNT(*) FROM wager_transactions WHERE id = $1 AND reference_transaction_id IS NOT NULL`, txID) != 1 {
		t.Error("resolved reference not recorded")
	}
	r = submitAPI(t, refund)
	if r.Status != 200 || !r.bool("idempotentReplay") || r.money("balance") != "100.00" {
		t.Errorf("replay after resolution: %d %s", r.Status, r.Raw)
	}
	assertReconciled(t, walletID)
}

func TestReversalReferenceExpires(t *testing.T) {
	if !inProcess {
		t.Skip("depends on the short retry budget of the in-process config")
	}
	walletID, playerID := openWallet(t, "100.00")
	rb := operation{ext: "rb-" + uid(), kind: "ROLLBACK", amount: "30.00", ref: "never-" + uid(), walletID: walletID, playerID: playerID}
	r := submitAPI(t, rb)
	if r.Status != 202 {
		t.Fatalf("rollback: %d %s", r.Status, r.Raw)
	}
	txID := r.str("transactionId")
	waitUntil(t, 30*time.Second, "rollback to expire", func() bool {
		return queryString(t, `SELECT status::text FROM wager_transactions WHERE id = $1`, txID) == "REJECTED"
	})
	if code := queryString(t, `SELECT failure_code FROM wager_transactions WHERE id = $1`, txID); code != "REFERENCE_NOT_FOUND" {
		t.Errorf("failure code %s", code)
	}
	if n := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'WagerTransactionRejected' AND payload->'data'->>'transactionId' = $1`, txID); n != 1 {
		t.Errorf("rejected event count %d", n)
	}
	r = submitAPI(t, rb)
	if r.Status != 422 || r.str("failureCode") != "REFERENCE_NOT_FOUND" || !r.bool("idempotentReplay") {
		t.Errorf("replay of expired: %d %s", r.Status, r.Raw)
	}
	if storedBalance(t, walletID) != 10000 {
		t.Error("expired reversal moved money")
	}
}
