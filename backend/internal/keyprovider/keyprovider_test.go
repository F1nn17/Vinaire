package keyprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// a Transit engine that "encrypts" by reversing and tagging, and checks the token
func fakeVault(t *testing.T, logins *atomic.Int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.URL.Path == "/v1/auth/kubernetes/login":
			logins.Add(1)
			if body["role"] != "vinaire" || body["jwt"] != "sa-jwt" {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
				return
			}
			_, _ = w.Write([]byte(`{"auth":{"client_token":"k8s-token","lease_duration":3600}}`))
			return
		}
		if tok := r.Header.Get("X-Vault-Token"); tok != "root" && tok != "k8s-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		switch r.URL.Path {
		case "/v1/transit/encrypt/vinaire":
			raw, _ := base64.StdEncoding.DecodeString(body["plaintext"])
			_, _ = w.Write([]byte(`{"data":{"ciphertext":"vault:v1:` + base64.StdEncoding.EncodeToString(reverse(raw)) + `"}}`))
		case "/v1/transit/decrypt/vinaire":
			c := strings.TrimPrefix(strings.TrimPrefix(body["ciphertext"], "vault:v2:"), "vault:v1:")
			raw, err := base64.StdEncoding.DecodeString(c)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":["invalid ciphertext"]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"plaintext":"` + base64.StdEncoding.EncodeToString(reverse(raw)) + `"}}`))
		case "/v1/transit/rewrap/vinaire":
			c := strings.TrimPrefix(body["ciphertext"], "vault:v1:")
			_, _ = w.Write([]byte(`{"data":{"ciphertext":"vault:v2:` + c + `"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

func newTestVault(t *testing.T, srv *httptest.Server, mod func(*VaultConfig)) Provider {
	cfg := VaultConfig{Addr: srv.URL, AllowInsecure: true, Token: "root", HTTP: srv.Client()}
	if mod != nil {
		mod(&cfg)
	}
	p, err := NewVault(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWrapUnwrapAndRewrap(t *testing.T) {
	var logins atomic.Int32
	p := newTestVault(t, fakeVault(t, &logins), nil)
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i + 1) // not a palindrome, so the fake's reversal really changes it
	}
	w, err := p.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w, base64.StdEncoding.EncodeToString(dek)) {
		t.Error("the wrapped key carries the key in the clear")
	}
	got, err := p.Unwrap(context.Background(), w)
	if err != nil || !bytes.Equal(got, dek) {
		t.Fatalf("unwrap: %v %v", got, err)
	}
	rw, err := p.(Rewrapper).Rewrap(context.Background(), w)
	if err != nil || !strings.HasPrefix(rw, "vault:v2:") {
		t.Fatalf("rewrap: %q %v", rw, err)
	}
	if got, err := p.Unwrap(context.Background(), rw); err != nil || !bytes.Equal(got, dek) {
		t.Errorf("a rewrapped key must open to the same key: %v", err)
	}
	if p.Name() != "vault:transit/vinaire" {
		t.Errorf("name: %s", p.Name())
	}
}

func TestVaultRefusesWrongTokenAndTheErrorHasNoKey(t *testing.T) {
	var logins atomic.Int32
	p := newTestVault(t, fakeVault(t, &logins), func(c *VaultConfig) { c.Token = "stolen-or-expired" })
	_, err := p.Wrap(context.Background(), bytes.Repeat([]byte{9}, 32))
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("want a permission error, got %v", err)
	}
	if strings.Contains(err.Error(), base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))) {
		t.Error("the error echoes the key")
	}
	// something that isn't a 32-byte key comes back from Vault
	short := newTestVault(t, fakeVault(t, &logins), nil)
	if _, err := short.Unwrap(context.Background(), "vault:v1:"+base64.StdEncoding.EncodeToString([]byte("abc"))); err == nil {
		t.Error("a wrong-length key was accepted")
	}
}

func TestKubernetesAuthLogsInOnceAndCachesTheToken(t *testing.T) {
	var logins atomic.Int32
	srv := fakeVault(t, &logins)
	jwt := filepath.Join(t.TempDir(), "jwt")
	_ = os.WriteFile(jwt, []byte("sa-jwt\n"), 0o600)
	p := newTestVault(t, srv, func(c *VaultConfig) { c.Token, c.K8sRole, c.JWTFile = "", "vinaire", jwt })
	for i := 0; i < 3; i++ {
		if _, err := p.Wrap(context.Background(), bytes.Repeat([]byte{1}, 32)); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Errorf("logged in %d times; the token should be cached", logins.Load())
	}
	bad := newTestVault(t, srv, func(c *VaultConfig) { c.Token, c.K8sRole, c.JWTFile = "", "someone-else", jwt })
	if _, err := bad.Wrap(context.Background(), bytes.Repeat([]byte{1}, 32)); err == nil {
		t.Error("a wrong Kubernetes-auth role was accepted")
	}
}

func TestConfigurationIsStrict(t *testing.T) {
	for name, cfg := range map[string]VaultConfig{
		"no address":               {Token: "t"},
		"plain http":               {Addr: "http://vault:8200", Token: "t"},
		"credentials in the URL":   {Addr: "https://u:p@vault:8200", Token: "t"},
		"a path escape in the key": {Addr: "https://vault", Token: "t", KeyName: "../sys"},
		"a path escape in a mount": {Addr: "https://vault", Token: "t", TransitMount: "transit/../sys"},
		"no way to sign in":        {Addr: "https://vault"},
		"a query in the key name":  {Addr: "https://vault", Token: "t", KeyName: "a?b=c"},
		"a file scheme as address": {Addr: "file:///etc/passwd", Token: "t"},
	} {
		if _, err := NewVault(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	t.Setenv("KEY_PROVIDER", "")
	if _, err := FromEnv(); err != ErrNotConfigured {
		t.Errorf("default must be no provider, got %v", err)
	}
	t.Setenv("KEY_PROVIDER", "kms-of-my-dreams")
	if _, err := FromEnv(); err == nil || err == ErrNotConfigured {
		t.Errorf("an unknown provider must be an error, got %v", err)
	}
}
