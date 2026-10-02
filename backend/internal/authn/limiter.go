package authn

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Login throttling (Roadmap Phase 36). A failed login is counted against the
// client's address and, separately, against the token that was tried, so
// neither guessing many tokens from one address nor hammering one token from
// many addresses gets far. Once a counter reaches its limit that key is
// refused for a while, and each further block lasts twice as long as the last
// (capped), which is the backoff. A success clears the counters.
const (
	ipFailLimit    = 20 // failures per window from one address
	tokenFailLimit = 5  // failures per window for one token
	failWindow     = 10 * time.Minute
	baseBlock      = time.Minute
	maxBlock       = 15 * time.Minute
	maxTracked     = 10000 // keys kept; older ones are dropped first
)

type attempts struct {
	fails        int
	windowStart  time.Time
	blockedUntil time.Time
	blocks       int
	lastSeen     time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	byIP    map[string]*attempts
	byToken map[string]*attempts
	now     func() time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{byIP: map[string]*attempts{}, byToken: map[string]*attempts{}, now: time.Now}
}

// tokenKey identifies a token for counting without keeping the token itself.
func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// clientIP is the address a login is counted against: the connection's peer,
// or — only when the deployment says a trusted proxy sets it
// (TRUST_FORWARDED_FOR) — the first X-Forwarded-For entry. Trusting that header
// without a proxy in front would let anyone pick their own "address".
func clientIP(r *http.Request, trustForwarded bool) string {
	if trustForwarded {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); net.ParseIP(first) != nil {
				return first
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *loginLimiter) get(m map[string]*attempts, key string) *attempts {
	a := m[key]
	if a == nil {
		if len(m) >= maxTracked {
			l.pruneLocked(m)
		}
		a = &attempts{}
		m[key] = a
	}
	return a
}

// pruneLocked drops entries that are idle and no longer blocked; if that isn't
// enough, the whole map (bounded memory beats perfect accounting).
func (l *loginLimiter) pruneLocked(m map[string]*attempts) {
	now := l.now()
	for k, a := range m {
		if now.After(a.blockedUntil) && now.Sub(a.lastSeen) > failWindow {
			delete(m, k)
		}
	}
	if len(m) >= maxTracked {
		clear(m)
	}
}

// allowed reports whether a login attempt may proceed now, and if not, for how
// long to wait.
func (l *loginLimiter) allowed(ip, tkey string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	var wait time.Duration
	for _, a := range []*attempts{l.byIP[ip], l.byToken[tkey]} {
		if a != nil && now.Before(a.blockedUntil) {
			if d := a.blockedUntil.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait == 0, wait
}

func (l *loginLimiter) fail(ip, tkey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	count := func(a *attempts, limit int) {
		a.lastSeen = now
		if now.Sub(a.windowStart) > failWindow {
			a.fails, a.windowStart = 0, now
		}
		a.fails++
		if a.fails >= limit {
			d := baseBlock << a.blocks
			if d > maxBlock || d <= 0 {
				d = maxBlock
			}
			a.blockedUntil = now.Add(d)
			a.blocks++
			a.fails = 0
		}
	}
	count(l.get(l.byIP, ip), ipFailLimit)
	count(l.get(l.byToken, tkey), tokenFailLimit)
}

func (l *loginLimiter) success(ip, tkey string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
	delete(l.byToken, tkey)
}
