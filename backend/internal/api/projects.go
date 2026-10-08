package api

import (
	"log/slog"
	"net/http"
	"regexp"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/shiraku/vinaire/backend/internal/k8s"
)

// Projects (Roadmap Phase 53): a project is a set of namespaces carrying the label
// vinaire.io/project=<name>. Nothing about a project is kept inside Vinaire — it lives in the
// cluster, on the namespaces, so the cluster's RBAC decides who may see or change it, and a
// cluster inspected with kubectl shows the same grouping.
//
// These two handlers only READ, as the caller. Everything that changes a project goes through
// paths that already exist and are already audited: labelling or creating a namespace and its
// ResourceQuota is Phase 25's YAML apply, and adding or removing a member is Phase 44's
// role-binding endpoints.

// ProjectLabel is the label that puts a namespace in a project.
const ProjectLabel = "vinaire.io/project"

// projectNameRe is a valid label value that is also a sane short name.
var projectNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,40}[a-z0-9])?$`)

// ProjectRow is one line of the projects list.
type ProjectRow struct {
	Name       string          `json:"name"`
	Namespaces []string        `json:"namespaces"`
	Pods       *int            `json:"pods"`
	Quota      []QuotaUsageRow `json:"quota"`
	Members    int             `json:"members"`
	Href       string          `json:"href"`
}

// ProjectNamespace is a namespace inside a project, with its quota.
type ProjectNamespace struct {
	Name  string          `json:"name"`
	Pods  *int            `json:"pods"`
	Quota []QuotaUsageRow `json:"quota"`
	Age   string          `json:"age"`
	Href  string          `json:"href"`
}

// ProjectMember is a subject that holds a role in one or more of the project's namespaces.
type ProjectMember struct {
	Kind      string         `json:"kind"` // User | Group | ServiceAccount
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"` // ServiceAccount only
	Grants    []ProjectGrant `json:"grants"`
}

// ProjectGrant is one binding that gives a member a role in a namespace.
type ProjectGrant struct {
	Namespace string `json:"namespace"`
	Binding   string `json:"binding"`
	RoleKind  string `json:"roleKind"`
	RoleName  string `json:"roleName"`
}

// ProjectDetail is GET /api/v1/projects/{name}.
type ProjectDetail struct {
	Name       string             `json:"name"`
	Namespaces []ProjectNamespace `json:"namespaces"`
	Members    []ProjectMember    `json:"members"`
	// MembersKnown is false when the caller may not list role bindings.
	MembersKnown bool `json:"membersKnown"`
}

type nsInfo struct {
	pods   *int
	quotas []corev1.ResourceQuota
}

// namespaceFigures gathers pod counts and quotas per namespace, tolerating a caller who may
// list namespaces but not pods or quotas (the same stance as the Namespaces list).
func (h *Handler) namespaceFigures(pods func() ([]corev1.Pod, error), quotas func() ([]corev1.ResourceQuota, error)) map[string]*nsInfo {
	out := map[string]*nsInfo{}
	get := func(ns string) *nsInfo {
		if out[ns] == nil {
			out[ns] = &nsInfo{}
		}
		return out[ns]
	}
	if list, err := pods(); err != nil {
		slog.Warn("projects: list pods failed", "error", err)
	} else {
		counts := map[string]int{}
		for _, p := range list {
			counts[p.Namespace]++
		}
		for ns := range counts {
			n := counts[ns]
			get(ns).pods = &n
		}
	}
	if list, err := quotas(); err != nil {
		slog.Warn("projects: list resourcequotas failed", "error", err)
	} else {
		for _, q := range list {
			get(q.Namespace).quotas = append(get(q.Namespace).quotas, q)
		}
	}
	return out
}

