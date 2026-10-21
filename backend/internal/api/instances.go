package api

import (
	"net/http"

	"github.com/f1nn17/vinaire/backend/internal/instances"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// Instances handles GET /api/v1/instances (Roadmap Phase 77a): the installed applications —
// Marketplace charts and "Install from an image" — each with the objects that belong to it and what
// deleting it would remove or leave. It lists as the caller and writes nothing; the deletes it leads to
// are the existing, audited ones (DELETE statefulsets/deployments/pvcs, Helm uninstall). A kind the
// caller may not list is reported in `unreadable` rather than silently treated as absent.
func (h *Handler) Instances(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var snap instances.Snapshot
	unreadable := []string{}
	var firstErr error
	note := func(kind string, err error) {
		if err != nil {
			unreadable = append(unreadable, kind)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	var err error
	snap.StatefulSets, err = k8s.ListStatefulSets(ctx, cs, "")
	note("StatefulSet", err)
	snap.Deployments, err = k8s.ListDeployments(ctx, cs, "")
	note("Deployment", err)
	snap.Services, err = k8s.ListServices(ctx, cs, "")
	note("Service", err)
	snap.PVCs, err = k8s.ListPersistentVolumeClaims(ctx, cs, "")
	note("PersistentVolumeClaim", err)
	snap.Secrets, err = k8s.ListSecrets(ctx, cs, "")
	note("Secret", err)
	// Nothing readable at all is a real error (401/403 for the caller), not an empty list.
	if len(unreadable) == 5 {
		respondK8sError(w, firstErr)
		return
	}
	list := instances.Build(snap)
	respondJSON(w, map[string]any{"instances": list, "unreadable": unreadable})
}
