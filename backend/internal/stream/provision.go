package stream

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/provision"
)

// ServeProvision streams a node-provisioning run's progress (Roadmap Phase 30)
// over a WebSocket: first everything logged so far (so a reconnecting client
// catches up — `?after=<seq>` skips what it already has), then live events
// until the run ends. Only the identity that started the run can watch it. The
// stream carries progress text only; credentials never pass through here (they
// arrive once, in the POST body that started the run).
func ServeProvision(reg *provision.Registry, auth *authn.Manager, allowedOrigins []string) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := auth.Authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		job, ok := reg.Get(chi.URLParam(r, "id"), sess.OwnerKey())
		if !ok {
			http.Error(w, "no such provisioning run", http.StatusNotFound)
			return
		}
		after := -1
		if v := r.URL.Query().Get("after"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				after = n
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("provision: websocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		replay, live, cancel := job.Subscribe(after)
		defer cancel()

		ctx, stop := context.WithCancel(r.Context())
		defer stop()
		// Detect the client going away (we never expect to read from it).
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					stop()
					return
				}
			}
		}()

		for _, ev := range replay {
			if err := conn.WriteJSON(ev); err != nil {
				return
			}
		}
		for {
			select {
			case ev, open := <-live:
				if !open {
					return
				}
				if err := conn.WriteJSON(ev); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}
