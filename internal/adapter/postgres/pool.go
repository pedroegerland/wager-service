package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	URL              string
	MaxConns         int32
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
}

func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	if cfg.ConnectTimeout > 0 {
		pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}
	if cfg.StatementTimeout > 0 {
		pc.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(cfg.StatementTimeout.Milliseconds())
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}
	return pool, nil
}
