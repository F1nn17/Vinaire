package keycloak

import (
	"net/url"
	"strings"
	"testing"
)

// Roadmap Phase 50: the fields that become URL path segments, JSON and a form.
func FuzzParamsValidate(f *testing.F) {
	f.Add("https://kc.example.com", "master", "admin", "pw", "vinaire", "vinaire", "https://v.example.com/cb", "https://v.example.com", "alice", "a@b.co", "longpassword")
	f.Add("file:///etc/passwd", "../master", "a", "p", "a/b", "a?b", "https://*/cb", "x", "u", "no-at", "short")
	f.Fuzz(func(t *testing.T, base, adminRealm, admin, pass, realm, client, redirect, origin, user, email, userPass string) {
		p := Params{BaseURL: base, AdminRealm: adminRealm, AdminUser: admin, AdminPass: NewSecret(pass),
			Realm: realm, ClientID: client, RedirectURL: redirect, Origin: origin, User: user, UserEmail: email, UserPass: NewSecret(userPass)}
		if p.Validate() != nil {
			return
		}
		u, err := url.Parse(base)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			t.Fatalf("accepted base URL %q", base)
		}
		for _, seg := range []string{adminRealm, realm, client} {
			if strings.ContainsAny(seg, "/?#%\\ \t\r\n\"'") || seg == ".." || seg == "." {
				t.Fatalf("a path segment %q was accepted", seg)
			}
		}
		if strings.Contains(redirect, "*") || strings.Contains(origin, "*") {
			t.Fatal("a wildcard redirect/origin was accepted")
		}
		if user != "" && strings.ContainsAny(user, "\"'\\/ \t\r\n") {
			t.Fatalf("user name %q accepted", user)
		}
	})
}
