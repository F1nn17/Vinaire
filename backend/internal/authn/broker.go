package authn

import (
	"context"
	"time"
)

// Identity is who Keycloak (or another OIDC provider) says has just signed in. Subject is the provider's
// stable id for the person; Username is only a label and may change.
type Identity struct {
	// Issuer is the provider that vouched for the person — together with Subject it IS the identity: the same
	// subject under another issuer (another Keycloak, another realm) is someone else.
	Issuer   string
	Subject  string
	Username string
	// TokenTTL asks for a credential of this lifetime (0: the broker's own default). A session that can be
	// renewed asks for a short one (RenewableTokenTTL).
	TokenTTL time.Duration
}

// BrokeredToken is the Kubernetes credential an IdentityBroker hands out in place of the ID token.
type BrokeredToken struct {
	// Token is the bearer token the session will use against the cluster.
	Token string
	// Expiry is when it stops working.
	Expiry time.Time
	// KubernetesUser is the name the cluster knows the holder by, e.g. system:serviceaccount:vinaire-users:oidc-alice.
	KubernetesUser string
}

// IdentityBroker turns a verified OIDC identity into a Kubernetes credential (Roadmap Phase 80).
//
// Without a broker the ID token itself is the session's bearer, which only works when the cluster's API
// server is configured to trust the issuer — and Kubernetes accepts only an https issuer. With one,
// Vinaire verifies the sign-in itself and the cluster sees an ordinary credential of the person's own
// (a ServiceAccount token), so RBAC still decides everything and the issuer can be reached over http on a
// private network. A broker never grants rights: it only names who is calling.
type IdentityBroker interface {
	TokenFor(ctx context.Context, id Identity) (BrokeredToken, error)
}

// SetIdentityBroker sets (or, with nil, clears) the broker used by OIDC sign-in.
func (m *Manager) SetIdentityBroker(b IdentityBroker) {
	m.oidcMu.Lock()
	m.broker = b
	m.oidcMu.Unlock()
}

// IdentityBroker returns the broker in use, if any.
func (m *Manager) IdentityBroker() IdentityBroker {
	m.oidcMu.RLock()
	defer m.oidcMu.RUnlock()
	return m.broker
}
