package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"

	"github.com/shiraku/vinaire/backend/internal/authz"
	"github.com/shiraku/vinaire/backend/internal/k8s"
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
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	secrets, err := k8s.ListSecrets(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]SecretRow, 0, len(secrets))
	for _, s := range secrets {
		rows = append(rows, secretRow(s))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row SecretRow) string { return row.Namespace })
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
// it's meant to catch). The caller's identity now comes from their verified
// OIDC token (Roadmap Phase 10) instead of a best-effort source IP.
func (h *Handler) RevealSecretKey(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")
	if _, _, ok := h.authorize(w, r, namespace, "read"); !ok {
		return
	}

	var req revealRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Key == "" {
		respondError(w, http.StatusBadRequest, `request body must be {"key": "<key name>"}`)
		return
	}

	secret, err := k8s.GetSecret(r.Context(), cluster.Clientset, namespace, name)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	value, ok := secret.Data[req.Key]
	if !ok {
		respondError(w, http.StatusNotFound, "key not found in secret")
		return
	}

	claims, _ := authz.FromContext(r.Context())
	slog.Warn("secret revealed",
		"namespace", namespace,
		"secret", name,
		"key", req.Key,
		"user", claims.Username,
		"remote_addr", r.RemoteAddr,
	)

	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, revealResponse{Value: string(value)})
}
