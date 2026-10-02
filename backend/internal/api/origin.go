package api

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginGuard is CSRF defence in depth for every state-changing request
// (Roadmap Phase 36), on top of the session cookie's SameSite attribute: a
// browser always sends Origin (and Sec-Fetch-Site) on a cross-site POST, so a
// request that says it comes from somewhere other than this site or an allowed
// origin is refused before it reaches a handler. Requests that carry neither
// header (curl, scripts, tests) aren't browsers and can't be a CSRF vector, so
// they pass — they still need a valid session.
//
// Safe methods are left alone. /proxy/ is exempt: a proxied pod's own page, in
// its opaque sandbox origin, legitimately POSTs to its own backend.
func OriginGuard(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range allowedOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/proxy/") {
				next.ServeHTTP(w, r)
				return
			}
			if !requestOriginOK(r, allowed) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestOriginOK(r *http.Request, allowed map[string]bool) bool {
	origin := r.Header.Get("Origin")
	site := r.Header.Get("Sec-Fetch-Site")
	if origin == "" {
		// A browser that says the request is cross-site but sends no Origin (it
		// can happen for some navigations) is refused; no header at all is a
		// non-browser client.
		return site == "" || site == "same-origin" || site == "none"
	}
	if origin == "null" {
		return false // a sandboxed or redirected context
	}
	if allowed[strings.TrimRight(origin, "/")] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == r.Host // same origin
}
