package oidcbroker

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/f1nn17/vinaire/backend/internal/authn"
)

const (
	ns         = "vinaire-users"
	testIssuer = "http://keycloak.home/realms/vinaire"
)

// newBroker gives a broker over a fake cluster whose TokenRequest answers with a recognisable token.
func newBroker(t *testing.T, objs ...runtime.Object) (*Broker, *fake.Clientset, *[]string) {
	t.Helper()
	cs := fake.NewSimpleClientset(objs...)
	var asked []string
	cs.PrependReactor("create", "serviceaccounts/token", func(a k8stesting.Action) (bool, runtime.Object, error) {
		ca := a.(k8stesting.CreateAction)
		tr := ca.GetObject().(*authenticationv1.TokenRequest)
		name := a.(k8stesting.CreateActionImpl).Name // the account is in the request path, not in the body
		asked = append(asked, ca.GetNamespace()+"/"+name)
		exp := metav1.NewTime(time.Now().Add(time.Duration(*tr.Spec.ExpirationSeconds) * time.Second))
		return true, &authenticationv1.TokenRequest{Status: authenticationv1.TokenRequestStatus{Token: "tok-" + name, ExpirationTimestamp: exp}}, nil
	})
	var events []string
	return &Broker{Clientset: cs, Namespace: ns, TTL: time.Hour, Audit: func(a, n, _ string, _ map[string]any) { events = append(events, a+":"+n) }}, cs, &events
}

func TestFirstSignInCreatesAnAccountWithNoRightsAndATokenForIt(t *testing.T) {
	b, cs, events := newBroker(t)
	tok, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "6f1c-uuid", Username: "Shiraku"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "tok-oidc-shiraku" || tok.KubernetesUser != "system:serviceaccount:vinaire-users:oidc-shiraku" {
		t.Errorf("unexpected token: %+v", tok)
	}
	if d := time.Until(tok.Expiry); d < 59*time.Minute || d > time.Hour {
		t.Errorf("expiry in %s, want about an hour", d)
	}
	sa, err := cs.CoreV1().ServiceAccounts(ns).Get(context.Background(), "oidc-shiraku", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sa.Labels[LabelManagedBy] != "vinaire" || sa.Labels[LabelSubject] != IdentityLabel(testIssuer, "6f1c-uuid") || sa.Annotations[AnnotationUsername] != "Shiraku" {
		t.Errorf("labels/annotations: %v %v", sa.Labels, sa.Annotations)
	}
	// it must never create anything but ServiceAccounts and tokens: no (cluster)role, no binding
	for _, a := range cs.Actions() {
		if a.GetVerb() == "create" && a.GetResource().Resource != "serviceaccounts" && a.GetResource().Resource != "serviceaccounts/token" {
			t.Errorf("created a %s", a.GetResource().Resource)
		}
	}
	if strings.Join(*events, ",") != "oidc-user-provisioned:oidc-shiraku,oidc-token-issued:oidc-shiraku" {
		t.Errorf("audit: %v", *events)
	}
}

func TestSecondSignInReusesTheAccountEvenIfTheUsernameChanged(t *testing.T) {
	b, cs, events := newBroker(t)
	ctx := context.Background()
	if _, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-1", Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	tok, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-1", Username: "alice.renamed"})
	if err != nil || tok.KubernetesUser != "system:serviceaccount:vinaire-users:oidc-alice" {
		t.Fatalf("the same subject must keep its account: %+v %v", tok, err)
	}
	list, _ := cs.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Errorf("want one account, got %d", len(list.Items))
	}
	if n := strings.Count(strings.Join(*events, ","), "oidc-user-provisioned"); n != 1 {
		t.Errorf("provisioned %d times", n)
	}
}

func TestTwoPeopleWithTheSameUsernameGetDifferentAccounts(t *testing.T) {
	b, _, _ := newBroker(t)
	ctx := context.Background()
	a, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-a", Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-b", Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if a.KubernetesUser == c.KubernetesUser {
		t.Fatalf("two subjects share an account: %s", a.KubernetesUser)
	}
}

