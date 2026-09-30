package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type walletRepository struct {
	tx pgx.Tx
}

const walletColumns = `id, player_id, currency, balance, version, created_at, updated_at`

func scanWallet(row pgx.Row) (*wallet.Wallet, error) {
	var (
		id, playerID         uuid.UUID
		currency             string
		balance, version     int64
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&id, &playerID, &currency, &balance, &version, &createdAt, &updatedAt); err != nil {
		return nil, translateError(err)
	}
	m, err := money.FromUnits(balance, currency)
	if err != nil {
		return nil, err
	}
	return wallet.Rehydrate(id, playerID, m, version, createdAt, updatedAt)
}

func (r *walletRepository) Insert(ctx context.Context, w *wallet.Wallet) error {
	_, err := r.tx.Exec(ctx,
		`INSERT INTO wallets (`+walletColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		w.ID(), w.PlayerID(), w.Currency(), w.Balance().Units(), w.Version(), w.CreatedAt(), w.UpdatedAt())
	return translateError(err)
}

func (r *walletRepository) Get(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return scanWallet(r.tx.QueryRow(ctx, `SELECT `+walletColumns+` FROM wallets WHERE id = $1`, id))
}

func (r *walletRepository) GetForUpdate(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return scanWallet(r.tx.QueryRow(ctx, `SELECT `+walletColumns+` FROM wallets WHERE id = $1 FOR UPDATE`, id))
}

func (r *walletRepository) Update(ctx context.Context, w *wallet.Wallet, expectedVersion int64) error {
	tag, err := r.tx.Exec(ctx,
		`UPDATE wallets SET balance = $1, version = $2, updated_at = $3 WHERE id = $4 AND version = $5`,
		w.Balance().Units(), w.Version(), w.UpdatedAt(), w.ID(), expectedVersion)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() != 1 {
		return port.ErrStale
	}
	return nil
}
