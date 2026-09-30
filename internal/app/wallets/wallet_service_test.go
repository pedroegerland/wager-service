package wallets

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port/porttest"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

var t0 = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func brl(units int64) money.Money { return money.MustFromUnits(units, "BRL") }

func newService() (*Service, *porttest.InMemoryStore) {
	store := porttest.NewInMemoryStore()
	return NewService(store, &porttest.SteppingClock{Time: t0}, slog.Default(), nil), store
}

func TestOpenWithBalanceWritesOpeningAndEvents(t *testing.T) {
	svc, store := newService()
	w, err := svc.Open(context.Background(), uuid.New(), brl(100000), "corr")
	if err != nil {
		t.Fatal(err)
	}
	if w.Version() != 1 || w.Balance().Units() != 100000 {
		t.Errorf("wallet %s v%d", w.Balance(), w.Version())
	}
	entries := store.LedgerEntries()
	if len(entries) != 1 || entries[0].BalanceBefore().Units() != 0 || entries[0].BalanceAfter().Units() != 100000 {
		t.Errorf("ledger %+v", entries)
	}
	types := store.EventTypes()
	if len(types) != 2 || types[0] != event.TypeWagerTransactionProcessed || types[1] != event.TypeWalletBalanceChanged {
		t.Errorf("events %v", types)
	}
	evs := store.Events()
	if evs[1].Data.(event.WalletBalanceChanged).WalletVersion != 1 {
		t.Error("opening balance change must carry wallet version 1")
	}
}

func TestOpenWithZeroWritesNothingElse(t *testing.T) {
	svc, store := newService()
	if _, err := svc.Open(context.Background(), uuid.New(), brl(0), "corr"); err != nil {
		t.Fatal(err)
	}
	if len(store.LedgerEntries()) != 0 || len(store.EventTypes()) != 0 {
		t.Error("zero opening must not create ledger or events")
	}
}

func TestOpenRejectsInvalidInput(t *testing.T) {
	svc, _ := newService()
	if _, err := svc.Open(context.Background(), uuid.Nil, brl(1), "corr"); err == nil {
		t.Error("nil player")
	}
	if _, err := svc.Open(context.Background(), uuid.New(), brl(-1), "corr"); err == nil {
		t.Error("negative balance")
	}
}

func TestGetAndLedgerNotFound(t *testing.T) {
	svc, _ := newService()
	if _, err := svc.Get(context.Background(), uuid.New()); !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("get: %v", err)
	}
	if _, err := svc.Ledger(context.Background(), uuid.New(), "", 10); !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("ledger: %v", err)
	}
	if _, err := svc.GetTransaction(context.Background(), uuid.New()); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("transaction: %v", err)
	}
	if _, err := svc.GetProviderTransaction(context.Background(), "p", "x"); !errors.Is(err, ErrTransactionNotFound) {
		t.Errorf("provider transaction: %v", err)
	}
}

func TestReconcileConsistent(t *testing.T) {
	svc, _ := newService()
	w, err := svc.Open(context.Background(), uuid.New(), brl(5000), "corr")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.Reconcile(context.Background(), w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Consistent || rec.CheckedEntries != 1 || !rec.Difference.IsZero() || rec.CalculatedBalance.Units() != 5000 {
		t.Errorf("%+v", rec)
	}
	if _, err := svc.Reconcile(context.Background(), uuid.New()); !errors.Is(err, ErrWalletNotFound) {
		t.Errorf("missing wallet: %v", err)
	}
}

func TestReconcileDetectsDivergence(t *testing.T) {
	svc, store := newService()
	w, err := svc.Open(context.Background(), uuid.New(), brl(5000), "corr")
	if err != nil {
		t.Fatal(err)
	}
	tampered, _ := w.Credit(brl(100), uuid.New(), t0)
	_ = tampered
	store.SeedWallet(w)

	rec, err := svc.Reconcile(context.Background(), w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Consistent || rec.Difference.Units() != 100 {
		t.Errorf("expected stored - calculated = 100, got %+v", rec)
	}
}
