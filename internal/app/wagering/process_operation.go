package wagering

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type Config struct {
	MaxReferenceAttempts int
	ReferenceBaseBackoff time.Duration
	ReferenceMaxBackoff  time.Duration
}

func (c Config) withDefaults() Config {
	if c.MaxReferenceAttempts <= 0 {
		c.MaxReferenceAttempts = 6
	}
	if c.ReferenceBaseBackoff <= 0 {
		c.ReferenceBaseBackoff = 2 * time.Second
	}
	if c.ReferenceMaxBackoff <= 0 {
		c.ReferenceMaxBackoff = 2 * time.Minute
	}
	return c
}

type Processor struct {
	uow     port.UnitOfWork
	clock   port.Clock
	cfg     Config
	log     *slog.Logger
	metrics port.Metrics
}

func NewProcessor(uow port.UnitOfWork, clock port.Clock, cfg Config, log *slog.Logger, m port.Metrics) *Processor {
	if m == nil {
		m = port.NopMetrics{}
	}
	return &Processor{uow: uow, clock: clock, cfg: cfg.withDefaults(), log: log, metrics: m}
}

func (p *Processor) Process(ctx context.Context, op Operation) (Result, error) {
	start := p.clock.Now()
	hash := op.Hash()

	if _, err := p.buildTransaction(op, hash, start); err != nil {
		return Result{}, err
	}

	var res Result
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		res, err = p.processInTransaction(ctx, op, hash)
		if errors.Is(err, port.ErrConflict) || errors.Is(err, port.ErrStale) {
			p.metrics.ConcurrencyConflict()
			p.log.WarnContext(ctx, "retrying after write conflict", "err", err, "providerId", op.ProviderID, "externalTransactionId", op.ExternalTransactionID)
			continue
		}
		break
	}
	if err == nil {
		p.metrics.ProcessingLatency(op.Kind, p.clock.Now().Sub(start))
		if !res.IdempotentReplay {
			p.metrics.TransactionResult(op.Kind, res.Status)
		}
	}
	return res, err
}

func (p *Processor) buildTransaction(op Operation, hash string, now time.Time) (*wager.Transaction, error) {
	return wager.NewExternal(op.Kind, op.WalletID, op.PlayerID, op.Money, wager.External{
		ProviderID:            op.ProviderID,
		ExternalTransactionID: op.ExternalTransactionID,
		IdempotencyKey:        op.IdempotencyKey,
		PayloadHash:           hash,
		RoundID:               op.RoundID,
		GameID:                op.GameID,
		ReferenceExternalTxID: op.ReferenceExternalTransactionID,
	}, now)
}

func (p *Processor) processInTransaction(ctx context.Context, op Operation, hash string) (Result, error) {
	var res Result
	err := p.uow.Do(ctx, func(ctx context.Context, r port.Repos) error {
		now := p.clock.Now()

		completeWith := func(result Result) error {
			res = result
			if op.Inbox != nil {
				return r.Inbox.Complete(ctx, *op.Inbox, now)
			}
			return nil
		}

		if op.Inbox != nil {
			state, err := r.Inbox.Register(ctx, *op.Inbox, now)
			if err != nil {
				return err
			}
			switch state {
			case port.InboxHashMismatch:
				return ErrInboxHashMismatch
			case port.InboxDuplicate:
				p.metrics.InboxDuplicate()
				existing, err := r.Transactions.FindByIdempotencyKey(ctx, op.ProviderID, op.IdempotencyKey)
				if errors.Is(err, port.ErrNotFound) {
					return ErrInboxInconsistent
				}
				if err != nil {
					return err
				}
				res = resultFromTransaction(existing, true)
				return nil
			}
		}

		w, err := r.Wallets.GetForUpdate(ctx, op.WalletID)
		if errors.Is(err, port.ErrNotFound) {
			return ErrWalletNotFound
		}
		if err != nil {
			return err
		}

		existing, err := r.Transactions.FindByIdempotencyKey(ctx, op.ProviderID, op.IdempotencyKey)
		if err == nil {
			if existing.External().PayloadHash != hash {
				p.metrics.IdempotencyConflict()
				return ErrIdempotencyConflict
			}
			p.metrics.IdempotentReplay()
			return completeWith(resultFromTransaction(existing, true))
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}

		_, err = r.Transactions.FindByExternalID(ctx, op.ProviderID, op.ExternalTransactionID)
		if err == nil {
			return ErrExternalIDReused
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}

		tx, err := p.buildTransaction(op, hash, now)
		if err != nil {
			return err
		}
		out, err := p.applyBusinessRules(ctx, r, tx, w, now, op.CorrelationID)
		if err != nil {
			return err
		}
		if err := r.Transactions.Insert(ctx, tx); err != nil {
			return err
		}
		if err := p.persistOutcome(ctx, r, out); err != nil {
			return err
		}
		return completeWith(resultFromTransaction(tx, false))
	})
	return res, err
}