// An account that was not made for this subject — by hand, by another tool — is never adopted.
func TestAnAccountOfTheSameNameThatIsNotOursIsNeverTakenOver(t *testing.T) {
	existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "oidc-carol", Namespace: ns}}
	b, _, _ := newBroker(t, existing)
	tok, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "sub-c", Username: "carol"})
	if err != nil {
		t.Fatal(err)
	}
	if tok.KubernetesUser == "system:serviceaccount:vinaire-users:oidc-carol" {
		t.Fatal("took over an account it did not make")
	}
	if !strings.HasPrefix(tok.KubernetesUser, "system:serviceaccount:vinaire-users:oidc-carol-") {
		t.Errorf("want a suffixed name, got %s", tok.KubernetesUser)
	}
}

func TestDuplicateAccountsForOneSubjectAreRefused(t *testing.T) {
	l := map[string]string{LabelSubject: IdentityLabel(testIssuer, "sub-d"), LabelManagedBy: "vinaire"}
	b, _, _ := newBroker(t,
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "oidc-d1", Namespace: ns, Labels: l}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "oidc-d2", Namespace: ns, Labels: l}})
	if _, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "sub-d", Username: "d"}); err == nil {
		t.Fatal("two accounts for one subject must not be guessed between")
	}
}

func TestTokenLifetimeIsClampedToWhatKubernetesAccepts(t *testing.T) {
	b, _, _ := newBroker(t)
	b.TTL = time.Minute
	tok, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "s", Username: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(tok.Expiry); d < 9*time.Minute {
		t.Errorf("lifetime %s is below the minimum Kubernetes accepts", d)
	}
}

func TestWorksOnlyWithAConfiguredClusterAndUsableSubject(t *testing.T) {
	if _, err := (&Broker{}).TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "s"}); err == nil {
		t.Error("an unconfigured broker must refuse")
	}
	b, _, _ := newBroker(t)
	for _, bad := range []string{"", "has space", "new\nline", strings.Repeat("a", 300)} {
		if _, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: bad, Username: "x"}); err == nil {
			t.Errorf("subject %q should be refused", bad)
		}
	}
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func FuzzAccountName(f *testing.F) {
	for _, s := range []string{"", "alice", "Alice Smith", "a@b.c", "../../etc", "ÄÖÜ", "-", "--x--", strings.Repeat("z", 200), "x\x00y", "kube-system"} {
		f.Add(s, "sub")
	}
	f.Fuzz(func(t *testing.T, username, subject string) {
		n := AccountName(username, subject)
		if len(n) > 63 || !dnsLabel.MatchString(n) || !strings.HasPrefix(n, "oidc-") || !nameRe.MatchString(n) {
			t.Fatalf("AccountName(%q) = %q is not a safe name", username, n)
		}
	})
}

func TestWithNoConfiguredLifetimeTheTokenLivesFourHours(t *testing.T) {
	b, _, _ := newBroker(t)
	b.TTL = 0
	tok, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "s", Username: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(tok.Expiry); d < 3*time.Hour+55*time.Minute || d > 4*time.Hour {
		t.Errorf("an unset lifetime gave %s, want four hours", d)
	}
	if DefaultTTL != 4*time.Hour {
		t.Errorf("DefaultTTL changed to %s — update AGENTS.md, docs/security.md and the chart comment with it", DefaultTTL)
	}
}

// ---- Phase 82: the identity is issuer + subject

