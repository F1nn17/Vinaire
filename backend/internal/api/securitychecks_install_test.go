package api

import (
	"testing"

	"github.com/f1nn17/vinaire/backend/internal/clusters"
	"github.com/f1nn17/vinaire/backend/internal/securitychecks"
)

// Roadmap Phase 86: the "This installation" checks get Vinaire's own settings, never a guess.

// a registry store that can say whether it seals what it keeps
type fakeSealingStore struct {
	clusters.Store
	encrypted bool
}

func (s fakeSealingStore) Encrypted() bool { return s.encrypted }

func TestAddInstallationSaysWhatIsNotKnown(t *testing.T) {
	snap := &securitychecks.Snapshot{Unavailable: map[string]string{}}
	(&Handler{}).addInstallation(snap)
	if snap.Installation != nil || snap.Unavailable["installation"] == "" {
		t.Fatalf("a Vinaire that doesn't know its configuration must say so: %+v", snap)
	}

	snap = &securitychecks.Snapshot{Unavailable: map[string]string{}}
	(&Handler{Install: &securitychecks.Installation{AuthMode: "oidc"}}).addInstallation(snap)
	if snap.Installation == nil || snap.Unavailable["installation-chart"] == "" || snap.Unavailable["installation"] != "" {
		t.Fatalf("without the chart's hints only the chart facts are unknown: %+v", snap.Unavailable)
	}

	snap = &securitychecks.Snapshot{Unavailable: map[string]string{}}
	(&Handler{Install: &securitychecks.Installation{AuthMode: "oidc", Chart: true}}).addInstallation(snap)
	if len(snap.Unavailable) != 0 {
		t.Fatalf("%+v", snap.Unavailable)
	}
}

func TestAnUnencryptedRegistryIsFoundFromTheStoreItself(t *testing.T) {
	inst := &securitychecks.Installation{AuthMode: "oidc", Chart: true}
	snap := &securitychecks.Snapshot{Unavailable: map[string]string{}}
	h := &Handler{Install: inst}
	h.addInstallation(snap)
	if snap.Installation.StoreUnencrypted {
		t.Fatal("no store is not an unencrypted store")
	}
	h.ClusterStore = fakeSealingStore{encrypted: true}
	h.addInstallation(snap)
	if snap.Installation.StoreUnencrypted {
		t.Fatal("a sealed store was reported as unencrypted")
	}
	h.ClusterStore = fakeSealingStore{encrypted: false}
	h.addInstallation(snap)
	if !snap.Installation.StoreUnencrypted {
		t.Fatal("an unsealed store was not reported")
	}
	if inst.StoreUnencrypted {
		t.Fatal("the handler's own settings must not be changed by a request")
	}
}
