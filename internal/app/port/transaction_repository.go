package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type TransactionRepository interface {
	Insert(ctx context.Context, tx *wager.Transaction) error
	Update(ctx context.Context, tx *wager.Transaction) error
	Get(ctx context.Context, id uuid.UUID) (*wager.Transaction, error)
	FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error)
	FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error)

	HasProcessedReversal(ctx context.Context, providerID, referenceExternalID string) (bool, error)

	ClaimPendingReferences(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error)
}
