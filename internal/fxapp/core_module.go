package fxapp

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/config"
	"github.com/pedroegerland/wager-service/internal/observability"
)

var CoreModule = fx.Module("core",
	fx.Provide(
		func(cfg config.Config) *slog.Logger { return observability.NewLogger(cfg.LogLevel) },
		observability.NewPrometheusMetrics,
		func(m *observability.PrometheusMetrics) port.Metrics { return m },
		func() port.Clock { return port.SystemClock{} },
	),
)
