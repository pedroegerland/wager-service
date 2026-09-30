package wallets

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

type Reconciliation struct {
	WalletID          uuid.UUID
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}

func (s *Service) Reconcile(ctx context.Context, walletID uuid.UUID) (Reconciliation, error) {
	var rec Reconciliation
	err := s.uow.DoSnapshot(ctx, func(ctx context.Context, r port.Repos) error {
		w, err := r.Wallets.Get(ctx, walletID)
		if err != nil {
			return err
		}
		net, count, err := r.Ledger.Sum(ctx, walletID)
		if err != nil {
			return err
		}
		calc, err := money.FromUnits(net, w.Currency())
		if err != nil {
			return err
		}
		diff, err := w.Balance().Sub(calc)
		if err != nil {
			return err
		}
		rec = Reconciliation{
			WalletID:          walletID,
			StoredBalance:     w.Balance(),
			CalculatedBalance: calc,
			Difference:        diff,
			Consistent:        diff.IsZero(),
			CheckedEntries:    count,
		}
		return nil
	})
	if errors.Is(err, port.ErrNotFound) {
		return Reconciliation{}, ErrWalletNotFound
	}
	if err != nil {
		return Reconciliation{}, err
	}
	if !rec.Consistent {
		s.metrics.ReconciliationDivergence()
		s.log.ErrorContext(ctx, "reconciliation divergence",
			"walletId", walletID, "stored", rec.StoredBalance.String(), "calculated", rec.CalculatedBalance.String(),
			"difference", rec.Difference.String(), "entries", rec.CheckedEntries)
	}
	return rec, nil
}
