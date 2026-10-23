package config

import "testing"

func TestValidateCORSOrigins(t *testing.T) {
	for _, ok := range [][]string{nil, {"https://vinaire.example.com"}, {"http://localhost:3100", "https://a.example:8443/"}} {
		if err := ValidateCORSOrigins(ok); err != nil {
			t.Errorf("%v rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"*", "https://*.example.com", "vinaire.example.com", "ftp://x", "https://", "https://x.example/path", "https://x.example?q=1", "javascript:alert(1)"} {
		if err := ValidateCORSOrigins([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFrontendURLDefaultsToTheSameOriginWhenThisProcessServesTheUI(t *testing.T) {
	if got := defaultFrontendURLFor("/app/web"); got != "/" {
		t.Errorf("with STATIC_DIR set the browser must stay on this origin, got %q", got)
	}
	if got := defaultFrontendURLFor("  "); got != defaultFrontendURL {
		t.Errorf("a blank STATIC_DIR is no STATIC_DIR: got %q", got)
	}
	if got := defaultFrontendURLFor(""); got != "http://localhost:3100" {
		t.Errorf("the dev default changed: %q", got)
	}
}

func TestFrontendURLFromTheEnvironmentWins(t *testing.T) {
	t.Setenv("STATIC_DIR", "/app/web")
	t.Setenv("FRONTEND_URL", "https://vinaire.example.com")
	if got := Load().FrontendURL; got != "https://vinaire.example.com" {
		t.Errorf("got %q", got)
	}
	t.Setenv("FRONTEND_URL", "")
	if got := Load().FrontendURL; got != "/" {
		t.Errorf("an empty FRONTEND_URL with STATIC_DIR set should be %q, got %q", "/", got)
	}
}
