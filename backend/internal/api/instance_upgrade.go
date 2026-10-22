package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/f1nn17/vinaire/backend/internal/instances"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

type upgradeRequest struct {
	Tag         string `json:"tag"`
	AcceptMajor bool   `json:"acceptMajor"` // the person accepts a change of major version
	Revert      bool   `json:"revert"`      // put back the image from before the last upgrade
	ConfirmName string `json:"confirmName"` // must repeat the instance's name
}

// UpgradeInstance handles POST /api/v1/instances/{namespace}/{name}/upgrade (Roadmap Phase 77d): changes
// the tag of the image of an instance installed from an image — or, with `revert`, puts the previous
// image back. As the caller. The repository is never taken from the request (see internal/instances),
// a change of major version needs `acceptMajor`, and the body must repeat the instance's name. Audited
// as `instance-upgrade` / `instance-revert` (from and to). Charts are upgraded from the Marketplace.
func (h *Handler) UpgradeInstance(w http.ResponseWriter, r *http.Request) {
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
	var req upgradeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must repeat the instance's name in "confirmName"`)
		return
	}
	list, err := cs.AppsV1().StatefulSets(namespace).List(r.Context(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/instance=" + name + ",app.kubernetes.io/managed-by=vinaire"})
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if len(list.Items) != 1 {
		respondError(w, http.StatusNotFound, "no instance installed from an image has this name here")
		return
	}
	sts := list.Items[0]
	var plan instances.UpgradePlan
	if req.Revert {
		plan, err = instances.PlanRevert(sts)
	} else {
		plan, err = instances.PlanUpgrade(sts, req.Tag, req.AcceptMajor)
	}
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := k8s.PatchStatefulSetImage(r.Context(), cs, namespace, sts.Name, plan.Patch); err != nil {
		respondK8sError(w, err)
		return
	}
	action := "instance-upgrade"
	if req.Revert {
		action = "instance-revert"
	}
	h.auditLog(r, action, namespace, name, "from", plan.From, "to", plan.To, "major", plan.Major)
	respondJSON(w, map[string]any{"status": "ok", "from": plan.From, "to": plan.To})
}
