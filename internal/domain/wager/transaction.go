package wager

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

var ErrTerminal = errors.New("wager: transaction is terminal")

type Origin string

const (
	OriginInternal Origin = "INTERNAL"
	OriginExternal Origin = "EXTERNAL"
)

type External struct {
	ProviderID            string
	ExternalTransactionID string
	IdempotencyKey        string
	PayloadHash           string
	RoundID               string
	GameID                string
	ReferenceExternalTxID string
}

type Transaction struct {
	id       uuid.UUID
	origin   Origin
	kind     Kind
	status   Status
	walletID uuid.UUID
	playerID uuid.UUID
	money    money.Money
	external *External

	referenceTxID *uuid.UUID
	failureCode   FailureCode
	balanceAfter  *money.Money

	attempts      int
	nextAttemptAt *time.Time

	createdAt   time.Time
	updatedAt   time.Time
	processedAt *time.Time
}

func NewOpening(walletID, playerID uuid.UUID, amount money.Money, now time.Time) (*Transaction, error) {
	if walletID == uuid.Nil || playerID == uuid.Nil {
		return nil, errors.New("wager: opening requires wallet and player")
	}
	if !amount.IsValid() || !amount.IsPositive() {
		return nil, errors.New("wager: opening amount must be positive")
	}
	return &Transaction{
		id:           uuid.Must(uuid.NewV7()),
		origin:       OriginInternal,
		kind:         KindOpening,
		status:       StatusProcessed,
		walletID:     walletID,
		playerID:     playerID,
		money:        amount,
		balanceAfter: &amount,
		createdAt:    now,
		updatedAt:    now,
		processedAt:  &now,
	}, nil
}

func NewExternal(kind Kind, walletID, playerID uuid.UUID, amount money.Money, external External, now time.Time) (*Transaction, error) {
	if kind == KindOpening {
		return nil, domain.Invalid("kind", "OPENING is reserved for internal use")
	}
	switch kind {
	case KindBet, KindWin, KindLoss, KindRefund, KindRollback:
	default:
		return nil, domain.Invalid("kind", "unknown kind %q", kind)
	}
	if walletID == uuid.Nil {
		return nil, domain.Invalid("walletId", "required")
	}
	if playerID == uuid.Nil {
		return nil, domain.Invalid("playerId", "required")
	}
	if !amount.IsValid() {
		return nil, domain.Invalid("money", "required")
	}
	if amount.IsNegative() {
		return nil, domain.Invalid("money.amount", "must not be negative")
	}
	if kind == KindLoss {
		if !amount.IsZero() {
			return nil, domain.Invalid("money.amount", "LOSS must carry 0.00")
		}
	} else if !amount.IsPositive() {
		return nil, domain.Invalid("money.amount", "%s requires an amount greater than zero", kind)
	}
	for f, v := range map[string]string{
		"providerId":            external.ProviderID,
		"externalTransactionId": external.ExternalTransactionID,
		"idempotencyKey":        external.IdempotencyKey,
		"payloadHash":           external.PayloadHash,
		"roundId":               external.RoundID,
		"gameId":                external.GameID,
	} {
		if v == "" {
			return nil, domain.Invalid(f, "required")
		}
	}
	if kind.RequiresReference() && external.ReferenceExternalTxID == "" {
		return nil, domain.Invalid("referenceExternalTransactionId", "required for %s", kind)
	}
	if (kind == KindBet || kind == KindLoss) && external.ReferenceExternalTxID != "" {
		return nil, domain.Invalid("referenceExternalTransactionId", "not allowed for %s", kind)
	}
	e := external
	return &Transaction{
		id:        uuid.Must(uuid.NewV7()),
		origin:    OriginExternal,
		kind:      kind,
		status:    StatusPending,
		walletID:  walletID,
		playerID:  playerID,
		money:     amount,
		external:  &e,
		createdAt: now,
		updatedAt: now,
	}, nil
}

