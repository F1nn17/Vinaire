package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// Roadmap Phase 26: reach a pod's port through the Kubernetes API's
// portforward subresource — the `kubectl port-forward` mechanism — as a
// net.Conn, WITHOUT opening any local OS listener on this server. This is a
// read-style access path (nothing in the cluster is mutated); whether the
// caller may use it is the real RBAC on `pods/portforward` for their own
// identity.

// errorStreamGrace is how long a finished read waits for an error-stream message.
const errorStreamGrace = 250 * time.Millisecond

// ActivePortForwardConns is how many pod connections are currently open.
// Exposed so tests (and logs) can confirm sessions really close.
var ActivePortForwardConns atomic.Int64

// DialPodPort opens one SPDY port-forward connection to namespace/pod:port and
// returns it as a net.Conn. Closing the returned conn tears down the whole
// SPDY connection, so nothing outlives the caller that dialed it.
func DialPodPort(ctx context.Context, clientset *kubernetes.Clientset, cfg *rest.Config, namespace, pod string, port int) (net.Conn, error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("portforward")

	rt, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("port-forward transport: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: rt}, http.MethodPost, req.URL())

	type result struct {
		conn httpstream.Connection
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
		done <- result{c, err}
	}()
	var streamConn httpstream.Connection
	select {
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("port-forward to %s/%s: %w", namespace, pod, r.err)
		}
		streamConn = r.conn
	case <-ctx.Done():
		// The dial may still finish; make sure whatever it opens is closed.
		go func() {
			if r := <-done; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}

	headers := http.Header{}
	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	headers.Set(corev1.PortHeader, strconv.Itoa(port))
	headers.Set(corev1.PortForwardRequestIDHeader, "0")
	errorStream, err := streamConn.CreateStream(headers)
	if err != nil {
		_ = streamConn.Close()
		return nil, fmt.Errorf("creating error stream: %w", err)
	}
	// We never write to the error stream.
	_ = errorStream.Close()

	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	dataStream, err := streamConn.CreateStream(headers)
	if err != nil {
		_ = streamConn.Close()
		return nil, fmt.Errorf("creating data stream: %w", err)
	}

	pc := &podConn{
		conn:    streamConn,
		data:    dataStream,
		errorCh: make(chan string, 1),
		addr:    podAddr(fmt.Sprintf("%s/%s:%d", namespace, pod, port)),
	}
	// The kubelet reports "connection refused" and similar on the error
	// stream; surface that instead of a bare EOF.
	go func() {
		msg, _ := io.ReadAll(errorStream)
		if len(msg) > 0 {
			pc.errorCh <- string(msg)
			slog.Debug("port-forward error stream", "target", pc.addr, "message", string(msg))
		}
	}()

	ActivePortForwardConns.Add(1)
	slog.Debug("port-forward connection opened", "target", pc.addr, "active", ActivePortForwardConns.Load())
	return pc, nil
}

type podAddr string

func (a podAddr) Network() string { return "k8s-portforward" }
func (a podAddr) String() string  { return string(a) }

// podConn adapts a port-forward data stream to net.Conn so an http.Transport
// can dial through it. SPDY streams have no deadlines, so those are no-ops —
// lifetime is bounded by the caller's context and Close instead.
type podConn struct {
	conn      httpstream.Connection
	data      httpstream.Stream
	errorCh   chan string
	addr      podAddr
	closeOnce sync.Once
	received  atomic.Bool
}

func (c *podConn) Read(p []byte) (int, error) {
	n, err := c.data.Read(p)
	if n > 0 {
		c.received.Store(true)
	}
	// A refused connection never yields data; a normal close after a response
	// has, so it skips the wait and adds no latency.
	if err != nil && !c.received.Load() {
		// The kubelet's explanation (e.g. "connection refused") arrives on the
		// error stream a moment after the data stream ends — give it a beat so
		// the caller gets a real reason, not a bare EOF.
		select {
		case msg := <-c.errorCh:
			return n, errors.New(msg)
		case <-time.After(errorStreamGrace):
		}
	}
	return n, err
}

func (c *podConn) Write(p []byte) (int, error) { return c.data.Write(p) }

func (c *podConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.data.Reset()
		_ = c.conn.Close()
		slog.Debug("port-forward connection closed", "target", c.addr, "active", ActivePortForwardConns.Add(-1))
	})
	return nil
}

func (c *podConn) LocalAddr() net.Addr              { return c.addr }
func (c *podConn) RemoteAddr() net.Addr             { return c.addr }
func (c *podConn) SetDeadline(time.Time) error      { return nil }
func (c *podConn) SetReadDeadline(time.Time) error  { return nil }
func (c *podConn) SetWriteDeadline(time.Time) error { return nil }
