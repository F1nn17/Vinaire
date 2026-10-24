// Package keycloak sets up Vinaire's sign-in through Keycloak (Roadmap Phase 47):
// with a Keycloak administrator's credentials it creates a realm, a confidential
// OIDC client for Vinaire and, optionally, a first user through Keycloak's Admin
// REST API, and hands back what Vinaire needs to talk OIDC to it.
//
// The administrator's password is used for one token request and never stored,
// logged or returned (Secret prints [REDACTED]); the client secret that comes back
// is the one thing that is kept, and only sealed (see internal/api/keycloak.go).
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Secret holds a credential so it can't reach a log or a response by accident.
type Secret struct{ v string }

func NewSecret(v string) Secret             { return Secret{v: v} }
func (s Secret) Reveal() string             { return s.v }
func (s Secret) IsSet() bool                { return s.v != "" }
func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }

// Params is one setup request.
type Params struct {
	BaseURL    string // the Keycloak server, e.g. https://keycloak.example.com
	AdminRealm string // the realm the admin logs into; "master" by default
	AdminUser  string
	AdminPass  Secret

	Realm       string // the realm Vinaire's users live in; created if missing
	ClientID    string // the OIDC client for Vinaire
	RedirectURL string // Vinaire's /api/v1/auth/oidc/callback
	Origin      string // Vinaire's own origin (a CORS/web-origin entry)

	// LoginTheme asks for the realm to use the "vinaire" login theme (Roadmap Phase 85b): Keycloak's own sign-in page in
	// Vinaire's look, with English and Russian. The theme itself has to be installed on the Keycloak (the wizard hands
	// it out); if Keycloak doesn't list it, the step is skipped — never an error — and the realm is left alone.
	LoginTheme bool

	// An optional first user.
	User      string
	UserEmail string
	UserPass  Secret
	// UserMustChangePassword makes the password temporary.
	UserMustChangePassword bool
}

// Result is what a successful setup produced.
type Result struct {
	IssuerURL    string
	ClientID     string
	ClientSecret Secret
	Steps        []Step
}

// Step is one thing the setup did, for the audit trail and the wizard.
type Step struct {
	What   string `json:"what"`
	Status string `json:"status"` // "created", "reused", "updated", "skipped"
}

var (
	ErrInvalid = errors.New("invalid setup request")
	// ErrAuth means Keycloak refused the administrator's login.
	ErrAuth = errors.New("keycloak refused the administrator's credentials")

	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	userRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)
	emailRe = regexp.MustCompile(`^[^@\s]{1,64}@[^@\s]{1,255}$`)
)

// Validate checks every field that ends up in a URL path, a JSON body or a form,
// so none of them can reach Keycloak as anything but the plain value it should be.
func (p Params) Validate() error {
	u, err := url.Parse(p.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: the Keycloak URL must be http(s)://host[:port][/path] with no credentials or query", ErrInvalid)
	}
	for field, v := range map[string]string{"admin realm": p.AdminRealm, "realm": p.Realm, "client id": p.ClientID} {
		if !nameRe.MatchString(v) {
			return fmt.Errorf("%w: %s must be letters, digits, '.', '_' or '-' (up to 63)", ErrInvalid, field)
		}
	}
	if p.AdminUser == "" || len(p.AdminUser) > 128 || !p.AdminPass.IsSet() {
		return fmt.Errorf("%w: the administrator's user name and password are needed", ErrInvalid)
	}
	for field, v := range map[string]string{"redirect URL": p.RedirectURL, "origin": p.Origin} {
		ru, err := url.Parse(v)
		if err != nil || (ru.Scheme != "https" && ru.Scheme != "http") || ru.Host == "" || ru.User != nil || ru.Fragment != "" || strings.ContainsAny(v, "*") {
			return fmt.Errorf("%w: the %s must be a plain http(s) URL (no wildcard)", ErrInvalid, field)
		}
	}
	if p.User != "" {
		if !userRe.MatchString(p.User) {
			return fmt.Errorf("%w: the user name must be letters, digits, '.', '_', '@' or '-'", ErrInvalid)
		}
		if !emailRe.MatchString(p.UserEmail) {
			return fmt.Errorf("%w: the first user needs an e-mail address", ErrInvalid)
		}
		if len(p.UserPass.Reveal()) < 8 || len(p.UserPass.Reveal()) > 128 {
			return fmt.Errorf("%w: the first user's password must be 8–128 characters", ErrInvalid)
		}
	}
	return nil
}

