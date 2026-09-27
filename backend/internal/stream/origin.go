package stream

import (
	"net/http"
	"net/url"
)

// originChecker builds a gorilla/websocket CheckOrigin function that mirrors
// the CORS config (see internal/config) — the WebSocket handshake bypasses
// chi's CORS middleware, so every WS upgrade in this package origin-checks
// via this instead.
//
// A same-origin request (Origin's host matches the request's own Host) is
// always allowed, regardless of allowedOrigins. The single combined
// production image (Roadmap Phase 10) serves the frontend and API from the
// same host — a browser never applies CORS to that plain fetch() traffic
// either, but the WebSocket handshake's Origin header still gets sent and
// still needs an explicit check, so without this a same-origin deployment
// (e.g. behind an Ingress host that was never known ahead of time to put
// in CORS_ALLOWED_ORIGINS) 403s on every WS upgrade — this broke Pod logs,
// the Pod terminal, and Phase 2's live table updates identically, since all
// three share this same checker.
func originChecker(allowedOrigins []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (curl, tests) don't send Origin.
			return true
		}
		if u, err := url.Parse(origin); err == nil && u.Host == r.Host {
			return true
		}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		return false
	}
}
