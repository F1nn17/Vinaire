// Roadmap Phase 80: the identity broker against a real cluster, with the Role the chart really ships.
// The chart's users-namespace manifests are rendered with helm and applied, so a change to that Role that
// widens it — or breaks the broker — fails here. Needs helm and kubectl; skipped without them.
//
//go:build integration

package integration

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/oidcbroker"
)

var authenticationSelfReview = authenticationv1.SelfSubjectReview{}

const brokerIssuer = "http://keycloak.home/realms/vinaire"

const (
	brokerRelease = "bk"
	brokerHomeNS  = "broker-ci" // where "Vinaire" runs
	brokerUsersNS = "bk-vinaire-users"
)

// as returns a clientset that authenticates with token alone, against the cluster of the current kubeconfig.
func as(t *testing.T, token string) kubernetes.Interface {
	t.Helper()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.BearerToken, cfg.BearerTokenFile = token, ""
	cfg.CertData, cfg.KeyData, cfg.CertFile, cfg.KeyFile = nil, nil, "", ""
	cfg.Username, cfg.Password = "", ""
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestBrokerWorksWithTheChartsRoleAndNothingMore(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	rendered, err := exec.Command("helm", "template", brokerRelease, "../../charts/vinaire", "--namespace", brokerHomeNS,
		"--show-only", "templates/users-namespace.yaml").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, rendered)
	}
	kubectl(t, "", "create", "namespace", brokerHomeNS)
	t.Cleanup(func() {
		_ = exec.Command("kubectl", "delete", "namespace", brokerHomeNS, brokerUsersNS, "--wait=false").Run()
	})
	kubectl(t, string(rendered), "apply", "-f", "-")
	// the ServiceAccount the chart's Role is bound to — "Vinaire's own identity"
	sa := brokerRelease + "-vinaire"
	kubectl(t, "", "-n", brokerHomeNS, "create", "serviceaccount", sa)
	vinaire := as(t, strings.TrimSpace(kubectl(t, "", "-n", brokerHomeNS, "create", "token", sa, "--duration=30m")))

	ctx := context.Background()
	b := &oidcbroker.Broker{Clientset: vinaire, Namespace: brokerUsersNS, TTL: time.Hour}

	t.Run("a first sign-in gets an account and a token that works", func(t *testing.T) {
		tok, err := b.TokenFor(ctx, authn.Identity{Issuer: brokerIssuer, Subject: "11111111-aaaa", Username: "Shiraku"})
		if err != nil {
			t.Fatal(err)
		}
		want := "system:serviceaccount:" + brokerUsersNS + ":oidc-shiraku"
		if tok.KubernetesUser != want {
			t.Fatalf("got %s, want %s", tok.KubernetesUser, want)
		}
		person := as(t, tok.Token)
		review, err := person.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationSelfReview, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if review.Status.UserInfo.Username != want {
			t.Errorf("the cluster sees %q", review.Status.UserInfo.Username)
		}
		// a new account has no rights at all
		if _, err := person.CoreV1().Pods("default").List(ctx, metav1.ListOptions{}); err == nil {
			t.Error("a new account could list pods")
		}
		if _, err := person.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
			t.Error("a new account could list namespaces")
		}
	})

	t.Run("the same person gets the same account again", func(t *testing.T) {
		tok, err := b.TokenFor(ctx, authn.Identity{Issuer: brokerIssuer, Subject: "11111111-aaaa", Username: "renamed"})
		if err != nil || !strings.HasSuffix(tok.KubernetesUser, ":oidc-shiraku") {
			t.Fatalf("%+v %v", tok, err)
		}
	})

	t.Run("Vinaire's own rights stop at the users' namespace", func(t *testing.T) {
		if _, err := vinaire.CoreV1().ServiceAccounts("default").Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sneaky"}}, metav1.CreateOptions{}); err == nil {
			t.Error("it could create an account outside its namespace")
		}
		if _, err := vinaire.CoreV1().Secrets(brokerUsersNS).List(ctx, metav1.ListOptions{}); err == nil {
			t.Error("it could read Secrets")
		}
		if _, err := vinaire.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err == nil {
			t.Error("it could list role bindings")
		}
		// minting a token for an account outside its namespace must fail
		if _, err := vinaire.CoreV1().ServiceAccounts("kube-system").CreateToken(ctx, "default", &authenticationv1.TokenRequest{}, metav1.CreateOptions{}); err == nil {
			t.Error("it minted a token in kube-system")
		}
	})

	t.Run("an account made before the issuer counted is claimed, not duplicated, with the chart's Role", func(t *testing.T) {
		// as an older Vinaire made it: labelled with the subject alone, no issuer annotation
		kubectl(t, "", "-n", brokerUsersNS, "create", "serviceaccount", "oidc-legacy")
		kubectl(t, "", "-n", brokerUsersNS, "label", "serviceaccount", "oidc-legacy",
			oidcbroker.LabelSubject+"="+oidcbroker.SubjectLabel("33333333-cccc"), oidcbroker.LabelManagedBy+"=vinaire")
		adopting := &oidcbroker.Broker{Clientset: vinaire, Namespace: brokerUsersNS, TTL: time.Hour, AdoptLegacyIssuer: brokerIssuer}
		tok, err := adopting.TokenFor(ctx, authn.Identity{Issuer: brokerIssuer, Subject: "33333333-cccc", Username: "legacy"})
		if err != nil {
			t.Fatal(err)
		}
		if want := "system:serviceaccount:" + brokerUsersNS + ":oidc-legacy"; tok.KubernetesUser != want {
			t.Fatalf("got %s, want the old account %s", tok.KubernetesUser, want)
		}
		sa, err := vinaire.CoreV1().ServiceAccounts(brokerUsersNS).Get(ctx, "oidc-legacy", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if sa.Labels[oidcbroker.LabelSubject] != oidcbroker.IdentityLabel(brokerIssuer, "33333333-cccc") || sa.Annotations[oidcbroker.AnnotationIssuer] != brokerIssuer {
			t.Errorf("not relabelled: %v %v", sa.Labels, sa.Annotations)
		}
		// the right to patch stops at the users' namespace
		if _, err := vinaire.CoreV1().ServiceAccounts("default").Patch(ctx, "default", types.MergePatchType, []byte(`{"metadata":{"labels":{"x":"y"}}}`), metav1.PatchOptions{}); err == nil {
			t.Error("it could patch an account outside its namespace")
		}
	})

	t.Run("the same subject under another issuer is another account", func(t *testing.T) {
		other, err := b.TokenFor(ctx, authn.Identity{Issuer: "http://other-keycloak.home/realms/vinaire", Subject: "11111111-aaaa", Username: "Shiraku"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(other.KubernetesUser, ":oidc-shiraku") {
			t.Fatalf("another issuer reached the first issuer's account: %s", other.KubernetesUser)
		}
	})

	t.Run("deleting the account ends its token within seconds", func(t *testing.T) {
		tok, err := b.TokenFor(ctx, authn.Identity{Issuer: brokerIssuer, Subject: "22222222-bbbb", Username: "temp"})
		if err != nil {
			t.Fatal(err)
		}
		person := as(t, tok.Token)
		if _, err := person.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationSelfReview, metav1.CreateOptions{}); err != nil {
			t.Fatalf("the token should work before the account is deleted: %v", err)
		}
		kubectl(t, "", "-n", brokerUsersNS, "delete", "serviceaccount", "oidc-temp")
		eventually(t, 40*time.Second, func() bool {
			_, err := person.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authenticationSelfReview, metav1.CreateOptions{})
			return err != nil
		})
	})
}
