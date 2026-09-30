package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
)

type tokenVerifier interface {
	Verify(ctx context.Context, raw string) (auth.Principal, error)
}

func requireBearerToken(v tokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(h), "bearer ") || len(h) < 8 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="wager"`)
				writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing bearer token")
				return
			}
			p, err := v.Verify(r.Context(), strings.TrimSpace(h[7:]))
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="wager", error="invalid_token"`)
				writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid or expired token")
				return
			}
			addLogAttrs(r.Context(), "clientId", p.ClientID, "providerId", p.ProviderID)
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		})
	}
}

func requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFrom(r.Context())
			if !ok || !p.HasRole(role) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
