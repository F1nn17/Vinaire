package authn

import (
	"strings"
	"testing"
)

// Roadmap Phase 93: who owns a background job follows the identity (issuer + subject), not the display name.

func TestTwoPeopleWithTheSameNameInDifferentRealmsDoNotShareAnOwnerKey(t *testing.T) {
	a := &Session{Username: "alice", Issuer: "https://kc/realms/one", Subject: "sub-1", K8sToken: "t1"}
	b := &Session{Username: "alice", Issuer: "https://kc/realms/two", Subject: "sub-1", K8sToken: "t2"}
	c := &Session{Username: "alice", Issuer: "https://kc/realms/one", Subject: "sub-2", K8sToken: "t3"}
	if a.OwnerKey() == b.OwnerKey() || a.OwnerKey() == c.OwnerKey() || b.OwnerKey() == c.OwnerKey() {
		t.Fatalf("different people share a key: %q %q %q", a.OwnerKey(), b.OwnerKey(), c.OwnerKey())
	}
}

func TestOnePersonKeepsTheirKeyWhateverTheyAreCalledAndHoweverTheirCredentialRenews(t *testing.T) {
	before := &Session{Username: "alice", Issuer: "https://kc/realms/one", Subject: "sub-1", K8sToken: "old"}
	after := &Session{Username: "alice-renamed", Issuer: "https://kc/realms/one", Subject: "sub-1", K8sToken: "new (renewed)"}
	if before.OwnerKey() != after.OwnerKey() {
		t.Fatalf("a rename or a renewal changed who owns a running job: %q vs %q", before.OwnerKey(), after.OwnerKey())
	}
}

func TestTheOwnerKeyRevealsNeitherTheNameNorTheCredential(t *testing.T) {
	s := &Session{Username: "alice", Issuer: "https://kc/realms/one", Subject: "sub-1", K8sToken: "SECRET-TOKEN"}
	k := s.OwnerKey()
	for _, leak := range []string{"alice", "sub-1", "kc", "SECRET-TOKEN"} {
		if strings.Contains(k, leak) {
			t.Errorf("the key %q contains %q", k, leak)
		}
	}
}

func TestAPastedTokenIsToldApartByTheTokenAndANameAloneIsNotAnIdentity(t *testing.T) {
	x := &Session{K8sToken: "token-x"}
	y := &Session{K8sToken: "token-y"}
	if x.OwnerKey() == y.OwnerKey() || !strings.HasPrefix(x.OwnerKey(), "token:") {
		t.Fatalf("%q %q", x.OwnerKey(), y.OwnerKey())
	}
	// a session that carries a name but no issuer/subject must not be keyed by the name
	n1 := &Session{Username: "bob", K8sToken: "t-a"}
	n2 := &Session{Username: "bob", K8sToken: "t-b"}
	if n1.OwnerKey() == n2.OwnerKey() {
		t.Fatal("two sessions that only share a display name share an owner key")
	}
}
