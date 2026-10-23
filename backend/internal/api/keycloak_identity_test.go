package api

import (
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/f1nn17/vinaire/backend/internal/oidcbroker"
)

func brokerReady() *Handler {
	return &Handler{BrokerClientset: fake.NewSimpleClientset(), BrokerNamespace: "vinaire-users", BrokerTTL: time.Hour}
}

// The point of Phase 80: a Keycloak on plain http can never be trusted by the API server, so the wizard must
// not offer that combination — it used to print --oidc-* flags that Kubernetes refuses.
func TestAnHTTPKeycloakIsNeverPairedWithTheAPIServerMode(t *testing.T) {
	h := brokerReady()
	if _, err := h.chooseIdentityMode(IdentityAPIServer, "http://192.168.88.5:8080"); err == nil {
		t.Fatal("an http Keycloak with the API-server mode must be refused")
	}
	if m, err := h.chooseIdentityMode("", "http://192.168.88.5:8080"); err != nil || m != IdentityServiceAccount {
		t.Fatalf("default for http: %q %v", m, err)
	}
	if m, err := h.chooseIdentityMode("", "https://keycloak.example.com"); err != nil || m != IdentityAPIServer {
		t.Fatalf("default for https: %q %v", m, err)
	}
	if m, err := h.chooseIdentityMode(IdentityServiceAccount, "https://keycloak.example.com"); err != nil || m != IdentityServiceAccount {
		t.Fatalf("https may choose service accounts too: %q %v", m, err)
	}
	if _, err := h.chooseIdentityMode("admin", "https://keycloak.example.com"); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
}

func TestServiceAccountModeNeedsAClusterAndANamespace(t *testing.T) {
	for name, h := range map[string]*Handler{
		"no cluster":   {BrokerNamespace: "x"},
		"no namespace": {BrokerClientset: fake.NewSimpleClientset()},
	} {
		if _, err := h.chooseIdentityMode(IdentityServiceAccount, "http://192.168.88.5"); err == nil {
			t.Errorf("%s: should be refused", name)
		}
		if ok, why := h.brokerAvailable(); ok || why == "" {
			t.Errorf("%s: brokerAvailable = %v %q", name, ok, why)
		}
		if h.brokerFor(IdentityServiceAccount, "") != nil {
			t.Errorf("%s: a broker was built without somewhere to work", name)
		}
	}
	if brokerReady().brokerFor(IdentityAPIServer, "") != nil {
		t.Error("the API-server mode has no broker")
	}
	if brokerReady().brokerFor(IdentityServiceAccount, "") == nil {
		t.Error("the service-account mode needs its broker")
	}
}

func TestSettingsSavedBeforePhase80KeepTheirMode(t *testing.T) {
	if (&storedOIDC{}).mode() != IdentityAPIServer || (&storedOIDC{IdentityMode: IdentityServiceAccount}).mode() != IdentityServiceAccount {
		t.Fatal("an old setting must keep using the ID token")
	}
}

func TestTheBrokerCarriesThePinnedIssuerForLegacyAccounts(t *testing.T) {
	b, ok := brokerReady().brokerFor(IdentityServiceAccount, "http://keycloak.home/realms/vinaire").(*oidcbroker.Broker)
	if !ok || b.AdoptLegacyIssuer != "http://keycloak.home/realms/vinaire" {
		t.Fatalf("the pinned issuer didn't reach the broker: %+v", b)
	}
}
