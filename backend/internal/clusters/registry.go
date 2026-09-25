// Package clusters holds the set of Kubernetes clusters this backend is
// configured to talk to (Roadmap Phase 11) — one Entry per kubeconfig
// context, keyed by context name. It has no dependency on internal/api or
// internal/stream so both can depend on it without an import cycle.
package clusters

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// Entry is everything needed to talk to one configured cluster.
type Entry struct {
	Name             string
	Clientset        *kubernetes.Clientset
	RESTConfig       *rest.Config
	MetricsClientset *metricsclientset.Clientset
	GatewayClientset *gatewayclientset.Clientset
}

// Registry holds every cluster Vinaire is configured to talk to. Entries
// preserve configuration order; the first one added is the default used
// when a request doesn't specify a cluster.
type Registry struct {
	order   []string
	entries map[string]*Entry
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*Entry)}
}

// Add registers e under e.Name, keeping insertion order for Names/Default.
func (r *Registry) Add(e *Entry) {
	if _, exists := r.entries[e.Name]; !exists {
		r.order = append(r.order, e.Name)
	}
	r.entries[e.Name] = e
}

func (r *Registry) Get(name string) (*Entry, bool) {
	e, ok := r.entries[name]
	return e, ok
}

// Default is the first cluster added.
func (r *Registry) Default() *Entry {
	if len(r.order) == 0 {
		return nil
	}
	return r.entries[r.order[0]]
}

// Names lists every configured cluster in configuration order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// Resolve picks the Entry for name, or the Default if name is empty. ok is
// false only when a non-empty name doesn't match any configured cluster.
func (r *Registry) Resolve(name string) (*Entry, bool) {
	if name == "" {
		d := r.Default()
		return d, d != nil
	}
	return r.Get(name)
}

// Empty reports whether no cluster was successfully configured at all.
func (r *Registry) Empty() bool { return len(r.order) == 0 }
