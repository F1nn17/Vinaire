package api

import (
	"strings"
	"testing"

	"github.com/f1nn17/vinaire/backend/internal/authn"
)

// Roadmap Phase 101: the write limit counts people, not names.
func TestTheWriteLimitCountsPeopleNotNames(t *testing.T) {
	a := &authn.Session{ID: "1", Username: "shiraku", Issuer: "https://kc/realms/one", Subject: "sub-1", K8sToken: "t1"}
	b := &authn.Session{ID: "2", Username: "shiraku", Issuer: "https://kc/realms/two", Subject: "sub-1", K8sToken: "t2"}
	if rateIdentity(a) == rateIdentity(b) {
		t.Fatal("two people with one user name under different realms share a limit")
	}
	// the same person on a renewed credential or a second login is still one
	again := &authn.Session{ID: "3", Username: "renamed", Issuer: a.Issuer, Subject: a.Subject, K8sToken: "t1-renewed"}
	if rateIdentity(a) != rateIdentity(again) {
		t.Fatal("one person got two limits")
	}
	// two pasted tokens are two identities even when the name is the same
	p1 := &authn.Session{ID: "4", Username: "system:serviceaccount:ns:x", K8sToken: "tok-a"}
	p2 := &authn.Session{ID: "5", Username: "system:serviceaccount:ns:x", K8sToken: "tok-b"}
	if rateIdentity(p1) == rateIdentity(p2) {
		t.Fatal("two tokens share a limit through their name")
	}
	for _, s := range []*authn.Session{a, b, p1} {
		if k := rateIdentity(s); strings.Contains(k, s.Username) || strings.Contains(k, s.K8sToken) {
			t.Fatalf("the key %q carries a name or a token", k)
		}
	}
	if rateIdentity(&authn.Session{ID: "only-id"}) != "only-id" || rateIdentity(&authn.Session{}) != "anonymous" {
		t.Fatal("fallbacks")
	}
}
