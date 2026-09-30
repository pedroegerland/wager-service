package fxapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

func waitForDependency(ctx context.Context, attempts int, wait time.Duration, fn func() error, log *slog.Logger, what string) error {
	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i == attempts {
			break
		}
		log.Warn("waiting for dependency", "dependency", what, "attempt", i, "err", err)
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), err)
		case <-time.After(wait):
		}
	}
	return fmt.Errorf("%s not ready after %d attempts: %w", what, attempts, err)
}
