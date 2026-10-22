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

const ns = "vinaire-users"

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
	tok, err := b.TokenFor(context.Background(), authn.Identity{Subject: "6f1c-uuid", Username: "Shiraku"})
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
	if sa.Labels[LabelManagedBy] != "vinaire" || sa.Labels[LabelSubject] != SubjectLabel("6f1c-uuid") || sa.Annotations[AnnotationUsername] != "Shiraku" {
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
	if _, err := b.TokenFor(ctx, authn.Identity{Subject: "sub-1", Username: "alice"}); err != nil {
		t.Fatal(err)
	}
	tok, err := b.TokenFor(ctx, authn.Identity{Subject: "sub-1", Username: "alice.renamed"})
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
	a, err := b.TokenFor(ctx, authn.Identity{Subject: "sub-a", Username: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.TokenFor(ctx, authn.Identity{Subject: "sub-b", Username: "bob"})
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
	tok, err := b.TokenFor(context.Background(), authn.Identity{Subject: "sub-c", Username: "carol"})
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
	l := map[string]string{LabelSubject: SubjectLabel("sub-d"), LabelManagedBy: "vinaire"}
	b, _, _ := newBroker(t,
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "oidc-d1", Namespace: ns, Labels: l}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "oidc-d2", Namespace: ns, Labels: l}})
	if _, err := b.TokenFor(context.Background(), authn.Identity{Subject: "sub-d", Username: "d"}); err == nil {
		t.Fatal("two accounts for one subject must not be guessed between")
	}
}

func TestTokenLifetimeIsClampedToWhatKubernetesAccepts(t *testing.T) {
	b, _, _ := newBroker(t)
	b.TTL = time.Minute
	tok, err := b.TokenFor(context.Background(), authn.Identity{Subject: "s", Username: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(tok.Expiry); d < 9*time.Minute {
		t.Errorf("lifetime %s is below the minimum Kubernetes accepts", d)
	}
}

func TestWorksOnlyWithAConfiguredClusterAndUsableSubject(t *testing.T) {
	if _, err := (&Broker{}).TokenFor(context.Background(), authn.Identity{Subject: "s"}); err == nil {
		t.Error("an unconfigured broker must refuse")
	}
	b, _, _ := newBroker(t)
	for _, bad := range []string{"", "has space", "new\nline", strings.Repeat("a", 300)} {
		if _, err := b.TokenFor(context.Background(), authn.Identity{Subject: bad, Username: "x"}); err == nil {
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
