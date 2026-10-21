package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/f1nn17/vinaire/backend/internal/backup"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// Backups of an application installed from an image (Roadmap Phase 77b). Everything here is made as
// the caller, so the cluster's RBAC decides; the objects are built by internal/backup from the
// instance's own StatefulSet — a request never names an image, a command, a URL or an endpoint.
// Creating is audited as `backup-create`, deleting as `backup-delete`.

const targetAnnotation = "vinaire.io/backup-target"

type backupTargetInfo struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix,omitempty"`
}

// BackupConfig handles GET /api/v1/backups/config: where dumps may go and whether snapshots exist.
func (h *Handler) BackupConfig(w http.ResponseWriter, r *http.Request) {
	dyn, apiext, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}
	targets := []backupTargetInfo{}
	if h.Backup != nil {
		for _, t := range h.Backup.Targets {
			targets = append(targets, backupTargetInfo{Name: t.Name, Endpoint: t.Endpoint, Bucket: t.Bucket, Prefix: t.Prefix})
		}
	}
	snap := map[string]any{"available": false, "classes": []string{}}
	if _, err := apiext.ApiextensionsV1().CustomResourceDefinitions().Get(r.Context(), backup.SnapshotCRD, metav1.GetOptions{}); err == nil {
		if classes, err := k8s.ListSnapshotClasses(r.Context(), dyn); err == nil && len(classes) > 0 {
			snap = map[string]any{"available": true, "classes": classes}
		}
	}
	respondJSON(w, map[string]any{"s3Targets": targets, "snapshots": snap})
}

// BackupItem is one backup of an instance, as the list shows it.
type BackupItem struct {
	Kind      string    `json:"kind"`           // job | cronjob | snapshot
	Role      string    `json:"role,omitempty"` // "restore" for a Job that loaded a dump
	Name      string    `json:"name"`
	Created   time.Time `json:"created"`
	State     string    `json:"state"` // running | succeeded | failed | scheduled | ready | pending
	Target    string    `json:"target,omitempty"`
	Schedule  string    `json:"schedule,omitempty"`
	Claim     string    `json:"claim,omitempty"`
	Suspended bool      `json:"suspended,omitempty"`
}

func jobState(j batchv1.Job) string {
	switch {
	case j.Status.Succeeded > 0:
		return "succeeded"
	case j.Status.Failed > 0 && j.Status.Active == 0:
		return "failed"
	}
	return "running"
}

// InstanceBackups handles GET /api/v1/instances/{namespace}/{name}/backups.
func (h *Handler) InstanceBackups(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or name")
		return
	}
	jobs, crons, err := k8s.ListBackupObjects(r.Context(), cs, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	items := []BackupItem{}
	for _, j := range jobs {
		items = append(items, BackupItem{Kind: "job", Name: j.Name, Created: j.CreationTimestamp.Time, State: jobState(j), Target: j.Annotations[targetAnnotation]})
	}
	for _, c := range crons {
		susp := c.Spec.Suspend != nil && *c.Spec.Suspend
		items = append(items, BackupItem{Kind: "cronjob", Name: c.Name, Created: c.CreationTimestamp.Time, State: "scheduled",
			Schedule: c.Spec.Schedule, Target: c.Annotations[targetAnnotation], Suspended: susp})
	}
	if restores, err := k8s.ListRestoreJobs(r.Context(), cs, namespace, name); err == nil {
		for _, j := range restores {
			items = append(items, BackupItem{Kind: "job", Role: "restore", Name: j.Name, Created: j.CreationTimestamp.Time, State: jobState(j),
				Target: "from " + j.Labels[backup.LabelRestoreOf]})
		}
	}
	// Snapshots need the CRDs; a cluster without them just has none.
	if dyn, _, ok := h.resolveDynamic(w, r); ok {
		if snaps, err := k8s.ListSnapshots(r.Context(), dyn, namespace, name); err == nil {
			for _, s := range snaps {
				state := "pending"
				if ready, found, _ := unstructured.NestedBool(s.Object, "status", "readyToUse"); found && ready {
					state = "ready"
				}
				claim, _, _ := unstructured.NestedString(s.Object, "spec", "source", "persistentVolumeClaimName")
				items = append(items, BackupItem{Kind: "snapshot", Name: s.GetName(), Created: s.GetCreationTimestamp().Time, State: state, Claim: claim})
			}
		}
	} else {
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Created.After(items[j].Created) })
	respondJSON(w, map[string]any{"backups": items})
}

type createBackupRequest struct {
	Kind              string `json:"kind"`   // dump | snapshot
	Target            string `json:"target"` // "pvc" or the name of a configured S3 target
	Size              string `json:"size"`   // size of the claim a PVC target creates (default 5Gi)
	CredentialsSecret string `json:"credentialsSecret"`
	Schedule          string `json:"schedule"` // empty: run once now
	SnapshotClass     string `json:"snapshotClass"`
}

