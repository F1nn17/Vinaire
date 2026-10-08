package main

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Roadmap Phase 36: response headers.
//
// Every response (except /proxy/, whose pages carry their own sandboxing CSP and
// are somebody else's content) gets the headers that cost nothing: no MIME
// sniffing, no framing, no referrer, HSTS when the request came over HTTPS.
// API responses additionally get a CSP that allows nothing.
// Pages get a real CSP from staticFileHandler (see cspIndex): `script-src` lists
// the exact inline scripts the static export contains by hash instead of
// 'unsafe-inline', so an injected <script> doesn't run.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/proxy/") {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		// Pages, API answers and 404s are never stored (an upgrade must show at once,
		// and nothing about a cluster may sit in a shared cache); only the content-hashed build
		// files under /_next/static/ may be kept.
		if !strings.HasPrefix(r.URL.Path, "/_next/static/") {
			h.Set("Cache-Control", "no-store")
		}
		// Only API responses get the allow-nothing policy. Static assets must not:
		// a Web Worker takes its CSP from its own script's response, so a worker
		// script served with default-src 'none' could not even importScripts.
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		}
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// cspIndex maps each exported HTML page to the hashes of its inline scripts.
type cspIndex map[string][]string

var inlineScriptRe = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)

// buildCSPIndex reads every .html file under dir once, at startup, and records
// the sha256 of each inline <script> (one without a src). The export is static,
// so those hashes are stable until the next build.
func buildCSPIndex(dir string) cspIndex {
	idx := cspIndex{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".html") {
			return nil
		}
		data, rerr := os.ReadFile(path) // #nosec G304 G122 -- the export directory is operator-configured and read once at startup
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		var hashes []string
		seen := map[string]bool{}
		for _, m := range inlineScriptRe.FindAllSubmatch(data, -1) {
			if strings.Contains(string(m[1]), "src=") {
				continue // an external script: 'self' covers it
			}
			sum := sha256.Sum256(m[2])
			h := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
			if !seen[h] {
				seen[h] = true
				hashes = append(hashes, h)
			}
		}
		sort.Strings(hashes)
		idx[filepath.ToSlash(rel)] = hashes
		return nil
	})
	return idx
}

// policy is the CSP for one HTML page served to host. Styles keep
// 'unsafe-inline' (the UI sets style attributes, and Monaco injects styles);
// scripts do not. WebSocket and fetch traffic may only go back to this host.
func (c cspIndex) policy(page, host string) string {
	script := "'self'"
	if hashes := c[page]; len(hashes) > 0 {
		script += " " + strings.Join(hashes, " ")
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + script,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self' ws://" + host + " wss://" + host,
		"worker-src 'self' blob:",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}
