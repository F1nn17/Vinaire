package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	authzv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/k8s"
)

// RBAC management (Roadmap Phase 44): bind an existing Role or ClusterRole to a user,
// group or ServiceAccount, remove a binding, and ask "what can X do?".
//
// Deliberately narrow (see AGENTS.md): Vinaire creates and deletes only
// RoleBindings and ClusterRoleBindings — never Roles or ClusterRoles, so it can't
// invent new permissions, only hand out ones that already exist. Every call runs
// as the caller, so Kubernetes' own escalation check applies: nobody can bind a
// role carrying rights they don't hold themselves unless the cluster has granted
// them the `bind` verb on it. Granting cluster-wide needs the role's name typed.

const maxBindingSubjects = 20

var (
	// users and groups come from the identity provider: any printable name without
	// whitespace or control characters
	principalRe = regexp.MustCompile(`^[^\s\x00-\x1f\x7f]{1,253}$`)
	verbRe      = regexp.MustCompile(`^(\*|[a-z]{1,30})$`)
	rbacResRe   = regexp.MustCompile(`^(\*|[a-z0-9]([-a-z0-9./*]{0,100}[a-z0-9*])?)$`)
	nonNameChar = regexp.MustCompile(`[^a-z0-9.-]+`)
)

type subjectBody struct {
	Kind      string `json:"kind"` // User | Group | ServiceAccount
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"` // ServiceAccount only
}

func (s subjectBody) validate() error {
	switch s.Kind {
	case "User", "Group":
		if !principalRe.MatchString(s.Name) {
			return fmt.Errorf("invalid %s name %q", strings.ToLower(s.Kind), s.Name)
		}
		if s.Namespace != "" {
			return fmt.Errorf("a %s has no namespace", strings.ToLower(s.Kind))
		}
	case "ServiceAccount":
		if errs := validation.IsDNS1123Subdomain(s.Name); len(errs) > 0 {
			return fmt.Errorf("invalid ServiceAccount name %q", s.Name)
		}
		if errs := validation.IsDNS1123Label(s.Namespace); len(errs) > 0 {
			return fmt.Errorf("a ServiceAccount needs its namespace")
		}
	default:
		return fmt.Errorf("subject kind must be User, Group or ServiceAccount")
	}
	return nil
}

func (s subjectBody) rbac() rbacv1.Subject {
	if s.Kind == "ServiceAccount" {
		return rbacv1.Subject{Kind: "ServiceAccount", Name: s.Name, Namespace: s.Namespace}
	}
	return rbacv1.Subject{Kind: s.Kind, APIGroup: rbacv1.GroupName, Name: s.Name}
}