// Setup runs the steps against Keycloak through hc (the caller passes a guarded
// client). Every step is idempotent: an existing realm, client or user is reused,
// so running it twice is safe and it can repair a half-finished attempt.
func Setup(ctx context.Context, hc *http.Client, p Params) (Result, error) {
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	c := &client{hc: hc, base: strings.TrimRight(p.BaseURL, "/")}
	var res Result

	tok, err := c.adminToken(ctx, p)
	if err != nil {
		return res, err
	}
	c.token = tok

	// realm
	status, _, err := c.do(ctx, http.MethodPost, "/admin/realms", map[string]any{
		"realm": p.Realm, "enabled": true, "displayName": "Vinaire",
	})
	switch {
	case err != nil:
		return res, fmt.Errorf("create realm: %w", err)
	case status == http.StatusConflict:
		res.Steps = append(res.Steps, Step{"Realm " + p.Realm, "reused"})
	default:
		res.Steps = append(res.Steps, Step{"Realm " + p.Realm, "created"})
	}

	// client
	realmPath := "/admin/realms/" + url.PathEscape(p.Realm)
	id, existed, err := c.findClient(ctx, realmPath, p.ClientID)
	if err != nil {
		return res, fmt.Errorf("look for the client: %w", err)
	}
	if !existed {
		status, hdr, err := c.do(ctx, http.MethodPost, realmPath+"/clients", map[string]any{
			"clientId": p.ClientID, "name": "Vinaire", "enabled": true, "protocol": "openid-connect",
			"publicClient": false, "standardFlowEnabled": true, "directAccessGrantsEnabled": false,
			"serviceAccountsEnabled": false, "implicitFlowEnabled": false,
			"redirectUris": []string{p.RedirectURL}, "webOrigins": []string{p.Origin},
			"attributes": map[string]string{"pkce.code.challenge.method": "S256"},
		})
		if err != nil || status != http.StatusCreated {
			return res, fmt.Errorf("create the client: %w", orStatus(err, status))
		}
		loc := hdr.Get("Location")
		id = loc[strings.LastIndex(loc, "/")+1:]
		if id == "" {
			if id, _, err = c.findClient(ctx, realmPath, p.ClientID); err != nil || id == "" {
				return res, fmt.Errorf("the new client wasn't found afterwards")
			}
		}
		res.Steps = append(res.Steps, Step{"Client " + p.ClientID, "created"})
	} else {
		updated, err := c.ensureRedirect(ctx, realmPath, id, p.RedirectURL, p.Origin)
		if err != nil {
			return res, fmt.Errorf("update the client: %w", err)
		}
		st := "reused"
		if updated {
			st = "updated"
		}
		res.Steps = append(res.Steps, Step{"Client " + p.ClientID, st})
	}

	if p.LoginTheme {
		st, err := c.ensureLoginTheme(ctx, realmPath)
		if err != nil {
			return res, fmt.Errorf("set the login theme: %w", err)
		}
		res.Steps = append(res.Steps, Step{"Login theme " + LoginThemeName, st})
	}

	// the client's secret
	secret, err := c.clientSecret(ctx, realmPath, id)
	if err != nil {
		return res, fmt.Errorf("read the client secret: %w", err)
	}

	// first user
	if p.User != "" {
		status, _, err := c.do(ctx, http.MethodPost, realmPath+"/users", map[string]any{
			"username": p.User, "enabled": true, "email": p.UserEmail, "emailVerified": true,
			"firstName": p.User, "lastName": "Vinaire",
			"credentials": []map[string]any{{
				"type": "password", "value": p.UserPass.Reveal(), "temporary": p.UserMustChangePassword,
			}},
		})
		switch {
		case err != nil:
			return res, fmt.Errorf("create the user: %w", err)
		case status == http.StatusConflict:
			res.Steps = append(res.Steps, Step{"User " + p.User, "skipped — already exists (its password was not changed)"})
		case status != http.StatusCreated:
			return res, fmt.Errorf("create the user: keycloak answered %d", status)
		default:
			res.Steps = append(res.Steps, Step{"User " + p.User, "created"})
		}
	}

	res.IssuerURL = c.base + "/realms/" + url.PathEscape(p.Realm)
	res.ClientID = p.ClientID
	res.ClientSecret = NewSecret(secret)
	return res, nil
}

func orStatus(err error, status int) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("keycloak answered %d", status)
}

type client struct {
	hc    *http.Client
	base  string
	token string
}

// adminToken logs the administrator in with Keycloak's built-in admin-cli client.
func (c *client) adminToken(ctx context.Context, p Params) (string, error) {
	form := url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"},
		"username": {p.AdminUser}, "password": {p.AdminPass.Reveal()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/realms/"+url.PathEscape(p.AdminRealm)+"/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach Keycloak: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest:
		return "", ErrAuth
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("Keycloak answered %d to the administrator's login", resp.StatusCode)
	}
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &t) != nil || t.AccessToken == "" {
		return "", errors.New("Keycloak's answer to the administrator's login had no token")
	}
	return t.AccessToken, nil
}

// do sends a JSON request with the admin token; it returns the status and headers,
// and the body (for GETs).
func (c *client) do(ctx context.Context, method, path string, body any) (int, http.Header, error) {
	status, hdr, _, err := c.doBody(ctx, method, path, body)
	return status, hdr, err
}

