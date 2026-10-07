package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Node lifecycle actions (Roadmap Phase 40): cordon / uncordon, drain, labels,
// taints and removing a Node object. An explicit, deliberate extension to
// AGENTS.md's write list — see it before adding another write verb here. Every
// call runs with the *caller's* clientset, so the cluster's RBAC decides, and the
// HTTP handlers audit each one.

// CordonNode marks a node unschedulable (or schedulable again): the same single
// field `kubectl cordon` / `uncordon` sets. Pods already on it keep running.
func CordonNode(ctx context.Context, cs kubernetes.Interface, name string, unschedulable bool) error {
	patch, _ := json.Marshal(map[string]any{"spec": map[string]any{"unschedulable": unschedulable}})
	if _, err := cs.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("cordon node %s: %w", name, err)
	}
	return nil
}

// PatchNodeLabels sets and removes labels in one merge patch (null removes a key).
func PatchNodeLabels(ctx context.Context, cs kubernetes.Interface, name string, set map[string]string, remove []string) error {
	labels := map[string]any{}
	for k, v := range set {
		labels[k] = v
	}
	for _, k := range remove {
		labels[k] = nil
	}
	patch, _ := json.Marshal(map[string]any{"metadata": map[string]any{"labels": labels}})
	if _, err := cs.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("label node %s: %w", name, err)
	}
	return nil
}

// ReplaceNodeTaints replaces the node's whole taint list. The patch carries the
// resourceVersion that was read, so a taint added by someone else in the meantime
// is a 409 to retry, not silently dropped.
func ReplaceNodeTaints(ctx context.Context, cs kubernetes.Interface, name string, taints []corev1.Taint) error {
	node, err := cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get node %s: %w", name, err)
	}
	if taints == nil {
		taints = []corev1.Taint{}
	}
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"resourceVersion": node.ResourceVersion},
		"spec":     map[string]any{"taints": taints},
	})
	if _, err := cs.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("taint node %s: %w", name, err)
	}
	return nil
}

// DeleteNode removes the Node *object* — the same as `kubectl delete node`. The
// machine and its kubelet are untouched (a running kubelet registers the node
// again), so this is for a node that is already gone or being retired.
func DeleteNode(ctx context.Context, cs kubernetes.Interface, name string) error {
	if err := cs.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("delete node %s: %w", name, err)
	}
	return nil
}

// ---- drain ------------------------------------------------------------------

// DrainDisposition says what a drain would do with one pod.
type DrainDisposition string

const (
	DrainEvict     DrainDisposition = "evict"     // evicted (the controller recreates it elsewhere)
	DrainSkip      DrainDisposition = "skip"      // left alone: DaemonSet or mirror pod
	DrainUnmanaged DrainDisposition = "unmanaged" // no controller — would be gone for good; needs explicit consent
	DrainLocalData DrainDisposition = "localdata" // uses emptyDir — its data would be lost; needs explicit consent
)

// DrainPod is one pod in a drain plan.
type DrainPod struct {
	Namespace   string           `json:"namespace"`
	Name        string           `json:"name"`
	Disposition DrainDisposition `json:"disposition"`
	Reason      string           `json:"reason,omitempty"`
}

// DrainOptions are the explicit consents a drain needs, the same as kubectl's
// --force and --delete-emptydir-data. Without them such pods block the drain.
type DrainOptions struct {
	EvictUnmanaged  bool
	DeleteLocalData bool
}

// PlanDrain classifies every pod on the node the way `kubectl drain` does.
func PlanDrain(ctx context.Context, cs kubernetes.Interface, node string) ([]DrainPod, error) {
	list, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node})
	if err != nil {
		return nil, fmt.Errorf("list pods on node %s: %w", node, err)
	}
	return classifyPods(list.Items), nil
}