func (t *Transaction) ID() uuid.UUID              { return t.id }
func (t *Transaction) Origin() Origin             { return t.origin }
func (t *Transaction) Kind() Kind                 { return t.kind }
func (t *Transaction) Status() Status             { return t.status }
func (t *Transaction) WalletID() uuid.UUID        { return t.walletID }
func (t *Transaction) PlayerID() uuid.UUID        { return t.playerID }
func (t *Transaction) Money() money.Money         { return t.money }
func (t *Transaction) External() *External        { return t.external }
func (t *Transaction) ReferenceTxID() *uuid.UUID  { return t.referenceTxID }
func (t *Transaction) FailureCode() FailureCode   { return t.failureCode }
func (t *Transaction) BalanceAfter() *money.Money { return t.balanceAfter }
func (t *Transaction) Attempts() int              { return t.attempts }
func (t *Transaction) NextAttemptAt() *time.Time  { return t.nextAttemptAt }
func (t *Transaction) CreatedAt() time.Time       { return t.createdAt }
func (t *Transaction) UpdatedAt() time.Time       { return t.updatedAt }
func (t *Transaction) ProcessedAt() *time.Time    { return t.processedAt }
func (t *Transaction) IsTerminal() bool           { return t.status.IsTerminal() }

func (t *Transaction) ProviderID() string {
	if t.external == nil {
		return ""
	}
	return t.external.ProviderID
}

func (t *Transaction) ensureCanTransitionTo(to Status) error {
	if t.status.IsTerminal() {
		return &domain.TransitionError{From: string(t.status), To: string(to)}
	}
	switch to {
	case StatusPendingReference, StatusProcessed, StatusRejected, StatusFailed:
	default:
		return &domain.TransitionError{From: string(t.status), To: string(to)}
	}
	return nil
}

func (t *Transaction) ResolveReference(id uuid.UUID) error {
	if t.status.IsTerminal() {
		return ErrTerminal
	}
	if t.external == nil || t.external.ReferenceExternalTxID == "" {
		return errors.New("wager: transaction has no reference to resolve")
	}
	t.referenceTxID = &id
	return nil
}

func (t *Transaction) MarkProcessed(balanceAfter money.Money, now time.Time) error {
	if err := t.ensureCanTransitionTo(StatusProcessed); err != nil {
		return err
	}
	if !balanceAfter.IsValid() {
		return errors.New("wager: balanceAfter required")
	}
	t.status = StatusProcessed
	t.balanceAfter = &balanceAfter
	t.processedAt = &now
	t.updatedAt = now
	t.nextAttemptAt = nil
	return nil
}

func (t *Transaction) MarkPendingReference(nextAttempt, now time.Time) error {
	if err := t.ensureCanTransitionTo(StatusPendingReference); err != nil {
		return err
	}
	t.status = StatusPendingReference
	t.attempts++
	t.nextAttemptAt = &nextAttempt
	t.updatedAt = now
	return nil
}

func (t *Transaction) Reject(code FailureCode, balanceAt money.Money, now time.Time) error {
	if err := t.ensureCanTransitionTo(StatusRejected); err != nil {
		return err
	}
	if code == "" {
		return errors.New("wager: rejection requires a failure code")
	}
	t.status = StatusRejected
	t.failureCode = code
	if balanceAt.IsValid() {
		b := balanceAt
		t.balanceAfter = &b
	}
	t.processedAt = &now
	t.updatedAt = now
	t.nextAttemptAt = nil
	return nil
}

func (t *Transaction) Fail(code FailureCode, now time.Time) error {
	if err := t.ensureCanTransitionTo(StatusFailed); err != nil {
		return err
	}
	if code == "" {
		return errors.New("wager: failure requires a failure code")
	}
	t.status = StatusFailed
	t.failureCode = code
	t.processedAt = &now
	t.updatedAt = now
	t.nextAttemptAt = nil
	return nil
}
