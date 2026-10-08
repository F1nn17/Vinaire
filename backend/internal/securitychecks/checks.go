// Package securitychecks is the light version of a cluster security review (Roadmap Phase
// 54): a fixed list of checks over what the signed-in user can already read — pods, roles,
// bindings, namespaces, services, ingresses. It is read-only and never needs a privileged
// Job on a node; what it cannot see through the Kubernetes API (kubelet and API-server flags,
// file permissions on a node, etcd encryption) is outside it, and said to be.
//
// The engine is a pure function over a Snapshot, so every check can be tested with
// hand-made objects. internal/api builds the Snapshot with the caller's own identity.
package securitychecks

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

// Snapshot is everything the checks read. A kind the caller may not list is absent from the
// slices and named in Unavailable, so the checks that need it answer "unknown" rather than
// "all clear".
type Snapshot struct {
	Namespaces          []corev1.Namespace
	Pods                []corev1.Pod
	Services            []corev1.Service
	Ingresses           []networkingv1.Ingress
	NetworkPolicies     []networkingv1.NetworkPolicy
	ResourceQuotas      []corev1.ResourceQuota
	Roles               []rbacv1.Role
	ClusterRoles        []rbacv1.ClusterRole
	RoleBindings        []rbacv1.RoleBinding
	ClusterRoleBindings []rbacv1.ClusterRoleBinding

	// Unavailable: kind -> why it couldn't be read.
	Unavailable map[string]string
}

// Severity of a check that fails.
type Severity string

const (
	High   Severity = "high"
	Medium Severity = "medium"
	Low    Severity = "low"
)

// Status of a check.
type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Unknown Status = "unknown"
)

// Finding is one thing a check found. Detail is a short fact (a container name, a role),
// never a sentence, so the interface can phrase it in either language.
type Finding struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Detail    string `json:"detail,omitempty"`
	// System: it is in a system namespace (kube-system…) and is not counted against the check.
	System bool `json:"system,omitempty"`
}