type bindingBody struct {
	Scope       string        `json:"scope"` // "cluster" | "namespace"
	Namespace   string        `json:"namespace,omitempty"`
	Name        string        `json:"name,omitempty"` // generated when empty
	RoleKind    string        `json:"roleKind"`       // ClusterRole | Role
	RoleName    string        `json:"roleName"`
	Subjects    []subjectBody `json:"subjects"`
	ConfirmName string        `json:"confirmName,omitempty"` // cluster scope: the role's name, typed
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generatedBindingName is "vinaire-<role>-<random>" cut to a valid object name.
func generatedBindingName(role string) string {
	r := strings.Trim(nonNameChar.ReplaceAllString(strings.ToLower(role), "-"), "-.")
	if len(r) > 40 {
		r = strings.Trim(r[:40], "-.")
	}
	if r == "" {
		r = "role"
	}
	return "vinaire-" + r + "-" + randomSuffix()
}

func validateBinding(b *bindingBody) error {
	switch b.Scope {
	case "cluster":
		if b.RoleKind != "ClusterRole" {
			return errors.New("a cluster-wide binding can only refer to a ClusterRole")
		}
		if b.Namespace != "" {
			return errors.New("a cluster-wide binding has no namespace")
		}
		if b.ConfirmName != b.RoleName {
			return errors.New(`granting a role cluster-wide needs "confirmName" to repeat the role's name`)
		}
	case "namespace":
		if errs := validation.IsDNS1123Label(b.Namespace); len(errs) > 0 {
			return errors.New("a namespaced binding needs a valid namespace")
		}
		if b.RoleKind != "ClusterRole" && b.RoleKind != "Role" {
			return errors.New("roleKind must be Role or ClusterRole")
		}
	default:
		return errors.New(`scope must be "cluster" or "namespace"`)
	}
	if errs := validation.IsDNS1123Subdomain(b.RoleName); len(errs) > 0 && !strings.Contains(b.RoleName, ":") {
		return errors.New("invalid role name")
	}
	if len(b.RoleName) == 0 || len(b.RoleName) > 253 {
		return errors.New("invalid role name")
	}
	if len(b.Subjects) == 0 || len(b.Subjects) > maxBindingSubjects {
		return fmt.Errorf("give between 1 and %d subjects", maxBindingSubjects)
	}
	for _, s := range b.Subjects {
		if err := s.validate(); err != nil {
			return err
		}
	}
	if b.Name == "" {
		b.Name = generatedBindingName(b.RoleName)
	} else if errs := validation.IsDNS1123Subdomain(b.Name); len(errs) > 0 {
		return errors.New("invalid binding name")
	}
	return nil
}

func callerLabel(r *http.Request) string {
	if sess, ok := authn.FromContext(r.Context()); ok && sess.Username != "" {
		return sess.Username
	}
	return "token session"
}

// CreateBinding handles POST /api/v1/rbac/bindings.
func (h *Handler) CreateBinding(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	var b bindingBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if err := validateBinding(&b); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	subjects := make([]rbacv1.Subject, 0, len(b.Subjects))
	for _, s := range b.Subjects {
		subjects = append(subjects, s.rbac())
	}
	meta := metav1.ObjectMeta{
		Name:        b.Name,
		Labels:      map[string]string{"app.kubernetes.io/managed-by": "vinaire"},
		Annotations: map[string]string{"vinaire.io/created-by": callerLabel(r)},
	}
	ref := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: b.RoleKind, Name: b.RoleName}
	var err error
	if b.Scope == "cluster" {
		err = k8s.CreateClusterRoleBinding(r.Context(), cs, &rbacv1.ClusterRoleBinding{ObjectMeta: meta, RoleRef: ref, Subjects: subjects})
	} else {
		meta.Namespace = b.Namespace
		err = k8s.CreateRoleBinding(r.Context(), cs, &rbacv1.RoleBinding{ObjectMeta: meta, RoleRef: ref, Subjects: subjects})
	}
	if err != nil {
		h.auditLog(r, "rbac-bind-failed", b.Namespace, b.Name, "role", b.RoleKind+"/"+b.RoleName, "error", truncateAudit(err.Error()))
		if msg, ok := escalationMessage(err, b.RoleName); ok {
			respondError(w, http.StatusForbidden, msg)
			return
		}
		respondK8sError(w, err)
		return
	}
	who := make([]string, 0, len(subjects))
	for _, s := range b.Subjects {
		who = append(who, s.Kind+":"+s.Namespace+"/"+s.Name)
	}
	h.auditLog(r, "rbac-bind", b.Namespace, b.Name, "scope", b.Scope, "role", b.RoleKind+"/"+b.RoleName, "subjects", who)
	w.WriteHeader(http.StatusCreated)
	respondJSON(w, map[string]string{"name": b.Name})
}

// protectedBinding reports bindings Vinaire refuses to delete: the cluster's own
// bootstrap bindings (system:*) and the cluster-admin binding.
func protectedBinding(name string) bool {
	return strings.HasPrefix(name, "system:") || name == "cluster-admin"
}

func (h *Handler) deleteBinding(w http.ResponseWriter, r *http.Request, namespace string) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if protectedBinding(name) {
		respondError(w, http.StatusBadRequest, "binding "+name+" is part of the cluster's own setup and can't be removed from Vinaire")
		return
	}
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 && !strings.Contains(name, ":") {
		respondError(w, http.StatusBadRequest, "invalid binding name")
		return
	}
	var b confirmBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if b.ConfirmName != name {
		respondError(w, http.StatusBadRequest, `request body must be {"confirmName": "<the binding's name>"}`)
		return
	}
	var err error
	action := "rbac-unbind"
	if namespace == "" {
		err = k8s.DeleteClusterRoleBinding(r.Context(), cs, name)
	} else {
		err = k8s.DeleteRoleBinding(r.Context(), cs, namespace, name)
	}
	if err != nil {
		respondK8sError(w, err)
		return
	}
	h.auditLog(r, action, namespace, name)
	respondJSON(w, map[string]string{"status": "ok"})
}

