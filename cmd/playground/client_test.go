package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeKeycloak(t *testing.T, issued *atomic.Int32, expiresIn int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/protocol/openid-connect/token") {
			http.NotFound(w, r)
			return
		}
		n := issued.Add(1)
		fmt.Fprintf(w, `{"access_token":"tok-%d","expires_in":%d}`, n, expiresIn)
	}))
}

func TestCallRenewsExpiredTokenOnce(t *testing.T) {
	var issued atomic.Int32
	kc := fakeKeycloak(t, &issued, 300)
	defer kc.Close()

	var apiCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if r.Header.Get("Authorization") == "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":"UNAUTHENTICATED","message":"invalid or expired token"}`)
			return
		}
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer api.Close()

	s := newSession()
	s.apiURL, s.keycloakURL = api.URL, kc.URL
	colorsEnabled = false

	r, err := s.call("GET", "/health/ready", "provider-a", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.status != 200 || issued.Load() != 2 || apiCalls.Load() != 2 {
		t.Errorf("status=%d tokens issued=%d api calls=%d", r.status, issued.Load(), apiCalls.Load())
	}
	r, _ = s.call("GET", "/health/ready", "provider-a", nil, nil)
	if r.status != 200 || issued.Load() != 2 {
		t.Errorf("second call must reuse the renewed token: status=%d issued=%d", r.status, issued.Load())
	}
}

func TestTokenIsRenewedBeforeExpiry(t *testing.T) {
	var issued atomic.Int32
	kc := fakeKeycloak(t, &issued, 300)
	defer kc.Close()
	s := newSession()
	s.keycloakURL = kc.URL

	if _, err := s.token("provider-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.token("provider-a"); err != nil || issued.Load() != 1 {
		t.Fatalf("a fresh token must be reused: issued=%d", issued.Load())
	}
	s.mu.Lock()
	s.tokens["provider-a"] = cachedToken{value: "old", expiresAt: time.Now().Add(5 * time.Second)}
	s.mu.Unlock()
	tok, err := s.token("provider-a")
	if err != nil || tok == "old" || issued.Load() != 2 {
		t.Errorf("token within 15s of expiry must be renewed: tok=%s issued=%d err=%v", tok, issued.Load(), err)
	}
}
