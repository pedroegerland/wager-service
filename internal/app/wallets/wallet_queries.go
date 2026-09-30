package wallets

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	var w *wallet.Wallet
	err := s.uow.DoSnapshot(ctx, func(ctx context.Context, r port.Repos) error {
		var err error
		w, err = r.Wallets.Get(ctx, id)
		return err
	})
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrWalletNotFound
	}
	return w, err
}

func (s *Service) Ledger(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (port.LedgerPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var page port.LedgerPage
	err := s.uow.DoSnapshot(ctx, func(ctx context.Context, r port.Repos) error {
		if _, err := r.Wallets.Get(ctx, walletID); err != nil {
			return err
		}
		var err error
		page, err = r.Ledger.List(ctx, walletID, cursor, limit)
		return err
	})
	if errors.Is(err, port.ErrNotFound) {
		return port.LedgerPage{}, ErrWalletNotFound
	}
	return page, err
}
