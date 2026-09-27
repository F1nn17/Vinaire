package stream

import "sync"

// HubRegistry maps cluster name -> Hub, safe for concurrent reads (every
// WebSocket handshake, via ServeWS) and the rare writes that happen when a
// cluster is added/removed at runtime (cluster management via the web UI).
// Before that feature, `hubs` was a plain map built once at startup and
// never mutated again; now it can grow/shrink while requests are in flight.
type HubRegistry struct {
	mu   sync.RWMutex
	hubs map[string]*Hub
}

func NewHubRegistry() *HubRegistry {
	return &HubRegistry{hubs: make(map[string]*Hub)}
}

func (r *HubRegistry) Set(name string, hub *Hub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hubs[name] = hub
}

func (r *HubRegistry) Delete(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hubs, name)
}

func (r *HubRegistry) Get(name string) (*Hub, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hubs[name]
	return h, ok
}
