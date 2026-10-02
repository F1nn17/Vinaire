package stream

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/clusters"
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
// This Hub's informers watch with the backend's own credentials, so what they
// see is the whole cluster. Each event is therefore checked, as the connected
// user, against the cluster's RBAC before it is sent (see authz.go): a
// namespace the user's own token can't list never surfaces here either.
// authn.ModeDisabled has no user identity and skips the check.
func ServeWS(hubs *HubRegistry, registry *clusters.Registry, defaultCluster string, allowedOrigins []string, auth *authn.Manager) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := auth.Authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		clusterName := r.URL.Query().Get("cluster")
		if clusterName == "" {
			clusterName = defaultCluster
		}
		var authorizer *eventAuthorizer
		if auth.Mode != authn.ModeDisabled {
			cs, err := clientsetFor(registry, auth, sess, clusterName)
			if err != nil {
				http.Error(w, "unknown cluster", http.StatusBadRequest)
				return
			}
			authorizer = newEventAuthorizer(ssarCheck(cs))
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
				if authorizer != nil && !authorizer.allow(r.Context(), evt) {
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
