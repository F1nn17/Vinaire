package stream

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginChecker(t *testing.T) {
	check := originChecker([]string{"https://vinaire.example.com", "http://localhost:3100"})
	cases := []struct {
		origin, host string
		want         bool
	}{
		{"https://evil.example", "vinaire.example.com", false},
		{"http://vinaire.example.com.evil.example", "vinaire.example.com", false},
		{"null", "vinaire.example.com", false},
		{"::::", "vinaire.example.com", false},
		{"https://vinaire.example.com", "vinaire.example.com", true}, // same origin
		{"http://10.1.2.3:8080", "10.1.2.3:8080", true},
		{"http://localhost:3100", "localhost:8080", true}, // allow-listed
		{"", "vinaire.example.com", true},                 // not a browser: no Origin
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.Host = c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := check(r); got != c.want {
			t.Errorf("Origin %q on host %q: %v, want %v", c.origin, c.host, got, c.want)
		}
	}
}

// Every WebSocket endpoint (live stream, logs, exec, provisioning log) must check
// the handshake's Origin. A new endpoint that forgets CheckOrigin would accept a
// cross-site connection carrying the user's cookie, so this fails the build.
func TestEveryWebSocketUpgraderChecksTheOrigin(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	upgraders := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "websocket.Upgrader{") {
				upgraders++
				if !strings.Contains(line, "CheckOrigin: originChecker(") {
					t.Errorf("%s: a websocket.Upgrader without originChecker: %s", f, strings.TrimSpace(line))
				}
			}
		}
	}
	if upgraders < 4 {
		t.Errorf("found %d upgraders, expected the 4 known endpoints", upgraders)
	}
}
