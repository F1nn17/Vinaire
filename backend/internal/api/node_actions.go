package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// Node lifecycle (Roadmap Phase 40): cordon / uncordon, drain, labels, taints and
// removing a Node object. Each runs with the caller's own clientset (the cluster's
// RBAC decides) and is audited. See AGENTS.md's write list.

const (
	maxNodeBody   = 64 << 10
	maxNodeLabels = 50
	maxNodeTaints = 50
	// A drain runs inside one request; pods a PodDisruptionBudget holds back are
	// retried for this long and then reported as pending, never forced.
	drainWait = 75 * time.Second
)

// protectedNodeLabels are identity labels Vinaire won't change: schedulers and
// volume topology rely on them, and a wrong edit strands the node.
var protectedNodeLabels = map[string]bool{
	"kubernetes.io/hostname": true,
}

func nodeNameParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := chi.URLParam(r, "name")
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		respondError(w, http.StatusBadRequest, "invalid node name")
		return "", false
	}
	return name, true
}

func decodeNodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxNodeBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}

type confirmBody struct {
	ConfirmName string `json:"confirmName"`
}

func (h *Handler) setCordon(w http.ResponseWriter, r *http.Request, unschedulable bool) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	if err := k8s.CordonNode(r.Context(), cs, name, unschedulable); err != nil {
		respondK8sError(w, err)
		return
	}
	action := "uncordon-node"
	if unschedulable {
		action = "cordon-node"
	}
	h.auditLog(r, action, "", name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// CordonNode handles POST /api/v1/nodes/{name}/cordon.
func (h *Handler) CordonNode(w http.ResponseWriter, r *http.Request) { h.setCordon(w, r, true) }

// UncordonNode handles POST /api/v1/nodes/{name}/uncordon.
func (h *Handler) UncordonNode(w http.ResponseWriter, r *http.Request) { h.setCordon(w, r, false) }

// DrainPlan handles GET /api/v1/nodes/{name}/drain-plan: what a drain would do to
// each pod, so the UI can show it (and the consents it needs) before anything moves.
func (h *Handler) DrainPlan(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	plan, err := k8s.PlanDrain(r.Context(), cs, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	respondJSON(w, map[string]any{"pods": plan})
}

type drainBody struct {
	ConfirmName     string `json:"confirmName"`
	EvictUnmanaged  bool   `json:"evictUnmanaged"`
	DeleteLocalData bool   `json:"deleteLocalData"`
}

// DrainNode handles POST /api/v1/nodes/{name}/drain.
func (h *Handler) DrainNode(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	var b drainBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if b.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must repeat the node's name as "confirmName"`)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), drainWait+30*time.Second)
	defer cancel()
	res, err := k8s.DrainNode(ctx, cs, name, k8s.DrainOptions{EvictUnmanaged: b.EvictUnmanaged, DeleteLocalData: b.DeleteLocalData}, drainWait)
	if err != nil {
		if errors.Is(err, k8s.ErrDrainBlocked) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		h.auditLog(r, "drain-node-failed", "", name, "error", truncateAudit(err.Error()))
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, "drain-node", "", name, "evicted", res.Evicted, "skipped", res.Skipped, "pending", len(res.Pending),
		"failed", len(res.Failed), "evictUnmanaged", b.EvictUnmanaged, "deleteLocalData", b.DeleteLocalData)
	respondJSON(w, res)
}

func truncateAudit(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

type labelsBody struct {
	Set    map[string]string `json:"set"`
	Remove []string          `json:"remove"`
}

func validateLabelChange(b labelsBody) error {
	if len(b.Set)+len(b.Remove) == 0 {
		return errors.New("nothing to change")
	}
	if len(b.Set)+len(b.Remove) > maxNodeLabels {
		return fmt.Errorf("too many labels in one change (max %d)", maxNodeLabels)
	}
	check := func(k string) error {
		if errs := validation.IsQualifiedName(k); len(errs) > 0 {
			return fmt.Errorf("invalid label key %q: %s", k, errs[0])
		}
		if protectedNodeLabels[k] {
			return fmt.Errorf("label %q can't be changed from Vinaire", k)
		}
		return nil
	}
	for k, v := range b.Set {
		if err := check(k); err != nil {
			return err
		}
		if errs := validation.IsValidLabelValue(v); len(errs) > 0 {
			return fmt.Errorf("invalid value %q for label %q: %s", v, k, errs[0])
		}
	}
	for _, k := range b.Remove {
		if err := check(k); err != nil {
			return err
		}
		if _, both := b.Set[k]; both {
			return fmt.Errorf("label %q is both set and removed", k)
		}
	}
	return nil
}

// SetNodeLabels handles PUT /api/v1/nodes/{name}/labels.
func (h *Handler) SetNodeLabels(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	var b labelsBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if err := validateLabelChange(b); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := k8s.PatchNodeLabels(r.Context(), cs, name, b.Set, b.Remove); err != nil {
		respondK8sError(w, err)
		return
	}
	setKeys := make([]string, 0, len(b.Set))
	for k := range b.Set {
		setKeys = append(setKeys, k)
	}
	sort.Strings(setKeys)
	h.auditLog(r, "label-node", "", name, "set", setKeys, "removed", b.Remove)
	respondJSON(w, map[string]string{"status": "ok"})
}

type taintBody struct {
	Taints []struct {
		Key    string `json:"key"`
		Value  string `json:"value"`
		Effect string `json:"effect"`
	} `json:"taints"`
}

func validateTaints(b taintBody) ([]corev1.Taint, error) {
	if len(b.Taints) > maxNodeTaints {
		return nil, fmt.Errorf("too many taints (max %d)", maxNodeTaints)
	}
	out := make([]corev1.Taint, 0, len(b.Taints))
	seen := map[string]bool{}
	for _, t := range b.Taints {
		if errs := validation.IsQualifiedName(t.Key); len(errs) > 0 {
			return nil, fmt.Errorf("invalid taint key %q: %s", t.Key, errs[0])
		}
		if errs := validation.IsValidLabelValue(t.Value); len(errs) > 0 {
			return nil, fmt.Errorf("invalid taint value %q: %s", t.Value, errs[0])
		}
		switch corev1.TaintEffect(t.Effect) {
		case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			return nil, fmt.Errorf("invalid taint effect %q (NoSchedule, PreferNoSchedule or NoExecute)", t.Effect)
		}
		id := t.Key + "|" + t.Effect
		if seen[id] {
			return nil, fmt.Errorf("taint %s:%s is listed twice", t.Key, t.Effect)
		}
		seen[id] = true
		out = append(out, corev1.Taint{Key: t.Key, Value: t.Value, Effect: corev1.TaintEffect(t.Effect)})
	}
	return out, nil
}

// SetNodeTaints handles PUT /api/v1/nodes/{name}/taints — the node's complete
// taint list, replaced (guarded by the resourceVersion it was read at).
func (h *Handler) SetNodeTaints(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	var b taintBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	taints, err := validateTaints(b)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := k8s.ReplaceNodeTaints(r.Context(), cs, name, taints); err != nil {
		respondK8sError(w, err)
		return
	}
	desc := make([]string, 0, len(taints))
	for _, t := range taints {
		desc = append(desc, fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect))
	}
	h.auditLog(r, "taint-node", "", name, "taints", desc)
	respondJSON(w, map[string]string{"status": "ok"})
}

// DeleteNodeObject handles DELETE /api/v1/nodes/{name} — removes the Node object
// only; the body must repeat the name.
func (h *Handler) DeleteNodeObject(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name, ok := nodeNameParam(w, r)
	if !ok {
		return
	}
	var b confirmBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if b.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the node's name>"}`)
		return
	}
	if err := k8s.DeleteNode(r.Context(), cs, name); err != nil {
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, "delete-node", "", name)
	respondJSON(w, map[string]string{"status": "ok"})
}
