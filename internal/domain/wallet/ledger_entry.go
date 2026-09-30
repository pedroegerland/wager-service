package wallet

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
)

type Direction string

const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

var ErrLedgerArithmetic = errors.New("ledger: balanceAfter does not match balanceBefore and amount")

type LedgerEntry struct {
	id            uuid.UUID
	walletID      uuid.UUID
	transactionID uuid.UUID
	direction     Direction
	amount        money.Money
	balanceBefore money.Money
	balanceAfter  money.Money
	createdAt     time.Time
}

func newLedgerEntry(walletID, txID uuid.UUID, dir Direction, amount, before, after money.Money, at time.Time) (LedgerEntry, error) {
	e := LedgerEntry{
		id:            uuid.Must(uuid.NewV7()),
		walletID:      walletID,
		transactionID: txID,
		direction:     dir,
		amount:        amount,
		balanceBefore: before,
		balanceAfter:  after,
		createdAt:     at,
	}
	if err := e.check(); err != nil {
		return LedgerEntry{}, err
	}
	return e, nil
}

func RehydrateLedgerEntry(id, walletID, txID uuid.UUID, dir Direction, amount, before, after money.Money, at time.Time) (LedgerEntry, error) {
	e := LedgerEntry{id, walletID, txID, dir, amount, before, after, at}
	if err := e.check(); err != nil {
		return LedgerEntry{}, err
	}
	return e, nil
}

func (e LedgerEntry) check() error {
	if e.id == uuid.Nil || e.walletID == uuid.Nil || e.transactionID == uuid.Nil {
		return errors.New("ledger: missing identifiers")
	}
	if !e.amount.IsPositive() {
		return errors.New("ledger: amount must be positive")
	}
	var want money.Money
	var err error
	switch e.direction {
	case Debit:
		want, err = e.balanceBefore.Sub(e.amount)
	case Credit:
		want, err = e.balanceBefore.Add(e.amount)
	default:
		return errors.New("ledger: unknown direction")
	}
	if err != nil {
		return err
	}
	if !want.Equal(e.balanceAfter) {
		return ErrLedgerArithmetic
	}
	return nil
}

func (e LedgerEntry) ID() uuid.UUID              { return e.id }
func (e LedgerEntry) WalletID() uuid.UUID        { return e.walletID }
func (e LedgerEntry) TransactionID() uuid.UUID   { return e.transactionID }
func (e LedgerEntry) Direction() Direction       { return e.direction }
func (e LedgerEntry) Amount() money.Money        { return e.amount }
func (e LedgerEntry) BalanceBefore() money.Money { return e.balanceBefore }
func (e LedgerEntry) BalanceAfter() money.Money  { return e.balanceAfter }
func (e LedgerEntry) CreatedAt() time.Time       { return e.createdAt }
