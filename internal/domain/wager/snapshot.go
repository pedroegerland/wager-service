package wager

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
)

type Snapshot struct {
	ID            uuid.UUID
	Origin        Origin
	Kind          Kind
	Status        Status
	WalletID      uuid.UUID
	PlayerID      uuid.UUID
	Money         money.Money
	External      *External
	ReferenceTxID *uuid.UUID
	FailureCode   FailureCode
	BalanceAfter  *money.Money
	Attempts      int
	NextAttemptAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ProcessedAt   *time.Time
}

func Rehydrate(s Snapshot) (*Transaction, error) {
	if s.ID == uuid.Nil || s.WalletID == uuid.Nil || s.PlayerID == uuid.Nil {
		return nil, errors.New("wager: missing identifiers")
	}
	if !s.Money.IsValid() {
		return nil, errors.New("wager: invalid persisted money")
	}
	switch s.Status {
	case StatusPending, StatusPendingReference, StatusProcessed, StatusRejected, StatusFailed:
	default:
		return nil, errors.New("wager: unknown persisted status")
	}
	if (s.Origin == OriginExternal) != (s.External != nil) {
		return nil, errors.New("wager: origin and external metadata disagree")
	}
	return &Transaction{
		id:            s.ID,
		origin:        s.Origin,
		kind:          s.Kind,
		status:        s.Status,
		walletID:      s.WalletID,
		playerID:      s.PlayerID,
		money:         s.Money,
		external:      s.External,
		referenceTxID: s.ReferenceTxID,
		failureCode:   s.FailureCode,
		balanceAfter:  s.BalanceAfter,
		attempts:      s.Attempts,
		nextAttemptAt: s.NextAttemptAt,
		createdAt:     s.CreatedAt,
		updatedAt:     s.UpdatedAt,
		processedAt:   s.ProcessedAt,
	}, nil
}

func (t *Transaction) Snapshot() Snapshot {
	var external *External
	if t.external != nil {
		e := *t.external
		external = &e
	}
	return Snapshot{
		ID: t.id, Origin: t.origin, Kind: t.kind, Status: t.status,
		WalletID: t.walletID, PlayerID: t.playerID, Money: t.money, External: external,
		ReferenceTxID: t.referenceTxID, FailureCode: t.failureCode, BalanceAfter: t.balanceAfter,
		Attempts: t.attempts, NextAttemptAt: t.nextAttemptAt,
		CreatedAt: t.createdAt, UpdatedAt: t.updatedAt, ProcessedAt: t.processedAt,
	}
}
