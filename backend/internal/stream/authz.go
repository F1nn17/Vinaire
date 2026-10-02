package stream

import (
	"context"
	"log/slog"
	"sync"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/shiraku/vinaire/backend/internal/api"
)

// The informers behind the live-update stream watch the whole cluster with
// Vinaire's own credentials, so the events they produce are NOT scoped to what a
// connected user could list themselves. Before an event goes to a client it is
// therefore checked, with that client's own token, against the cluster's RBAC
// (Roadmap Phase 36): may this identity `list` this kind in this namespace? If
// not — or if that can't be determined — the event is dropped. Answers are
// cached for a few seconds per connection, so a burst of events costs one check
// per (kind, namespace), not one per event.

const authzCacheTTL = 10 * time.Second

type resourceRef struct{ group, resource string }

// kindResources maps an event's Kind to the RBAC resource it is a view of.
var kindResources = map[string]resourceRef{
	"pods":         {"", "pods"},
	"deployments":  {"apps", "deployments"},
	"statefulsets": {"apps", "statefulsets"},
	"daemonsets":   {"apps", "daemonsets"},
	"jobs":         {"batch", "jobs"},
	"nodes":        {"", "nodes"},
}

// accessCheck asks whether the connected identity may list group/resource in
// namespace ("" = cluster-wide).
type accessCheck func(ctx context.Context, group, resource, namespace string) (bool, error)

type cachedAnswer struct {
	allowed bool
	at      time.Time
}

type eventAuthorizer struct {
	check accessCheck
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]cachedAnswer
}

func newEventAuthorizer(check accessCheck) *eventAuthorizer {
	return &eventAuthorizer{check: check, now: time.Now, cache: map[string]cachedAnswer{}}
}

// eventNamespace is the namespace an event's row belongs to, and whether it could
// be told at all (an unknown row shape is refused rather than guessed).
func eventNamespace(evt Event) (string, bool) {
	switch row := evt.Row.(type) {
	case api.WorkloadRow:
		return row.Namespace, true
	case api.NodeRow:
		return "", true // nodes are cluster-scoped
	}
	return "", false
}

// allow reports whether evt may be sent to this connection's user.
func (a *eventAuthorizer) allow(ctx context.Context, evt Event) bool {
	ref, known := kindResources[evt.Kind]
	ns, shaped := eventNamespace(evt)
	if !known || !shaped {
		return false
	}
	key := evt.Kind + "/" + ns
	now := a.now()
	a.mu.Lock()
	if c, ok := a.cache[key]; ok && now.Sub(c.at) < authzCacheTTL {
		a.mu.Unlock()
		return c.allowed
	}
	a.mu.Unlock()

	allowed, err := a.check(ctx, ref.group, ref.resource, ns)
	if err != nil {
		// can't tell: refuse, and don't cache the failure
		slog.Debug("stream: access check failed, dropping the event", "kind", evt.Kind, "namespace", ns, "error", err)
		return false
	}
	a.mu.Lock()
	a.cache[key] = cachedAnswer{allowed: allowed, at: now}
	a.mu.Unlock()
	return allowed
}

// ssarCheck checks access with a SelfSubjectAccessReview made as the user the
// clientset belongs to.
func ssarCheck(cs *kubernetes.Clientset) accessCheck {
	return func(ctx context.Context, group, resource, namespace string) (bool, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		res, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authzv1.SelfSubjectAccessReview{
			Spec: authzv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authzv1.ResourceAttributes{
				Verb: "list", Group: group, Resource: resource, Namespace: namespace,
			}},
		}, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}
		return res.Status.Allowed, nil
	}
}

// AuditFunc records an action that has no plain HTTP request to hang an audit
// entry on (a WebSocket session); detail never contains credentials.
type AuditFunc func(action, name, identity string, detail map[string]any)
