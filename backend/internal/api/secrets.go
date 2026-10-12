package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// SecretKeyRow matches the frontend's SecretKey type. Deliberately no
// value field — see the package doc on RevealSecretKey.
type SecretKeyRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// SecretRow matches the frontend's SecretDetail type
// (frontend/src/lib/types.ts).
type SecretRow struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Type      string         `json:"type"`
	Age       string         `json:"age"`
	Keys      []SecretKeyRow `json:"keys"`
}

// Secrets handles GET /api/v1/secrets?namespace= — metadata and key *names*
// only. Secret values are never included here, masked or otherwise: there's
// nothing for a browser to accidentally leak if it was never sent. Use the
// explicit, audited RevealSecretKey endpoint to fetch one value at a time.
func (h *Handler) Secrets(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	secrets, err := k8s.ListSecrets(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]SecretRow, 0, len(secrets))
	for _, s := range secrets {
		rows = append(rows, secretRow(s))
	}
	respondJSON(w, rows)
}

func secretRow(s corev1.Secret) SecretRow {
	keys := make([]SecretKeyRow, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, SecretKeyRow{ID: k, Label: k})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Label < keys[j].Label })

	return SecretRow{
		ID:        string(s.UID),
		Name:      s.Name,
		Namespace: s.Namespace,
		Type:      string(s.Type),
		Age:       formatAge(s.CreationTimestamp.Time),
		Keys:      keys,
	}
}

type revealRequest struct {
	Key string `json:"key"`
}

type revealResponse struct {
	Value string `json:"value"`
}

// RevealSecretKey handles POST /api/v1/secrets/{namespace}/{name}/reveal.
// This is the ONLY path in the backend that ever returns a decoded secret
// value, and every call is audit-logged (namespace/secret/key + caller
// identity — never the value itself, or the audit log becomes the leak
// it's meant to catch).
func (h *Handler) RevealSecretKey(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	var req revealRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Key == "" {
		respondError(w, http.StatusBadRequest, `request body must be {"key": "<key name>"}`)
		return
	}

	secret, err := k8s.GetSecret(r.Context(), clientset, namespace, name)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	value, ok := secret.Data[req.Key]
	if !ok {
		respondError(w, http.StatusNotFound, "key not found in secret")
		return
	}

	h.auditLog(r, "reveal_secret", namespace, name, "key", req.Key)

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, revealResponse{Value: string(value)})
}

type deleteSecretRequest struct {
	ConfirmName string `json:"confirmName"`
}

// DeleteSecret handles DELETE /api/v1/secrets/{namespace}/{name} (owner's decision of 2026-10-09: what
// an uninstalled chart leaves behind — a database's password Secret — must be removable): a plain
// Delete() of one Secret, as the caller. The body must repeat the Secret's name, a system
// namespace is refused, and so is a Helm release record (type helm.sh/release.v1): deleting that
// makes Helm forget a release whose objects are still running — uninstall it instead. Audited as
// delete-secret (namespace, name, type — never a value).
func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if !k8sNameRe.MatchString(namespace) || !k8sNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid namespace or secret name")
		return
	}
	if reservedNamespaces[namespace] {
		respondError(w, http.StatusBadRequest, "namespace "+namespace+" is a system namespace; its secrets can't be deleted from Vinaire")
		return
	}
	var req deleteSecretRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the secret's name>"}`)
		return
	}
	typ, err := k8s.GetSecretType(r.Context(), clientset, namespace, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	if typ == "helm.sh/release.v1" {
		respondError(w, http.StatusConflict, "this Secret is the record of a Helm release; uninstall the release from the Marketplace instead")
		return
	}
	if err := k8s.DeleteSecret(r.Context(), clientset, namespace, name); err != nil {
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, "delete-secret", namespace, name, "type", string(typ))
	respondJSON(w, map[string]string{"status": "ok"})
}
