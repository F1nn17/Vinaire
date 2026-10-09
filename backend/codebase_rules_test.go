package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Roadmap Phase 36: properties that a grep can enforce, enforced on every build.
// Each rule scans the non-test Go sources and fails with the offending line, so a
// future change that quietly weakens one has to do so in the open — by editing the
// rule, in a diff a reviewer will see.

func goSources(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "integration" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(path)] = strings.Split(string(data), "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 20 {
		t.Fatalf("only %d source files found — the walk isn't covering the code base", len(out))
	}
	return out
}

// code returns the line without its // comment, so a comment that mentions a
// forbidden name (like this package's own explanations) doesn't count as a use.
func code(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// SSH host keys are always verified against a fingerprint the user confirmed.
func TestNoInsecureIgnoreHostKey(t *testing.T) {
	for file, lines := range goSources(t) {
		for n, l := range lines {
			if strings.Contains(code(l), "InsecureIgnoreHostKey") {
				t.Errorf("%s:%d uses ssh.InsecureIgnoreHostKey — host keys must be pinned: %s", file, n+1, strings.TrimSpace(l))
			}
		}
	}
}

// Skipping TLS verification is allowed in exactly two places, each with a reason.
func TestInsecureSkipVerifyOnlyWhereReviewed(t *testing.T) {
	allowed := map[string]string{
		"internal/api/portforward.go":   "pods serve self-signed certificates; the connection is a tunnel through the Kubernetes API, not the open network",
		"internal/clusters/discover.go": "an identity probe of a local cluster: nothing is sent to it or trusted from it",
		"internal/clusters/store.go":    "a cluster added without a CA certificate — the UI says so and asks (Phase 24)",
	}
	for file, lines := range goSources(t) {
		for n, l := range lines {
			if strings.Contains(code(l), "InsecureSkipVerify") || regexp.MustCompile(`Insecure:\s*true`).MatchString(code(l)) {
				if _, ok := allowed[file]; !ok {
					t.Errorf("%s:%d disables TLS verification and isn't on the reviewed list: %s", file, n+1, strings.TrimSpace(l))
				}
			}
		}
	}
}

// A WebSocket upgrader that accepts every origin lets any website open a socket
// with the user's cookie.
func TestNoPermissiveCheckOrigin(t *testing.T) {
	permissive := regexp.MustCompile(`CheckOrigin:\s*func\([^)]*\)\s*bool\s*\{\s*return true`)
	for file, lines := range goSources(t) {
		joined := strings.Join(lines, "\n")
		if permissive.MatchString(joined) {
			t.Errorf("%s has a WebSocket CheckOrigin that returns true", file)
		}
	}
}

// Responses are never given a wildcard CORS origin by hand (the cors middleware,
// fed an explicit allow-list, is the only thing that sets them).
func TestNoWildcardCORSHeaders(t *testing.T) {
	for file, lines := range goSources(t) {
		for n, l := range lines {
			c := code(l)
			if strings.Contains(c, "Access-Control-Allow-Origin") && strings.Contains(c, `"*"`) {
				t.Errorf("%s:%d sets a wildcard CORS origin: %s", file, n+1, strings.TrimSpace(l))
			}
		}
	}
}

// Nothing builds a shell command line out of strings: user-influenced values
// reach a remote shell only through the provisioning package's validated,
// single-quoted templates (which are covered by their own tests).
func TestNoLocalShellExecution(t *testing.T) {
	shell := regexp.MustCompile(`exec\.Command(Context)?\([^)]*"(sh|bash|cmd|powershell)"`)
	for file, lines := range goSources(t) {
		for n, l := range lines {
			if shell.MatchString(code(l)) {
				t.Errorf("%s:%d runs a local shell: %s", file, n+1, strings.TrimSpace(l))
			}
		}
	}
}

// Roadmap Phase 50: every handler that reads a request body bounds it first, so no
// request can make Vinaire buffer an unbounded amount of memory.
func TestEveryRequestBodyIsSizeLimited(t *testing.T) {
	for file, lines := range goSources(t) {
		if !strings.HasPrefix(file, "internal/api/") && !strings.HasPrefix(file, "internal/stream/") && !strings.HasPrefix(file, "internal/authn/") {
			continue
		}
		for n, l := range lines {
			c := code(l)
			if !strings.Contains(c, "r.Body") && !strings.Contains(c, "req.Body") {
				continue
			}
			if strings.Contains(c, "MaxBytesReader") || strings.Contains(c, "Body.Close") {
				continue
			}
			t.Errorf("%s:%d reads a request body without http.MaxBytesReader: %s", file, n+1, strings.TrimSpace(l))
		}
	}
}

// Roadmap Phase 61: the API server authorizes a service proxy by the name
// "scheme:service:port" — which is how Vinaire always calls it (ProxyGet("http", name, port)) —
// not by the bare Service name. A role listing bare names grants nothing: Prometheus and Loki
// answered "forbidden" to a viewer who had, on paper, the right to reach them.
func TestServiceProxyRulesNameSchemeServicePort(t *testing.T) {
	for _, file := range []string{"../charts/vinaire/templates/_helpers.tpl", "../AGENTS.md"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !strings.Contains(line, `"services/proxy"`) {
				continue
			}
			// the rule's resourceNames line is within the next few lines (or just before)
			for j := i - 2; j <= i+3 && j < len(lines); j++ {
				if j < 0 || !strings.Contains(lines[j], "resourceNames:") {
					continue
				}
				for _, name := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(lines[j], -1) {
					if !regexp.MustCompile(`^https?:[a-z0-9.-]+:[0-9]+$`).MatchString(name[1]) {
						t.Errorf("%s:%d: services/proxy resourceName %q must be scheme:service:port", file, j+1, name[1])
					}
				}
			}
		}
	}
}
