package api

import (
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// ConfigMapKeyRow matches the frontend's ConfigMapKey type.
type ConfigMapKeyRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ConfigMapRow matches the frontend's ConfigMapDetail type
// (frontend/src/lib/types.ts). Unlike Secrets, ConfigMap data isn't
// sensitive, so values are included directly here — no reveal step needed.
type ConfigMapRow struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Age       string            `json:"age"`
	Keys      []ConfigMapKeyRow `json:"keys"`
}

func (h *Handler) ConfigMaps(w http.ResponseWriter, r *http.Request) {
	cluster, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")
	allowed, allowAll, ok := h.authorize(w, r, namespace, "read")
	if !ok {
		return
	}

	configMaps, err := k8s.ListConfigMaps(r.Context(), cluster.Clientset, namespace)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	rows := make([]ConfigMapRow, 0, len(configMaps))
	for _, cm := range configMaps {
		rows = append(rows, configMapRow(cm))
	}
	rows = filterByAllowedNamespace(rows, namespace, allowed, allowAll, func(row ConfigMapRow) string { return row.Namespace })
	respondJSON(w, rows)
}

func configMapRow(cm corev1.ConfigMap) ConfigMapRow {
	keys := make([]ConfigMapKeyRow, 0, len(cm.Data)+len(cm.BinaryData))
	for k, v := range cm.Data {
		keys = append(keys, ConfigMapKeyRow{Label: k, Value: v})
	}
	for k := range cm.BinaryData {
		// Binary values aren't meaningfully displayable as text; show a
		// placeholder rather than mangling bytes through a JSON string.
		keys = append(keys, ConfigMapKeyRow{Label: k, Value: "<binary data>"})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Label < keys[j].Label })

	return ConfigMapRow{
		ID:        string(cm.UID),
		Name:      cm.Name,
		Namespace: cm.Namespace,
		Age:       formatAge(cm.CreationTimestamp.Time),
		Keys:      keys,
	}
}
