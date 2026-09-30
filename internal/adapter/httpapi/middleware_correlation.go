package httpapi

import (
	"context"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

type ctxKey int

const (
	correlationKey ctxKey = iota
	logAttrsKey
)

func CorrelationID(ctx context.Context) string {
	s, _ := ctx.Value(correlationKey).(string)
	return s
}

type logAttrs struct {
	mu    sync.Mutex
	attrs []any
}

func addLogAttrs(ctx context.Context, kv ...any) {
	if la, ok := ctx.Value(logAttrsKey).(*logAttrs); ok {
		la.mu.Lock()
		la.attrs = append(la.attrs, kv...)
		la.mu.Unlock()
	}
}

func withCorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" || len(id) > 128 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Correlation-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), correlationKey, id)))
	})
}
