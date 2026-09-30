package httpapi

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
)

type RateLimiter struct {
	rps   rate.Limit
	burst int
	ttl   time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func NewRateLimiter(rps float64, burst int, ttl time.Duration) *RateLimiter {
	if burst <= 0 {
		burst = 1
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RateLimiter{rps: rate.Limit(rps), burst: burst, ttl: ttl, buckets: map[string]*bucket{}}
}

func (l *RateLimiter) allowRequest(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	r := b.lim.ReserveN(now, 1)
	if !r.OK() {
		return false, time.Second
	}
	delay := r.DelayFrom(now)
	if delay > 0 {
		r.CancelAt(now)
		return false, delay
	}
	return true, 0
}

func (l *RateLimiter) RemoveIdleBuckets(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, b := range l.buckets {
		if now.Sub(b.seen) > l.ttl {
			delete(l.buckets, k)
		}
	}
}

func (l *RateLimiter) Reset(subject string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.buckets["sub:"+subject]
	delete(l.buckets, "sub:"+subject)
	return ok
}

func (l *RateLimiter) ResetAll() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.buckets)
	l.buckets = map[string]*bucket{}
	return n
}

func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + clientIPOf(r)
		if p, ok := auth.PrincipalFrom(r.Context()); ok && p.Subject != "" {
			key = "sub:" + p.Subject
		}
		ok, wait := l.allowRequest(key, time.Now())
		if !ok {
			secs := int(wait.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIPOf(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
