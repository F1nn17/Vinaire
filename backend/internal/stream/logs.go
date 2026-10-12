package stream

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// logLine matches the frontend's LogLine type (frontend/src/lib/types.ts).
type logLine struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// tailLines caps how much log history a newly opened stream backfills
// before switching to following live output — enough to orient, not a full
// history dump.
const tailLines = 200

// ServeLogs upgrades to a WebSocket and streams one container's log lines
// (the `kubectl logs -f` equivalent) to the client as they're written,
// until the client disconnects or the pod's log stream ends. Route params:
// {namespace}/{name}; optional `?container=` and `?cluster=` (Roadmap
// Phase 11) query params. The session cookie (Roadmap Phase 10) rides
// along on the WebSocket handshake automatically, same as any other HTTP
// request — no query-param token needed. The resulting clientset
// authenticates as the caller's own logged-in identity, so an
// unauthorized pod/namespace fails with a real 403 from the Kubernetes API
// itself when GetLogs is called below, same as `kubectl logs` would.
func ServeLogs(registry *clusters.Registry, auth *authn.Manager, allowedOrigins []string) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := auth.Authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		namespace := chi.URLParam(r, "namespace")
		clientset, err := clientsetFor(registry, auth, sess, r.URL.Query().Get("cluster"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		name := chi.URLParam(r, "name")
		container := resolveContainer(r.Context(), clientset, namespace, name, r.URL.Query().Get("container"))

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("logs: websocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Cancel the log stream promptly once the client disconnects.
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					cancel()
					return
				}
			}
		}()

		tail := int64(tailLines)
		readCloser, err := clientset.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
			Container:  container,
			Follow:     true,
			Timestamps: true,
			TailLines:  &tail,
		}).Stream(ctx)
		if err != nil {
			slog.Warn("logs: stream failed", "namespace", namespace, "name", name, "container", container, "error", err)
			_ = conn.WriteJSON(logLine{Level: "ERROR", Text: err.Error()})
			return
		}
		defer readCloser.Close()

		scanner := bufio.NewScanner(readCloser)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if err := conn.WriteJSON(parseLogLine(scanner.Text())); err != nil {
				return
			}
		}
	}
}

// parseLogLine splits a kubelet timestamped log line ("<RFC3339Nano
// timestamp> <message>", from PodLogOptions.Timestamps) into a LogLine.
// The log level is a best-effort heuristic — most raw container output
// doesn't carry a machine-readable level, so this greps the message for
// common level words rather than pretending to parse an arbitrary format.
func parseLogLine(raw string) logLine {
	ts, text := "", raw
	if i := strings.IndexByte(raw, ' '); i > 0 {
		if t, err := time.Parse(time.RFC3339Nano, raw[:i]); err == nil {
			ts = t.Format("15:04:05")
			text = raw[i+1:]
		}
	}

	return logLine{
		Time:  ts,
		Level: guessLogLevel(text),
		Text:  text,
	}
}

func guessLogLevel(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "fatal"), strings.Contains(lower, "panic"):
		return "ERROR"
	case strings.Contains(lower, "warn"):
		return "WARN"
	default:
		return "INFO"
	}
}
