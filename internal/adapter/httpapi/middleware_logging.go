package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			la := &logAttrs{}
			r = r.WithContext(context.WithValue(r.Context(), logAttrsKey, la))
			next.ServeHTTP(rec, r)
			attrs := []any{
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"durationMs", time.Since(start).Milliseconds(), "correlationId", CorrelationID(r.Context()),
			}
			la.mu.Lock()
			attrs = append(attrs, la.attrs...)
			la.mu.Unlock()
			log.InfoContext(r.Context(), "http request", attrs...)
		})
	}
}
