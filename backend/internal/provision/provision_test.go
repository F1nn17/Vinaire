package provision

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSecretNeverPrints(t *testing.T) {
	s := Secret("hunter2-super-secret")
	type wrapper struct {
		Pw Secret `json:"pw"`
	}
	outputs := []string{
		fmt.Sprint(s), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s), fmt.Sprintf("%s", s),
		fmt.Sprintf("%v", wrapper{s}), fmt.Sprintf("%+v", &wrapper{s}),
	}
	b, _ := json.Marshal(wrapper{s})
	outputs = append(outputs, string(b))
	var sb strings.Builder
	slog.New(slog.NewTextHandler(&sb, nil)).Info("x", "pw", s, "w", wrapper{s})
	outputs = append(outputs, sb.String())

	for _, o := range outputs {
		if strings.Contains(o, "hunter2") {
			t.Errorf("secret leaked in %q", o)
		}
	}

	// ...but decoding into one still works.
	var w wrapper
	if err := json.Unmarshal([]byte(`{"pw":"real-value"}`), &w); err != nil || w.Pw.Reveal() != "real-value" {
		t.Errorf("decode: %v %q", err, w.Pw.Reveal())
	}
}

func TestRedactor(t *testing.T) {
	key := Secret("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAAB\nQQQQ-line-two-of-key\n-----END OPENSSH PRIVATE KEY-----")
	r := NewRedactor(Secret("p@ssw0rd!"), key, Secret("ab"))
	got := r.Redact("login p@ssw0rd! ok; key line b3BlbnNzaC1rZXktdjEAAAAAB; ab stays")
	if strings.Contains(got, "p@ssw0rd!") || strings.Contains(got, "b3BlbnNzaC1") {
		t.Errorf("not redacted: %q", got)
	}
	if !strings.Contains(got, "ab stays") {
		t.Errorf("a too-short secret must not be replaced everywhere: %q", got)
	}
	r.Add(Secret("K10abcdef0123456789::server:tok"))
	if strings.Contains(r.Redact("token K10abcdef0123456789::server:tok"), "K10abcdef") {
		t.Error("late-added secret not redacted")
	}
}

func TestValidateTargetRefusesDangerousAddresses(t *testing.T) {
	ctx := context.Background()
	for _, host := range []string{"127.0.0.1", "127.1.2.3", "::1", "169.254.169.254", "0.0.0.0", "224.0.0.1", "fe80::1"} {
		if _, err := ValidateTarget(ctx, host, 22, "root"); err == nil {
			t.Errorf("%s should be refused", host)
		}
	}
	for _, host := range []string{"10.0.0.5", "192.168.1.20", "172.19.0.2"} {
		if _, err := ValidateTarget(ctx, host, 22, "root"); err != nil {
			t.Errorf("%s should be allowed: %v", host, err)
		}
	}
	bad := []struct {
		host string
		port int
		user string
	}{
		{"bad host", 22, "root"}, {"1.2.3.4;rm -rf /", 22, "root"}, {"1.2.3.4", 0, "root"}, {"1.2.3.4", 70000, "root"},
		{"1.2.3.4", 22, "root; id"}, {"1.2.3.4", 22, "$(id)"},
	}
	for _, c := range bad {
		if _, err := ValidateTarget(ctx, c.host, c.port, c.user); err == nil {
			t.Errorf("%+v should be rejected", c)
		}
	}
}

func TestJoinScriptCannotBeInjectedInto(t *testing.T) {
	// Whatever slips past validation is still single-quoted.
	script := joinScript(JoinInfo{
		ServerURL: "https://10.0.0.1:6443",
		Token:     Secret("K10abc::server:x'; touch /tmp/pwned; echo '"),
		NodeName:  "n1",
	})
	if strings.Contains(script, "export K3S_TOKEN='K10abc::server:x'; touch") {
		t.Errorf("quote escape broke out of the token:\n%s", script)
	}
	if got := shQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shQuote = %s", got)
	}

	// And validation rejects those values before they get that far.
	k := K3s{}
	for _, j := range []JoinInfo{
		{ServerURL: "https://10.0.0.1:6443; id", Token: "K10aaaaaaaa::server:x"},
		{ServerURL: "http://10.0.0.1:6443", Token: "K10aaaaaaaa::server:x"},
		{ServerURL: "https://10.0.0.1:6443", Token: "bad token with spaces"},
		{ServerURL: "https://10.0.0.1:6443", Token: "K10aaaaaaaa::server:x", Version: "v1; id"},
		{ServerURL: "https://10.0.0.1:6443", Token: "K10aaaaaaaa::server:x", NodeName: "Bad Name"},
	} {
		if err := k.JoinWorker(context.Background(), nil, j, func(string) {}); err == nil {
			t.Errorf("%+v should be rejected before running anything", j)
		}
	}
}

// testServer is a minimal SSH server that records every authentication
// attempt, so tests can prove credentials were (not) sent.
type testServer struct {
	addr          string
	fingerprint   string
	passwordTries atomic.Int32
}

func newTestServer(t *testing.T, password string) *testServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	ts := &testServer{fingerprint: ssh.FingerprintSHA256(hostKey.PublicKey())}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			ts.passwordTries.Add(1)
			if string(pw) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ts.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				conn, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					_ = ch.Reject(ssh.Prohibited, "test server")
				}
				_ = conn.Close()
			}()
		}
	}()
	return ts
}

func TestFetchHostKeyReturnsFingerprintWithoutAuthenticating(t *testing.T) {
	ts := newTestServer(t, "pw")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info, err := FetchHostKey(ctx, ts.addr)
	if err != nil {
		t.Fatal(err)
	}
	if info.Fingerprint != ts.fingerprint || info.KeyType != "ssh-ed25519" {
		t.Errorf("got %+v, want fingerprint %s", info, ts.fingerprint)
	}
	if n := ts.passwordTries.Load(); n != 0 {
		t.Errorf("host-key fetch attempted authentication %d time(s)", n)
	}
}

// The core promise: a host key that isn't the one the user confirmed aborts the
// handshake BEFORE the password is ever sent.
func TestDialRefusesWrongHostKeyBeforeSendingCredentials(t *testing.T) {
	ts := newTestServer(t, "correct-password")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	creds := Credentials{Username: "root", Password: "correct-password"}

	wrong := "SHA256:" + strings.Repeat("A", 43)
	if _, err := Dial(ctx, ts.addr, creds, wrong); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("expected a host key mismatch error, got %v", err)
	}
	if n := ts.passwordTries.Load(); n != 0 {
		t.Fatalf("password was sent to a server with the wrong host key (%d attempts)", n)
	}

	// With the right fingerprint the login goes through.
	r, err := Dial(ctx, ts.addr, creds, ts.fingerprint)
	if err != nil {
		t.Fatalf("login with the pinned key failed: %v", err)
	}
	r.Close()
	if n := ts.passwordTries.Load(); n == 0 {
		t.Error("expected the pinned login to authenticate")
	}
}