// DeleteRoleBinding handles DELETE /api/v1/rbac/rolebindings/{namespace}/{name}.
func (h *Handler) DeleteRoleBinding(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "namespace")
	if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
		respondError(w, http.StatusBadRequest, "invalid namespace")
		return
	}
	h.deleteBinding(w, r, ns)
}

// DeleteClusterRoleBinding handles DELETE /api/v1/rbac/clusterrolebindings/{name}.
func (h *Handler) DeleteClusterRoleBinding(w http.ResponseWriter, r *http.Request) {
	h.deleteBinding(w, r, "")
}

// ---- "what can X do?" ---------------------------------------------------------

// impliedGroups are the groups Kubernetes puts every such principal in; a binding
// to one of them applies to the subject too, and a SubjectAccessReview must name
// them to give the same answer as the real authorizer.
func (s subjectBody) impliedGroups() []string {
	switch s.Kind {
	case "ServiceAccount":
		return []string{"system:serviceaccounts", "system:serviceaccounts:" + s.Namespace, "system:authenticated"}
	case "User":
		return []string{"system:authenticated"}
	}
	return nil
}

type checkBody struct {
	Subject     subjectBody `json:"subject"`
	Verb        string      `json:"verb"`
	Group       string      `json:"group"`
	Resource    string      `json:"resource"`
	Subresource string      `json:"subresource,omitempty"`
	Namespace   string      `json:"namespace,omitempty"`
	Name        string      `json:"name,omitempty"`
}

func (c checkBody) validate() error {
	if err := c.Subject.validate(); err != nil {
		return err
	}
	if !verbRe.MatchString(c.Verb) {
		return errors.New("invalid verb")
	}
	if c.Group != "" && !groupRe.MatchString(c.Group) {
		return errors.New("invalid API group")
	}
	if !rbacResRe.MatchString(c.Resource) {
		return errors.New("invalid resource")
	}
	if c.Subresource != "" && !resourceRe.MatchString(c.Subresource) {
		return errors.New("invalid subresource")
	}
	if c.Namespace != "" && !nsRe.MatchString(c.Namespace) {
		return errors.New("invalid namespace")
	}
	if c.Name != "" && !objNameRe.MatchString(c.Name) {
		return errors.New("invalid name")
	}
	return nil
}

// CheckAccess handles POST /api/v1/rbac/check: a SubjectAccessReview — the
// cluster's own answer to "may this subject do this?", as the caller. Needs the
// permission to create subjectaccessreviews (the chart's -rbac-admin role).
func (h *Handler) CheckAccess(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	var b checkBody
	if !decodeNodeBody(w, r, &b) {
		return
	}
	if err := b.validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	spec := authzv1.SubjectAccessReviewSpec{
		ResourceAttributes: &authzv1.ResourceAttributes{
			Namespace: b.Namespace, Verb: b.Verb, Group: b.Group, Resource: b.Resource,
			Subresource: b.Subresource, Name: b.Name,
		},
		Groups: b.Subject.impliedGroups(),
	}
	switch b.Subject.Kind {
	case "ServiceAccount":
		spec.User = "system:serviceaccount:" + b.Subject.Namespace + ":" + b.Subject.Name
	case "User":
		spec.User = b.Subject.Name
	case "Group":
		spec.Groups = append(spec.Groups, b.Subject.Name)
		spec.User = "vinaire-check" // a SAR needs a user; the group is what is asked about
	}
	res, err := cs.AuthorizationV1().SubjectAccessReviews().Create(r.Context(), &authzv1.SubjectAccessReview{Spec: spec}, metav1.CreateOptions{})
	if err != nil {
		respondK8sError(w, err)
		return
	}
	respondJSON(w, map[string]any{
		"allowed": res.Status.Allowed, "denied": res.Status.Denied,
		"reason": res.Status.Reason, "evaluationError": res.Status.EvaluationError,
	})
}

// SubjectBinding is one binding that applies to a subject.
type SubjectBinding struct {
	Kind      string     `json:"kind"` // RoleBinding | ClusterRoleBinding
	Name      string     `json:"name"`
	Namespace string     `json:"namespace,omitempty"`
	RoleKind  string     `json:"roleKind"`
	RoleName  string     `json:"roleName"`
	Via       string     `json:"via"` // "direct" or the group that carries it
	Rules     []RBACRule `json:"rules"`
}

