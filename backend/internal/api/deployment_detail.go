package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// DeploymentPodRow matches the frontend's DeploymentPodRow type.
type DeploymentPodRow struct {
	Name        string `json:"name"`
	Node        string `json:"node"`
	Ready       string `json:"ready"`
	Status      string `json:"status"`
	StatusColor string `json:"statusColor"`
	Age         string `json:"age"`
}

// podRowsFor turns a pod list into the rows the workload detail pages show (Deployments, and
// since Roadmap Phase 66 StatefulSets, DaemonSets and Jobs).
func podRowsFor(pods *corev1.PodList) []DeploymentPodRow {
	rows := make([]DeploymentPodRow, 0)
	if pods == nil {
		return rows
	}
	for _, p := range pods.Items {
		ready, total := podReadyCount(p)
		pStatus, pColor := podStatus(p)
		rows = append(rows, DeploymentPodRow{
			Name:        p.Name,
			Node:        p.Spec.NodeName,
			Ready:       fmt.Sprintf("%d/%d", ready, total),
			Status:      pStatus,
			StatusColor: string(pColor),
			Age:         formatAge(p.CreationTimestamp.Time),
		})
	}
	return rows
}

// listSelectedPods lists the pods a workload selector matches, as the caller. A caller who may
// not list pods just sees none: the detail page still works, as it does for a Deployment.
func listSelectedPods(ctx context.Context, cs kubernetes.Interface, namespace string, sel *metav1.LabelSelector) *corev1.PodList {
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil || selector.Empty() {
		return nil // an empty selector would match every pod in the namespace
	}
	list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		slog.Warn("list pods for workload failed", "namespace", namespace, "error", err)
		return nil
	}
	return list
}

// DeploymentRevisionRow matches the frontend's Revision type.
type DeploymentRevisionRow struct {
	Revision int64  `json:"revision"`
	Current  bool   `json:"current"`
	Image    string `json:"image"`
	Age      string `json:"age"`
}

// DeploymentDetailResponse matches the frontend's DeploymentDetail type
// (frontend/src/lib/types.ts).
type DeploymentDetailResponse struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	StatusColor  string `json:"statusColor"`
	ReadySummary string `json:"readySummary"`
	Replicas     string `json:"replicas"`
	// DesiredReplicas/Paused back Roadmap Phase 33's Scale control and
	// "Rollout paused" badge — Replicas above stays the existing
	// human-readable "N/M ready" string the Overview tab already renders.
	DesiredReplicas int32                   `json:"desiredReplicas"`
	Paused          bool                    `json:"paused"`
	Strategy        string                  `json:"strategy"`
	Image           string                  `json:"image"`
	ManagedBy       string                  `json:"managedBy"`
	Age             string                  `json:"age"`
	Pods            []DeploymentPodRow      `json:"pods"`
	Revisions       []DeploymentRevisionRow `json:"revisions"`
	YAML            string                  `json:"yaml"`
}

// DeploymentDetail handles GET /api/v1/deployments/{namespace}/{name} —
// metadata plus its live pods (matched via the Deployment's own selector,
// the same approach Phase 9's RollbackDeployment already uses to find
// ReplicaSets) and revision history (ReplicaSets carrying the
// deployment.kubernetes.io/revision annotation, same mechanism `kubectl
// rollout history` reads).
func (h *Handler) DeploymentDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	deployment, err := clientset.AppsV1().Deployments(namespace).Get(r.Context(), name, metav1.GetOptions{})
	if err != nil {
		respondK8sError(w, err)
		return
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "invalid deployment selector: "+err.Error())
		return
	}

	var pods *corev1.PodList
	if list, err := clientset.CoreV1().Pods(namespace).List(r.Context(), metav1.ListOptions{LabelSelector: selector.String()}); err != nil {
		slog.Warn("list pods for deployment failed", "namespace", namespace, "name", name, "error", err)
	} else {
		pods = list
	}

	var replicaSets *appsv1.ReplicaSetList
	if list, err := clientset.AppsV1().ReplicaSets(namespace).List(r.Context(), metav1.ListOptions{LabelSelector: selector.String()}); err != nil {
		slog.Warn("list replicasets for deployment failed", "namespace", namespace, "name", name, "error", err)
	} else {
		replicaSets = list
	}

	respondJSON(w, buildDeploymentDetail(*deployment, pods, replicaSets))
}

func buildDeploymentDetail(d appsv1.Deployment, pods *corev1.PodList, replicaSets *appsv1.ReplicaSetList) DeploymentDetailResponse {
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	status, color := readyStatus(d.Status.ReadyReplicas, desired)

	image := ""
	if len(d.Spec.Template.Spec.Containers) > 0 {
		image = d.Spec.Template.Spec.Containers[0].Image
	}

	managedBy := d.Labels["app.kubernetes.io/managed-by"]
	if managedBy == "" {
		managedBy = "—"
	}

	podRows := podRowsFor(pods)

	currentRevision := d.Annotations["deployment.kubernetes.io/revision"]
	revisionRows := make([]DeploymentRevisionRow, 0)
	if replicaSets != nil {
		for _, rs := range replicaSets.Items {
			if !deploymentOwns(rs.OwnerReferences, d.UID) {
				continue
			}
			revAnnotation := rs.Annotations["deployment.kubernetes.io/revision"]
			revision, err := strconv.ParseInt(revAnnotation, 10, 64)
			if err != nil {
				continue
			}
			rsImage := ""
			if len(rs.Spec.Template.Spec.Containers) > 0 {
				rsImage = rs.Spec.Template.Spec.Containers[0].Image
			}
			revisionRows = append(revisionRows, DeploymentRevisionRow{
				Revision: revision,
				Current:  revAnnotation == currentRevision,
				Image:    rsImage,
				Age:      formatAge(rs.CreationTimestamp.Time),
			})
		}
	}
	sort.Slice(revisionRows, func(i, j int) bool { return revisionRows[i].Revision > revisionRows[j].Revision })

	d.TypeMeta = metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"}
	yaml := marshalYAML(d)

	return DeploymentDetailResponse{
		Name:            d.Name,
		Namespace:       d.Namespace,
		Status:          status,
		StatusColor:     string(color),
		ReadySummary:    fmt.Sprintf("%s · %s", status, formatReady(d.Status.ReadyReplicas, desired)),
		Replicas:        fmt.Sprintf("%s ready", formatReady(d.Status.ReadyReplicas, desired)),
		DesiredReplicas: desired,
		Paused:          d.Spec.Paused,
		Strategy:        string(d.Spec.Strategy.Type),
		Image:           image,
		ManagedBy:       managedBy,
		Age:             formatAge(d.CreationTimestamp.Time),
		Pods:            podRows,
		Revisions:       revisionRows,
		YAML:            yaml,
	}
}

// deploymentOwns duplicates internal/k8s's identical unexported hasOwner —
// that package's helper isn't exported, and this is a two-line check not
// worth exporting a whole function for.
func deploymentOwns(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, ref := range refs {
		if ref.UID == uid {
			return true
		}
	}
	return false
}
