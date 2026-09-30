package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
)

func TestRateLimiterPerSubject(t *testing.T) {
	l := NewRateLimiter(1, 2, time.Minute)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func(sub string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if sub != "" {
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{Subject: sub}))
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if call("alice") != 200 || call("alice") != 200 {
		t.Fatal("burst should pass")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(auth.WithPrincipal(httptest.NewRequest("GET", "/", nil).Context(), auth.Principal{Subject: "alice"}))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}

	if call("bob") != 200 {
		t.Error("other subjects must have their own bucket")
	}

	if call("") != 200 || call("") != 200 || call("") != 429 {
		t.Error("ip bucket should behave like subject bucket")
	}
}

func TestRateLimiterSweep(t *testing.T) {
	l := NewRateLimiter(10, 10, time.Millisecond)
	l.allowRequest("a", time.Now())
	l.allowRequest("b", time.Now())
	l.RemoveIdleBuckets(time.Now().Add(time.Second))
	if len(l.buckets) != 0 {
		t.Errorf("expected buckets swept, got %d", len(l.buckets))
	}
}

func TestRateLimiterReset(t *testing.T) {
	l := NewRateLimiter(1, 1, time.Minute)
	if ok, _ := l.allowRequest("sub:alice", time.Now()); !ok {
		t.Fatal("first request should pass")
	}
	if ok, _ := l.allowRequest("sub:alice", time.Now()); ok {
		t.Fatal("second request should be throttled")
	}
	if !l.Reset("alice") {
		t.Error("reset should report the bucket existed")
	}
	if ok, _ := l.allowRequest("sub:alice", time.Now()); !ok {
		t.Error("request after reset should pass")
	}
	if l.Reset("nobody") {
		t.Error("unknown subject reports false")
	}
	l.allowRequest("sub:bob", time.Now())
	if n := l.ResetAll(); n != 2 {
		t.Errorf("reset all: %d", n)
	}
}
