package wagering

import (
	"context"
	"time"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

func (p *Processor) waitForReference(out *outcome, now time.Time, correlationID string) (*outcome, error) {
	tx := out.tx
	if tx.Attempts() >= p.cfg.MaxReferenceAttempts {
		if err := tx.Reject(wager.CodeReferenceNotFound, out.wallet.Balance(), now); err != nil {
			return nil, err
		}
		out.events = append(out.events, event.NewWagerTransactionRejected(tx, correlationID))
		return out, nil
	}
	if err := tx.MarkPendingReference(now.Add(p.backoff(tx.Attempts())), now); err != nil {
		return nil, err
	}
	out.events = append(out.events, event.NewWagerTransactionPendingReference(tx, correlationID))
	return out, nil
}

func (p *Processor) backoff(attempt int) time.Duration {
	d := p.cfg.ReferenceBaseBackoff
	for i := 0; i < attempt && d < p.cfg.ReferenceMaxBackoff; i++ {
		d *= 2
	}
	if d > p.cfg.ReferenceMaxBackoff {
		d = p.cfg.ReferenceMaxBackoff
	}
	return d
}

func (p *Processor) RetryOnePendingReference(ctx context.Context) (bool, error) {
	var did bool
	err := p.uow.Do(ctx, func(ctx context.Context, r port.Repos) error {
		now := p.clock.Now()
		txs, err := r.Transactions.ClaimPendingReferences(ctx, now, 1)
		if err != nil || len(txs) == 0 {
			return err
		}
		tx := txs[0]
		did = true
		p.metrics.ReferenceRetry()

		w, err := r.Wallets.GetForUpdate(ctx, tx.WalletID())
		if err != nil {
			return err
		}
		out, err := p.applyBusinessRules(ctx, r, tx, w, now, tx.ID().String())
		if err != nil {
			return err
		}
		if err := r.Transactions.Update(ctx, tx); err != nil {
			return err
		}
		if err := p.persistOutcome(ctx, r, out); err != nil {
			return err
		}
		if tx.IsTerminal() {
			p.metrics.TransactionResult(tx.Kind(), tx.Status())
		}
		p.log.InfoContext(ctx, "pending reference retried",
			"transactionId", tx.ID(), "walletId", tx.WalletID(), "providerId", tx.ProviderID(),
			"status", tx.Status(), "attempts", tx.Attempts())
		return nil
	})
	return did, err
}
