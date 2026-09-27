// Package stream fans out live Kubernetes informer events (added/modified/
// deleted) to connected WebSocket clients. internal/k8s stays a one-shot
// read API; this package is the long-lived-watch counterpart, feeding
// Roadmap Phase 2's "live updates" slice.
package stream

import (
	"log/slog"
	"sync"
)

// Event is one add/modify/delete diff for a single resource, broadcast to
// every connected WebSocket client as JSON. Row's concrete shape depends on
// Kind (api.WorkloadRow for "pods"/"deployments"/"statefulsets"/
// "daemonsets"/"jobs", api.NodeRow for "nodes") — kept as `any` rather than
// a shared struct since different resource kinds' rows genuinely have
// different fields (see registerInformer's generic rowOf parameter).
type Event struct {
	Type string `json:"type"` // "added" | "modified" | "deleted"
	Kind string `json:"kind"` // "pods" | "deployments" | "statefulsets" | "daemonsets" | "jobs" | "nodes"
	Row  any    `json:"row"`
}

// eventBufferSize bounds how many events a single slow client can lag
// behind before Hub starts dropping events for it, rather than blocking
// every other client (or the informer event loop) on one stuck connection.
const eventBufferSize = 32

// Hub is a simple in-process pub/sub: informers publish Events, connected
// WebSocket handlers subscribe to receive every Event broadcast afterward.
type Hub struct {
	mu      sync.Mutex
	clients map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan Event]struct{})}
}

func (h *Hub) subscribe() chan Event {
	ch := make(chan Event, eventBufferSize)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Broadcast sends evt to every currently-subscribed client. A client whose
// buffer is full has the event dropped for it rather than blocking the
// informer event loop that all other clients (and cache updates) depend on.
func (h *Hub) Broadcast(evt Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.clients {
		select {
		case ch <- evt:
		default:
			slog.Warn("stream: client too slow, dropping event", "kind", evt.Kind, "type", evt.Type)
		}
	}
}
