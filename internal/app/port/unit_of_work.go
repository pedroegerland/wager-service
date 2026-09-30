package port

import (
	"context"
)

type Repos struct {
	Wallets      WalletRepository
	Transactions TransactionRepository
	Ledger       LedgerRepository
	Outbox       OutboxRepository
	Inbox        InboxRepository
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, r Repos) error) error

	DoSnapshot(ctx context.Context, fn func(ctx context.Context, r Repos) error) error
}
