package api

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/shiraku/vinaire/backend/internal/authn"
)

// WriteRateLimiter throttles the three Phase 9 write endpoints (restart/
// delete/rollback) per logged-in identity — a simple token bucket, not a
// general API gateway. Keying on identity rather than source IP matters
// once real auth exists (Phase 10): multiple users behind the same NAT/
// proxy would otherwise throttle each other.
type WriteRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	limit    rate.Limit
	burst    int
}

// NewWriteRateLimiter allows burst immediate requests, refilling at limit
// (requests/sec) thereafter, independently per identity.
func NewWriteRateLimiter(limit rate.Limit, burst int) *WriteRateLimiter {
	return &WriteRateLimiter{limiters: make(map[string]*rate.Limiter), limit: limit, burst: burst}
}

func (rl *WriteRateLimiter) limiterFor(identity string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	l, ok := rl.limiters[identity]
	if !ok {
		l = rate.NewLimiter(rl.limit, rl.burst)
		rl.limiters[identity] = l
	}
	return l
}

// Middleware rejects a request over the caller's rate limit with a real
// 429, rather than silently dropping or slow-rolling it.
func (rl *WriteRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := "anonymous"
		if sess, ok := authn.FromContext(r.Context()); ok {
			switch {
			case sess.Username != "":
				identity = sess.Username
			case sess.ID != "":
				identity = sess.ID
			}
		}

		if !rl.limiterFor(identity).Allow() {
			respondError(w, http.StatusTooManyRequests, "rate limit exceeded for write actions — slow down and try again")
			return
		}
		next.ServeHTTP(w, r)
	})
}