// CreateBackup handles POST /api/v1/instances/{namespace}/{name}/backups.
func (h *Handler) CreateBackup(w http.ResponseWriter, r *http.Request) {
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
	var req createBackupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	list, err := cs.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + name + ",app.kubernetes.io/managed-by=vinaire"})
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if len(list.Items) != 1 {
		respondError(w, http.StatusNotFound, "no instance installed from an image has this name here")
		return
	}
	now := time.Now()

	switch req.Kind {
	case "snapshot":
		dyn, _, ok := h.resolveDynamic(w, r)
		if !ok {
			return
		}
		pvcs, err := cs.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + name})
		if err != nil {
			respondK8sError(w, err)
			return
		}
		made := []string{}
		for _, c := range pvcs.Items {
			if c.Labels[backup.LabelBackupOf] != "" { // the claim dumps go to is not data to snapshot
				continue
			}
			snap, err := backup.Snapshot(namespace, name, c.Name, req.SnapshotClass, now)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := k8s.CreateSnapshot(ctx, dyn, snap); err != nil {
				respondK8sError(w, err)
				return
			}
			made = append(made, snap.GetName())
		}
		if len(made) == 0 {
			respondError(w, http.StatusConflict, "the instance has no volume claim to snapshot")
			return
		}
		h.auditLog(r, "backup-create", namespace, name, "kind", "snapshot", "snapshots", made)
		respondJSON(w, map[string]any{"status": "ok", "created": made})
	case "dump":
		src, err := backup.SourceFrom(list.Items[0])
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		tgt := backup.Target{UploadImage: h.Backup.Image()}
		annotation := "pvc:" + backup.PVCName(name)
		if req.Target != "pvc" {
			t, found := h.Backup.Target(req.Target)
			if !found {
				respondError(w, http.StatusBadRequest, "unknown backup target")
				return
			}
			tgt.S3, tgt.CredentialsSecret = &t, req.CredentialsSecret
			annotation = "s3:" + t.Name
		}
		if tgt.S3 == nil {
			size := req.Size
			if size == "" {
				size = "5Gi"
			}
			claim, err := backup.ClaimFor(src, size)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			if _, err := k8s.EnsureBackupClaim(ctx, cs, claim); err != nil {
				respondK8sError(w, err)
				return
			}
		}
		if req.Schedule != "" {
			cj, err := backup.CronJob(src, tgt, req.Schedule)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			cj.Annotations = map[string]string{targetAnnotation: annotation}
			if err := k8s.CreateBackupCronJob(ctx, cs, cj); err != nil {
				respondBackupError(w, err)
				return
			}
			h.auditLog(r, "backup-create", namespace, name, "kind", "dump-schedule", "target", annotation, "schedule", req.Schedule)
			respondJSON(w, map[string]any{"status": "ok", "created": []string{cj.Name}})
			return
		}
		job, err := backup.Job(src, tgt, now)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		job.Annotations = map[string]string{targetAnnotation: annotation}
		if err := k8s.CreateBackupJob(ctx, cs, job); err != nil {
			respondBackupError(w, err)
			return
		}
		h.auditLog(r, "backup-create", namespace, name, "kind", "dump", "target", annotation)
		respondJSON(w, map[string]any{"status": "ok", "created": []string{job.Name}})
	default:
		respondError(w, http.StatusBadRequest, `kind must be "dump" or "snapshot"`)
	}
}

func respondBackupError(w http.ResponseWriter, err error) {
	if apierrors.IsAlreadyExists(err) {
		respondError(w, http.StatusConflict, "this backup already exists (an instance has one schedule)")
		return
	}
	respondK8sError(w, err)
}

type deleteBackupRequest struct {
	ConfirmName string `json:"confirmName"`
}

// DeleteBackup handles DELETE /api/v1/backups/{namespace}/{kind}/{name}: removes one backup object that
// Vinaire made. The dump files already written to a claim or a bucket stay where they are.
func (h *Handler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	dyn, _, ok := h.resolveDynamic(w, r)
	if !ok {
		return
	}
	namespace, kind, name := chi.URLParam(r, "namespace"), chi.URLParam(r, "kind"), chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) || (kind != "job" && kind != "cronjob" && kind != "snapshot") {
		respondError(w, http.StatusBadRequest, "invalid namespace, kind or name")
		return
	}
	if reservedNamespaces[namespace] {
		respondError(w, http.StatusBadRequest, "namespace "+namespace+" is a system namespace")
		return
	}
	var req deleteBackupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the backup's name>"}`)
		return
	}
	labels, err := k8s.GetBackupLabels(r.Context(), cs, dyn, kind, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if labels[backup.LabelManagedBy] != "vinaire" || (labels[backup.LabelBackupOf] == "" && labels[backup.LabelRestoreInto] == "") {
		respondError(w, http.StatusConflict, "this object is not a backup made by Vinaire")
		return
	}
	if err := k8s.DeleteBackupObject(r.Context(), cs, dyn, kind, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, "backup-delete", namespace, name, "kind", kind, "instance", labels[backup.LabelBackupOf])
	respondJSON(w, map[string]string{"status": "ok"})
}
