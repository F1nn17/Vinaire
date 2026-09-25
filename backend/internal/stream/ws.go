package stream

import (
	"log/slog"
	"net/http"
	"slices"

	"github.com/gorilla/websocket"

	"github.com/shiraku/vinaire/backend/internal/authz"
)

// ServeWS upgrades the request to a WebSocket and streams every Event
// broadcast on the selected cluster's hub to this client as JSON, until the
// client disconnects. allowedOrigins mirrors the CORS config (see
// internal/config) — the WebSocket handshake bypasses chi's CORS
// middleware, so origin-checking happens here instead, via the Upgrader.
//
// A browser's WebSocket API can't attach an Authorization header, so the
// caller's token travels as `?access_token=` instead (see
// authz.Verifier.VerifyRequest) and the target cluster as `?cluster=`
// (Roadmap Phase 11) — both are query params on the same handshake URL.
// Events for namespaces the caller's groups don't grant "read" on
// (Roadmap Phase 10) are silently dropped rather than sent and filtered
// client-side, so an unauthorized namespace's data never reaches the
// browser at all.
func ServeWS(hubs map[string]*Hub, defaultCluster string, allowedOrigins []string, verifier *authz.Verifier, policy *authz.Policy) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := verifier.VerifyRequest(r)
		if err != nil {
			http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		clusterName := r.URL.Query().Get("cluster")
		if clusterName == "" {
			clusterName = defaultCluster
		}
		hub, ok := hubs[clusterName]
		if !ok {
			http.Error(w, "unknown cluster", http.StatusBadRequest)
			return
		}
		allowedNamespaces, allowAll := policy.AllowedNamespaces(claims, "read")

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
				if !allowAll && !slices.Contains(allowedNamespaces, evt.Row.Namespace) {
					continue
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
