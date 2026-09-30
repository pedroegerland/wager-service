package fxapp

import (
	"log/slog"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/pedroegerland/wager-service/internal/config"
)

func Options(cfg config.Config) fx.Option {
	return fx.Options(
		fx.Supply(cfg),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log.With("component", "fx")}
		}),
		fx.StartTimeout(60*time.Second),
		fx.StopTimeout(cfg.ShutdownTimeout+5*time.Second),

		CoreModule,
		PersistenceModule,
		MessagingModule,
		AuthModule,
		UseCaseModule,
		HTTPModule,
		WorkerModule,
	)
}
