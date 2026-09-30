package httpapi

import (
	"context"
	"net/http"
	"time"
)

type ReadinessChecker interface {
	Ready(ctx context.Context) map[string]error
}

func (h *handlers) liveness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handlers) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := h.ready.Ready(ctx)
	out := map[string]string{}
	status := http.StatusOK
	for name, err := range checks {
		if err != nil {
			out[name] = "down: " + err.Error()
			status = http.StatusServiceUnavailable
		} else {
			out[name] = "ok"
		}
	}
	writeJSON(w, status, out)
}
