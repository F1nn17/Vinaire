package keycloak

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

// Which sign-in theme does this Keycloak really serve, and switching the realm to the Vinaire one (Roadmap Phase 99).
//
// The first needs no password: a realm's login page is public, and the stylesheets it links are served from
// /resources/<version>/login/<theme>/…, so the name of every theme the page uses is in its own HTML. The second needs the
// administrator's password, once, as the setup does.

// themePathRe finds a theme's name in a resource path. Only a plain name is ever returned.
var themePathRe = regexp.MustCompile(`/resources/[A-Za-z0-9._-]+/login/([A-Za-z0-9._-]{1,64})/`)

// maxPage bounds the page that is read.
const maxPage = 512 << 10

// ThemeNames lists, in order and once each, the login themes a Keycloak page links resources of. Pure: it is given
// whatever the server sent and returns only names made of letters, digits, '.', '_' and '-'.
func ThemeNames(page []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range themePathRe.FindAllSubmatch(page, 200) {
		n := string(m[1])
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// PageTheme is what a realm's sign-in page serves.
type PageTheme struct {
	// Themes: every login theme the page links resources of (a child theme and the parent it extends).
	Themes []string `json:"themes"`
	// Active: the Vinaire theme is one of them.
	Active bool `json:"active"`
}

// CheckPage loads the realm's sign-in page (as an anonymous visitor would) through hc — the guarded client — and
// reports which themes it uses. issuer is the realm's URL, redirectURL the one registered for the client.
func CheckPage(ctx context.Context, hc *http.Client, issuer, clientID, redirectURL string) (PageTheme, error) {
	if _, err := url.Parse(issuer); err != nil || !strings.HasPrefix(issuer, "http") {
		return PageTheme{}, errors.New("the saved issuer is not a URL")
	}
	q := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirectURL}, "response_type": {"code"}, "scope": {"openid"},
		// the client requires PKCE: any well-formed challenge gets the page drawn (it is never completed)
		"code_challenge": {"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"}, "code_challenge_method": {"S256"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/protocol/openid-connect/auth?"+q.Encode(), nil)
	if err != nil {
		return PageTheme{}, err
	}
	jar, _ := cookiejar.New(nil) // the page sets cookies and redirects to the form with them
	c := *hc
	c.Jar = jar
	first := req.URL.Host
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("too many redirects")
		}
		if r.URL.Host != first { // never follow Keycloak's page to another host
			return http.ErrUseLastResponse
		}
		return nil
	}
	resp, err := c.Do(req)
	if err != nil {
		return PageTheme{}, fmt.Errorf("reach Keycloak: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if resp.StatusCode != http.StatusOK {
		return PageTheme{}, fmt.Errorf("Keycloak answered %d for the sign-in page", resp.StatusCode)
	}
	names := ThemeNames(body)
	if len(names) == 0 {
		return PageTheme{}, errors.New("the sign-in page names no theme (it may not be Keycloak's login page)")
	}
	pt := PageTheme{Themes: names}
	for _, n := range names {
		if n == LoginThemeName {
			pt.Active = true
		}
	}
	return pt, nil
}

// ApplyLoginTheme switches the realm to the Vinaire theme (and the languages), as the setup would, with the
// administrator's login — and does nothing else: no client, no user, no settings of Vinaire's. The status is
// "updated", "reused" or "skipped — …" when this Keycloak doesn't have the theme installed.
func ApplyLoginTheme(ctx context.Context, hc *http.Client, baseURL, adminRealm, adminUser string, adminPass Secret, realm string) (string, error) {
	p := Params{BaseURL: baseURL, AdminRealm: adminRealm, AdminUser: adminUser, AdminPass: adminPass, Realm: realm}
	u, err := url.Parse(p.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: the Keycloak URL must be http(s)://host[:port][/path]", ErrInvalid)
	}
	for field, v := range map[string]string{"admin realm": p.AdminRealm, "realm": p.Realm} {
		if !nameRe.MatchString(v) {
			return "", fmt.Errorf("%w: %s must be letters, digits, '.', '_' or '-' (up to 63)", ErrInvalid, field)
		}
	}
	if p.AdminUser == "" || len(p.AdminUser) > 128 || !p.AdminPass.IsSet() {
		return "", fmt.Errorf("%w: the administrator's user name and password are needed", ErrInvalid)
	}
	c := &client{hc: hc, base: strings.TrimRight(p.BaseURL, "/")}
	tok, err := c.adminToken(ctx, p)
	if err != nil {
		return "", err
	}
	c.token = tok
	return c.ensureLoginTheme(ctx, "/admin/realms/"+url.PathEscape(p.Realm))
}
