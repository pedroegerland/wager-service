package event

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func brl(u int64) money.Money { return money.MustFromUnits(u, "BRL") }

func bet(t *testing.T) *wager.Transaction {
	t.Helper()
	tx, err := wager.NewExternal(wager.KindBet, uuid.New(), uuid.New(), brl(2500), wager.External{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "k", PayloadHash: "h", RoundID: "r", GameID: "g",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestProcessedEnvelope(t *testing.T) {
	tx := bet(t)
	_ = tx.MarkProcessed(brl(7500), now.Add(time.Second))
	ev := NewWagerTransactionProcessed(tx, "corr-1")
	if ev.EventType != TypeWagerTransactionProcessed || ev.Version != 1 || ev.EventID == uuid.Nil {
		t.Errorf("envelope %+v", ev)
	}
	if ev.AggregateID != tx.WalletID() || ev.CorrelationID != "corr-1" || ev.CausationID != tx.ID().String() {
		t.Errorf("ids %+v", ev)
	}
	if !ev.OccurredAt.Equal(now.Add(time.Second)) || ev.OccurredAt.Location() != time.UTC {
		t.Errorf("occurredAt %s", ev.OccurredAt)
	}
	d := ev.Data.(WagerTransactionProcessed)
	if d.BalanceAfter.Amount != "75.00" || d.Money.Amount != "25.00" || d.ProviderID != "provider-a" {
		t.Errorf("data %+v", d)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(b, &back)
	if back["occurredAt"] != "2026-09-08T12:00:01Z" {
		t.Errorf("occurredAt json %v", back["occurredAt"])
	}
	if back["data"].(map[string]any)["money"].(map[string]any)["amount"] != "25.00" {
		t.Error("money must serialize as decimal string")
	}
}

func TestRejectedAndPendingEnvelopes(t *testing.T) {
	tx := bet(t)
	_ = tx.Reject(wager.CodeInsufficientFunds, brl(10), now)
	rej := NewWagerTransactionRejected(tx, "c")
	if rej.EventType != TypeWagerTransactionRejected || rej.Data.(WagerTransactionRejected).FailureCode != wager.CodeInsufficientFunds {
		t.Errorf("%+v", rej)
	}

	ref, _ := wager.NewExternal(wager.KindRefund, uuid.New(), uuid.New(), brl(1), wager.External{
		ProviderID: "p", ExternalTransactionID: "x", IdempotencyKey: "k", PayloadHash: "h", RoundID: "r", GameID: "g", ReferenceExternalTxID: "bet-1",
	}, now)
	_ = ref.MarkPendingReference(now.Add(time.Minute), now)
	pend := NewWagerTransactionPendingReference(ref, "c")
	d := pend.Data.(WagerTransactionPendingReference)
	if pend.EventType != TypeWagerTransactionPendingReference || d.Attempt != 1 || d.ReferenceExternalTransactionID != "bet-1" || d.NextAttemptAt == nil {
		t.Errorf("%+v", d)
	}
}

func TestBalanceChangedEnvelope(t *testing.T) {
	w, _ := wallet.Open(uuid.New(), brl(1000), now)
	entry, _ := w.Debit(brl(300), uuid.New(), now)
	ev := NewWalletBalanceChanged(entry, w.Version(), "c")
	d := ev.Data.(WalletBalanceChanged)
	if ev.EventType != TypeWalletBalanceChanged || ev.AggregateID != w.ID() {
		t.Errorf("%+v", ev)
	}
	if d.Direction != wallet.Debit || d.BalanceBefore.Amount != "10.00" || d.BalanceAfter.Amount != "7.00" || d.WalletVersion != 2 {
		t.Errorf("%+v", d)
	}
}
