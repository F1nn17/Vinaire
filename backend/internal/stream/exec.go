package stream

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// shell is the command exec'd into the container. Distroless/scratch images
// have no shell at all — that's a real failure mode (see risk notes in
// ROADMAP.md), surfaced to the client as a session-ended message rather
// than a silently hung connection.
var shell = []string{"/bin/sh"}

// execControlMessage is what the frontend sends over the exec WebSocket —
// either a chunk of typed input or a terminal resize. Server-to-client
// messages are raw PTY output bytes, not JSON (xterm.js just wants to
// `write()` them directly).
type execControlMessage struct {
	Type string `json:"type"` // "stdin" | "resize"
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// ServeExec upgrades to a WebSocket and bridges it to a TTY exec session
// inside a container (the `kubectl exec -it ... -- /bin/sh` equivalent),
// using client-go's SPDY executor against the pod's exec subresource. Route
// params: {namespace}/{name}; optional `?container=` query param.
func ServeExec(clientset *kubernetes.Clientset, restConfig *rest.Config, allowedOrigins []string) http.HandlerFunc {
	upgrader := websocket.Upgrader{CheckOrigin: originChecker(allowedOrigins)}

	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")
		container := resolveContainer(r.Context(), clientset, namespace, name, r.URL.Query().Get("container"))

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("exec: websocket upgrade failed", "error", err)
			return
		}
		defer conn.Close()

		req := clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(namespace).
			Name(name).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: container,
				Command:   shell,
				Stdin:     true,
				Stdout:    true,
				Stderr:    true,
				TTY:       true,
			}, scheme.ParameterCodec)

		executor, err := remotecommand.NewSPDYExecutor(restConfig, http.MethodPost, req.URL())
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("exec setup failed: "+err.Error()+"\r\n"))
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		stdin := &wsStdin{ctx: ctx, msgs: make(chan []byte, 16)}
		sizeQueue := &wsTerminalSizeQueue{sizes: make(chan remotecommand.TerminalSize, 4)}

		go readClientMessages(ctx, cancel, conn, stdin.msgs, sizeQueue.sizes)

		out := &wsWriter{conn: conn}
		err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             stdin,
			Stdout:            out,
			Stderr:            out,
			Tty:               true,
			TerminalSizeQueue: sizeQueue,
		})
		if err != nil && ctx.Err() == nil {
			slog.Warn("exec: session ended", "namespace", namespace, "name", name, "container", container, "error", err)
			_ = out.writeText("\r\n[session ended: " + err.Error() + "]\r\n")
		}
	}
}

// readClientMessages decodes stdin/resize control messages from the client
// until it disconnects, then cancels ctx so the exec session tears down
// promptly instead of hanging on a dead connection.
func readClientMessages(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stdin chan<- []byte, sizes chan<- remotecommand.TerminalSize) {
	defer cancel()
	defer close(stdin)
	defer close(sizes)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg execControlMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "stdin":
			select {
			case stdin <- []byte(msg.Data):
			case <-ctx.Done():
				return
			}
		case "resize":
			select {
			case sizes <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// wsStdin adapts a channel of client-typed byte chunks into the io.Reader
// remotecommand.StreamOptions.Stdin expects, buffering any part of a chunk
// that doesn't fit the caller's read buffer in one call.
type wsStdin struct {
	ctx     context.Context
	msgs    chan []byte
	pending []byte
}

func (s *wsStdin) Read(p []byte) (int, error) {
	if len(s.pending) == 0 {
		select {
		case data, ok := <-s.msgs:
			if !ok {
				return 0, io.EOF
			}
			s.pending = data
		case <-s.ctx.Done():
			return 0, io.EOF
		}
	}

	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

// wsWriter adapts a WebSocket connection into the io.Writer
// remotecommand.StreamOptions.Stdout/Stderr expect. Both stdout and stderr
// share one instance here (and thus one mutex) since only one goroutine may
// write to a gorilla/websocket connection at a time.
type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsWriter) writeText(s string) error {
	_, err := w.Write([]byte(s))
	return err
}

// wsTerminalSizeQueue adapts client resize messages into the
// remotecommand.TerminalSizeQueue the SPDY executor polls for PTY resizes
// (called once immediately for the initial size, then again per resize).
type wsTerminalSizeQueue struct {
	sizes chan remotecommand.TerminalSize
}

func (q *wsTerminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.sizes
	if !ok {
		return nil
	}
	return &size
}
