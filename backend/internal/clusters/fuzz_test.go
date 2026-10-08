package clusters

import (
	"net"
	"net/url"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
)

// Roadmap Phase 50: fuzzing the parsers that take text from outside. Run as ordinary
// tests they check the seeds; `go test -fuzz=FuzzParseKubeconfig` (the Security workflow
// does, briefly) looks for inputs that break the stated properties.

var tokenNow = time.Unix(1_800_000_000, 0)

const goodKubeconfig = `apiVersion: v1
kind: Config
current-context: c
clusters:
- name: c
  cluster: {server: "https://10.0.0.1:6443", certificate-authority-data: Y2E=}
contexts:
- name: c
  context: {cluster: c, user: u}
users:
- name: u
  user: {token: abc}
`

func FuzzParseKubeconfig(f *testing.F) {
	for _, seed := range []string{
		goodKubeconfig,
		"", "{}", "not yaml: [",
		`apiVersion: v1
kind: Config
current-context: c
clusters: [{name: c, cluster: {server: "https://10.0.0.1"}}]
contexts: [{name: c, context: {cluster: c, user: u}}]
users: [{name: u, user: {exec: {command: sh, args: [-c, id]}}}]`,
		`apiVersion: v1
kind: Config
current-context: c
clusters: [{name: c, cluster: {server: "https://169.254.169.254", certificate-authority: /etc/passwd}}]
contexts: [{name: c, context: {cluster: c, user: u}}]
users: [{name: u, user: {tokenFile: /var/run/secrets/token, client-key: /root/.ssh/id_rsa}}]`,
		`apiVersion: v1
kind: Config
clusters: [{name: c, cluster: {server: "http://x", proxy-url: "http://evil"}}]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := ParseKubeconfig(data, ParseOptions{})
		if err != nil {
			return
		}
		// what was accepted is exactly the safe subset...
		cfg, err := clientcmd.Load(info.Kubeconfig)
		if err != nil {
			t.Fatalf("the normalized kubeconfig doesn't load: %v", err)
		}
		if len(cfg.Clusters) != 1 || len(cfg.AuthInfos) != 1 || len(cfg.Contexts) != 1 {
			t.Fatalf("not a single context: %d/%d/%d", len(cfg.Clusters), len(cfg.AuthInfos), len(cfg.Contexts))
		}
		for _, c := range cfg.Clusters {
			if c.ProxyURL != "" || c.CertificateAuthority != "" {
				t.Fatalf("a proxy or a file reference survived: %+v", c)
			}
			if err := checkServer(c.Server); err != nil {
				t.Fatalf("accepted a server that checkServer refuses: %q: %v", c.Server, err)
			}
		}
		for _, u := range cfg.AuthInfos {
			if u.Exec != nil || u.AuthProvider != nil || u.Username != "" || u.Password != "" ||
				u.TokenFile != "" || u.ClientCertificate != "" || u.ClientKey != "" {
				t.Fatalf("a command or a file reference survived: %+v", u)
			}
		}
		// ...and parsing it again changes nothing
		again, err := ParseKubeconfig(info.Kubeconfig, ParseOptions{})
		if err != nil || again.Server != info.Server || again.CredentialKind != info.CredentialKind {
			t.Fatalf("not idempotent: %v", err)
		}
		// the REST config built from it never runs a command
		rc, err := RESTConfigFromKubeconfig(info.Kubeconfig)
		if err == nil && (rc.ExecProvider != nil || rc.AuthProvider != nil) {
			t.Fatal("the REST config carries an exec/auth-provider plugin")
		}
	})
}

func FuzzCheckServer(f *testing.F) {
	for _, s := range []string{"https://10.0.0.1:6443", "http://x", "https://169.254.169.254", "https://[fe80::1]", "https://0.0.0.0", "https://224.0.0.1", "", "://", "https://user:pw@host"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if checkServer(s) != nil {
			return
		}
		u, err := url.Parse(s)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			t.Fatalf("accepted %q", s)
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()) {
			t.Fatalf("accepted a forbidden address: %q", s)
		}
	})
}

func FuzzTokenExpiryAndKeys(f *testing.F) {
	for _, s := range []string{"", "a.b.c", "eyJhbGciOiJub25lIn0.eyJleHAiOjF9.x", "....", "AAAA", "k3s://x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		TokenExpiry(s)
		CredentialHealth(s, tokenNow)
		if k, err := ParseKey(s); err == nil && len(k) != 32 {
			t.Fatalf("a key of %d bytes was accepted", len(k))
		}
		if ks, err := ParseKeys(s); err == nil {
			for _, k := range ks {
				if len(k) != 32 {
					t.Fatalf("a key of %d bytes was accepted", len(k))
				}
			}
		}
	})
}
