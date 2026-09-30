package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type WalletRepository interface {
	Insert(ctx context.Context, w *wallet.Wallet) error
	Get(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)

	GetForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error)

	Update(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error
}
