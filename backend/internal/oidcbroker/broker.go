// Package oidcbroker gives a person who signed in through Keycloak a Kubernetes credential of their own
// (Roadmap Phase 80), so that sign-in works when the cluster's API server cannot be pointed at the issuer —
// Kubernetes accepts only an https issuer, and a home server often has none.
//
// Each person gets a ServiceAccount in one dedicated namespace, found by a label made from their stable
// OIDC subject, and a short-lived token for it (the TokenRequest API). The cluster then sees
// system:serviceaccount:<namespace>:<name> and RBAC decides everything: a new account has no rights at
// all, an administrator binds roles to it exactly as to any other ServiceAccount, and deleting the account
// ends its tokens within seconds.
//
// What it must never do, and the tests hold it to:
//   - grant a right: it creates ServiceAccounts and tokens, never a binding;
//   - touch anything outside its namespace — the chart gives Vinaire a Role there and nowhere else;
//   - use or return an account it did not create (managed-by label) or that belongs to another subject;
//   - put a provider-supplied string into a name or a label unchecked (names are derived and validated).
//
// It uses Vinaire's own identity, not the caller's — there is no caller yet at sign-in. That is why the
// Role is as narrow as it is.
package oidcbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/f1nn17/vinaire/backend/internal/authn"
)

const (
	// LabelSubject carries a digest of the OIDC subject; the digest keeps the value a valid label
	// whatever the provider uses as a subject.
	LabelSubject   = "vinaire.io/oidc-sub"
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// AnnotationUsername is the name the person had when the account was made (a label for humans; the
	// subject is the identity).
	AnnotationUsername = "vinaire.io/oidc-username"
	// AnnotationSubject lets an administrator tell accounts apart in kubectl.
	AnnotationSubject = "vinaire.io/oidc-subject"

	// MinTTL is the shortest lifetime Kubernetes accepts for a requested token.
	MinTTL = 10 * time.Minute
	// DefaultTTL is used when none is configured (SESSION_MAX_TTL unset). Four hours: long enough for a working
	// session, short enough that a disabled person loses access the same day. Until sessions are renewed
	// (Roadmap Phase 84) the session ends when the token does.
	DefaultTTL = 4 * time.Hour

	prefix = "oidc-"
)

// Audit is the hook for audit entries (Handler.AuditBackground).
type Audit func(action, name, identity string, detail map[string]any)

// Broker implements authn.IdentityBroker.
type Broker struct {
	// Clientset is Vinaire's own identity.
	Clientset kubernetes.Interface
	// Namespace holds the people's ServiceAccounts, and nothing else.
	Namespace string
	// TTL is how long a token lives (clamped to at least MinTTL).
	TTL   time.Duration
	Audit Audit

	mu sync.Mutex // one provisioning at a time: sign-ins are rare and a race could make two accounts
}

var _ authn.IdentityBroker = (*Broker)(nil)

var (
	subjectRe = regexp.MustCompile(`^[\x21-\x7e]{1,255}$`) // printable ASCII, no spaces
	nameRe    = regexp.MustCompile(`^oidc-[a-z0-9]([-a-z0-9]{0,52}[a-z0-9])?$`)
)

// SubjectLabel is the label value for a subject: the first 40 hex characters of its SHA-256.
func SubjectLabel(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:])[:40]
}

// AccountName derives a ServiceAccount name from a username: lower case, anything that is not a letter or a
// digit becomes a single dash, at most 40 characters of it, prefixed "oidc-". A name with nothing usable in
// it falls back to the subject's digest. The result is always a valid DNS label of at most 63 characters.
func AccountName(username, subject string) string {
	var b strings.Builder
	dash := true // no leading dash
	for _, r := range strings.ToLower(username) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	core := strings.Trim(b.String(), "-")
	if core == "" {
		core = SubjectLabel(subject)[:12]
	}
	return prefix + core
}

func (b *Broker) ttl() time.Duration {
	if b.TTL < MinTTL {
		if b.TTL == 0 {
			return DefaultTTL
		}
		return MinTTL
	}
	return b.TTL
}

