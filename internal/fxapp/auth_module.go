package fxapp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
	"github.com/pedroegerland/wager-service/internal/config"
)

var AuthModule = fx.Module("auth",
	fx.Provide(func(lc fx.Lifecycle, cfg config.Config, log *slog.Logger) (*auth.Verifier, error) {
		v, err := auth.NewVerifier(context.Background(), auth.Config{
			Issuer: cfg.OIDCIssuer, JWKSURL: cfg.OIDCJWKSURL, Audience: cfg.OIDCAudience,
		})
		if err != nil {
			return nil, err
		}

		lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
			return waitForDependency(ctx, 60, time.Second, func() error { return fetchJWKS(ctx, cfg.OIDCJWKSURL) }, log, "oidc")
		}})
		return v, nil
	}),
)

func fetchJWKS(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: status %d", resp.StatusCode)
	}
	return nil
}
