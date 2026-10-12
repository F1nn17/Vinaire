package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// RoleRow matches the frontend's RoleRow type (frontend/src/lib/types.ts).
type RoleRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Rules     int    `json:"rules"`
	Age       string `json:"age"`
}

// Roles handles GET /api/v1/roles?namespace=.
func (h *Handler) Roles(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	roles, err := k8s.ListRoles(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]RoleRow, 0, len(roles))
	for _, ro := range roles {
		rows = append(rows, RoleRow{
			Name:      ro.Name,
			Namespace: ro.Namespace,
			Rules:     len(ro.Rules),
			Age:       formatAge(ro.CreationTimestamp.Time),
		})
	}
	respondJSON(w, rows)
}

// ClusterRoleRow matches the frontend's ClusterRoleRow type.
// ClusterRoles are cluster-scoped.
type ClusterRoleRow struct {
	Name  string `json:"name"`
	Rules int    `json:"rules"`
	Age   string `json:"age"`
}

// ClusterRoles handles GET /api/v1/clusterroles.
func (h *Handler) ClusterRoles(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	roles, err := k8s.ListClusterRoles(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]ClusterRoleRow, 0, len(roles))
	for _, cr := range roles {
		rows = append(rows, ClusterRoleRow{
			Name:  cr.Name,
			Rules: len(cr.Rules),
			Age:   formatAge(cr.CreationTimestamp.Time),
		})
	}
	respondJSON(w, rows)
}

// RoleBindingRow matches the frontend's RoleBindingRow type.
type RoleBindingRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	RoleKind  string `json:"roleKind"`
	RoleName  string `json:"roleName"`
	Subjects  string `json:"subjects"`
	Age       string `json:"age"`
}

// RoleBindings handles GET /api/v1/rolebindings?namespace=.
func (h *Handler) RoleBindings(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	bindings, err := k8s.ListRoleBindings(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]RoleBindingRow, 0, len(bindings))
	for _, rb := range bindings {
		rows = append(rows, RoleBindingRow{
			Name:      rb.Name,
			Namespace: rb.Namespace,
			RoleKind:  rb.RoleRef.Kind,
			RoleName:  rb.RoleRef.Name,
			Subjects:  subjectsString(rb.Subjects),
			Age:       formatAge(rb.CreationTimestamp.Time),
		})
	}
	respondJSON(w, rows)
}

// ClusterRoleBindingRow matches the frontend's ClusterRoleBindingRow type.
// ClusterRoleBindings are cluster-scoped.
type ClusterRoleBindingRow struct {
	Name     string `json:"name"`
	RoleKind string `json:"roleKind"`
	RoleName string `json:"roleName"`
	Subjects string `json:"subjects"`
	Age      string `json:"age"`
}

// ClusterRoleBindings handles GET /api/v1/clusterrolebindings.
func (h *Handler) ClusterRoleBindings(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}

	bindings, err := k8s.ListClusterRoleBindings(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]ClusterRoleBindingRow, 0, len(bindings))
	for _, crb := range bindings {
		rows = append(rows, ClusterRoleBindingRow{
			Name:     crb.Name,
			RoleKind: crb.RoleRef.Kind,
			RoleName: crb.RoleRef.Name,
			Subjects: subjectsString(crb.Subjects),
			Age:      formatAge(crb.CreationTimestamp.Time),
		})
	}
	respondJSON(w, rows)
}

// ServiceAccountRow matches the frontend's ServiceAccountRow type.
type ServiceAccountRow struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Secrets   int    `json:"secrets"`
	Age       string `json:"age"`
}