// Result is a check and what it found.
type Result struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Severity    Severity  `json:"severity"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Remediation string    `json:"remediation"`
	Status      Status    `json:"status"`
	Reason      string    `json:"reason,omitempty"` // why it is unknown
	Total       int       `json:"total"`            // findings outside system namespaces
	SystemTotal int       `json:"systemTotal"`      // findings in system namespaces
	Findings    []Finding `json:"findings"`         // capped; non-system first
}

// MaxFindings caps what one check returns (Total says how many there were).
const MaxFindings = 100

// Check is one entry of the list.
type Check struct {
	ID, Category string
	Severity     Severity
	Title        string
	Description  string
	Remediation  string
	// Needs names the Snapshot kinds the check reads.
	Needs []string
	Run   func(s *Snapshot) []Finding
}

var systemNamespaces = map[string]bool{"kube-system": true, "kube-public": true, "kube-node-lease": true}

// IsSystemNamespace is true for the cluster's own namespaces.
func IsSystemNamespace(ns string) bool { return systemNamespaces[ns] }

// RunAll evaluates every check against the snapshot, most serious failures first.
func RunAll(s *Snapshot) []Result {
	out := make([]Result, 0, len(Checks))
	for _, c := range Checks {
		r := Result{ID: c.ID, Category: c.Category, Severity: c.Severity, Title: c.Title,
			Description: c.Description, Remediation: c.Remediation, Findings: []Finding{}}
		var missing []string
		for _, need := range c.Needs {
			if why, bad := s.Unavailable[need]; bad {
				missing = append(missing, why)
			}
		}
		if len(missing) > 0 {
			r.Status, r.Reason = Unknown, strings.Join(missing, "; ")
			out = append(out, r)
			continue
		}
		found := c.Run(s)
		var user, system []Finding
		for _, f := range found {
			if f.System || IsSystemNamespace(f.Namespace) {
				f.System = true
				system = append(system, f)
			} else {
				user = append(user, f)
			}
		}
		sortFindings(user)
		sortFindings(system)
		r.Total, r.SystemTotal = len(user), len(system)
		r.Status = Pass
		if len(user) > 0 {
			r.Status = Fail
		}
		all := append(user, system...)
		if len(all) > MaxFindings {
			all = all[:MaxFindings]
		}
		r.Findings = all
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func rank(r Result) int {
	base := map[Severity]int{High: 0, Medium: 10, Low: 20}[r.Severity]
	switch r.Status {
	case Fail:
		return base
	case Unknown:
		return 40
	}
	return 50 + base
}

func sortFindings(f []Finding) {
	sort.Slice(f, func(i, j int) bool {
		a, b := f[i], f[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})
}

// Checks is the list, in the order they are explained to people.
var Checks = []Check{
	// ---- access ----
	{
		ID: "rbac-cluster-admin", Category: "Access", Severity: High,
		Title:       "Who holds cluster-admin",
		Description: "cluster-admin can do anything anywhere in the cluster. Every extra person, group or ServiceAccount bound to it widens what a stolen token can destroy.",
		Remediation: "Bind people to the narrower built-in roles (view, edit, admin) in the namespaces they work in, and keep cluster-admin for a very short list.",
		Needs:       []string{"clusterrolebindings"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for _, b := range s.ClusterRoleBindings {
				if b.RoleRef.Name != "cluster-admin" {
					continue
				}
				for _, sub := range b.Subjects {
					if strings.HasPrefix(sub.Name, "system:") {
						continue
					}
					out = append(out, Finding{Kind: "ClusterRoleBinding", Name: b.Name, Detail: subjectText(sub), System: sub.Kind == "ServiceAccount" && IsSystemNamespace(sub.Namespace)})
				}
			}
			return out
		},
	},
	{
		ID: "rbac-wildcard", Category: "Access", Severity: High,
		Title:       "Roles that allow everything",
		Description: "A role with “*” for both verbs and resources is cluster-admin in disguise, scoped to whatever it is bound in.",
		Remediation: "List the verbs and resources the role really needs.",
		Needs:       []string{"roles", "clusterroles"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for _, r := range s.ClusterRoles {
				if r.AggregationRule != nil || builtInRole(r.Name) {
					continue
				}
				if wildcardRules(r.Rules) {
					out = append(out, Finding{Kind: "ClusterRole", Name: r.Name})
				}
			}
			for _, r := range s.Roles {
				if wildcardRules(r.Rules) && !strings.HasPrefix(r.Name, "system:") {
					out = append(out, Finding{Kind: "Role", Namespace: r.Namespace, Name: r.Name})
				}
			}
			return out
		},
	},
	{
		ID: "rbac-anonymous", Category: "Access", Severity: High,
		Title:       "Rights given to anonymous users",
		Description: "A binding to system:anonymous or system:unauthenticated gives rights to anyone who can reach the API server, with no login.",
		Remediation: "Delete the binding unless it is meant to be public (a health check, for instance) and the role is as small as it can be.",
		Needs:       []string{"clusterrolebindings", "rolebindings"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			isAnon := func(sub rbacv1.Subject) bool {
				return (sub.Kind == "User" && sub.Name == "system:anonymous") || (sub.Kind == "Group" && sub.Name == "system:unauthenticated")
			}
			for _, b := range s.ClusterRoleBindings {
				if strings.HasPrefix(b.Name, "system:") {
					continue // the cluster's own (discovery, health), by design
				}
				for _, sub := range b.Subjects {
					if isAnon(sub) {
						out = append(out, Finding{Kind: "ClusterRoleBinding", Name: b.Name, Detail: b.RoleRef.Name})
					}
				}
			}
			for _, b := range s.RoleBindings {
				for _, sub := range b.Subjects {
					if isAnon(sub) && !strings.HasPrefix(b.Name, "system:") {
						out = append(out, Finding{Kind: "RoleBinding", Namespace: b.Namespace, Name: b.Name, Detail: b.RoleRef.Name})
					}
				}
			}
			return out
		},
	},
	{
		ID: "rbac-default-sa", Category: "Access", Severity: Medium,
		Title:       "Roles bound to a “default” ServiceAccount",
		Description: "Every pod that doesn't name a ServiceAccount runs as “default”. Giving it rights gives them to all of those pods, including ones added later.",
		Remediation: "Create a ServiceAccount for the workload that needs the rights and bind the role to that.",
		Needs:       []string{"clusterrolebindings", "rolebindings"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for _, b := range s.ClusterRoleBindings {
				if strings.HasPrefix(b.Name, "system:") {
					continue
				}
				for _, sub := range b.Subjects {
					if sub.Kind == "ServiceAccount" && sub.Name == "default" {
						out = append(out, Finding{Kind: "ClusterRoleBinding", Name: b.Name, Detail: sub.Namespace + " → " + b.RoleRef.Name})
					}
				}
			}
			for _, b := range s.RoleBindings {
				if strings.HasPrefix(b.Name, "system:") {
					continue
				}
				for _, sub := range b.Subjects {
					if sub.Kind == "ServiceAccount" && sub.Name == "default" {
						out = append(out, Finding{Kind: "RoleBinding", Namespace: b.Namespace, Name: b.Name, Detail: b.RoleRef.Name})
					}
				}
			}
			return out
		},
	},

	// ---- workloads ----
	{
		ID: "pod-privileged", Category: "Workloads", Severity: High,
		Title:       "Privileged containers",
		Description: "A privileged container has nearly the rights of root on the node it runs on; breaking out of it is trivial.",
		Remediation: "Drop privileged: true. If it needs one capability, add only that capability.",
		Needs:       []string{"pods"},
		Run: containerCheck(func(p *corev1.Pod, c corev1.Container) (string, bool) {
			return c.Name, c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged
		}),
	},
	{
		ID: "pod-host-access", Category: "Workloads", Severity: High,
		Title:       "Pods that share the node's network, processes or files",
		Description: "hostNetwork, hostPID, hostIPC and hostPath volumes let a pod see or change things on the node itself, outside its own container.",
		Remediation: "Use a Service or an emptyDir/PersistentVolume instead. Keep these only for node agents that truly need them.",
		Needs:       []string{"pods"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for i := range s.Pods {
				p := &s.Pods[i]
				if !running(p) {
					continue
				}
				var what []string
				if p.Spec.HostNetwork {
					what = append(what, "hostNetwork")
				}
				if p.Spec.HostPID {
					what = append(what, "hostPID")
				}
				if p.Spec.HostIPC {
					what = append(what, "hostIPC")
				}
				for _, v := range p.Spec.Volumes {
					if v.HostPath != nil {
						what = append(what, "hostPath "+v.HostPath.Path)
					}
				}
				if len(what) > 0 {
					out = append(out, Finding{Kind: "Pod", Namespace: p.Namespace, Name: p.Name, Detail: strings.Join(what, ", ")})
				}
			}
			return out
		},
	},
	{
		ID: "pod-capabilities", Category: "Workloads", Severity: Medium,
		Title:       "Containers with dangerous capabilities",
		Description: "Capabilities such as SYS_ADMIN, NET_ADMIN, SYS_PTRACE, SYS_MODULE or ALL give a container much of what root has.",
		Remediation: "Drop ALL capabilities and add back only the one the container needs.",
		Needs:       []string{"pods"},
		Run: containerCheck(func(p *corev1.Pod, c corev1.Container) (string, bool) {
			if c.SecurityContext == nil || c.SecurityContext.Capabilities == nil {
				return "", false
			}
			var bad []string
			for _, cap := range c.SecurityContext.Capabilities.Add {
				switch strings.ToUpper(string(cap)) {
				case "ALL", "SYS_ADMIN", "NET_ADMIN", "SYS_PTRACE", "SYS_MODULE":
					bad = append(bad, string(cap))
				}
			}
			return c.Name + ": " + strings.Join(bad, ", "), len(bad) > 0
		}),
	},
	{
		ID: "pod-root", Category: "Workloads", Severity: Low,
		Title:       "Containers that may run as root",
		Description: "A container that doesn't set runAsNonRoot (or a non-zero runAsUser) runs as whatever the image says — often root. A bug in the application then starts from root's position.",
		Remediation: "Set securityContext.runAsNonRoot: true (and a runAsUser) on the pod or container.",
		Needs:       []string{"pods"},
		Run: containerCheck(func(p *corev1.Pod, c corev1.Container) (string, bool) {
			nonRoot := func(sc *corev1.SecurityContext, psc *corev1.PodSecurityContext) bool {
				if sc != nil {
					if sc.RunAsNonRoot != nil {
						return *sc.RunAsNonRoot
					}
					if sc.RunAsUser != nil {
						return *sc.RunAsUser != 0
					}
				}
				if psc != nil {
					if psc.RunAsNonRoot != nil {
						return *psc.RunAsNonRoot
					}
					if psc.RunAsUser != nil {
						return *psc.RunAsUser != 0
					}
				}
				return false
			}
			return c.Name, !nonRoot(c.SecurityContext, p.Spec.SecurityContext)
		}),
	},
	{
		ID: "pod-image-latest", Category: "Workloads", Severity: Low,
		Title:       "Images that aren't pinned to a version",
		Description: "An image with no tag, or the tag “latest”, can change under a pod without anyone deciding to: a restart may bring a different program.",
		Remediation: "Use a version tag, or better a digest (image@sha256:…).",
		Needs:       []string{"pods"},
		Run: containerCheck(func(p *corev1.Pod, c corev1.Container) (string, bool) {
			img := c.Image
			if strings.Contains(img, "@sha256:") {
				return "", false
			}
			name := img[strings.LastIndex(img, "/")+1:]
			tag := ""
			if i := strings.LastIndex(name, ":"); i >= 0 {
				tag = name[i+1:]
			}
			return img, tag == "" || tag == "latest"
		}),
	},
	{
		ID: "pod-no-limits", Category: "Workloads", Severity: Low,
		Title:       "Containers with no memory limit",
		Description: "Without a memory limit one container can use all of a node's memory and push its neighbours out.",
		Remediation: "Set resources.limits.memory (the “Requests & Limits” page shows what is missing).",
		Needs:       []string{"pods"},
		Run: containerCheck(func(p *corev1.Pod, c corev1.Container) (string, bool) {
			_, has := c.Resources.Limits[corev1.ResourceMemory]
			return c.Name, !has
		}),
	},

	// ---- namespaces ----
	{
		ID: "ns-pod-security", Category: "Namespaces", Severity: Medium,
		Title:       "Namespaces with no Pod Security enforcement",
		Description: "The label pod-security.kubernetes.io/enforce makes the cluster itself refuse privileged and host-sharing pods in that namespace — the safety net under the checks above.",
		Remediation: "Label the namespace: kubectl label ns <name> pod-security.kubernetes.io/enforce=baseline (or restricted).",
		Needs:       []string{"namespaces"},
		Run: namespaceCheck(func(s *Snapshot, ns corev1.Namespace) (string, bool) {
			_, has := ns.Labels["pod-security.kubernetes.io/enforce"]
			return "", !has
		}),
	},
	{
		ID: "ns-network-policy", Category: "Namespaces", Severity: Medium,
		Title:       "Namespaces with no NetworkPolicy",
		Description: "Without any NetworkPolicy every pod can talk to every other pod in the cluster, so one compromised pod can reach all the rest.",
		Remediation: "Start with a default-deny policy per namespace and allow the traffic you need (this also requires a network plugin that enforces NetworkPolicy).",
		Needs:       []string{"namespaces", "networkpolicies"},
		Run: namespaceCheck(func(s *Snapshot, ns corev1.Namespace) (string, bool) {
			for _, np := range s.NetworkPolicies {
				if np.Namespace == ns.Name {
					return "", false
				}
			}
			return "", true
		}),
	},
	{
		ID: "ns-quota", Category: "Namespaces", Severity: Low,
		Title:       "Namespaces with no resource quota",
		Description: "Without a ResourceQuota, one namespace can consume what the others need.",
		Remediation: "Add a ResourceQuota (a project's quota template does this for every namespace in it).",
		Needs:       []string{"namespaces", "resourcequotas"},
		Run: namespaceCheck(func(s *Snapshot, ns corev1.Namespace) (string, bool) {
			for _, q := range s.ResourceQuotas {
				if q.Namespace == ns.Name {
					return "", false
				}
			}
			return "", true
		}),
	},

	// ---- exposure ----
	{
		ID: "svc-exposed", Category: "Exposure", Severity: Medium,
		Title:       "Services reachable from outside the cluster",
		Description: "NodePort and LoadBalancer Services open a port on the nodes (or a cloud address) to whatever can reach them.",
		Remediation: "Prefer ClusterIP behind an Ingress; put a firewall or an allow-list in front of what must stay open.",
		Needs:       []string{"services"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for _, svc := range s.Services {
				if svc.Spec.Type != corev1.ServiceTypeNodePort && svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
					continue
				}
				var ports []string
				for _, p := range svc.Spec.Ports {
					ports = append(ports, fmt.Sprint(p.Port))
				}
				out = append(out, Finding{Kind: "Service", Namespace: svc.Namespace, Name: svc.Name, Detail: string(svc.Spec.Type) + " " + strings.Join(ports, ",")})
			}
			return out
		},
	},
	{
		ID: "ingress-no-tls", Category: "Exposure", Severity: Medium,
		Title:       "Ingresses without TLS",
		Description: "An Ingress with no tls section serves plain HTTP: passwords and tokens cross the network readable.",
		Remediation: "Add a tls section (cert-manager can issue the certificate).",
		Needs:       []string{"ingresses"},
		Run: func(s *Snapshot) []Finding {
			var out []Finding
			for _, ing := range s.Ingresses {
				if len(ing.Spec.TLS) == 0 {
					var hosts []string
					for _, r := range ing.Spec.Rules {
						if r.Host != "" {
							hosts = append(hosts, r.Host)
						}
					}
					out = append(out, Finding{Kind: "Ingress", Namespace: ing.Namespace, Name: ing.Name, Detail: strings.Join(hosts, ", ")})
				}
			}
			return out
		},
	},
}

// ---- helpers ----

func subjectText(s rbacv1.Subject) string {
	if s.Kind == "ServiceAccount" {
		return "ServiceAccount " + s.Namespace + "/" + s.Name
	}
	return s.Kind + " " + s.Name
}

func builtInRole(name string) bool {
	return strings.HasPrefix(name, "system:") || name == "cluster-admin" || name == "admin" || name == "edit" || name == "view"
}

func wildcardRules(rules []rbacv1.PolicyRule) bool {
	has := func(list []string) bool {
		for _, v := range list {
			if v == "*" {
				return true
			}
		}
		return false
	}
	for _, r := range rules {
		if has(r.Verbs) && has(r.Resources) {
			return true
		}
	}
	return false
}

func running(p *corev1.Pod) bool {
	return p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed
}

// containerCheck builds a check over every container (init containers too) of every live pod.
func containerCheck(test func(p *corev1.Pod, c corev1.Container) (detail string, flagged bool)) func(*Snapshot) []Finding {
	return func(s *Snapshot) []Finding {
		var out []Finding
		for i := range s.Pods {
			p := &s.Pods[i]
			if !running(p) {
				continue
			}
			var details []string
			for _, c := range p.Spec.InitContainers {
				if d, bad := test(p, c); bad {
					details = append(details, d)
				}
			}
			for _, c := range p.Spec.Containers {
				if d, bad := test(p, c); bad {
					details = append(details, d)
				}
			}
			if len(details) > 0 {
				out = append(out, Finding{Kind: "Pod", Namespace: p.Namespace, Name: p.Name, Detail: strings.Join(details, "; ")})
			}
		}
		return out
	}
}

// namespaceCheck builds a check over the namespaces people made (system ones are skipped, and so
// are namespaces being deleted).
func namespaceCheck(test func(s *Snapshot, ns corev1.Namespace) (detail string, flagged bool)) func(*Snapshot) []Finding {
	return func(s *Snapshot) []Finding {
		var out []Finding
		for _, ns := range s.Namespaces {
			if IsSystemNamespace(ns.Name) || ns.Status.Phase == corev1.NamespaceTerminating {
				continue
			}
			if d, bad := test(s, ns); bad {
				out = append(out, Finding{Kind: "Namespace", Name: ns.Name, Detail: d})
			}
		}
		return out
	}
}