func (b *Broker) audit(action, name, identity string, detail map[string]any) {
	if b.Audit != nil {
		b.Audit(action, name, identity, detail)
	}
}

// ErrNotConfigured says the broker has nowhere to work.
var ErrNotConfigured = errors.New("the identity broker has no cluster or namespace to work in")

// TokenFor finds or creates the person's ServiceAccount and returns a token for it.
func (b *Broker) TokenFor(ctx context.Context, id authn.Identity) (authn.BrokeredToken, error) {
	if b.Clientset == nil || b.Namespace == "" {
		return authn.BrokeredToken{}, ErrNotConfigured
	}
	if !subjectRe.MatchString(id.Subject) {
		return authn.BrokeredToken{}, errors.New("the identity provider gave no usable subject")
	}
	account, created, err := b.account(ctx, id)
	if err != nil {
		return authn.BrokeredToken{}, err
	}
	secs := int64(b.ttl().Seconds())
	tr, err := b.Clientset.CoreV1().ServiceAccounts(b.Namespace).CreateToken(ctx, account, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &secs},
	}, metav1.CreateOptions{})
	if err != nil {
		return authn.BrokeredToken{}, fmt.Errorf("request a token for %s/%s: %w", b.Namespace, account, err)
	}
	user := fmt.Sprintf("system:serviceaccount:%s:%s", b.Namespace, account)
	b.audit("oidc-token-issued", account, id.Username, map[string]any{
		"serviceAccount": b.Namespace + "/" + account, "subject": id.Subject, "provisioned": created,
		"expires": tr.Status.ExpirationTimestamp.UTC().Format(time.RFC3339),
	})
	return authn.BrokeredToken{Token: tr.Status.Token, Expiry: tr.Status.ExpirationTimestamp.Time, KubernetesUser: user}, nil
}

// account returns the name of the person's ServiceAccount, creating it when there is none.
func (b *Broker) account(ctx context.Context, id authn.Identity) (name string, created bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sas := b.Clientset.CoreV1().ServiceAccounts(b.Namespace)
	label := SubjectLabel(id.Subject)
	sel := LabelSubject + "=" + label + "," + LabelManagedBy + "=vinaire"

	list, err := sas.List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return "", false, fmt.Errorf("look up the account in %s: %w", b.Namespace, err)
	}
	switch len(list.Items) {
	case 1:
		return list.Items[0].Name, false, nil
	case 0:
	default:
		return "", false, fmt.Errorf("%d accounts in %s claim the same identity — remove the extra ones", len(list.Items), b.Namespace)
	}

	base := AccountName(id.Username, id.Subject)
	for _, candidate := range []string{base, base + "-" + label[:6]} {
		if !nameRe.MatchString(candidate) {
			return "", false, fmt.Errorf("derived an invalid account name %q", candidate)
		}
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: candidate, Namespace: b.Namespace,
			Labels:      map[string]string{LabelSubject: label, LabelManagedBy: "vinaire"},
			Annotations: map[string]string{AnnotationUsername: clip(id.Username, 120), AnnotationSubject: clip(id.Subject, 200)},
		}}
		_, err := sas.Create(ctx, sa, metav1.CreateOptions{})
		if err == nil {
			b.audit("oidc-user-provisioned", candidate, id.Username, map[string]any{"serviceAccount": b.Namespace + "/" + candidate, "subject": id.Subject})
			return candidate, true, nil
		}
		if !apierrors.IsAlreadyExists(err) {
			return "", false, fmt.Errorf("create the account %s/%s: %w", b.Namespace, candidate, err)
		}
		// The name is taken. If it is ours (a race, or the label lookup lagged) use it; if it belongs to
		// someone else, try the next name — never take over an account we did not make for this subject.
		if existing, gerr := sas.Get(ctx, candidate, metav1.GetOptions{}); gerr == nil &&
			existing.Labels[LabelSubject] == label && existing.Labels[LabelManagedBy] == "vinaire" {
			return candidate, false, nil
		}
	}
	return "", false, fmt.Errorf("couldn't find a free account name for %q in %s", id.Username, b.Namespace)
}

func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
