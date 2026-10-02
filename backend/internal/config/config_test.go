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
