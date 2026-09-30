package fxapp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
	"github.com/pedroegerland/wager-service/internal/adapter/httpapi"
	"github.com/pedroegerland/wager-service/internal/adapter/sqs"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
	"github.com/pedroegerland/wager-service/internal/config"
	"github.com/pedroegerland/wager-service/internal/observability"
)

type readinessChecker struct {
	pool *pgxpool.Pool
	sqs  *awssqs.Client
	cfg  config.Config
}

func (r readinessChecker) Ready(ctx context.Context) map[string]error {
	return map[string]error{
		"postgres": r.pool.Ping(ctx),
		"sqs":      sqs.CheckQueue(ctx, r.sqs, r.cfg.SQSInboundQueueURL),
	}
}

var HTTPModule = fx.Module("http",
	fx.Provide(
		func(pool *pgxpool.Pool, c *awssqs.Client, cfg config.Config) httpapi.ReadinessChecker {
			return readinessChecker{pool: pool, sqs: c, cfg: cfg}
		},
		func(cfg config.Config, w *wallets.Service, p *wagering.Processor, v *auth.Verifier, r httpapi.ReadinessChecker, m *observability.PrometheusMetrics, log *slog.Logger) http.Handler {
			return httpapi.NewRouter(httpapi.Config{
				RateLimitRPS: cfg.RateLimitRPS, RateLimitBurst: cfg.RateLimitBurst,
			}, httpapi.Deps{Wallets: w, Wagers: p, Verifier: v, Ready: r, Metrics: m.Handler(), Log: log.With("component", "http")})
		},
		func(cfg config.Config, h http.Handler, log *slog.Logger) *httpapi.Server {
			return httpapi.NewServer(httpapi.Config{
				Addr: cfg.HTTPAddr, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
				ShutdownTimeout: cfg.ShutdownTimeout,
			}, h, log.With("component", "http"))
		},
	),
	fx.Invoke(func(lc fx.Lifecycle, s *httpapi.Server) {
		lc.Append(fx.Hook{OnStart: s.Start, OnStop: s.Stop})
	}),
)
