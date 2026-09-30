package fxapp

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/config"
)

var PersistenceModule = fx.Module("persistence",
	fx.Provide(
		providePool,
		func(pool *pgxpool.Pool) port.UnitOfWork { return postgres.NewUnitOfWork(pool) },
	),
	fx.Invoke(runMigrationsOnStart),
)

func providePool(lc fx.Lifecycle, cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := postgres.NewPool(ctx, postgres.Config{
		URL: cfg.DatabaseURL, MaxConns: cfg.DBMaxConns, ConnectTimeout: 5 * time.Second, StatementTimeout: cfg.DBStatementTimeout,
	})
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return waitForDependency(ctx, 30, time.Second, func() error { return pool.Ping(ctx) }, log, "postgres")
		},

		OnStop: func(context.Context) error { pool.Close(); return nil },
	})
	return pool, nil
}

func runMigrationsOnStart(lc fx.Lifecycle, cfg config.Config, log *slog.Logger) {
	if !cfg.MigrateOnStart {
		return
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		return waitForDependency(ctx, 30, time.Second, func() error {
			m, err := postgres.NewMigrator(cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer m.Close()
			return m.Up()
		}, log, "migrations")
	}})
}
