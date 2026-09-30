package wagering

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/port/porttest"
	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

var t0 = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func brl(s string) money.Money {
	m, err := money.Parse(s, "BRL")
	if err != nil {
		panic(err)
	}
	return m
}

type testEnv struct {
	store  *porttest.InMemoryStore
	proc   *Processor
	wallet *wallet.Wallet
}

func newTestEnv(t *testing.T, balance string) *testEnv {
	t.Helper()
	store := porttest.NewInMemoryStore()
	w, err := wallet.Open(uuid.New(), brl(balance), t0)
	if err != nil {
		t.Fatal(err)
	}
	store.SeedWallet(w)
	proc := NewProcessor(store, &porttest.SteppingClock{Time: t0}, Config{MaxReferenceAttempts: 2, ReferenceBaseBackoff: time.Second}, slog.Default(), nil)
	return &testEnv{store: store, proc: proc, wallet: w}
}

func (e *testEnv) op(kind wager.Kind, ext, amount, ref string) Operation {
	return Operation{
		ProviderID: "provider-a", ExternalTransactionID: ext, IdempotencyKey: "provider-a:" + ext,
		PlayerID: e.wallet.PlayerID(), WalletID: e.wallet.ID(), RoundID: "round-1", GameID: "game",
		Kind: kind, Money: brl(amount), ReferenceExternalTransactionID: ref, CorrelationID: "corr",
	}
}

func (e *testEnv) balance(t *testing.T) string {
	t.Helper()
	w, _ := e.store.Repos().Wallets.Get(context.Background(), e.wallet.ID())
	return w.Balance().String()
}

