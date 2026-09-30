package fxapp

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
	"github.com/pedroegerland/wager-service/internal/config"
)

var UseCaseModule = fx.Module("usecases",
	fx.Provide(
		func(uow port.UnitOfWork, clock port.Clock, cfg config.Config, log *slog.Logger, m port.Metrics) *wagering.Processor {
			return wagering.NewProcessor(uow, clock, wagering.Config{
				MaxReferenceAttempts: cfg.ReferenceMaxAttempts,
				ReferenceBaseBackoff: cfg.ReferenceBaseBackoff,
				ReferenceMaxBackoff:  cfg.ReferenceMaxBackoff,
			}, log.With("component", "wagering"), m)
		},
		func(uow port.UnitOfWork, clock port.Clock, log *slog.Logger, m port.Metrics) *wallets.Service {
			return wallets.NewService(uow, clock, log.With("component", "wallets"), m)
		},
	),
)
