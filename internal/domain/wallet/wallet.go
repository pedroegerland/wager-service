package wallet

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

var (
	ErrInsufficientFunds = errors.New("wallet: insufficient funds")
	ErrCurrencyMismatch  = errors.New("wallet: currency mismatch")
	ErrNonPositiveAmount = errors.New("wallet: amount must be positive")
)

type Wallet struct {
	id        uuid.UUID
	playerID  uuid.UUID
	currency  string
	balance   money.Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func Open(playerID uuid.UUID, initial money.Money, now time.Time) (*Wallet, error) {
	if playerID == uuid.Nil {
		return nil, domain.Invalid("playerId", "required")
	}
	if !initial.IsValid() {
		return nil, domain.Invalid("initialBalance", "required")
	}
	if initial.IsNegative() {
		return nil, domain.Invalid("initialBalance", "must not be negative")
	}
	return &Wallet{
		id:        uuid.Must(uuid.NewV7()),
		playerID:  playerID,
		currency:  initial.Currency(),
		balance:   initial,
		version:   1,
		createdAt: now,
		updatedAt: now,
	}, nil
}

func (w *Wallet) OpeningEntry(openingTxID uuid.UUID) (LedgerEntry, error) {
	if w.version != 1 || !w.balance.IsPositive() {
		return LedgerEntry{}, errors.New("wallet: opening entry only applies to a new wallet with positive balance")
	}
	if openingTxID == uuid.Nil {
		return LedgerEntry{}, errors.New("wallet: opening transaction id required")
	}
	zero, _ := money.Zero(w.currency)
	return newLedgerEntry(w.id, openingTxID, Credit, w.balance, zero, w.balance, w.createdAt)
}

func Rehydrate(id, playerID uuid.UUID, balance money.Money, version int64, createdAt, updatedAt time.Time) (*Wallet, error) {
	if id == uuid.Nil || playerID == uuid.Nil {
		return nil, errors.New("wallet: missing identifiers")
	}
	if !balance.IsValid() || balance.IsNegative() {
		return nil, errors.New("wallet: invalid persisted balance")
	}
	if version < 1 {
		return nil, errors.New("wallet: invalid persisted version")
	}
	return &Wallet{
		id:        id,
		playerID:  playerID,
		currency:  balance.Currency(),
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

func (w *Wallet) ID() uuid.UUID        { return w.id }
func (w *Wallet) PlayerID() uuid.UUID  { return w.playerID }
func (w *Wallet) Currency() string     { return w.currency }
func (w *Wallet) Balance() money.Money { return w.balance }
func (w *Wallet) Version() int64       { return w.version }
func (w *Wallet) CreatedAt() time.Time { return w.createdAt }
func (w *Wallet) UpdatedAt() time.Time { return w.updatedAt }

func (w *Wallet) validateMovement(m money.Money) error {
	if !m.IsValid() || m.Currency() != w.currency {
		return ErrCurrencyMismatch
	}
	if !m.IsPositive() {
		return ErrNonPositiveAmount
	}
	return nil
}

func (w *Wallet) Debit(m money.Money, txID uuid.UUID, now time.Time) (LedgerEntry, error) {
	if err := w.validateMovement(m); err != nil {
		return LedgerEntry{}, err
	}
	after, err := w.balance.Sub(m)
	if err != nil {
		return LedgerEntry{}, err
	}
	if after.IsNegative() {
		return LedgerEntry{}, ErrInsufficientFunds
	}
	entry, err := newLedgerEntry(w.id, txID, Debit, m, w.balance, after, now)
	if err != nil {
		return LedgerEntry{}, err
	}
	w.applyBalance(after, now)
	return entry, nil
}

func (w *Wallet) Credit(m money.Money, txID uuid.UUID, now time.Time) (LedgerEntry, error) {
	if err := w.validateMovement(m); err != nil {
		return LedgerEntry{}, err
	}
	after, err := w.balance.Add(m)
	if err != nil {
		return LedgerEntry{}, err
	}
	entry, err := newLedgerEntry(w.id, txID, Credit, m, w.balance, after, now)
	if err != nil {
		return LedgerEntry{}, err
	}
	w.applyBalance(after, now)
	return entry, nil
}

func (w *Wallet) applyBalance(after money.Money, now time.Time) {
	w.balance = after
	w.version++
	w.updatedAt = now
}
