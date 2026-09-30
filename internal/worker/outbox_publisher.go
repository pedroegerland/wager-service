package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

type OutboxConfig struct {
	Owner        string
	PollInterval time.Duration
	BatchSize    int
	Lease        time.Duration
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
}

type OutboxPublisher struct {
	uow     port.UnitOfWork
	pub     port.EventPublisher
	cfg     OutboxConfig
	log     *slog.Logger
	metrics port.Metrics
	owner   string

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func NewOutboxPublisher(uow port.UnitOfWork, pub port.EventPublisher, cfg OutboxConfig, log *slog.Logger, m port.Metrics) *OutboxPublisher {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.Lease <= 0 {
		cfg.Lease = 30 * time.Second
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = time.Minute
	}
	if m == nil {
		m = port.NopMetrics{}
	}
	owner := cfg.Owner
	if owner == "" {
		host, _ := os.Hostname()
		owner = fmt.Sprintf("%s/%s", host, uuid.NewString()[:8])
	}
	return &OutboxPublisher{uow: uow, pub: pub, cfg: cfg, log: log, metrics: m, owner: owner}
}

func (w *OutboxPublisher) Owner() string { return w.owner }

func (w *OutboxPublisher) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(runCtx)
	w.log.Info("outbox publisher started", "owner", w.owner)
	return nil
}

func (w *OutboxPublisher) Stop(ctx context.Context) error {
	w.once.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
	})
	if w.done == nil {
		return nil
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *OutboxPublisher) run(ctx context.Context) {
	defer close(w.done)
	t := time.NewTicker(w.cfg.PollInterval)
	defer t.Stop()
	for {
		n, err := w.PublishDueEvents(ctx)
		if err != nil && ctx.Err() == nil {
			w.log.Warn("outbox pass failed", "err", err)
		}
		if n == w.cfg.BatchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *OutboxPublisher) PublishDueEvents(ctx context.Context) (int, error) {
	var batch []port.OutboxRecord
	err := w.uow.Do(ctx, func(ctx context.Context, r port.Repos) error {
		var err error
		batch, err = r.Outbox.Claim(ctx, w.owner, w.cfg.Lease, w.cfg.BatchSize)
		if err != nil {
			return err
		}
		lag, err := r.Outbox.Lag(ctx)
		if err == nil {
			w.metrics.OutboxLag(lag)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, rec := range batch {
		if ctx.Err() != nil {
			return len(batch), ctx.Err()
		}
		pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		pubErr := w.pub.Publish(pubCtx, rec)
		cancel()

		markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		if pubErr != nil {
			w.metrics.OutboxRetry()
			next := time.Now().Add(retryDelay(rec.Attempts, w.cfg.BaseBackoff, w.cfg.MaxBackoff))
			w.log.Warn("publish failed", "eventId", rec.ID, "eventType", rec.EventType, "attempt", rec.Attempts+1, "err", pubErr)
			err = w.uow.Do(markCtx, func(ctx context.Context, r port.Repos) error {
				return r.Outbox.Reschedule(ctx, rec.ID, w.owner, next, pubErr.Error())
			})
		} else {
			w.metrics.OutboxPublished()
			err = w.uow.Do(markCtx, func(ctx context.Context, r port.Repos) error {
				return r.Outbox.MarkPublished(ctx, rec.ID, w.owner)
			})
			if errors.Is(err, port.ErrStale) {
				w.log.Info("outbox record taken over by another publisher", "eventId", rec.ID)
				err = nil
			}
		}
		cancel()
		if err != nil {
			w.log.Warn("outbox bookkeeping failed", "eventId", rec.ID, "err", err)
		}
	}
	return len(batch), nil
}

func retryDelay(attempt int, base, max time.Duration) time.Duration {
	d := base
	for i := 0; i < attempt && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}