func (c *client) doBody(ctx context.Context, method, path string, body any) (int, http.Header, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reach Keycloak: %w", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, resp.Header, out, errors.New("this Keycloak account isn't allowed to do that (it needs the realm-management / admin role)")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, resp.Header, out, ErrAuth
	}
	return resp.StatusCode, resp.Header, out, nil
}

func (c *client) findClient(ctx context.Context, realmPath, clientID string) (id string, existed bool, err error) {
	status, _, body, err := c.doBody(ctx, http.MethodGet, realmPath+"/clients?clientId="+url.QueryEscape(clientID), nil)
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("keycloak answered %d", status)
	}
	var list []struct {
		ID       string `json:"id"`
		ClientID string `json:"clientId"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", false, err
	}
	for _, cl := range list {
		if cl.ClientID == clientID {
			return cl.ID, true, nil
		}
	}
	return "", false, nil
}

// ensureRedirect adds Vinaire's redirect URI and origin to an existing client, if
// they aren't there. Everything else on the client is left as the administrator set it.
func (c *client) ensureRedirect(ctx context.Context, realmPath, id, redirect, origin string) (bool, error) {
	path := realmPath + "/clients/" + url.PathEscape(id)
	status, _, body, err := c.doBody(ctx, http.MethodGet, path, nil)
	if err != nil || status != http.StatusOK {
		return false, orStatus(err, status)
	}
	var rep map[string]any
	if err := json.Unmarshal(body, &rep); err != nil {
		return false, err
	}
	changed := false
	add := func(key, val string) {
		list, _ := rep[key].([]any)
		for _, x := range list {
			if s, _ := x.(string); s == val {
				return
			}
		}
		rep[key] = append(list, val)
		changed = true
	}
	add("redirectUris", redirect)
	add("webOrigins", origin)
	if rep["publicClient"] == true {
		return false, errors.New("the existing client is a public client; Vinaire needs a confidential one (pick another client id, or change it in Keycloak)")
	}
	if !changed {
		return false, nil
	}
	status, _, err = c.do(ctx, http.MethodPut, path, rep)
	if err != nil || status != http.StatusNoContent {
		return false, orStatus(err, status)
	}
	return true, nil
}

// LoginThemeName is the theme Vinaire hands out (frontend/src/lib/keycloakTheme.ts).
const LoginThemeName = "vinaire"

// ensureLoginTheme points the realm's login pages at the "vinaire" theme and turns on English and Russian — only when
// Keycloak says the theme is installed (GET /admin/serverinfo lists the themes it can load). It returns the step's
// status: "updated", "reused", or "skipped — …" when the theme isn't there (the page stays Keycloak's own).
func (c *client) ensureLoginTheme(ctx context.Context, realmPath string) (string, error) {
	status, _, body, err := c.doBody(ctx, http.MethodGet, "/admin/serverinfo", nil)
	if err != nil || status != http.StatusOK {
		return "", orStatus(err, status)
	}
	var info struct {
		Themes struct {
			Login []struct {
				Name string `json:"name"`
			} `json:"login"`
		} `json:"themes"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", err
	}
	found := false
	for _, t := range info.Themes.Login {
		if t.Name == LoginThemeName {
			found = true
		}
	}
	if !found {
		return "skipped — the theme isn't installed on this Keycloak (the pages keep Keycloak's own look)", nil
	}
	status, _, body, err = c.doBody(ctx, http.MethodGet, realmPath, nil)
	if err != nil || status != http.StatusOK {
		return "", orStatus(err, status)
	}
	var rep map[string]any
	if err := json.Unmarshal(body, &rep); err != nil {
		return "", err
	}
	changed := false
	if rep["loginTheme"] != LoginThemeName {
		rep["loginTheme"] = LoginThemeName
		changed = true
	}
	// languages: only when the administrator hasn't set up their own list
	if on, _ := rep["internationalizationEnabled"].(bool); !on {
		rep["internationalizationEnabled"] = true
		rep["supportedLocales"] = []string{"en", "ru"}
		rep["defaultLocale"] = "en"
		changed = true
	}
	if !changed {
		return "reused", nil
	}
	status, _, err = c.do(ctx, http.MethodPut, realmPath, rep)
	if err != nil || status != http.StatusNoContent {
		return "", orStatus(err, status)
	}
	return "updated", nil
}

func (c *client) clientSecret(ctx context.Context, realmPath, id string) (string, error) {
	path := realmPath + "/clients/" + url.PathEscape(id) + "/client-secret"
	status, _, body, err := c.doBody(ctx, http.MethodGet, path, nil)
	if err != nil || status != http.StatusOK {
		return "", orStatus(err, status)
	}
	var s struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(body, &s) != nil || s.Value == "" {
		// a client with no secret yet: ask Keycloak to make one
		status, _, body, err = c.doBody(ctx, http.MethodPost, path, nil)
		if err != nil || status != http.StatusOK {
			return "", orStatus(err, status)
		}
		if json.Unmarshal(body, &s) != nil || s.Value == "" {
			return "", errors.New("Keycloak returned no client secret")
		}
	}
	return s.Value, nil
}
