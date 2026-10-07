package api

import (
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func okBinding() bindingBody {
	return bindingBody{
		Scope: "namespace", Namespace: "shop", RoleKind: "ClusterRole", RoleName: "vinaire-viewer",
		Subjects: []subjectBody{{Kind: "ServiceAccount", Name: "me", Namespace: "vinaire"}},
	}
}

func TestBindingValidation(t *testing.T) {
	b := okBinding()
	if err := validateBinding(&b); err != nil {
		t.Fatalf("a good binding was refused: %v", err)
	}
	if !strings.HasPrefix(b.Name, "vinaire-vinaire-viewer-") {
		t.Errorf("generated name = %q", b.Name)
	}

	// cluster-wide needs the role's name typed, and only a ClusterRole
	c := bindingBody{Scope: "cluster", RoleKind: "ClusterRole", RoleName: "cluster-admin",
		Subjects: []subjectBody{{Kind: "User", Name: "alice@example.com"}}}
	if err := validateBinding(&c); err == nil {
		t.Error("a cluster-wide grant without the typed confirmation was accepted")
	}
	c.ConfirmName = "cluster-admin"
	if err := validateBinding(&c); err != nil {
		t.Errorf("confirmed cluster-wide grant refused: %v", err)
	}
	c.RoleKind = "Role"
	if err := validateBinding(&c); err == nil {
		t.Error("a cluster-wide binding to a Role was accepted")
	}

	bad := map[string]func(*bindingBody){
		"no subjects":          func(b *bindingBody) { b.Subjects = nil },
		"too many subjects":    func(b *bindingBody) { b.Subjects = make([]subjectBody, 21) },
		"bad scope":            func(b *bindingBody) { b.Scope = "galaxy" },
		"no namespace":         func(b *bindingBody) { b.Namespace = "" },
		"bad namespace":        func(b *bindingBody) { b.Namespace = "../x" },
		"bad role kind":        func(b *bindingBody) { b.RoleKind = "Pod" },
		"empty role":           func(b *bindingBody) { b.RoleName = "" },
		"role with a slash":    func(b *bindingBody) { b.RoleName = "a/b" },
		"bad binding name":     func(b *bindingBody) { b.Name = "Bad Name" },
		"SA without namespace": func(b *bindingBody) { b.Subjects = []subjectBody{{Kind: "ServiceAccount", Name: "x"}} },
		"user with whitespace": func(b *bindingBody) { b.Subjects = []subjectBody{{Kind: "User", Name: "a b"}} },
		"user with newline":    func(b *bindingBody) { b.Subjects = []subjectBody{{Kind: "User", Name: "a\nb"}} },
		"group with namespace": func(b *bindingBody) { b.Subjects = []subjectBody{{Kind: "Group", Name: "g", Namespace: "x"}} },
		"unknown subject kind": func(b *bindingBody) { b.Subjects = []subjectBody{{Kind: "Robot", Name: "r"}} },
	}
	for name, mutate := range bad {
		b := okBinding()
		mutate(&b)
		if err := validateBinding(&b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// roles such as "system:node" contain a colon and are legitimate
	sys := okBinding()
	sys.RoleName = "system:node-reader"
	if err := validateBinding(&sys); err != nil {
		t.Errorf("a system: role name was refused: %v", err)
	}
}

func TestProtectedBindingsCannotBeDeleted(t *testing.T) {
	for _, n := range []string{"cluster-admin", "system:node", "system:controller:x"} {
		if !protectedBinding(n) {
			t.Errorf("%q is not protected", n)
		}
	}
	if protectedBinding("vinaire-viewer-1a2b3c") {
		t.Error("an ordinary binding was protected")
	}
}

func TestCheckValidation(t *testing.T) {
	good := checkBody{Subject: subjectBody{Kind: "User", Name: "bob"}, Verb: "list", Group: "apps", Resource: "deployments", Namespace: "shop"}
	if err := good.validate(); err != nil {
		t.Fatalf("good check refused: %v", err)
	}
	for name, mutate := range map[string]func(*checkBody){
		"verb with path chars": func(c *checkBody) { c.Verb = "get;rm" },
		"resource traversal":   func(c *checkBody) { c.Resource = "../x" },
		"group with space":     func(c *checkBody) { c.Group = "a b" },
		"namespace traversal":  func(c *checkBody) { c.Namespace = "../x" },
		"bad subject":          func(c *checkBody) { c.Subject = subjectBody{Kind: "User", Name: ""} },
	} {
		c := good
		mutate(&c)
		if err := c.validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	wild := checkBody{Subject: subjectBody{Kind: "User", Name: "bob"}, Verb: "*", Resource: "*"}
	if err := wild.validate(); err != nil {
		t.Errorf("a wildcard check was refused: %v", err)
	}
}

func TestSubjectMatchingIncludesImpliedGroups(t *testing.T) {
	sa := subjectBody{Kind: "ServiceAccount", Name: "me", Namespace: "vinaire"}
	groups := map[string]bool{}
	for _, g := range sa.impliedGroups() {
		groups[g] = true
	}
	if via, ok := subjectMatches(rbacv1.Subject{Kind: "ServiceAccount", Name: "me", Namespace: "vinaire"}, sa, groups); !ok || via != "direct" {
		t.Errorf("direct SA match: %q %v", via, ok)
	}
	if _, ok := subjectMatches(rbacv1.Subject{Kind: "ServiceAccount", Name: "me", Namespace: "other"}, sa, groups); ok {
		t.Error("a ServiceAccount of the same name in another namespace matched")
	}
	if via, ok := subjectMatches(rbacv1.Subject{Kind: "Group", Name: "system:serviceaccounts:vinaire"}, sa, groups); !ok || !strings.HasPrefix(via, "group ") {
		t.Errorf("namespace group match: %q %v", via, ok)
	}
	if _, ok := subjectMatches(rbacv1.Subject{Kind: "Group", Name: "system:serviceaccounts:other"}, sa, groups); ok {
		t.Error("another namespace's group matched")
	}
	user := subjectBody{Kind: "User", Name: "alice"}
	ug := map[string]bool{"system:authenticated": true}
	if _, ok := subjectMatches(rbacv1.Subject{Kind: "User", Name: "alice"}, user, ug); !ok {
		t.Error("a user didn't match its own binding")
	}
	if _, ok := subjectMatches(rbacv1.Subject{Kind: "Group", Name: "system:authenticated"}, user, ug); !ok {
		t.Error("system:authenticated didn't apply to a user")
	}
	if _, ok := subjectMatches(rbacv1.Subject{Kind: "User", Name: "alice"}, subjectBody{Kind: "Group", Name: "alice"}, nil); ok {
		t.Error("a User binding matched a Group of the same name")
	}
}

func TestEscalationRefusalIsShortened(t *testing.T) {
	long := `rolebindings.rbac.authorization.k8s.io "x" is forbidden: user "u" is attempting to grant RBAC permissions not currently held:
{APIGroups:["*"], Resources:["*"], Verbs:["*"]}
{NonResourceURLs:["*"], Verbs:["*"]}
{APIGroups:["apps"], Resources:["deployments"], Verbs:["get"]}
{APIGroups:["batch"], Resources:["jobs"], Verbs:["get"]}`
	err := &apierrors.StatusError{ErrStatus: metav1.Status{Status: "Failure", Code: 403, Reason: metav1.StatusReasonForbidden, Message: long}}
	msg, ok := escalationMessage(err, "cluster-admin")
	if !ok || !strings.Contains(msg, "4 permission rule(s)") || strings.Count(msg, "{") > 3 || !strings.Contains(msg, "bind") {
		t.Errorf("message = %q (%v)", msg, ok)
	}
	if _, ok := escalationMessage(err, "x"); !ok {
		t.Error("not recognised twice")
	}
	other := &apierrors.StatusError{ErrStatus: metav1.Status{Code: 403, Reason: metav1.StatusReasonForbidden, Message: "cannot create rolebindings"}}
	if _, ok := escalationMessage(other, "x"); ok {
		t.Error("an ordinary 403 was rewritten as an escalation")
	}
}
