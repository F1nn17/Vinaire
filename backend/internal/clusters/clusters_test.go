package clusters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const inlineKubeconfig = `apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: prod-cluster
  cluster:
    server: https://10.0.0.5:6443
    certificate-authority-data: Q0E=
- name: other-cluster
  cluster:
    server: https://10.9.9.9:6443
users:
- name: prod-user
  user:
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
- name: other-user
  user:
    token: OTHER-CLUSTER-SECRET-TOKEN
contexts:
- name: prod
  context: {cluster: prod-cluster, user: prod-user}
- name: other
  context: {cluster: other-cluster, user: other-user}
`

func TestParseKubeconfigKeepsOnlyTheChosenContext(t *testing.T) {
	info, err := ParseKubeconfig([]byte(inlineKubeconfig), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Context != "prod" || info.Server != "https://10.0.0.5:6443" || info.CredentialKind != CredClientCert {
		t.Errorf("unexpected info: %+v", info)
	}
	out := string(info.Kubeconfig)
	if strings.Contains(out, "OTHER-CLUSTER-SECRET-TOKEN") || strings.Contains(out, "10.9.9.9") {
		t.Errorf("another context's credentials leaked into the stored kubeconfig:\n%s", out)
	}

	other, err := ParseKubeconfig([]byte(inlineKubeconfig), ParseOptions{Context: "other"})
	if err != nil || other.CredentialKind != CredToken {
		t.Fatalf("other context: %v %+v", err, other)
	}
	if strings.Contains(string(other.Kubeconfig), "CERT") {
		t.Error("the prod client certificate leaked into the other context's kubeconfig")
	}
}

func TestParseKubeconfigRefusesUnsafeFeatures(t *testing.T) {
	base := func(user, cluster string) string {
		return "apiVersion: v1\nkind: Config\ncurrent-context: c\n" +
			"clusters:\n- name: k\n  cluster:\n    server: https://10.0.0.5:6443\n" + cluster +
			"users:\n- name: u\n  user:\n" + user +
			"contexts:\n- name: c\n  context: {cluster: k, user: u}\n"
	}
	cases := map[string]string{
		"exec plugin (runs a command)": base("    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: /bin/sh\n      args: [-c, id]\n", ""),
		"auth-provider plugin":         base("    auth-provider:\n      name: gcp\n", ""),
		"basic auth":                   base("    username: a\n    password: b\n", ""),
		"client-key file reference":    base("    client-certificate-data: Q0VSVA==\n    client-key: /etc/ssh/ssh_host_rsa_key\n", ""),
		"token file reference":         base("    tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token\n", ""),
		"CA file reference":            base("    token: abc\n", "    certificate-authority: /etc/passwd\n"),
		"proxy-url":                    base("    token: abc\n", "    proxy-url: http://evil:3128\n"),
		"cloud metadata server":        strings.Replace(base("    token: abc\n", ""), "10.0.0.5", "169.254.169.254", 1),
		"plain http server":            strings.Replace(base("    token: abc\n", ""), "https://", "http://", 1),
	}
	for name, kc := range cases {
		if _, err := ParseKubeconfig([]byte(kc), ParseOptions{}); err == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	// ...and a plain token kubeconfig is fine.
	if _, err := ParseKubeconfig([]byte(base("    token: abc\n", "")), ParseOptions{}); err != nil {
		t.Errorf("a plain token kubeconfig should be accepted: %v", err)
	}
}

func TestLocalFilesAreInlinedOnlyWhenAllowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("CA-PEM"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cfg"), []byte("apiVersion: v1\nkind: Config\ncurrent-context: c\n"+
		"clusters:\n- name: k\n  cluster:\n    server: https://10.0.0.5:6443\n    certificate-authority: ca.crt\n"+
		"users:\n- name: u\n  user:\n    token: abc\n"+
		"contexts:\n- name: c\n  context: {cluster: k, user: u}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseKubeconfigFile(filepath.Join(dir, "cfg"), ParseOptions{}); err == nil {
		t.Error("a file reference must be refused when local files aren't allowed")
	}
	info, err := ParseKubeconfigFile(filepath.Join(dir, "cfg"), ParseOptions{AllowLocalFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(info.Kubeconfig), "ca.crt") {
		t.Error("the normalized kubeconfig must not keep a file reference")
	}
}

func TestServerOverride(t *testing.T) {
	kc := strings.Replace(inlineKubeconfig, "https://10.0.0.5:6443", "https://127.0.0.1:6443", 1)
	info, err := ParseKubeconfig([]byte(kc), ParseOptions{ServerOverride: "https://192.168.1.50:6443"})
	if err != nil || info.Server != "https://192.168.1.50:6443" {
		t.Fatalf("%v %+v", err, info)
	}
}

func TestCipherRoundTripAndTamper(t *testing.T) {
	key, _ := GenerateKey()
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`[{"name":"x","kubeconfig":"SECRET-KUBECONFIG"}]`)
	sealed := c.Seal(plain)
	if bytes.Contains(sealed, []byte("SECRET-KUBECONFIG")) || !IsSealed(sealed) {
		t.Fatalf("not sealed: %s", sealed)
	}
	if bytes.Equal(sealed, c.Seal(plain)) {
		t.Error("two seals of the same data must differ (random nonce)")
	}
	got, err := c.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip: %v %s", err, got)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-3] ^= 1
	if _, err := c.Open(tampered); err == nil {
		t.Error("a tampered payload must not open")
	}
	otherKey, _ := GenerateKey()
	other, _ := NewCipher(otherKey)
	if _, err := other.Open(sealed); err == nil {
		t.Error("the wrong key must not open it")
	}
	if legacy, err := c.Open([]byte(`[{"name":"old"}]`)); err != nil || string(legacy) != `[{"name":"old"}]` {
		t.Errorf("legacy plaintext must pass through: %v %s", err, legacy)
	}
	if _, err := NewCipher([]byte("short")); err == nil {
		t.Error("a short key must be rejected")
	}
}

func TestFileStoreIsSealedAndSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFileStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	in := []StoredCluster{{Name: "home", Server: "https://10.0.0.5:6443", Kubeconfig: "TOP-SECRET-ADMIN-KUBECONFIG", Type: TypeK3s, Origin: OriginDiscovered}}
	if err := s.Save(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, fileStoreName))
	if bytes.Contains(raw, []byte("TOP-SECRET")) || bytes.Contains(raw, []byte("10.0.0.5")) {
		t.Fatalf("the data file isn't encrypted: %s", raw)
	}

	// A new process (same directory) reads it back with the key file.
	s2, err := NewFileStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := s2.Load(context.Background())
	if err != nil || len(out) != 1 || out[0].Kubeconfig != "TOP-SECRET-ADMIN-KUBECONFIG" || out[0].Type != TypeK3s {
		t.Fatalf("reload: %v %+v", err, out)
	}

	// The wrong key refuses to read it rather than returning garbage.
	otherKey, _ := GenerateKey()
	s3, err := NewFileStore(t.TempDir(), encodeKey(otherKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s3.dir, fileStoreName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s3.Load(context.Background()); err == nil {
		t.Error("loading with the wrong key must fail")
	}
}

func TestSupportsAddNode(t *testing.T) {
	for typ, want := range map[string]bool{
		TypeK3s: true, TypeKubeadm: true, TypeUnknown: true,
		TypeK3d: false, TypeKind: false, TypeMinikube: false, TypeManaged: false,
	} {
		if SupportsAddNode(typ) != want {
			t.Errorf("SupportsAddNode(%s) = %v, want %v", typ, !want, want)
		}
	}
}
