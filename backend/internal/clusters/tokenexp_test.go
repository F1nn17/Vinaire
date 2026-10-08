package clusters

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

func jwtWithExp(exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	claims := "{}"
	if exp != 0 {
		claims = fmt.Sprintf(`{"exp":%d}`, exp)
	}
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(claims)) + ".sig"
}

func TestCredentialHealth(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"no token (a certificate)", "", CredOK},
		{"a token that never expires", jwtWithExp(0), CredNoExpiry},
		{"not a JWT at all", "opaque-token", CredNoExpiry},
		{"already expired", jwtWithExp(now.Unix() - 60), CredExpired},
		{"expiring tomorrow", jwtWithExp(now.Add(24 * time.Hour).Unix()), CredExpiring},
		{"good for a month", jwtWithExp(now.Add(30 * 24 * time.Hour).Unix()), CredOK},
	}
	for _, c := range cases {
		if got, _ := CredentialHealth(c.token, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
