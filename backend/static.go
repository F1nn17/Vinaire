package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticFileHandler serves the frontend's static export (Roadmap Phase 10 —
// see AGENTS.md's "Rendering model": Next.js never runs as a server here,
// only `next build`'s output under dir is served, as-is). `next export`
// writes both a clean-URL HTML file per route (e.g. login.html) and a same-
// named directory holding RSC prefetch data — a plain http.FileServer would
// happily serve that directory's listing instead of the page for a request
// to exactly `/login`, so a request for an extension-less path tries
// `<path>.html` first and only falls back to the file server (for actual
// assets under /_next/, favicon.ico, etc.) if that doesn't exist.
//
// Every HTML page is served with a Content-Security-Policy built from the
// hashes of its own inline scripts (see cspIndex), and nothing outside dir can
// be reached: a path with a backslash or NUL is refused outright, and a resolved
// file path must stay under dir.
func staticFileHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	csp := buildCSPIndex(dir)
	absDir, _ := filepath.Abs(dir)

	setPolicy := func(w http.ResponseWriter, r *http.Request, page string) {
		w.Header().Set("Content-Security-Policy", csp.policy(page, r.Host))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.ContainsAny(p, "\\\x00") {
			http.NotFound(w, r)
			return
		}
		// http.FileServer already serves index.html for "/" on its own;
		// only extension-less non-root paths need the <path>.html lookup.
		if p != "/" && !strings.Contains(path.Base(p), ".") {
			rel := strings.TrimPrefix(path.Clean("/"+p), "/") + ".html"
			htmlPath := filepath.Join(dir, filepath.FromSlash(rel))
			if contained(absDir, htmlPath) {
				if info, err := os.Stat(htmlPath); err == nil && !info.IsDir() { // #nosec G703 -- htmlPath is checked by contained() to stay under the export directory
					setPolicy(w, r, rel)
					http.ServeFile(w, r, htmlPath) // #nosec G703 -- htmlPath is checked by contained() to stay under the export directory
					return
				}
			}
		}
		switch {
		case p == "/" || p == "/index.html":
			setPolicy(w, r, "index.html")
		case strings.HasSuffix(p, ".html"):
			setPolicy(w, r, strings.TrimPrefix(path.Clean("/"+p), "/"))
		}
		fileServer.ServeHTTP(w, r)
	})
}

// contained reports whether file, once resolved, is inside root.
func contained(root, file string) bool {
	abs, err := filepath.Abs(file)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
