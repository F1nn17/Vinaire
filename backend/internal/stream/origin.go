package stream

import "net/http"

// originChecker builds a gorilla/websocket CheckOrigin function that mirrors
// the CORS config (see internal/config) — the WebSocket handshake bypasses
// chi's CORS middleware, so every WS upgrade in this package origin-checks
// via this instead.
func originChecker(allowedOrigins []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (curl, tests) don't send Origin.
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