func TestBetDebitsAndReplays(t *testing.T) {
	e := newTestEnv(t, "100.00")
	res, err := e.proc.Process(context.Background(), e.op(wager.KindBet, "bet-1", "25.00", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != wager.StatusProcessed || res.Balance.String() != "75.00" || res.IdempotentReplay {
		t.Fatalf("unexpected result %+v", res)
	}
	if e.balance(t) != "75.00" {
		t.Errorf("balance %s", e.balance(t))
	}
	if got := e.store.EventTypes(); len(got) != 2 || got[0] != event.TypeWagerTransactionProcessed || got[1] != event.TypeWalletBalanceChanged {
		t.Errorf("events %v", got)
	}

	if _, err := e.proc.Process(context.Background(), e.op(wager.KindWin, "win-1", "10.00", "")); err != nil {
		t.Fatal(err)
	}
	again, err := e.proc.Process(context.Background(), e.op(wager.KindBet, "bet-1", "25.00", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !again.IdempotentReplay || again.TransactionID != res.TransactionID || again.Balance.String() != "75.00" {
		t.Errorf("replay should return the original result, got %+v", again)
	}
	if e.balance(t) != "85.00" {
		t.Errorf("replay must not move money: %s", e.balance(t))
	}
	if len(e.store.LedgerEntries()) != 2 {
		t.Errorf("ledger entries %d", len(e.store.LedgerEntries()))
	}
}

func TestIdempotencyKeyConflict(t *testing.T) {
	e := newTestEnv(t, "100.00")
	if _, err := e.proc.Process(context.Background(), e.op(wager.KindBet, "bet-1", "25.00", "")); err != nil {
		t.Fatal(err)
	}
	c := e.op(wager.KindBet, "bet-1", "30.00", "")
	_, err := e.proc.Process(context.Background(), c)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	c = e.op(wager.KindBet, "bet-1", "25.00", "")
	c.IdempotencyKey = "something-else"
	_, err = e.proc.Process(context.Background(), c)
	if !errors.Is(err, ErrExternalIDReused) {
		t.Fatalf("expected external id reuse, got %v", err)
	}
	if e.balance(t) != "75.00" {
		t.Errorf("conflicts must not move money: %s", e.balance(t))
	}
}

func TestHashIgnoresKeyAndTransport(t *testing.T) {
	e := newTestEnv(t, "1.00")
	a := e.op(wager.KindBet, "x", "1.00", "")
	b := a
	b.IdempotencyKey = "other"
	b.CorrelationID = "other"
	b.Inbox = &port.InboxMessage{MessageID: "m"}
	if a.Hash() != b.Hash() {
		t.Error("hash must only cover business fields")
	}
	b.Money = brl("1.01")
	if a.Hash() == b.Hash() {
		t.Error("hash must change with the amount")
	}
	c := a
	c.ReferenceExternalTransactionID = "ref"
	if a.Hash() == c.Hash() {
		t.Error("hash must include the reference when present")
	}
}

func TestInsufficientFunds(t *testing.T) {
	e := newTestEnv(t, "10.00")
	res, err := e.proc.Process(context.Background(), e.op(wager.KindBet, "bet-1", "10.01", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != wager.StatusRejected || res.FailureCode != wager.CodeInsufficientFunds {
		t.Fatalf("got %+v", res)
	}
	if res.Balance.String() != "10.00" || e.balance(t) != "10.00" {
		t.Error("rejection must not move money")
	}
	if len(e.store.LedgerEntries()) != 0 {
		t.Error("rejection must not write to the ledger")
	}
	if got := e.store.EventTypes(); len(got) != 1 || got[0] != event.TypeWagerTransactionRejected {
		t.Errorf("events %v", got)
	}

	again, _ := e.proc.Process(context.Background(), e.op(wager.KindBet, "bet-1", "10.01", ""))
	if !again.IdempotentReplay || again.Status != wager.StatusRejected {
		t.Errorf("replayed rejection %+v", again)
	}
}

func TestLossNoMovement(t *testing.T) {
	e := newTestEnv(t, "10.00")
	res, err := e.proc.Process(context.Background(), e.op(wager.KindLoss, "loss-1", "0.00", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != wager.StatusProcessed || res.Balance.String() != "10.00" {
		t.Fatalf("got %+v", res)
	}
	w, _ := e.store.Repos().Wallets.Get(context.Background(), e.wallet.ID())
	if w.Version() != 1 || len(e.store.LedgerEntries()) != 0 {
		t.Error("LOSS must not bump version or touch the ledger")
	}
	if got := e.store.EventTypes(); len(got) != 1 || got[0] != event.TypeWagerTransactionProcessed {
		t.Errorf("events %v", got)
	}
	_, err = e.proc.Process(context.Background(), e.op(wager.KindLoss, "loss-2", "1.00", ""))
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("LOSS with amount should be a validation error, got %v", err)
	}
}

func TestZeroAmountsRejectedForMovingKinds(t *testing.T) {
	e := newTestEnv(t, "10.00")
	for _, k := range []wager.Kind{wager.KindBet, wager.KindWin, wager.KindRefund, wager.KindRollback} {
		_, err := e.proc.Process(context.Background(), e.op(k, "z-"+string(k), "0.00", "ref"))
		var ve *domain.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s with 0.00: %v", k, err)
		}
	}
}

func TestOpeningKindRejected(t *testing.T) {
	e := newTestEnv(t, "10.00")
	_, err := e.proc.Process(context.Background(), e.op(wager.KindOpening, "o", "1.00", ""))
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}
}

func TestRefundAndRollbackRules(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()
	must := func(c Operation) Result {
		t.Helper()
		r, err := e.proc.Process(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	must(e.op(wager.KindBet, "bet-1", "30.00", ""))

	r := must(e.op(wager.KindRefund, "ref-bad", "20.00", "bet-1"))
	if r.FailureCode != wager.CodeReferenceMismatch {
		t.Errorf("partial refund: %+v", r)
	}

	r = must(e.op(wager.KindRefund, "ref-1", "30.00", "bet-1"))
	if r.Status != wager.StatusProcessed || e.balance(t) != "100.00" {
		t.Fatalf("refund: %+v %s", r, e.balance(t))
	}

	r = must(e.op(wager.KindRollback, "rb-1", "30.00", "bet-1"))
	if r.FailureCode != wager.CodeReferenceAlreadyReversed {
		t.Errorf("double reversal: %+v", r)
	}
	r = must(e.op(wager.KindRefund, "ref-2", "30.00", "bet-1"))
	if r.FailureCode != wager.CodeReferenceAlreadyReversed {
		t.Errorf("double refund: %+v", r)
	}

	r = must(e.op(wager.KindRollback, "rb-2", "30.00", "ref-1"))
	if r.Status != wager.StatusProcessed || e.balance(t) != "70.00" {
		t.Errorf("rollback of refund: %+v %s", r, e.balance(t))
	}

	r = must(e.op(wager.KindRefund, "ref-3", "30.00", "ref-1"))
	if r.FailureCode != wager.CodeReferenceKindNotAllowed {
		t.Errorf("refund of refund: %+v", r)
	}

	must(e.op(wager.KindWin, "win-1", "50.00", ""))
	must(e.op(wager.KindBet, "bet-2", "100.00", ""))
	r = must(e.op(wager.KindRollback, "rb-3", "50.00", "win-1"))
	if r.FailureCode != wager.CodeReversalInsufficientFunds {
		t.Errorf("rollback without balance: %+v", r)
	}

	r = must(e.op(wager.KindRollback, "rb-4", "30.00", "ref-bad"))
	if r.FailureCode != wager.CodeReferenceNotProcessed {
		t.Errorf("rollback of rejected: %+v", r)
	}
}

func TestReferenceRoundMismatch(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()
	if _, err := e.proc.Process(ctx, e.op(wager.KindBet, "bet-1", "30.00", "")); err != nil {
		t.Fatal(err)
	}
	c := e.op(wager.KindRefund, "ref-1", "30.00", "bet-1")
	c.RoundID = "round-2"
	r, err := e.proc.Process(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.FailureCode != wager.CodeReferenceMismatch {
		t.Errorf("got %+v", r)
	}
}

func TestPendingReferenceThenResolved(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()
	r, err := e.proc.Process(ctx, e.op(wager.KindRefund, "ref-1", "30.00", "bet-1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != wager.StatusPendingReference || r.Balance != nil {
		t.Fatalf("got %+v", r)
	}
	if got := e.store.EventTypes(); len(got) != 1 || got[0] != event.TypeWagerTransactionPendingReference {
		t.Errorf("events %v", got)
	}

	again, _ := e.proc.Process(ctx, e.op(wager.KindRefund, "ref-1", "30.00", "bet-1"))
	if !again.IdempotentReplay || again.Status != wager.StatusPendingReference {
		t.Errorf("pending replay %+v", again)
	}

	e.proc.clock = &porttest.SteppingClock{Time: t0}
	if did, _ := e.proc.RetryOnePendingReference(ctx); did {
		t.Error("should not retry before nextAttemptAt")
	}

	if _, err := e.proc.Process(ctx, e.op(wager.KindBet, "bet-1", "30.00", "")); err != nil {
		t.Fatal(err)
	}
	e.proc.clock = &porttest.SteppingClock{Time: t0.Add(time.Minute)}
	did, err := e.proc.RetryOnePendingReference(ctx)
	if err != nil || !did {
		t.Fatalf("retry: %v %v", did, err)
	}
	tx, _ := e.store.Repos().Transactions.Get(ctx, r.TransactionID)
	if tx.Status() != wager.StatusProcessed || tx.ReferenceTxID() == nil {
		t.Errorf("after retry: %s", tx.Status())
	}
	if e.balance(t) != "100.00" {
		t.Errorf("balance %s", e.balance(t))
	}
}

func TestPendingReferenceExpires(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()
	r, _ := e.proc.Process(ctx, e.op(wager.KindRollback, "rb-1", "30.00", "ghost"))

	for i := 0; i < 3; i++ {
		e.proc.clock = &porttest.SteppingClock{Time: t0.Add(time.Duration(i+1) * time.Hour)}
		if _, err := e.proc.RetryOnePendingReference(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tx, _ := e.store.Repos().Transactions.Get(ctx, r.TransactionID)
	if tx.Status() != wager.StatusRejected || tx.FailureCode() != wager.CodeReferenceNotFound {
		t.Errorf("got %s %s attempts=%d", tx.Status(), tx.FailureCode(), tx.Attempts())
	}
	if did, _ := e.proc.RetryOnePendingReference(ctx); did {
		t.Error("terminal transaction must not be picked up again")
	}
}

func TestWalletChecks(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()

	c := e.op(wager.KindBet, "bet-1", "1.00", "")
	c.WalletID = uuid.New()
	if _, err := e.proc.Process(ctx, c); !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("unknown wallet: %v", err)
	}

	c = e.op(wager.KindBet, "bet-2", "1.00", "")
	c.PlayerID = uuid.New()
	r, err := e.proc.Process(ctx, c)
	if err != nil || r.FailureCode != wager.CodeWalletMismatch {
		t.Errorf("player mismatch: %+v %v", r, err)
	}

	c = e.op(wager.KindBet, "bet-3", "1.00", "")
	c.Money = money.MustFromUnits(100, "USD")
	r, err = e.proc.Process(ctx, c)
	if err != nil || r.FailureCode != wager.CodeCurrencyMismatch {
		t.Errorf("currency mismatch: %+v %v", r, err)
	}
}

func TestInboxDedup(t *testing.T) {
	e := newTestEnv(t, "100.00")
	ctx := context.Background()
	c := e.op(wager.KindBet, "bet-1", "10.00", "")
	c.Inbox = &port.InboxMessage{ConsumerName: "c", MessageID: "m-1", PayloadHash: "h"}
	if _, err := e.proc.Process(ctx, c); err != nil {
		t.Fatal(err)
	}
	r, err := e.proc.Process(ctx, c)
	if err != nil || !r.IdempotentReplay {
		t.Errorf("redelivery: %+v %v", r, err)
	}
	c.Inbox.PayloadHash = "other"
	if _, err := e.proc.Process(ctx, c); !errors.Is(err, ErrInboxHashMismatch) {
		t.Errorf("hash mismatch: %v", err)
	}

	c.Inbox = nil
	r, _ = e.proc.Process(ctx, c)
	if !r.IdempotentReplay || e.balance(t) != "90.00" {
		t.Errorf("http after sqs: %+v %s", r, e.balance(t))
	}
}
