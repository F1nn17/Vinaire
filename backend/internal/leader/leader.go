// Package leader elects one replica to do the work that must happen once (Roadmap
// Phase 48): with several Vinaire replicas, the GitOps poller would otherwise run in
// every one of them. It uses a coordination.k8s.io Lease in Vinaire's own namespace.
package leader

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// vars so a test can shorten them
var (
	leaseDuration = 30 * time.Second
	renewDeadline = 20 * time.Second
	retryPeriod   = 5 * time.Second
)

// LeaseName is the one Lease Vinaire uses (the chart's Role names it).
const LeaseName = "vinaire-leader"

// Elector says whether this replica is the leader. The zero value of a nil *Elector
// means "no election": every replica counts as leader (a single replica, or a process
// outside a cluster).
type Elector struct{ leading atomic.Bool }

// IsLeader is true for the elected replica — and always when no election is running.
func (e *Elector) IsLeader() bool { return e == nil || e.leading.Load() }

// Run starts competing for the Lease and returns at once; it keeps trying until ctx
// ends (a replica that loses the lease goes back to competing).
func Run(ctx context.Context, cs kubernetes.Interface, namespace, identity string) *Elector {
	e := &Elector{}
	lock := &resourcelock.LeaseLock{
		LeaseMeta:  metav1.ObjectMeta{Name: LeaseName, Namespace: namespace},
		Client:     cs.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: identity},
	}
	go func() {
		for ctx.Err() == nil {
			leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
				Lock:            lock,
				ReleaseOnCancel: true,
				LeaseDuration:   leaseDuration,
				RenewDeadline:   renewDeadline,
				RetryPeriod:     retryPeriod,
				Callbacks: leaderelection.LeaderCallbacks{
					OnStartedLeading: func(context.Context) {
						e.leading.Store(true)
						slog.Info("this replica is now the leader (GitOps polling runs here)", "identity", identity)
					},
					OnStoppedLeading: func() {
						e.leading.Store(false)
						slog.Info("this replica is no longer the leader", "identity", identity)
					},
				},
			})
		}
	}()
	return e
}
