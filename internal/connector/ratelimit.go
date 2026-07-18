package connector

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a best-effort per-IP fixed-window limiter for the OAuth
// endpoints. The real defenses are 256-bit tokens and single-use codes;
// this only blunts brute-force noise per instance (Cloud Run instances
// don't share state — that's fine for a best-effort layer).
type rateLimiter struct {
	mu      sync.Mutex
	perMin  int
	window  time.Duration
	started time.Time
	counts  map[string]int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{perMin: limit, window: window, started: time.Now(), counts: map[string]int{}}
}

func (l *rateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.started) > l.window {
		l.started, l.counts = time.Now(), map[string]int{}
	}
	l.counts[ip]++
	return l.counts[ip] <= l.perMin
}

func (s *server) rateLimited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limit.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// clientIP prefers the first X-Forwarded-For hop (what Google Frontends
// prepend on Cloud Run) and falls back to the socket peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok || first != "" {
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
