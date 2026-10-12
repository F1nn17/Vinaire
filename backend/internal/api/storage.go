package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// isDefaultStorageClassAnnotation is how Kubernetes marks the cluster's
// default StorageClass — there's no typed field for this, only a
// well-known annotation.
const isDefaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

// PVCRow matches the frontend's PVCRow type (frontend/src/lib/types.ts).
type PVCRow struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	StatusColor  string `json:"statusColor"`
	Capacity     string `json:"capacity"`
	AccessModes  string `json:"accessModes"`
	StorageClass string `json:"storageClass"`
	Age          string `json:"age"`
	Href         string `json:"href"`
}

// PersistentVolumeClaims handles GET /api/v1/pvcs?namespace=.
func (h *Handler) PersistentVolumeClaims(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	pvcs, err := k8s.ListPersistentVolumeClaims(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]PVCRow, 0, len(pvcs))
	for _, pvc := range pvcs {
		rows = append(rows, pvcRow(pvc))
	}
	respondJSON(w, rows)
}

func pvcRow(pvc corev1.PersistentVolumeClaim) PVCRow {
	status, color := pvcStatus(pvc)
	return PVCRow{
		Name:         pvc.Name,
		Namespace:    pvc.Namespace,
		Status:       status,
		StatusColor:  string(color),
		Capacity:     pvcCapacity(pvc),
		AccessModes:  accessModesString(pvc.Status.AccessModes),
		StorageClass: pvcStorageClassName(pvc),
		Age:          formatAge(pvc.CreationTimestamp.Time),
		Href:         detailHref("/storage/pvcs/detail", pvc.Namespace, pvc.Name),
	}
}

func pvcStatus(pvc corev1.PersistentVolumeClaim) (status string, color StatusColor) {
	switch pvc.Status.Phase {
	case corev1.ClaimBound:
		return "Bound", StatusOK
	case corev1.ClaimPending:
		return "Pending", StatusWarn
	case corev1.ClaimLost:
		return "Lost", StatusErr
	default:
		return string(pvc.Status.Phase), StatusWarn
	}
}

// pvcCapacity prefers the actually-provisioned capacity (Status.Capacity);
// an unbound PVC has none yet, so it falls back to what was requested.
func pvcCapacity(pvc corev1.PersistentVolumeClaim) string {
	if q, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		return q.String()
	}
	if q, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		return q.String() + " (requested)"
	}
	return "—"
}

func pvcStorageClassName(pvc corev1.PersistentVolumeClaim) string {
	if pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		return *pvc.Spec.StorageClassName
	}
	return "—"
}

func accessModesString[T ~string](modes []T) string {
	if len(modes) == 0 {
		return "—"
	}
	strs := make([]string, len(modes))
	for i, m := range modes {
		strs[i] = string(m)
	}
	return strings.Join(strs, ", ")
}

// PVRow matches the frontend's PVRow type. PersistentVolumes are
// cluster-scoped, unlike PVCs — no Namespace field.
type PVRow struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	StatusColor   string `json:"statusColor"`
	Capacity      string `json:"capacity"`
	AccessModes   string `json:"accessModes"`
	ReclaimPolicy string `json:"reclaimPolicy"`
	StorageClass  string `json:"storageClass"`
	ClaimRef      string `json:"claimRef"`
	Age           string `json:"age"`
}

// PersistentVolumes handles GET /api/v1/pvs.
func (h *Handler) PersistentVolumes(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	pvs, err := k8s.ListPersistentVolumes(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]PVRow, 0, len(pvs))
	for _, pv := range pvs {
		rows = append(rows, pvRow(pv))
	}
	respondJSON(w, rows)
}