// Projects handles GET /api/v1/projects.
func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	namespaces, err := k8s.ListNamespaces(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	byProject := map[string][]string{}
	for _, ns := range namespaces {
		if p := ns.Labels[ProjectLabel]; p != "" && projectNameRe.MatchString(p) {
			byProject[p] = append(byProject[p], ns.Name)
		}
	}
	figures := h.namespaceFigures(
		func() ([]corev1.Pod, error) { return k8s.ListPods(ctx, cs, "") },
		func() ([]corev1.ResourceQuota, error) { return k8s.ListResourceQuotas(ctx, cs, "") })

	// members: distinct subjects holding any binding in the project's namespaces
	subjectsByNS := map[string]map[string]bool{}
	if bindings, err := k8s.ListRoleBindings(ctx, cs, ""); err == nil {
		for _, b := range bindings {
			if subjectsByNS[b.Namespace] == nil {
				subjectsByNS[b.Namespace] = map[string]bool{}
			}
			for _, s := range b.Subjects {
				subjectsByNS[b.Namespace][subjectKey(s)] = true
			}
		}
	}

	rows := make([]ProjectRow, 0, len(byProject))
	for name, nss := range byProject {
		sort.Strings(nss)
		row := ProjectRow{Name: name, Namespaces: nss, Href: "/projects/detail?name=" + name}
		var quotas []corev1.ResourceQuota
		total, known := 0, false
		members := map[string]bool{}
		for _, ns := range nss {
			if f := figures[ns]; f != nil {
				quotas = append(quotas, f.quotas...)
				if f.pods != nil {
					total += *f.pods
					known = true
				}
			}
			for k := range subjectsByNS[ns] {
				members[k] = true
			}
		}
		if known {
			row.Pods = &total
		}
		row.Quota = sumQuota(quotas)
		row.Members = len(members)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	respondJSON(w, rows)
}

// ProjectDetail handles GET /api/v1/projects/{name}.
func (h *Handler) ProjectDetail(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !projectNameRe.MatchString(name) {
		respondError(w, http.StatusBadRequest, "invalid project name")
		return
	}
	ctx := r.Context()
	namespaces, err := k8s.ListNamespaces(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	var mine []corev1.Namespace
	inProject := map[string]bool{}
	for _, ns := range namespaces {
		if ns.Labels[ProjectLabel] == name {
			mine = append(mine, ns)
			inProject[ns.Name] = true
		}
	}
	if len(mine) == 0 {
		respondError(w, http.StatusNotFound, "no such project")
		return
	}
	figures := h.namespaceFigures(
		func() ([]corev1.Pod, error) { return k8s.ListPods(ctx, cs, "") },
		func() ([]corev1.ResourceQuota, error) { return k8s.ListResourceQuotas(ctx, cs, "") })

	d := ProjectDetail{Name: name, Namespaces: []ProjectNamespace{}, Members: []ProjectMember{}}
	for _, ns := range mine {
		pn := ProjectNamespace{Name: ns.Name, Age: formatAge(ns.CreationTimestamp.Time), Href: "/namespaces/detail?name=" + ns.Name}
		if f := figures[ns.Name]; f != nil {
			pn.Pods, pn.Quota = f.pods, quotaRows(f.quotas)
		} else {
			pn.Quota = []QuotaUsageRow{}
		}
		d.Namespaces = append(d.Namespaces, pn)
	}
	sort.Slice(d.Namespaces, func(i, j int) bool { return d.Namespaces[i].Name < d.Namespaces[j].Name })

	if bindings, err := k8s.ListRoleBindings(ctx, cs, ""); err == nil {
		d.MembersKnown = true
		d.Members = membersOf(bindings, inProject)
	}
	respondJSON(w, d)
}

func subjectKey(s rbacv1.Subject) string {
	return s.Kind + "/" + s.Namespace + "/" + s.Name
}

func membersOf(bindings []rbacv1.RoleBinding, inProject map[string]bool) []ProjectMember {
	byKey := map[string]*ProjectMember{}
	var order []string
	for _, b := range bindings {
		if !inProject[b.Namespace] {
			continue
		}
		for _, s := range b.Subjects {
			k := subjectKey(s)
			m := byKey[k]
			if m == nil {
				m = &ProjectMember{Kind: s.Kind, Name: s.Name, Namespace: s.Namespace, Grants: []ProjectGrant{}}
				byKey[k] = m
				order = append(order, k)
			}
			m.Grants = append(m.Grants, ProjectGrant{Namespace: b.Namespace, Binding: b.Name, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name})
		}
	}
	sort.Strings(order)
	out := make([]ProjectMember, 0, len(order))
	for _, k := range order {
		sort.Slice(byKey[k].Grants, func(i, j int) bool {
			a, b := byKey[k].Grants[i], byKey[k].Grants[j]
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Binding < b.Binding
		})
		out = append(out, *byKey[k])
	}
	return out
}

// sumQuota adds up the same resource across a project's quotas, so the list shows one
// figure per resource for the whole project (quantities are added exactly, not as floats).
func sumQuota(quotas []corev1.ResourceQuota) []QuotaUsageRow {
	synth := corev1.ResourceQuota{Status: corev1.ResourceQuotaStatus{Hard: corev1.ResourceList{}, Used: corev1.ResourceList{}}}
	for _, q := range quotas {
		for name, h := range q.Status.Hard {
			hs := synth.Status.Hard[name]
			hs.Add(h)
			synth.Status.Hard[name] = hs
			us := synth.Status.Used[name]
			us.Add(q.Status.Used[name])
			synth.Status.Used[name] = us
		}
	}
	rows := quotaRows([]corev1.ResourceQuota{synth})
	if len(rows) > 4 {
		rows = rows[:4]
	}
	return rows
}
