package keycloak

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var regexpPlain = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Roadmap Phase 99: which theme does the sign-in page serve, and switching the realm to the Vinaire one.

const stockPage = `<html><head>
<link rel="stylesheet" href="/resources/abc12/common/keycloak/vendor/patternfly-v5/patternfly.min.css">
<link rel="stylesheet" href="/resources/abc12/login/keycloak.v2/css/styles.css">
</head><body>Sign in</body></html>`

const vinairePage = `<html><head>
<link rel="stylesheet" href="/resources/abc12/login/keycloak.v2/css/styles.css">
<link rel="stylesheet" href="/resources/abc12/login/vinaire/css/vinaire.css">
</head></html>`

func TestTheThemeIsReadFromTheResourcePathsOfThePage(t *testing.T) {
	if got := strings.Join(ThemeNames([]byte(stockPage)), ","); got != "keycloak.v2" {
		t.Errorf("stock page: %q", got)
	}
	if got := strings.Join(ThemeNames([]byte(vinairePage)), ","); got != "keycloak.v2,vinaire" {
		t.Errorf("themed page: %q", got)
	}
	if got := ThemeNames([]byte("no theme here /resources/x/account/foo/")); len(got) != 0 {
		t.Errorf("a path that is not a login theme: %v", got)
	}
}

func TestAThemeNameThatIsNotAPlainNameIsNeverReturned(t *testing.T) {
	for _, evil := range []string{
		`/resources/a/login/<script>/x/`, `/resources/a/login/..%2f../x/`, `/resources/a/login/a b/x/`,
		`/resources/a/login/` + strings.Repeat("n", 80) + `/x/`, `/resources/../login/x/y`,
	} {
		for _, n := range ThemeNames([]byte(evil)) {
			if !regexpPlain.MatchString(n) || len(n) > 64 {
				t.Errorf("%q gave %q", evil, n)
			}
		}
	}
}

func fakePage(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/vinaire/protocol/openid-connect/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("code_challenge_method") != "S256" || r.URL.Query().Get("client_id") != "vinaire" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "AUTH_SESSION_ID", Value: "x", Path: "/"})
		http.Redirect(w, r, "/realms/vinaire/login-actions/authenticate", http.StatusFound)
	})
	mux.HandleFunc("/realms/vinaire/login-actions/authenticate", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("AUTH_SESSION_ID"); err != nil {
			w.WriteHeader(http.StatusBadRequest) // the page needs the cookie the first answer set
			return
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTheSignInPageIsLoadedLikeAVisitorAndItsThemeReported(t *testing.T) {
	srv := fakePage(t, stockPage, 200)
	got, err := CheckPage(context.Background(), srv.Client(), srv.URL+"/realms/vinaire", "vinaire", "https://v/cb")
	if err != nil || got.Active || len(got.Themes) != 1 || got.Themes[0] != "keycloak.v2" {
		t.Fatalf("%+v %v", got, err)
	}
	srv2 := fakePage(t, vinairePage, 200)
	got, err = CheckPage(context.Background(), srv2.Client(), srv2.URL+"/realms/vinaire", "vinaire", "https://v/cb")
	if err != nil || !got.Active {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestACheckThatCannotReadAThemeSaysSoInsteadOfGuessing(t *testing.T) {
	for name, tc := range map[string]struct {
		body   string
		status int
	}{"an error page": {"Not found", 404}, "a page without a theme": {"<html>hello</html>", 200}} {
		srv := fakePage(t, tc.body, tc.status)
		if got, err := CheckPage(context.Background(), srv.Client(), srv.URL+"/realms/vinaire", "vinaire", "https://v/cb"); err == nil {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if _, err := CheckPage(context.Background(), http.DefaultClient, "ftp://nope", "vinaire", "x"); err == nil {
		t.Error("a bad issuer was accepted")
	}
}

func TestTheRealmIsSwitchedWithTheAdministratorsLoginAndNothingElseIsTouched(t *testing.T) {
	f, srv := newFake(t)
	f.themes = []string{"keycloak", "vinaire"}
	f.realms["vinaire"] = true
	f.realmRep["vinaire"] = map[string]any{"realm": "vinaire", "displayName": "mine"}
	status, err := ApplyLoginTheme(context.Background(), srv.Client(), srv.URL, "master", "admin", NewSecret("s3cret-admin"), "vinaire")
	if err != nil || status != "updated" {
		t.Fatalf("%q %v", status, err)
	}
	if f.realmRep["vinaire"]["loginTheme"] != "vinaire" || f.realmRep["vinaire"]["displayName"] != "mine" || len(f.clients) != 0 || len(f.users) != 0 {
		t.Fatalf("something else was changed: %v clients=%v users=%v", f.realmRep["vinaire"], f.clients, f.users)
	}
	if status, _ = ApplyLoginTheme(context.Background(), srv.Client(), srv.URL, "master", "admin", NewSecret("s3cret-admin"), "vinaire"); status != "reused" {
		t.Errorf("a second run: %q", status)
	}
	// a Keycloak without the theme: skipped, and the realm is left alone
	f2, srv2 := newFake(t)
	f2.themes = []string{"keycloak"}
	f2.realmRep["vinaire"] = map[string]any{"realm": "vinaire"}
	if status, _ := ApplyLoginTheme(context.Background(), srv2.Client(), srv2.URL, "master", "admin", NewSecret("s3cret-admin"), "vinaire"); !strings.HasPrefix(status, "skipped") || f2.realmRep["vinaire"]["loginTheme"] != nil {
		t.Errorf("without the theme: %q %v", status, f2.realmRep["vinaire"])
	}
}

func TestTheAdministratorsLoginIsValidatedAndRefusedWhenWrong(t *testing.T) {
	_, srv := newFake(t)
	ctx := context.Background()
	if _, err := ApplyLoginTheme(ctx, srv.Client(), srv.URL, "master", "admin", NewSecret("wrong"), "vinaire"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("wrong password: %v", err)
	}
	for _, bad := range []struct{ base, realm, user, pass string }{
		{"ftp://x", "vinaire", "admin", "p"}, {srv.URL, "bad realm!", "admin", "p"}, {srv.URL, "vinaire", "", "p"}, {srv.URL, "vinaire", "admin", ""},
	} {
		if _, err := ApplyLoginTheme(ctx, srv.Client(), bad.base, "master", bad.user, NewSecret(bad.pass), bad.realm); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}

// What the extractor must never return: anything that is not a plain theme name.
func FuzzThemeNames(f *testing.F) {
	for _, s := range []string{stockPage, vinairePage, "", "/resources/a/login/b/", "/resources/a/login/../c/", "<script>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, page string) {
		for _, n := range ThemeNames([]byte(page)) {
			if n == "" || len(n) > 64 || !regexpPlain.MatchString(n) {
				t.Fatalf("%q came out as %q", page, n)
			}
		}
	})
}
