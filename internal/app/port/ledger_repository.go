package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type LedgerPage struct {
	Entries    []wallet.LedgerEntry
	NextCursor string
}

type LedgerRepository interface {
	Insert(ctx context.Context, e wallet.LedgerEntry) error
	List(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (LedgerPage, error)

	Sum(ctx context.Context, walletID uuid.UUID) (net int64, count int, err error)
}