func pvRow(pv corev1.PersistentVolume) PVRow {
	status, color := pvStatus(pv)
	capacity := "—"
	if q, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
		capacity = q.String()
	}
	storageClass := pv.Spec.StorageClassName
	if storageClass == "" {
		storageClass = "—"
	}

	return PVRow{
		Name:          pv.Name,
		Status:        status,
		StatusColor:   string(color),
		Capacity:      capacity,
		AccessModes:   accessModesString(pv.Spec.AccessModes),
		ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
		StorageClass:  storageClass,
		ClaimRef:      pvClaimRef(pv),
		Age:           formatAge(pv.CreationTimestamp.Time),
	}
}

func pvStatus(pv corev1.PersistentVolume) (status string, color StatusColor) {
	switch pv.Status.Phase {
	case corev1.VolumeBound:
		return "Bound", StatusOK
	case corev1.VolumeAvailable:
		return "Available", StatusOK
	case corev1.VolumeReleased:
		return "Released", StatusWarn
	case corev1.VolumeFailed:
		return "Failed", StatusErr
	case corev1.VolumePending:
		return "Pending", StatusWarn
	default:
		return string(pv.Status.Phase), StatusWarn
	}
}

func pvClaimRef(pv corev1.PersistentVolume) string {
	if pv.Spec.ClaimRef == nil {
		return "—"
	}
	return pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name
}

// StorageClassRow matches the frontend's StorageClassRow type.
// StorageClasses are cluster-scoped.
type StorageClassRow struct {
	Name              string `json:"name"`
	Provisioner       string `json:"provisioner"`
	ReclaimPolicy     string `json:"reclaimPolicy"`
	VolumeBindingMode string `json:"volumeBindingMode"`
	IsDefault         bool   `json:"isDefault"`
	Age               string `json:"age"`
}

// StorageClasses handles GET /api/v1/storageclasses.
func (h *Handler) StorageClasses(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	classes, err := k8s.ListStorageClasses(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]StorageClassRow, 0, len(classes))
	for _, sc := range classes {
		rows = append(rows, storageClassRow(sc))
	}
	respondJSON(w, rows)
}

func storageClassRow(sc storagev1.StorageClass) StorageClassRow {
	// Kubernetes' own default when these pointer fields are unset.
	reclaimPolicy := string(corev1.PersistentVolumeReclaimDelete)
	if sc.ReclaimPolicy != nil {
		reclaimPolicy = string(*sc.ReclaimPolicy)
	}
	bindingMode := string(storagev1.VolumeBindingImmediate)
	if sc.VolumeBindingMode != nil {
		bindingMode = string(*sc.VolumeBindingMode)
	}

	return StorageClassRow{
		Name:              sc.Name,
		Provisioner:       sc.Provisioner,
		ReclaimPolicy:     reclaimPolicy,
		VolumeBindingMode: bindingMode,
		IsDefault:         sc.Annotations[isDefaultStorageClassAnnotation] == "true",
		Age:               formatAge(sc.CreationTimestamp.Time),
	}
}

// PVCMountingPod matches the frontend's PVCMountingPod type.
type PVCMountingPod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// PVCDetailResponse matches the frontend's PVCDetail type.
type PVCDetailResponse struct {
	Name         string            `json:"name"`
	Namespace    string            `json:"namespace"`
	Status       string            `json:"status"`
	StatusColor  string            `json:"statusColor"`
	Capacity     string            `json:"capacity"`
	AccessModes  string            `json:"accessModes"`
	StorageClass string            `json:"storageClass"`
	VolumeName   string            `json:"volumeName"`
	Age          string            `json:"age"`
	Labels       map[string]string `json:"labels"`
	MountedBy    []PVCMountingPod  `json:"mountedBy"`
	Events       []PodEventRow     `json:"events"`
	YAML         string            `json:"yaml"`
}

