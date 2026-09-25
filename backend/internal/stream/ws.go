package stream

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
)

// ServeWS upgrades the request to a WebSocket and streams every Event
// broadcast on hub to this client as JSON, until the client disconnects.
// allowedOrigins mirrors the CORS config (see internal/config) — the
// WebSocket handshake bypasses chi's CORS middleware, so origin-checking
// happens here instead, via the Upgrader.
func ServeWS(hub *Hub, allowedOrigins []string) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
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