func subjectMatches(s rbacv1.Subject, who subjectBody, groups map[string]bool) (string, bool) {
	switch s.Kind {
	case "ServiceAccount":
		if who.Kind == "ServiceAccount" && s.Name == who.Name && s.Namespace == who.Namespace {
			return "direct", true
		}
	case "User":
		if who.Kind == "User" && s.Name == who.Name {
			return "direct", true
		}
	case "Group":
		if who.Kind == "Group" && s.Name == who.Name {
			return "direct", true
		}
		if groups[s.Name] {
			return "group " + s.Name, true
		}
	}
	return "", false
}

// SubjectBindings handles GET /api/v1/rbac/subject?kind=&name=&namespace= — every
// RoleBinding and ClusterRoleBinding that applies to the subject (directly or
// through the groups its kind is always in) with the rules each grants.
func (h *Handler) SubjectBindings(w http.ResponseWriter, r *http.Request) {
	_, cs, ok := h.resolveCluster(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	who := subjectBody{Kind: q.Get("kind"), Name: q.Get("name"), Namespace: q.Get("namespace")}
	if err := who.validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	groups := map[string]bool{}
	for _, g := range who.impliedGroups() {
		groups[g] = true
	}

	ctx := r.Context()
	rbs, err := k8s.ListRoleBindings(ctx, cs, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}
	crbs, err := k8s.ListClusterRoleBindings(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	roles, err := k8s.ListRoles(ctx, cs, "")
	if err != nil {
		respondK8sError(w, err)
		return
	}
	croles, err := k8s.ListClusterRoles(ctx, cs)
	if err != nil {
		respondK8sError(w, err)
		return
	}
	roleRules := map[string][]rbacv1.PolicyRule{}
	for _, ro := range roles {
		roleRules["Role/"+ro.Namespace+"/"+ro.Name] = ro.Rules
	}
	for _, cr := range croles {
		roleRules["ClusterRole//"+cr.Name] = cr.Rules
	}

	out := []SubjectBinding{}
	for _, rb := range rbs {
		for _, s := range rb.Subjects {
			if via, ok := subjectMatches(s, who, groups); ok {
				key := rb.RoleRef.Kind + "//" + rb.RoleRef.Name
				if rb.RoleRef.Kind == "Role" {
					key = "Role/" + rb.Namespace + "/" + rb.RoleRef.Name
				}
				out = append(out, SubjectBinding{Kind: "RoleBinding", Name: rb.Name, Namespace: rb.Namespace,
					RoleKind: rb.RoleRef.Kind, RoleName: rb.RoleRef.Name, Via: via, Rules: rbacRules(roleRules[key])})
				break
			}
		}
	}
	for _, crb := range crbs {
		for _, s := range crb.Subjects {
			if via, ok := subjectMatches(s, who, groups); ok {
				out = append(out, SubjectBinding{Kind: "ClusterRoleBinding", Name: crb.Name,
					RoleKind: crb.RoleRef.Kind, RoleName: crb.RoleRef.Name, Via: via, Rules: rbacRules(roleRules["ClusterRole//"+crb.RoleRef.Name])})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind > out[j].Kind // cluster-wide first
		}
		return out[i].Namespace+"/"+out[i].Name < out[j].Namespace+"/"+out[j].Name
	})
	respondJSON(w, map[string]any{"bindings": out})
}

// escalationMessage turns Kubernetes' escalation refusal — which lists, line by
// line, every permission of the role the caller doesn't hold (dozens for "edit") —
// into one readable sentence with a few examples.
func escalationMessage(err error, role string) (string, bool) {
	var status *apierrors.StatusError
	if !errors.As(err, &status) || !apierrors.IsForbidden(err) {
		return "", false
	}
	const marker = "attempting to grant RBAC permissions not currently held:"
	msg := status.Status().Message
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	var rules []string
	for _, l := range strings.Split(strings.TrimSpace(msg[i+len(marker):]), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			rules = append(rules, l)
		}
	}
	shown := rules
	if len(shown) > 3 {
		shown = shown[:3]
	}
	out := fmt.Sprintf("You can't give out %q: it contains %d permission rule(s) you don't hold yourself (for example %s). "+
		"Kubernetes only lets you grant what you have — ask someone who holds it, or an administrator who has been granted the `bind` permission on this role.",
		role, len(rules), strings.Join(shown, "; "))
	return out, true
}
