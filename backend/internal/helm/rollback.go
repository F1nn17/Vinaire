package helm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Roadmap Phase 60: stopping an install that has started creating objects, and taking back
// what it made.
//
// Everything here runs as the caller's own *rest.Config, like the install it undoes, and uses
// only what Helm already does — `helm uninstall` of the release this job itself created. Two
// guards keep it from ever touching something that isn't the job's:
//   - the release is only uninstalled if, just before the install began, no release of that
//     name existed (otherwise the install fails on its own, and the existing release is left
//     alone);
//   - the namespace is never deleted here. If the install created it, the job says so and the
//     interface offers the existing, audited namespace delete (Phase 27), which asks for the
//     name to be typed.
// PersistentVolumeClaims a chart's StatefulSets made are not removed by Helm either; they are
// listed (read as the caller) so nothing stays behind unnoticed.

const maxLeftovers = 50

// rollbackSettle is how long a rollback waits after interrupting Helm before it uninstalls.
// Helm keeps applying the chart in the background after its context is cancelled (its install
// returns at once, the work does not stop), so uninstalling in the same instant would race the
// objects still being created. A variable so tests needn't wait.
var rollbackSettle = 5 * time.Second

// prepareInstall runs just before Helm starts: it refuses a name that is already taken (so a
// later rollback can only ever remove this job's own release) and notes whether the install
// will create the namespace.
func prepareInstall(cfg *rest.Config, j *InstallJob) error {
	req := j.Request
	ac, err := configuration(cfg, req.Namespace)
	if err != nil {
		return err
	}
	_, err = action.NewGet(ac).Run(req.Release)
	switch {
	case err == nil:
		return fmt.Errorf("%w: a release with that name already exists in this namespace", ErrConflict)
	case errors.Is(err, driver.ErrReleaseNotFound):
		j.mu.Lock()
		j.ownsRelease = true
		j.mu.Unlock()
	default:
		// can't tell (no right to read releases here): the install will judge; without a
		// check the job won't claim the name, so it won't uninstall anything either
	}
	if req.CreateNamespace {
		if cs, err := kubernetes.NewForConfig(cfg); err == nil {
			if _, err := cs.CoreV1().Namespaces().Get(context.Background(), req.Namespace, metav1.GetOptions{}); apierrors.IsNotFound(err) {
				j.mu.Lock()
				j.nsCreated = true
				j.mu.Unlock()
			}
		}
	}
	return nil
}

// rollback uninstalls the job's release and lists what Helm leaves behind. It is the same
// whether the user cancelled mid-install or asks to undo a failed one.
func (js *Jobs) rollback(rl *Releases, cfg *rest.Config, j *InstallJob, interrupted bool) {
	j.mu.Lock()
	j.stage = StageRollingBack
	req := j.Request
	owns := j.ownsRelease
	j.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout+time.Minute)
	defer cancel()

	if interrupted {
		time.Sleep(rollbackSettle)
	}
	var err error
	if owns {
		err = rl.Uninstall(ctx, cfg, req.Namespace, req.Release)
		if errors.Is(err, ErrNotFound) {
			err = nil // Helm never got as far as recording the release: nothing to take back
		}
	} else {
		err = errors.New("the release name was not checked before the install, so nothing was uninstalled")
	}
	left := leftoverClaims(ctx, cfg, req.Namespace, req.Release)

	j.mu.Lock()
	j.rolledBack = err == nil
	j.rollbackErr = err
	j.leftovers = left
	hook := j.onRollback
	j.mu.Unlock()
	if hook != nil {
		hook(err)
	}
}

// leftoverClaims names the PersistentVolumeClaims that carry the release's instance label —
// Helm leaves them in place on uninstall. Best effort: a failure to read just shows none.
func leftoverClaims(ctx context.Context, cfg *rest.Config, namespace, release string) []string {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil
	}
	list, err := cs.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + release,
		Limit:         maxLeftovers,
	})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for _, p := range list.Items {
		out = append(out, p.Name)
	}
	return out
}

// RollbackFailed undoes an install that failed after Helm had started, on request (the same
// DELETE that cancels a running one). It reports whether a rollback was started; a job that
// isn't eligible, or already rolled back, is left alone.
func (js *Jobs) RollbackFailed(rl *Releases, cfg *rest.Config, j *InstallJob) bool {
	j.mu.Lock()
	ok := j.stage == StageFailed && j.failedAt == StageInstalling && j.ownsRelease && !j.rolledBack
	if ok {
		j.stage = StageRollingBack
	}
	j.mu.Unlock()
	if !ok {
		return false
	}
	go func() {
		js.rollback(rl, cfg, j, false)
		j.mu.Lock()
		j.stage = StageFailed
		j.finishedAt = time.Now()
		j.mu.Unlock()
	}()
	return true
}