func TestTheSameSubjectUnderTwoIssuersIsTwoPeople(t *testing.T) {
	b, cs, _ := newBroker(t)
	ctx := context.Background()
	a, err := b.TokenFor(ctx, authn.Identity{Issuer: "http://kc-a.home/realms/vinaire", Subject: "same-sub", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.TokenFor(ctx, authn.Identity{Issuer: "http://kc-b.home/realms/vinaire", Subject: "same-sub", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if a.KubernetesUser == c.KubernetesUser {
		t.Fatalf("one account for two issuers: %s", a.KubernetesUser)
	}
	list, _ := cs.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{})
	if len(list.Items) != 2 {
		t.Fatalf("want two accounts, got %d", len(list.Items))
	}
	for _, sa := range list.Items {
		if sa.Annotations[AnnotationIssuer] == "" {
			t.Errorf("%s has no issuer annotation", sa.Name)
		}
	}
	// and the same person under the same issuer is still one account
	again, err := b.TokenFor(ctx, authn.Identity{Issuer: "http://kc-a.home/realms/vinaire", Subject: "same-sub", Username: "alice"})
	if err != nil || again.KubernetesUser != a.KubernetesUser {
		t.Fatalf("repeat sign-in: %+v %v", again, err)
	}
}

func TestIdentityLabelSeparatesIssuerFromSubject(t *testing.T) {
	if IdentityLabel("http://a", "bc") == IdentityLabel("http://ab", "c") {
		t.Fatal("(issuer, subject) pairs that concatenate alike must not collide")
	}
	if IdentityLabel(testIssuer, "x") == SubjectLabel("x") {
		t.Fatal("the new label must differ from the legacy one, or legacy accounts would be indistinguishable")
	}
	if len(IdentityLabel(testIssuer, "x")) != 40 {
		t.Fatal("a label value must stay a valid, short label")
	}
}

func legacyAccount(name, subject string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns,
		Labels: map[string]string{LabelSubject: SubjectLabel(subject), LabelManagedBy: "vinaire"}}}
}

func TestALegacyAccountIsClaimedForTheIssuerVinaireWasSetUpWithAndKeepsItsName(t *testing.T) {
	legacy := legacyAccount("oidc-shiraku", "sub-123")
	b, cs, events := newBroker(t, legacy)
	b.AdoptLegacyIssuer = testIssuer
	ctx := context.Background()
	tok, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-123", Username: "shiraku"})
	if err != nil {
		t.Fatal(err)
	}
	// the same account — a RoleBinding to it must keep working
	if tok.KubernetesUser != "system:serviceaccount:vinaire-users:oidc-shiraku" {
		t.Fatalf("a new account was made instead of claiming the old one: %s", tok.KubernetesUser)
	}
	list, _ := cs.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{})
	if len(list.Items) != 1 {
		t.Fatalf("want the one account, got %d", len(list.Items))
	}
	sa := list.Items[0]
	if sa.Labels[LabelSubject] != IdentityLabel(testIssuer, "sub-123") || sa.Annotations[AnnotationIssuer] != testIssuer {
		t.Errorf("not relabelled: %v %v", sa.Labels, sa.Annotations)
	}
	if !strings.Contains(strings.Join(*events, ","), "oidc-account-adopted:oidc-shiraku") {
		t.Errorf("claiming an account must be audited: %v", *events)
	}
	// a second sign-in finds it by the new label and claims nothing
	if _, err := b.TokenFor(ctx, authn.Identity{Issuer: testIssuer, Subject: "sub-123", Username: "shiraku"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(*events, ","), "oidc-account-adopted"); n != 1 {
		t.Errorf("claimed %d times", n)
	}
}

func TestALegacyAccountIsNeverClaimedByAnotherIssuer(t *testing.T) {
	b, cs, _ := newBroker(t, legacyAccount("oidc-shiraku", "sub-123"))
	b.AdoptLegacyIssuer = testIssuer
	ctx := context.Background()
	tok, err := b.TokenFor(ctx, authn.Identity{Issuer: "http://other-keycloak.example/realms/x", Subject: "sub-123", Username: "shiraku"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(tok.KubernetesUser, ":oidc-shiraku") {
		t.Fatal("the same subject under another issuer got the old account and its rights")
	}
	old, _ := cs.CoreV1().ServiceAccounts(ns).Get(ctx, "oidc-shiraku", metav1.GetOptions{})
	if old.Labels[LabelSubject] != SubjectLabel("sub-123") || old.Annotations[AnnotationIssuer] != "" {
		t.Errorf("the legacy account was changed: %v %v", old.Labels, old.Annotations)
	}
}

func TestNothingIsClaimedUnlessTheOperatorSideSaidWhichIssuer(t *testing.T) {
	b, _, _ := newBroker(t, legacyAccount("oidc-shiraku", "sub-123"))
	// AdoptLegacyIssuer left empty
	tok, err := b.TokenFor(context.Background(), authn.Identity{Issuer: testIssuer, Subject: "sub-123", Username: "shiraku"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(tok.KubernetesUser, ":oidc-shiraku") {
		t.Fatal("a legacy account was claimed with no issuer pinned")
	}
}

func TestAnIssuerIsRequired(t *testing.T) {
	b, _, _ := newBroker(t)
	for _, bad := range []string{"", "keycloak.home", "ftp://x", "http://has space"} {
		if _, err := b.TokenFor(context.Background(), authn.Identity{Issuer: bad, Subject: "s", Username: "u"}); err == nil {
			t.Errorf("issuer %q should be refused", bad)
		}
	}
}
