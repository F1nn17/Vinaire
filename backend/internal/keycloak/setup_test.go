package keycloak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeKeycloak is just enough of the Admin REST API: realms, clients (with a
// secret), users — and it keeps state, so running the setup twice can be checked.
type fakeKeycloak struct {
	mu       sync.Mutex
	realms   map[string]bool
	clients  map[string]map[string]any // clientId -> representation
	users    map[string]bool
	themes   []string                  // login themes the server lists
	realmRep map[string]map[string]any // realm representations
	gotToken string
	bodies   []string // every request body, to prove no password leaks beyond the user's own credential
}

func newFake(t *testing.T) (*fakeKeycloak, *httptest.Server) {
	f := &fakeKeycloak{realms: map[string]bool{"master": true}, clients: map[string]map[string]any{}, users: map[string]bool{}, realmRep: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		raw := new(strings.Builder)
		_, _ = fmt.Fprint(raw, r.Method, " ", r.URL.RequestURI())
		if r.URL.Path == "/realms/master/protocol/openid-connect/token" {
			_ = r.ParseForm()
			if r.Form.Get("username") != "admin" || r.Form.Get("password") != "s3cret-admin" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"admin-token"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == "POST" && r.URL.Path == "/admin/realms":
			name := body["realm"].(string)
			if f.realms[name] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.realms[name] = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == "GET" && r.URL.Path == "/admin/serverinfo":
			var list []map[string]string
			for _, n := range f.themes {
				list = append(list, map[string]string{"name": n})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"themes": map[string]any{"login": list}})
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/admin/realms/") && strings.Count(r.URL.Path, "/") == 3:
			name := strings.TrimPrefix(r.URL.Path, "/admin/realms/")
			rep := f.realmRep[name]
			if rep == nil {
				rep = map[string]any{"realm": name, "enabled": true}
			}
			_ = json.NewEncoder(w).Encode(rep)
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/admin/realms/") && strings.Count(r.URL.Path, "/") == 3:
			f.realmRep[strings.TrimPrefix(r.URL.Path, "/admin/realms/")] = body
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/clients"):
			var out []map[string]any
			if c, ok := f.clients[r.URL.Query().Get("clientId")]; ok {
				out = append(out, c)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/clients"):
			id := "id-" + body["clientId"].(string)
			body["id"] = id
			f.clients[body["clientId"].(string)] = body
			w.Header().Set("Location", r.URL.Path+"/"+id)
			w.WriteHeader(http.StatusCreated)
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/clients/") && strings.HasSuffix(r.URL.Path, "/client-secret"):
			_, _ = w.Write([]byte(`{"type":"secret","value":"the-client-secret"}`))
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/clients/"):
			for _, c := range f.clients {
				if strings.HasSuffix(r.URL.Path, "/"+c["id"].(string)) {
					_ = json.NewEncoder(w).Encode(c)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "PUT" && strings.Contains(r.URL.Path, "/clients/"):
			f.clients[body["clientId"].(string)] = body
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/users"):
			name := body["username"].(string)
			if f.users[name] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.users[name] = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func params(base string) Params {
	return Params{
		BaseURL: base, AdminRealm: "master", AdminUser: "admin", AdminPass: NewSecret("s3cret-admin"),
		Realm: "vinaire", ClientID: "vinaire", RedirectURL: "https://vinaire.example.com/api/v1/auth/oidc/callback",
		Origin: "https://vinaire.example.com",
		User:   "alice", UserEmail: "alice@example.com", UserPass: NewSecret("alice-password"), UserMustChangePassword: true,
	}
}

func TestSetupCreatesRealmClientAndUserAndIsRepeatable(t *testing.T) {
	f, srv := newFake(t)
	res, err := Setup(context.Background(), srv.Client(), params(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if res.IssuerURL != srv.URL+"/realms/vinaire" || res.ClientID != "vinaire" || res.ClientSecret.Reveal() != "the-client-secret" {
		t.Errorf("result: %+v", res)
	}
	want := []string{"created", "created", "created"}
	for i, s := range res.Steps {
		if s.Status != want[i] {
			t.Errorf("step %d: %+v", i, s)
		}
	}
	c := f.clients["vinaire"]
	if c["publicClient"] != false || c["standardFlowEnabled"] != true || c["directAccessGrantsEnabled"] != false {
		t.Errorf("client must be confidential, code flow only: %+v", c)
	}

	// again: everything is reused, nothing breaks
	res, err = Setup(context.Background(), srv.Client(), params(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprint(res.Steps)
	if !strings.Contains(got, "reused") || !strings.Contains(got, "already exists") {
		t.Errorf("second run: %s", got)
	}
}

func TestSetupAddsOurRedirectToAnExistingClientAndRefusesAPublicOne(t *testing.T) {
	f, srv := newFake(t)
	f.clients["vinaire"] = map[string]any{"id": "id-vinaire", "clientId": "vinaire", "publicClient": false,
		"redirectUris": []any{"https://other/callback"}, "webOrigins": []any{}}
	res, err := Setup(context.Background(), srv.Client(), params(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(res.Steps), "updated") {
		t.Errorf("steps: %v", res.Steps)
	}
	uris := f.clients["vinaire"]["redirectUris"].([]any)
	if len(uris) != 2 {
		t.Errorf("redirect URIs: %v (the administrator's own entry must stay)", uris)
	}

	f.clients["vinaire"]["publicClient"] = true
	if _, err := Setup(context.Background(), srv.Client(), params(srv.URL)); err == nil || !strings.Contains(err.Error(), "public client") {
		t.Errorf("a public client must be refused, got %v", err)
	}
}

func TestSetupWrongAdminPasswordAndValidation(t *testing.T) {
	_, srv := newFake(t)
	p := params(srv.URL)
	p.AdminPass = NewSecret("wrong")
	if _, err := Setup(context.Background(), srv.Client(), p); !errors.Is(err, ErrAuth) {
		t.Errorf("wrong password: %v", err)
	}
	for name, mod := range map[string]func(*Params){
		"realm with a slash":   func(p *Params) { p.Realm = "../master" },
		"client with a space":  func(p *Params) { p.ClientID = "a b" },
		"url with credentials": func(p *Params) { p.BaseURL = "https://u:p@kc.example.com" },
		"url with a query":     func(p *Params) { p.BaseURL = "https://kc.example.com?x=1" },
		"wildcard redirect":    func(p *Params) { p.RedirectURL = "https://*/cb" },
		"file scheme":          func(p *Params) { p.BaseURL = "file:///etc/passwd" },
		"short user password":  func(p *Params) { p.UserPass = NewSecret("short") },
		"user without email":   func(p *Params) { p.UserEmail = "" },
		"user name injection":  func(p *Params) { p.User = `a","admin":"true` },
		"no admin password":    func(p *Params) { p.AdminPass = NewSecret("") },
	} {
		p := params(srv.URL)
		mod(&p)
		if err := p.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}

// A credential must print as [REDACTED] however it is formatted.
func TestSecretNeverPrints(t *testing.T) {
	p := params("https://kc.example.com")
	for _, out := range []string{fmt.Sprint(p), fmt.Sprintf("%v %+v %#v %s", p, p, p, p.AdminPass)} {
		if strings.Contains(out, "s3cret-admin") || strings.Contains(out, "alice-password") {
			t.Errorf("a credential leaked: %s", out)
		}
	}
	j, _ := json.Marshal(p)
	if strings.Contains(string(j), "s3cret-admin") || strings.Contains(string(j), "alice-password") {
		t.Errorf("a credential leaked into JSON: %s", j)
	}
}

// Roadmap Phase 85: the password grant exists on the client only while Vinaire's own sign-in form is meant to be on.
func TestDirectAccessIsOffByDefaultOnWhenAskedAndOffAgain(t *testing.T) {
	f, srv := newFake(t)
	ctx := context.Background()
	if _, err := Setup(ctx, srv.Client(), params(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if f.clients["vinaire"]["directAccessGrantsEnabled"] != false {
		t.Fatal("a new client must not allow the password grant unless asked")
	}
	on := params(srv.URL)
	on.DirectAccess = true
	res, err := Setup(ctx, srv.Client(), on)
	if err != nil {
		t.Fatal(err)
	}
	if f.clients["vinaire"]["directAccessGrantsEnabled"] != true || !strings.Contains(fmt.Sprint(res.Steps), "updated") {
		t.Fatalf("not switched on: %v %v", f.clients["vinaire"]["directAccessGrantsEnabled"], res.Steps)
	}
	// setting up again with the form off closes the grant again — it must not stay open by accident
	if _, err := Setup(ctx, srv.Client(), params(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if f.clients["vinaire"]["directAccessGrantsEnabled"] != false {
		t.Fatal("the password grant stayed on after the form was turned off")
	}
	// a fresh client made with the option on has it from the start
	f2, srv2 := newFake(t)
	on2 := params(srv2.URL)
	on2.DirectAccess = true
	if _, err := Setup(ctx, srv2.Client(), on2); err != nil {
		t.Fatal(err)
	}
	if f2.clients["vinaire"]["directAccessGrantsEnabled"] != true {
		t.Fatal("a new client made with the form on lacks the grant")
	}
}

// Roadmap Phase 85b: the realm uses the Vinaire login theme only when asked AND when Keycloak has the theme installed.
func TestLoginThemeIsSetOnlyWhenAskedAndInstalled(t *testing.T) {
	ctx := context.Background()
	f, srv := newFake(t)
	f.themes = []string{"keycloak", "keycloak.v2"}
	asked := params(srv.URL)
	asked.LoginTheme = true
	res, err := Setup(ctx, srv.Client(), asked)
	if err != nil {
		t.Fatal(err)
	}
	if f.realmRep["vinaire"]["loginTheme"] != nil || !strings.Contains(fmt.Sprint(res.Steps), "skipped — the theme isn't installed") {
		t.Fatalf("the realm was changed although the theme isn't installed: %v %v", f.realmRep["vinaire"], res.Steps)
	}
	f.themes = append(f.themes, "vinaire")
	res, err = Setup(ctx, srv.Client(), asked)
	if err != nil {
		t.Fatal(err)
	}
	rep := f.realmRep["vinaire"]
	if rep["loginTheme"] != "vinaire" || rep["internationalizationEnabled"] != true || !strings.Contains(fmt.Sprint(res.Steps), "Login theme vinaire updated") {
		t.Fatalf("theme not applied: %v %v", rep, res.Steps)
	}
	if res, _ = Setup(ctx, srv.Client(), asked); !strings.Contains(fmt.Sprint(res.Steps), "Login theme vinaire reused") {
		t.Errorf("a second run should change nothing: %v", res.Steps)
	}
	// not asked: an administrator's own choice of theme is never touched
	f2, srv2 := newFake(t)
	f2.themes = []string{"vinaire"}
	f2.realmRep["vinaire"] = map[string]any{"loginTheme": "mine"}
	if _, err := Setup(ctx, srv2.Client(), params(srv2.URL)); err != nil {
		t.Fatal(err)
	}
	if f2.realmRep["vinaire"]["loginTheme"] != "mine" {
		t.Fatal("the realm's theme was changed without being asked")
	}
	// the administrator's own language list is kept
	f3, srv3 := newFake(t)
	f3.themes = []string{"vinaire"}
	f3.realmRep["vinaire"] = map[string]any{"internationalizationEnabled": true, "supportedLocales": []any{"de"}}
	if _, err := Setup(ctx, srv3.Client(), asked2(srv3.URL)); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(f3.realmRep["vinaire"]["supportedLocales"]); got != "[de]" {
		t.Fatalf("languages overwritten: %s", got)
	}
}

func asked2(base string) Params {
	p := params(base)
	p.LoginTheme = true
	return p
}
