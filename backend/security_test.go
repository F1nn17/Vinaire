package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiraku/vinaire/backend/internal/authn"
	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// ---- headers and CSP ----------------------------------------------------------------

func TestSecurityHeaders(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))

	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/auth/config", nil))
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := api.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if csp := api.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("API CSP = %q", csp)
	}
	if api.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must not be sent over plain HTTP")
	}

	// HSTS over HTTPS (directly or behind a proxy that says so)
	r := httptest.NewRequest("GET", "/api/v1/x", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	https := httptest.NewRecorder()
	h.ServeHTTP(https, r)
	if !strings.Contains(https.Header().Get("Strict-Transport-Security"), "max-age=") {
		t.Error("no HSTS over HTTPS")
	}

	// a static asset must NOT carry the allow-nothing CSP: a Web Worker takes its
	// CSP from its own script's response and would be unable to importScripts
	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest("GET", "/_next/static/chunks/worker.js", nil))
	if csp := asset.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("a static asset got a CSP: %q", csp)
	}

	// the port-forward proxy's pages are somebody else's content: untouched
	proxy := httptest.NewRecorder()
	h.ServeHTTP(proxy, httptest.NewRequest("GET", "/proxy/c/ns/pod/80/", nil))
	if len(proxy.Header()) != 0 {
		t.Errorf("the proxy response was given headers: %v", proxy.Header())
	}
}

func writeExport(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	page := `<html><head><script src="/_next/a.js"></script><script>self.__next_f=[]</script></head><body><script type="application/json">{"a":1}</script></body></html>`
	for name, body := range map[string]string{"index.html": page, "login.html": page, "secret.txt": "top secret"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// something outside the served directory that must stay unreachable
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.html"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(filepath.Dir(dir), "outside.html")) })
	return dir
}

func TestPageCSPListsInlineScriptsByHashNotUnsafeInline(t *testing.T) {
	h := securityHeaders(staticFileHandler(writeExport(t)))
	r := httptest.NewRequest("GET", "/login", nil)
	r.Host = "vinaire.example.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	scriptSrc := ""
	for _, d := range strings.Split(csp, ";") {
		if d = strings.TrimSpace(d); strings.HasPrefix(d, "script-src") {
			scriptSrc = d
		}
	}
	if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
		t.Errorf("script-src allows inline/eval code: %s", scriptSrc)
	}
	if strings.Count(scriptSrc, "'sha256-") != 2 { // the inline script and the JSON one; the external one is covered by 'self'
		t.Errorf("script-src = %s", scriptSrc)
	}
	for _, must := range []string{"frame-ancestors 'none'", "object-src 'none'", "base-uri 'self'", "connect-src 'self' ws://vinaire.example.com wss://vinaire.example.com"} {
		if !strings.Contains(csp, must) {
			t.Errorf("CSP lacks %q: %s", must, csp)
		}
	}
	if strings.Contains(csp, "connect-src 'self' ws: ") || strings.Contains(csp, " *") {
		t.Errorf("connect-src is too broad: %s", csp)
	}
}

func TestStaticHandlerCannotEscapeItsDirectory(t *testing.T) {
	dir := writeExport(t)
	h := staticFileHandler(dir)
	for _, path := range []string{
		"/../outside", "/..%2foutside", "/%2e%2e/outside", "/a/../../outside",
		`/..\outside`, "/%5c..%5coutside", "/..%00", "/x%00",
	} {
		r := httptest.NewRequest("GET", "http://x"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), "outside") {
			t.Errorf("%q served a file from outside the directory", path)
		}
	}
	// and an ordinary file is still served
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/secret.txt", nil))
	if w.Code != 200 {
		t.Errorf("a file inside the directory: %d", w.Code)
	}
}

// ---- the canary test --------------------------------------------------------------
//
// Roadmap Phase 36: push recognisable secrets through the main flows, then
// confirm the marker appears in no log output and in no error response. Logging is
// captured at the most verbose level, so anything that logs anything shows up.

const canary = "CANARY-7f3a91c2"

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestCanaryNeverReachesTheLogsOrErrorResponses(t *testing.T) {
	logs := captureLogs(t)
	var responses strings.Builder

	// the request logger, with the secret in every place a browser or client can put one
	logged := requestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	r := httptest.NewRequest("POST", "/api/v1/secrets/ns/db/reveal?token="+canary, strings.NewReader(`{"token":"`+canary+`","password":"`+canary+`"}`))
	r.Header.Set("Authorization", "Bearer "+canary)
	r.AddCookie(&http.Cookie{Name: "vinaire_session", Value: canary})
	r.Header.Set("X-Cluster", "dev")
	w := httptest.NewRecorder()
	logged.ServeHTTP(w, r)
	if !strings.Contains(logs.String(), "request") {
		t.Fatal("the request logger logged nothing — the test isn't observing anything")
	}

	// token login: accepted, rejected and unreachable-cluster outcomes
	for _, validate := range []authn.TokenValidator{
		func(context.Context, string) error { return nil },
		func(context.Context, string) error { return authn.ErrInvalidToken },
		func(context.Context, string) error { return errors.New("dial tcp: " + "i/o timeout") },
	} {
		m, err := authn.NewManager(context.Background(), authn.ManagerConfig{AuthMode: "token", CookieSecure: true, CookieSameSite: "strict"})
		if err != nil {
			t.Fatal(err)
		}
		m.ValidateToken = validate
		body, _ := json.Marshal(map[string]string{"token": canary})
		lw := httptest.NewRecorder()
		m.HandleTokenLogin(lw, httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body)))
		responses.WriteString(lw.Body.String())
		for _, c := range lw.Result().Cookies() {
			if strings.Contains(c.Value, canary) {
				t.Error("the session cookie contains the token")
			}
		}
	}

	// a kubeconfig carrying the marker in every credential field: parse errors and
	// previews must not echo it back
	kubeconfig := `apiVersion: v1
kind: Config
current-context: c
clusters:
- name: c
  cluster: {server: "http://169.254.169.254", certificate-authority-data: ` + canary + `}
contexts:
- name: c
  context: {cluster: c, user: u}
users:
- name: u
  user: {token: ` + canary + `, client-key-data: ` + canary + `, password: ` + canary + `}
`
	if _, err := clusters.ParseKubeconfig([]byte(kubeconfig), clusters.ParseOptions{}); err != nil {
		responses.WriteString(err.Error())
	}
	if _, err := clusters.ParseKubeconfig([]byte("not: [valid yaml "+canary), clusters.ParseOptions{}); err != nil {
		responses.WriteString(err.Error())
	}

	if strings.Contains(logs.String(), canary) {
		t.Errorf("the canary reached the logs:\n%s", firstLineWith(logs.String(), canary))
	}
	if strings.Contains(responses.String(), canary) {
		t.Errorf("the canary reached an error response:\n%s", firstLineWith(responses.String(), canary))
	}
}

func firstLineWith(s, needle string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}
