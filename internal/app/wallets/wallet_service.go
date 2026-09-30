package wallets

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

var (
	ErrWalletExists        = errors.New("wallet already exists for player and currency")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransactionNotFound = errors.New("transaction not found")
)

type Service struct {
	uow     port.UnitOfWork
	clock   port.Clock
	log     *slog.Logger
	metrics port.Metrics
}

func NewService(uow port.UnitOfWork, clock port.Clock, log *slog.Logger, m port.Metrics) *Service {
	if m == nil {
		m = port.NopMetrics{}
	}
	return &Service{uow: uow, clock: clock, log: log, metrics: m}
}

func (s *Service) Open(ctx context.Context, playerID uuid.UUID, initial money.Money, correlationID string) (*wallet.Wallet, error) {
	now := s.clock.Now()
	w, err := wallet.Open(playerID, initial, now)
	if err != nil {
		return nil, err
	}
	err = s.uow.Do(ctx, func(ctx context.Context, r port.Repos) error {
		if err := r.Wallets.Insert(ctx, w); err != nil {
			if errors.Is(err, port.ErrConflict) {
				return ErrWalletExists
			}
			return err
		}
		if initial.IsZero() {
			return nil
		}
		tx, err := wager.NewOpening(w.ID(), w.PlayerID(), initial, now)
		if err != nil {
			return err
		}
		entry, err := w.OpeningEntry(tx.ID())
		if err != nil {
			return err
		}
		if err := r.Transactions.Insert(ctx, tx); err != nil {
			return err
		}
		if err := r.Ledger.Insert(ctx, entry); err != nil {
			return err
		}
		return r.Outbox.Add(ctx,
			event.NewWagerTransactionProcessed(tx, correlationID),
			event.NewWalletBalanceChanged(entry, w.Version(), correlationID),
		)
	})
	if err != nil {
		return nil, err
	}
	s.metrics.TransactionResult(wager.KindOpening, wager.StatusProcessed)
	return w, nil
}
