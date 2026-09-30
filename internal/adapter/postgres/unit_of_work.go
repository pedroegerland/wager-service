package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

type UnitOfWork struct {
	pool *pgxpool.Pool
}

func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork {
	return &UnitOfWork{pool: pool}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, r port.Repos) error) error {
	return u.runInTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, fn)
}

func (u *UnitOfWork) DoSnapshot(ctx context.Context, fn func(ctx context.Context, r port.Repos) error) error {
	return u.runInTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

func (u *UnitOfWork) runInTransaction(ctx context.Context, opts pgx.TxOptions, fn func(ctx context.Context, r port.Repos) error) (err error) {
	tx, err := u.pool.BeginTx(ctx, opts)
	if err != nil {
		return translateError(err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	repos := port.Repos{
		Wallets:      &walletRepository{tx: tx},
		Transactions: &transactionRepository{tx: tx},
		Ledger:       &ledgerRepository{tx: tx},
		Outbox:       &outboxRepository{tx: tx},
		Inbox:        &inboxRepository{tx: tx},
	}
	if err = fn(ctx, repos); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return translateError(err)
	}
	return nil
}
