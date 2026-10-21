package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/backup"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// Restoring (Roadmap Phase 77c). A restore never goes over a running instance: a dump is loaded into a
// NEW instance, and a snapshot becomes a NEW claim.

type restoreRequest struct {
	Source      string `json:"source"`      // the instance whose dump to load
	File        string `json:"file"`        // empty: the newest dump of the source
	ConfirmName string `json:"confirmName"` // must repeat the target instance's name
}

// instanceSource finds the one image-installed instance of that name and recognises its engine.
func (h *Handler) instanceSource(w http.ResponseWriter, r *http.Request, cs *kubernetes.Clientset, namespace, name string) (backup.Source, bool) {
	list, err := cs.AppsV1().StatefulSets(namespace).List(r.Context(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + name + ",app.kubernetes.io/managed-by=vinaire"})
	if err != nil {
		respondK8sError(w, err)
		return backup.Source{}, false
	}
	if len(list.Items) != 1 {
		respondError(w, http.StatusNotFound, "no instance installed from an image has this name here")
		return backup.Source{}, false
	}
	src, err := backup.SourceFrom(list.Items[0])
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return backup.Source{}, false
	}
	return src, true
}

// RestoreBackup handles POST /api/v1/instances/{namespace}/{name}/restore: loads a dump into the
// instance {name}, which must be a NEW instance (installed from an image, as the source was) — never the
// instance the dump came from. The Job reads the dump from the claim `<source>-backups` in the same
// namespace. The body must repeat the target's name. Audited as `backup-restore`.
func (h *Handler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or name")
		return
	}
	if reservedNamespaces[namespace] {
		respondError(w, http.StatusBadRequest, "namespace "+namespace+" is a system namespace")
		return
	}
	var req restoreRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must repeat the target instance's name in "confirmName"`)
		return
	}
	into, ok := h.instanceSource(w, r, cs, namespace, name)
	if !ok {
		return
	}
	job, err := backup.RestoreJob(into, req.Source, req.File, time.Now())
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The claim the dump lives on must be one Vinaire made for that source.
	pvc, err := cs.CoreV1().PersistentVolumeClaims(namespace).Get(r.Context(), backup.PVCName(req.Source), metav1.GetOptions{})
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if pvc.Labels[backup.LabelManagedBy] != "vinaire" || pvc.Labels[backup.LabelBackupOf] != req.Source {
		respondError(w, http.StatusConflict, "that claim was not made by Vinaire as a backup claim of "+req.Source)
		return
	}
	if err := k8s.CreateBackupJob(r.Context(), cs, job); err != nil {
		respondBackupError(w, err)
		return
	}
	h.auditLog(r, "backup-restore", namespace, name, "source", req.Source, "file", req.File, "job", job.Name)
	respondJSON(w, map[string]any{"status": "ok", "created": []string{job.Name}})
}

type restoreSnapshotRequest struct {
	Snapshot string `json:"snapshot"`
	Claim    string `json:"claim"` // the NEW claim to create
	Size     string `json:"size"`  // needed when the snapshot does not say how big it is
}

// RestoreSnapshot handles POST /api/v1/instances/{namespace}/{name}/restore-snapshot: creates a NEW claim
// whose data is a snapshot Vinaire took of instance {name}. It touches no existing claim; the new claim is
// for a new instance (or for a look inside). Audited as `backup-restore-snapshot`.
func (h *Handler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	dyn, _, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or name")
		return
	}
	if reservedNamespaces[namespace] {
		respondError(w, http.StatusBadRequest, "namespace "+namespace+" is a system namespace")
		return
	}
	var req restoreSnapshotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snap, err := k8s.GetSnapshot(r.Context(), dyn, namespace, req.Snapshot)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	l := snap.GetLabels()
	if l[backup.LabelManagedBy] != "vinaire" || l[backup.LabelBackupOf] != name {
		respondError(w, http.StatusConflict, "that snapshot was not taken by Vinaire for "+name)
		return
	}
	size := req.Size
	if rs, _, _ := unstructured.NestedString(snap.Object, "status", "restoreSize"); backup.ValidSize(rs) {
		size = rs
	}
	pvc, err := backup.ClaimFromSnapshot(namespace, name, req.Claim, req.Snapshot, size)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := k8s.CreateClaim(r.Context(), cs, pvc); err != nil {
		respondBackupError(w, err)
		return
	}
	h.auditLog(r, "backup-restore-snapshot", namespace, name, "snapshot", req.Snapshot, "claim", req.Claim)
	respondJSON(w, map[string]any{"status": "ok", "created": []string{pvc.Name}})
}