func classifyPods(pods []corev1.Pod) []DrainPod {
	out := make([]DrainPod, 0, len(pods))
	for _, p := range pods {
		d := DrainPod{Namespace: p.Namespace, Name: p.Name, Disposition: DrainEvict}
		owner := metav1.GetControllerOf(&p)
		switch {
		case p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed:
			d.Disposition, d.Reason = DrainEvict, "already finished"
		case p.Annotations[corev1.MirrorPodAnnotationKey] != "":
			d.Disposition, d.Reason = DrainSkip, "static (mirror) pod — managed by the kubelet"
		case owner != nil && owner.Kind == "DaemonSet":
			d.Disposition, d.Reason = DrainSkip, "DaemonSet pod — it would come straight back"
		case owner == nil:
			d.Disposition, d.Reason = DrainUnmanaged, "no controller — evicting it deletes it for good"
		case hasEmptyDir(&p):
			d.Disposition, d.Reason = DrainLocalData, "uses an emptyDir volume — its data is lost on eviction"
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func hasEmptyDir(p *corev1.Pod) bool {
	for _, v := range p.Spec.Volumes {
		if v.EmptyDir != nil {
			return true
		}
	}
	return false
}

// ErrDrainBlocked is returned when pods need consent that wasn't given.
var ErrDrainBlocked = errors.New("drain blocked")

// DrainResult is what a drain did.
type DrainResult struct {
	Evicted int         `json:"evicted"`
	Skipped int         `json:"skipped"`
	Pending []DrainPod  `json:"pending,omitempty"` // still there: a PodDisruptionBudget held them, or the time ran out
	Failed  []DrainFail `json:"failed,omitempty"`
}

// DrainFail is a pod that could not be evicted.
type DrainFail struct {
	Pod    string `json:"pod"`
	Reason string `json:"reason"`
}

// DrainNode cordons the node, then evicts its pods through the Eviction API —
// which honours PodDisruptionBudgets, unlike deleting the pods. A pod a budget
// currently protects (HTTP 429) is retried until `wait` runs out and then
// reported as pending, so nothing is forced through.
func DrainNode(ctx context.Context, cs kubernetes.Interface, node string, opt DrainOptions, wait time.Duration) (*DrainResult, error) {
	plan, err := PlanDrain(ctx, cs, node)
	if err != nil {
		return nil, err
	}
	for _, p := range plan {
		if p.Disposition == DrainUnmanaged && !opt.EvictUnmanaged {
			return nil, fmt.Errorf("%w: %s/%s has no controller (set evictUnmanaged to delete it anyway)", ErrDrainBlocked, p.Namespace, p.Name)
		}
		if p.Disposition == DrainLocalData && !opt.DeleteLocalData {
			return nil, fmt.Errorf("%w: %s/%s uses an emptyDir volume (set deleteLocalData to lose its data)", ErrDrainBlocked, p.Namespace, p.Name)
		}
	}
	// cordon first, so nothing new lands while pods are being moved
	if err := CordonNode(ctx, cs, node, true); err != nil {
		return nil, err
	}

	res := &DrainResult{}
	todo := []DrainPod{}
	for _, p := range plan {
		if p.Disposition == DrainSkip {
			res.Skipped++
			continue
		}
		todo = append(todo, p)
	}
	deadline := time.Now().Add(wait)
	for len(todo) > 0 {
		var blocked []DrainPod
		for _, p := range todo {
			err := cs.CoreV1().Pods(p.Namespace).EvictV1(ctx, &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace},
			})
			switch {
			case err == nil, apierrors.IsNotFound(err):
				res.Evicted++
			case apierrors.IsTooManyRequests(err):
				blocked = append(blocked, p) // a PodDisruptionBudget says not yet
			default:
				res.Failed = append(res.Failed, DrainFail{Pod: p.Namespace + "/" + p.Name, Reason: err.Error()})
			}
		}
		todo = blocked
		if len(todo) == 0 || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	res.Pending = todo
	return res, nil
}
