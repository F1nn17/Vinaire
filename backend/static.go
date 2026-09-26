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
func staticFileHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// http.FileServer already serves index.html for "/" on its own;
		// only extension-less non-root paths need the <path>.html lookup.
		if p != "/" && !strings.Contains(path.Base(p), ".") {
			htmlPath := filepath.Join(dir, filepath.FromSlash(path.Clean(p))+".html")
			if info, err := os.Stat(htmlPath); err == nil && !info.IsDir() {
				http.ServeFile(w, r, htmlPath)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
