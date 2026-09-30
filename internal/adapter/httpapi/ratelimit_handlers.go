package httpapi

import "net/http"

func (h *handlers) resetRateLimit(l *RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject := r.PathValue("subject")
		found := l.Reset(subject)
		h.log.InfoContext(r.Context(), "rate limit reset", "subject", subject, "found", found, "correlationId", CorrelationID(r.Context()))
		writeJSON(w, http.StatusOK, map[string]any{"subject": subject, "reset": found})
	}
}

func (h *handlers) resetAllRateLimits(l *RateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := l.ResetAll()
		h.log.InfoContext(r.Context(), "all rate limits reset", "buckets", n, "correlationId", CorrelationID(r.Context()))
		writeJSON(w, http.StatusOK, map[string]any{"reset": n})
	}
}
