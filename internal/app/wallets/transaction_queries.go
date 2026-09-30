package wallets

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

func (s *Service) GetTransaction(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	var tx *wager.Transaction
	err := s.uow.DoSnapshot(ctx, func(ctx context.Context, r port.Repos) error {
		var err error
		tx, err = r.Transactions.Get(ctx, id)
		return err
	})
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrTransactionNotFound
	}
	return tx, err
}

func (s *Service) GetProviderTransaction(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	var tx *wager.Transaction
	err := s.uow.DoSnapshot(ctx, func(ctx context.Context, r port.Repos) error {
		var err error
		tx, err = r.Transactions.FindByExternalID(ctx, providerID, externalID)
		return err
	})
	if errors.Is(err, port.ErrNotFound) {
		return nil, ErrTransactionNotFound
	}
	return tx, err
}
