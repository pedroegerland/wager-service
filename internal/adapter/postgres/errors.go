package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

func translateError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return port.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %w", &port.ConflictError{Constraint: pgErr.ConstraintName}, err)
		case "40001", "40P01", "0A000":
			return fmt.Errorf("%w: %w", port.ErrUnavailable, err)
		case "57P01", "57P02", "57P03", "08000", "08003", "08006", "53300":
			return fmt.Errorf("%w: %w", port.ErrUnavailable, err)
		}
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", port.ErrUnavailable, err)
	}
	if pgconn.SafeToRetry(err) || errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("%w: %w", port.ErrUnavailable, err)
	}
	return err
}
