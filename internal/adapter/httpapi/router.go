package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
)

type Deps struct {
	Wallets  *wallets.Service
	Wagers   *wagering.Processor
	Verifier tokenVerifier
	Ready    ReadinessChecker
	Metrics  http.Handler
	Log      *slog.Logger
}

func NewRouter(cfg Config, d Deps) http.Handler {
	h := &handlers{wallets: d.Wallets, wagers: d.Wagers, ready: d.Ready, log: d.Log}
	limiter := NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst, 5*time.Minute)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health/live", h.liveness)
	mux.HandleFunc("GET /health/ready", h.readiness)
	if d.Metrics != nil {
		mux.Handle("GET /metrics", d.Metrics)
	}

	authed := func(role string, fn http.HandlerFunc) http.Handler {
		return applyMiddlewares(fn, requireBearerToken(d.Verifier), limiter.Middleware, requireRole(role))
	}
	anyRole := func(fn http.HandlerFunc) http.Handler {
		return applyMiddlewares(fn, requireBearerToken(d.Verifier), limiter.Middleware)
	}

	mux.Handle("POST /wallets", authed(auth.RoleInternal, h.openWallet))
	mux.Handle("GET /wallets/{walletId}", authed(auth.RoleInternal, h.getWallet))
	mux.Handle("GET /wallets/{walletId}/ledger", authed(auth.RoleInternal, h.getLedger))
	mux.Handle("POST /wallets/{walletId}/reconciliation", authed(auth.RoleInternal, h.reconcile))

	mux.Handle("POST /wagering/transactions", anyRole(h.submitOperation))
	mux.Handle("GET /wagering/transactions/{transactionId}", anyRole(h.getTransaction))
	mux.Handle("GET /providers/{providerId}/wagering/transactions/{externalTransactionId}", anyRole(h.getProviderTransaction))

	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for range t.C {
			limiter.RemoveIdleBuckets(time.Now())
		}
	}()

	return applyMiddlewares(mux, withCorrelationID, logRequests(d.Log))
}