// PersistentVolumeClaimDetail handles GET /api/v1/pvcs/{namespace}/{name}.
func (h *Handler) PersistentVolumeClaimDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	pvc, err := k8s.GetPersistentVolumeClaim(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	// Scan every pod in the namespace for a volume referencing this PVC —
	// the same "scan the pod spec for references" style Phase 7's topology
	// graph uses for ConfigMap/Secret usage.
	pods, err := k8s.ListPods(r.Context(), clientset, namespace)
	if err != nil {
		slog.Warn("list pods for pvc mount scan failed", "namespace", namespace, "name", name, "error", err)
		pods = nil
	}

	events, err := k8s.ListEventsForObject(r.Context(), clientset, namespace, name)
	if err != nil {
		slog.Warn("list events failed", "namespace", namespace, "name", name, "error", err)
		events = nil
	}

	respondJSON(w, buildPVCDetail(*pvc, pods, events))
}

func buildPVCDetail(pvc corev1.PersistentVolumeClaim, pods []corev1.Pod, events []corev1.Event) PVCDetailResponse {
	status, color := pvcStatus(pvc)

	labels := pvc.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	sortEventsNewestFirst(events)
	eventRows := make([]PodEventRow, 0, len(events))
	for _, e := range events {
		eventRows = append(eventRows, podEventRow(e))
	}

	pvc.TypeMeta = metav1.TypeMeta{Kind: "PersistentVolumeClaim", APIVersion: "v1"}
	yaml := marshalYAML(pvc)

	return PVCDetailResponse{
		Name:         pvc.Name,
		Namespace:    pvc.Namespace,
		Status:       status,
		StatusColor:  string(color),
		Capacity:     pvcCapacity(pvc),
		AccessModes:  accessModesString(pvc.Status.AccessModes),
		StorageClass: pvcStorageClassName(pvc),
		VolumeName:   orDash(pvc.Spec.VolumeName),
		Age:          formatAge(pvc.CreationTimestamp.Time),
		Labels:       labels,
		MountedBy:    pvcMountingPods(pods, pvc.Name),
		Events:       eventRows,
		YAML:         yaml,
	}
}

type deletePVCRequest struct {
	ConfirmName string `json:"confirmName"`
}

// DeletePVC handles DELETE /api/v1/pvcs/{namespace}/{name}: removes one PersistentVolumeClaim, as the
// caller. It is irreversible — with the usual "Delete" reclaim policy the data goes with it — so:
// the body must repeat the claim's name, the cluster's own namespaces are refused, and a claim that
// a pod still mounts is refused with the pods named (Kubernetes would only mark it Terminating and
// keep the data until they go; saying so up front is clearer). Audited as delete-pvc.
func (h *Handler) DeletePVC(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or claim name")
		return
	}
	if reservedNamespaces[namespace] {
		respondError(w, http.StatusBadRequest, "namespace "+namespace+" is a system namespace; its claims can't be deleted from Vinaire")
		return
	}
	var req deletePVCRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the claim's name>"}`)
		return
	}
	pvc, err := k8s.GetPersistentVolumeClaim(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	// a caller who may not list pods gets no pre-check; the cluster's own protection still applies
	if pods, perr := k8s.ListPods(r.Context(), clientset, namespace); perr == nil {
		if using := pvcMountingPods(pods, name); len(using) > 0 {
			names := make([]string, 0, len(using))
			for _, p := range using {
				names = append(names, p.Name)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "the claim is still used by: " + strings.Join(names, ", ") + " — stop or delete them first",
				"pods":  names,
			})
			return
		}
	}
	if err := k8s.DeletePersistentVolumeClaim(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, "delete-pvc", namespace, name, "volume", pvc.Spec.VolumeName, "storageClass", pvcStorageClassName(*pvc))
	respondJSON(w, map[string]string{"status": "ok"})
}

func pvcMountingPods(pods []corev1.Pod, pvcName string) []PVCMountingPod {
	mounting := make([]PVCMountingPod, 0)
	for _, pod := range pods {
		for _, v := range pod.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvcName {
				mounting = append(mounting, PVCMountingPod{Name: pod.Name, Namespace: pod.Namespace})
				break
			}
		}
	}
	sort.Slice(mounting, func(i, j int) bool { return mounting[i].Name < mounting[j].Name })
	return mounting
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
