package stream

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/shiraku/vinaire/backend/internal/authn"
)

// ServeWS upgrades the request to a WebSocket and streams every Event
// broadcast on the selected cluster's hub to this client as JSON, until the
// client disconnects. allowedOrigins mirrors the CORS config (see
// internal/config) — the WebSocket handshake bypasses chi's CORS
// middleware, so origin-checking happens here instead, via the Upgrader.
//
// Unlike a REST call, a WebSocket handshake is still a plain HTTP request,
// so the session cookie (Roadmap Phase 10) rides along automatically —
// no query-param token workaround needed. The target cluster still comes
// from `?cluster=` (Roadmap Phase 11), since that isn't something a cookie
// can carry.
//
// This Hub's informers watch with the backend's own credentials (a Phase 2
// design predating per-user auth, unchanged here) — the stream isn't
// scoped to what the caller's own RBAC would let them `list`, only to
// "logged in or not". A namespace a user's own token can't otherwise list
// via REST may still surface here. Narrowing that would mean per-user
// informers (expensive) or a per-event RBAC check (also expensive); out of
// scope for this phase, flagged for whoever revisits this stream's trust
// model next.
func ServeWS(hubs *HubRegistry, defaultCluster string, allowedOrigins []string, auth *authn.Manager) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.Authenticate(r); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clusterName := r.URL.Query().Get("cluster")
		if clusterName == "" {
			clusterName = defaultCluster
		}
		hub, ok := hubs.Get(clusterName)
		if !ok {
			http.Error(w, "unknown cluster", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("stream: websocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		events := hub.subscribe()
		defer hub.unsubscribe(events)

		// gorilla/websocket requires an active reader to process control
		// frames (ping/pong/close) and to notice the peer disconnecting;
		// this connection is otherwise server-push-only, so incoming
		// messages are read and discarded.
		clientGone := make(chan struct{})
		go func() {
			defer close(clientGone)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for {
			select {
			case evt, ok := <-events:
				if !ok {
					return
				}
				if err := conn.WriteJSON(evt); err != nil {
					return
				}
			case <-clientGone:
				return
			}
		}
	}
}
