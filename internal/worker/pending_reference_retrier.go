package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pedroegerland/wager-service/internal/app/wagering"
)

type PendingReferenceConfig struct {
	PollInterval time.Duration
	MaxPerPass   int
}

type PendingReferenceRetrier struct {
	proc *wagering.Processor
	cfg  PendingReferenceConfig
	log  *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func NewPendingReferenceRetrier(proc *wagering.Processor, cfg PendingReferenceConfig, log *slog.Logger) *PendingReferenceRetrier {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxPerPass <= 0 {
		cfg.MaxPerPass = 100
	}
	return &PendingReferenceRetrier{proc: proc, cfg: cfg, log: log}
}

func (w *PendingReferenceRetrier) Start(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(runCtx)
	w.log.Info("pending reference retrier started")
	return nil
}

func (w *PendingReferenceRetrier) Stop(ctx context.Context) error {
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

func (w *PendingReferenceRetrier) run(ctx context.Context) {
	defer close(w.done)
	t := time.NewTicker(w.cfg.PollInterval)
	defer t.Stop()
	for {
		if _, err := w.RetryDueReferences(ctx); err != nil && ctx.Err() == nil {
			w.log.Warn("pending reference pass failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *PendingReferenceRetrier) RetryDueReferences(ctx context.Context) (int, error) {
	n := 0
	for n < w.cfg.MaxPerPass {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		did, err := w.proc.RetryOnePendingReference(ctx)
		if err != nil {
			return n, err
		}
		if !did {
			break
		}
		n++
	}
	return n, nil
}