// ServiceAccounts handles GET /api/v1/serviceaccounts?namespace=.
func (h *Handler) ServiceAccounts(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	accounts, err := k8s.ListServiceAccounts(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	rows := make([]ServiceAccountRow, 0, len(accounts))
	for _, sa := range accounts {
		rows = append(rows, ServiceAccountRow{
			Name:      sa.Name,
			Namespace: sa.Namespace,
			Secrets:   len(sa.Secrets),
			Age:       formatAge(sa.CreationTimestamp.Time),
		})
	}
	respondJSON(w, rows)
}

func subjectsString(subjects []rbacv1.Subject) string {
	if len(subjects) == 0 {
		return "—"
	}
	parts := make([]string, len(subjects))
	for i, s := range subjects {
		if s.Namespace != "" {
			parts[i] = fmt.Sprintf("%s:%s/%s", s.Kind, s.Namespace, s.Name)
		} else {
			parts[i] = fmt.Sprintf("%s:%s", s.Kind, s.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// RBACSubject matches the frontend's RBACSubject type.
type RBACSubject struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// RBACRule matches the frontend's RBACRule type — a policy rule rendered as
// plain readable strings rather than raw slices.
type RBACRule struct {
	APIGroups string `json:"apiGroups"`
	Resources string `json:"resources"`
	Verbs     string `json:"verbs"`
}

// RBACBindingSummary matches the frontend's RBACBindingSummary type. This is
// a static "what's configured" rollup — bindings correlated with the rules
// their referenced role actually grants — never a live SubjectAccessReview/
// "can I do X" check (see this phase's own scope note in ROADMAP.md).
type RBACBindingSummary struct {
	Namespace   string        `json:"namespace"`
	BindingKind string        `json:"bindingKind"`
	BindingName string        `json:"bindingName"`
	RoleKind    string        `json:"roleKind"`
	RoleName    string        `json:"roleName"`
	Subjects    []RBACSubject `json:"subjects"`
	Rules       []RBACRule    `json:"rules"`
}

// RBACSummary handles GET /api/v1/rbac/summary?namespace= — every
// RoleBinding in namespace (or every namespace, if empty), correlated with
// the rules its referenced Role or ClusterRole actually grants.
// ClusterRoleBindings have their own dedicated tab in the UI and aren't
// duplicated into this per-namespace view.
func (h *Handler) RBACSummary(w http.ResponseWriter, r *http.Request) {
	_, clientset, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	namespace := r.URL.Query().Get("namespace")

	roleBindings, err := k8s.ListRoleBindings(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	roles, err := k8s.ListRoles(r.Context(), clientset, namespace)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	clusterRoles, err := k8s.ListClusterRoles(r.Context(), clientset)
	if err != nil {
		respondK8sError(w, err)
		return
	}

	roleByKey := make(map[string]rbacv1.Role, len(roles))
	for _, ro := range roles {
		roleByKey[ro.Namespace+"/"+ro.Name] = ro
	}
	clusterRoleByName := make(map[string]rbacv1.ClusterRole, len(clusterRoles))
	for _, cr := range clusterRoles {
		clusterRoleByName[cr.Name] = cr
	}

	summaries := make([]RBACBindingSummary, 0, len(roleBindings))
	for _, rb := range roleBindings {
		var rules []rbacv1.PolicyRule
		switch rb.RoleRef.Kind {
		case "Role":
			if ro, found := roleByKey[rb.Namespace+"/"+rb.RoleRef.Name]; found {
				rules = ro.Rules
			}
		case "ClusterRole":
			if cr, found := clusterRoleByName[rb.RoleRef.Name]; found {
				rules = cr.Rules
			}
		}

		summaries = append(summaries, RBACBindingSummary{
			Namespace:   rb.Namespace,
			BindingKind: "RoleBinding",
			BindingName: rb.Name,
			RoleKind:    rb.RoleRef.Kind,
			RoleName:    rb.RoleRef.Name,
			Subjects:    rbacSubjects(rb.Subjects),
			Rules:       rbacRules(rules),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Namespace != summaries[j].Namespace {
			return summaries[i].Namespace < summaries[j].Namespace
		}
		return summaries[i].BindingName < summaries[j].BindingName
	})

	respondJSON(w, summaries)
}

func rbacSubjects(subjects []rbacv1.Subject) []RBACSubject {
	out := make([]RBACSubject, 0, len(subjects))
	for _, s := range subjects {
		out = append(out, RBACSubject{Kind: s.Kind, Name: s.Name, Namespace: s.Namespace})
	}
	return out
}

func rbacRules(rules []rbacv1.PolicyRule) []RBACRule {
	out := make([]RBACRule, 0, len(rules))
	for _, ru := range rules {
		out = append(out, RBACRule{
			APIGroups: apiGroupsString(ru.APIGroups),
			Resources: joinOrDash(ru.Resources),
			Verbs:     joinOrDash(ru.Verbs),
		})
	}
	return out
}

// apiGroupsString renders PolicyRule.APIGroups readably — an empty string
// entry means the core API group, which client-go represents as "".
func apiGroupsString(groups []string) string {
	if len(groups) == 0 {
		return "—"
	}
	parts := make([]string, len(groups))
	for i, g := range groups {
		if g == "" {
			parts[i] = "core"
		} else {
			parts[i] = g
		}
	}
	return strings.Join(parts, ", ")
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "—"
	}
	return strings.Join(items, ", ")
}
