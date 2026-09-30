package wallet

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func brl(units int64) money.Money { return money.MustFromUnits(units, "BRL") }

func TestOpenWithBalance(t *testing.T) {
	txID := uuid.Must(uuid.NewV7())
	w, err := Open(uuid.New(), brl(100000), now)
	if err != nil {
		t.Fatal(err)
	}
	if w.Version() != 1 {
		t.Errorf("version=%d want 1", w.Version())
	}
	entry, err := w.OpeningEntry(txID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Direction() != Credit || entry.BalanceBefore().Units() != 0 || entry.BalanceAfter().Units() != 100000 {
		t.Errorf("bad opening entry: %+v", entry)
	}
	if entry.TransactionID() != txID {
		t.Error("entry must point to the opening transaction")
	}
}

func TestOpenWithZero(t *testing.T) {
	w, err := Open(uuid.New(), brl(0), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.OpeningEntry(uuid.New()); err == nil {
		t.Error("zero opening must not produce a ledger entry")
	}
	if w.Version() != 1 || !w.Balance().IsZero() {
		t.Errorf("unexpected wallet state %d %s", w.Version(), w.Balance())
	}
}

func TestOpenRejectsBadInput(t *testing.T) {
	var ve *domain.ValidationError
	if _, err := Open(uuid.Nil, brl(1), now); !errors.As(err, &ve) {
		t.Errorf("nil player: %v", err)
	}
	if _, err := Open(uuid.New(), money.Money{}, now); !errors.As(err, &ve) {
		t.Errorf("zero-value money: %v", err)
	}
	if _, err := Open(uuid.New(), brl(-1), now); !errors.As(err, &ve) {
		t.Errorf("negative: %v", err)
	}
	w, _ := Open(uuid.New(), brl(1), now)
	if _, err := w.OpeningEntry(uuid.Nil); err == nil {
		t.Error("opening entry without tx id should fail")
	}
	_, _ = w.Credit(brl(1), uuid.New(), now)
	if _, err := w.OpeningEntry(uuid.New()); err == nil {
		t.Error("opening entry after a mutation should fail")
	}
}

func TestDebitCredit(t *testing.T) {
	w, _ := Open(uuid.New(), brl(10000), now)

	e, err := w.Debit(brl(2500), uuid.New(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().Units() != 7500 || w.Version() != 2 {
		t.Errorf("after debit: %s v%d", w.Balance(), w.Version())
	}
	if e.BalanceBefore().Units() != 10000 || e.BalanceAfter().Units() != 7500 {
		t.Errorf("entry: %d -> %d", e.BalanceBefore().Units(), e.BalanceAfter().Units())
	}

	_, err = w.Credit(brl(500), uuid.New(), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance().Units() != 8000 || w.Version() != 3 {
		t.Errorf("after credit: %s v%d", w.Balance(), w.Version())
	}
}

func TestDebitInsufficient(t *testing.T) {
	w, _ := Open(uuid.New(), brl(10000), now)
	_, err := w.Debit(brl(10001), uuid.New(), now)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("got %v", err)
	}

	if w.Balance().Units() != 10000 || w.Version() != 1 {
		t.Errorf("wallet mutated on failed debit: %s v%d", w.Balance(), w.Version())
	}

	if _, err := w.Debit(brl(10000), uuid.New(), now); err != nil {
		t.Fatal(err)
	}
	if !w.Balance().IsZero() {
		t.Error("expected zero balance")
	}
}

func TestCurrencyAndAmountChecks(t *testing.T) {
	w, _ := Open(uuid.New(), brl(10000), now)
	if _, err := w.Debit(money.MustFromUnits(100, "USD"), uuid.New(), now); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("usd debit: %v", err)
	}
	if _, err := w.Credit(money.Money{}, uuid.New(), now); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("zero-value credit: %v", err)
	}
	if _, err := w.Credit(brl(0), uuid.New(), now); !errors.Is(err, ErrNonPositiveAmount) {
		t.Errorf("zero credit: %v", err)
	}
	if _, err := w.Debit(brl(-5), uuid.New(), now); !errors.Is(err, ErrNonPositiveAmount) {
		t.Errorf("negative debit: %v", err)
	}
}

func TestRehydrate(t *testing.T) {
	id := uuid.New()
	w, err := Rehydrate(id, uuid.New(), brl(500), 7, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if w.ID() != id || w.Version() != 7 || w.Balance().Units() != 500 {
		t.Error("rehydrate lost state")
	}
	if _, err := Rehydrate(id, uuid.New(), brl(500), 0, now, now); err == nil {
		t.Error("version 0 should be rejected")
	}
	if _, err := Rehydrate(id, uuid.New(), brl(-1), 1, now, now); err == nil {
		t.Error("negative balance should be rejected")
	}
}

func TestLedgerEntryArithmetic(t *testing.T) {
	_, err := RehydrateLedgerEntry(uuid.New(), uuid.New(), uuid.New(), Debit, brl(10), brl(100), brl(95), now)
	if !errors.Is(err, ErrLedgerArithmetic) {
		t.Errorf("got %v", err)
	}
	_, err = RehydrateLedgerEntry(uuid.New(), uuid.New(), uuid.New(), Credit, brl(10), brl(100), brl(110), now)
	if err != nil {
		t.Errorf("valid credit: %v", err)
	}
}
