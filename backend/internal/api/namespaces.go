package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// Roadmap Phase 27: Namespaces as a managed resource. Reads are plain
// listers. Creating a namespace and creating/editing its ResourceQuota/
// LimitRange deliberately go through Phase 25's generic YAML apply rather
// than duplicate write endpoints here — the one genuinely new write is
// DELETE below (see AGENTS.md).

// reservedNamespaces can't be deleted through Vinaire even if the caller's
// RBAC would allow it: removing any of them breaks the cluster itself.
var reservedNamespaces = map[string]bool{
	"default":         true,
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// QuotaUsageRow is one resource of a ResourceQuota: how much of its hard
// limit is used. Pct is capped at 100 for the bar; Used/Hard are the raw
// Quantity strings.
type QuotaUsageRow struct {
	Resource string      `json:"resource"`
	Used     string      `json:"used"`
	Hard     string      `json:"hard"`
	Pct      int         `json:"pct"`
	Color    StatusColor `json:"color"`
}

// NamespaceRow matches the frontend's NamespaceRow type.
type NamespaceRow struct {
	Name        string          `json:"name"`
	Status      string          `json:"status"`
	StatusColor StatusColor     `json:"statusColor"`
	Age         string          `json:"age"`
	Pods        *int            `json:"pods"`
	Quota       []QuotaUsageRow `json:"quota"`
	Href        string          `json:"href"`
}

// quotaResourceOrder puts the figures people care about first; anything else
// sorts alphabetically after them.
var quotaResourceOrder = map[string]int{
	"pods": 0, "requests.cpu": 1, "cpu": 1, "requests.memory": 2, "memory": 2,
	"limits.cpu": 3, "limits.memory": 4,
}

func quotaRows(quotas []corev1.ResourceQuota) []QuotaUsageRow {
	rows := []QuotaUsageRow{}
	for _, q := range quotas {
		for name, hard := range q.Status.Hard {
			used := q.Status.Used[name]
			pct, color := 0, StatusOK
			if h := hard.AsApproximateFloat64(); h > 0 {
				pct = int(used.AsApproximateFloat64() / h * 100)
			}
			switch {
			case pct >= 100:
				pct, color = 100, StatusErr
			case pct >= 80:
				color = StatusWarn
			}
			rows = append(rows, QuotaUsageRow{
				Resource: string(name), Used: used.String(), Hard: hard.String(), Pct: pct, Color: color,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		oi, iok := quotaResourceOrder[rows[i].Resource]
		oj, jok := quotaResourceOrder[rows[j].Resource]
		switch {
		case iok && jok && oi != oj:
			return oi < oj
		case iok != jok:
			return iok
		}
		return rows[i].Resource < rows[j].Resource
	})
	return rows
}

func namespaceStatus(ns corev1.Namespace) (string, StatusColor) {
	if ns.Status.Phase == corev1.NamespaceTerminating {
		return "Terminating", StatusWarn
	}
	return "Active", StatusOK
}

// Namespaces handles GET /api/v1/namespaces.
func (h *Handler) Namespaces(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	namespaces, err := k8s.ListNamespaces(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	// Pod counts and quotas enrich the list but aren't worth failing it over
	// (a caller may be allowed to list namespaces but not pods/quotas).
	podCounts := map[string]int{}
	podsKnown := true
	if pods, err := k8s.ListPods(r.Context(), clientset, ""); err != nil {
		slog.Warn("namespaces: list pods failed", "error", err)
		podsKnown = false
	} else {
		for _, p := range pods {
			podCounts[p.Namespace]++
		}
	}
	quotasByNS := map[string][]corev1.ResourceQuota{}
	if quotas, err := k8s.ListResourceQuotas(r.Context(), clientset, ""); err != nil {
		slog.Warn("namespaces: list resourcequotas failed", "error", err)
	} else {
		for _, q := range quotas {
			quotasByNS[q.Namespace] = append(quotasByNS[q.Namespace], q)
		}
	}

	rows := make([]NamespaceRow, 0, len(namespaces))
	for _, ns := range namespaces {
		status, color := namespaceStatus(ns)
		row := NamespaceRow{
			Name: ns.Name, Status: status, StatusColor: color,
			Age:   formatAge(ns.CreationTimestamp.Time),
			Quota: quotaRows(quotasByNS[ns.Name]),
			Href:  "/namespaces/detail?name=" + url.QueryEscape(ns.Name),
		}
		if len(row.Quota) > 3 {
			row.Quota = row.Quota[:3]
		}
		if podsKnown {
			n := podCounts[ns.Name]
			row.Pods = &n
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	respondJSON(w, rows)
}

// QuotaDetail is one ResourceQuota with its usage and raw YAML.
type QuotaDetail struct {
	Name  string          `json:"name"`
	Items []QuotaUsageRow `json:"items"`
	YAML  string          `json:"yaml"`
}

// LimitRow is one line of a LimitRange: a type's per-resource bounds.
type LimitRow struct {
	Type           string `json:"type"`
	Resource       string `json:"resource"`
	Min            string `json:"min"`
	Max            string `json:"max"`
	Default        string `json:"default"`
	DefaultRequest string `json:"defaultRequest"`
}

// LimitRangeDetail is one LimitRange with its rows and raw YAML.
type LimitRangeDetail struct {
	Name   string     `json:"name"`
	Limits []LimitRow `json:"limits"`
	YAML   string     `json:"yaml"`
}

// NamespaceDetailResponse matches the frontend's NamespaceDetail type.
// Resources maps kind -> count; null means "couldn't list it" (e.g. RBAC),
// which must not read as zero.
type NamespaceDetailResponse struct {
	Name        string             `json:"name"`
	Status      string             `json:"status"`
	StatusColor StatusColor        `json:"statusColor"`
	Age         string             `json:"age"`
	Labels      map[string]string  `json:"labels"`
	Quotas      []QuotaDetail      `json:"quotas"`
	LimitRanges []LimitRangeDetail `json:"limitRanges"`
	Resources   map[string]*int    `json:"resources"`
	Protected   bool               `json:"protected"`
	YAML        string             `json:"yaml"`
}

func limitRows(lr corev1.LimitRange) []LimitRow {
	rows := []LimitRow{}
	for _, item := range lr.Spec.Limits {
		seen := map[corev1.ResourceName]bool{}
		for _, m := range []corev1.ResourceList{item.Min, item.Max, item.Default, item.DefaultRequest} {
			for name := range m {
				seen[name] = true
			}
		}
		names := make([]string, 0, len(seen))
		for n := range seen {
			names = append(names, string(n))
		}
		sort.Strings(names)
		for _, n := range names {
			get := func(m corev1.ResourceList) string {
				if q, ok := m[corev1.ResourceName(n)]; ok {
					return q.String()
				}
				return ""
			}
			rows = append(rows, LimitRow{
				Type: string(item.Type), Resource: n,
				Min: get(item.Min), Max: get(item.Max), Default: get(item.Default), DefaultRequest: get(item.DefaultRequest),
			})
		}
	}
	return rows
}

// NamespaceDetail handles GET /api/v1/namespaces/{name}.
func (h *Handler) NamespaceDetail(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")

	ns, err := k8s.GetNamespace(r.Context(), clientset, name)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	quotas := []QuotaDetail{}
	if list, err := k8s.ListResourceQuotas(r.Context(), clientset, name); err != nil {
		slog.Warn("namespace detail: list resourcequotas failed", "namespace", name, "error", err)
	} else {
		for _, q := range list {
			q.TypeMeta = metav1.TypeMeta{Kind: "ResourceQuota", APIVersion: "v1"}
			quotas = append(quotas, QuotaDetail{Name: q.Name, Items: quotaRows([]corev1.ResourceQuota{q}), YAML: marshalYAML(q)})
		}
	}
	limitRanges := []LimitRangeDetail{}
	if list, err := k8s.ListLimitRanges(r.Context(), clientset, name); err != nil {
		slog.Warn("namespace detail: list limitranges failed", "namespace", name, "error", err)
	} else {
		for _, lr := range list {
			rows := limitRows(lr)
			lr.TypeMeta = metav1.TypeMeta{Kind: "LimitRange", APIVersion: "v1"}
			limitRanges = append(limitRanges, LimitRangeDetail{Name: lr.Name, Limits: rows, YAML: marshalYAML(lr)})
		}
	}

	status, color := namespaceStatus(*ns)
	labels := ns.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	ns.TypeMeta = metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"}

	respondJSON(w, NamespaceDetailResponse{
		Name: ns.Name, Status: status, StatusColor: color,
		Age:         formatAge(ns.CreationTimestamp.Time),
		Labels:      labels,
		Quotas:      quotas,
		LimitRanges: limitRanges,
		Resources:   k8s.CountNamespaceResources(r.Context(), clientset, name),
		Protected:   reservedNamespaces[name],
		YAML:        marshalYAML(ns),
	})
}

type deleteNamespaceRequest struct {
	ConfirmName string `json:"confirmName"`
}

// DeleteNamespace handles DELETE /api/v1/namespaces/{name}. Deleting a
// namespace is irreversible and cascades to everything inside it, so the
// request must repeat the namespace's name (the UI makes the user type it),
// and the cluster's own system namespaces are refused outright.
func (h *Handler) DeleteNamespace(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")

	if reservedNamespaces[name] {
		respondError(w, http.StatusBadRequest, "namespace "+name+" is a system namespace and can't be deleted from Vinaire")
		return
	}
	var req deleteNamespaceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the namespace's name>"}`)
		return
	}

	if err := k8s.DeleteNamespace(r.Context(), clientset, name); err != nil {
		respondK8sError(w, err)
		return
	}

	h.auditLog(r, "delete-namespace", "", name)
	respondJSON(w, map[string]string{"status": "ok"})
}
