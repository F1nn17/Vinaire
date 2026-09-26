package authn

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// tokenExpiry reads the "exp" claim out of a JWT's payload without
// verifying its signature — Vinaire is not this token's issuer or
// verifier (the Kubernetes API server is, on every actual API call made
// with it); this is only used to size a token-mode session's TTL to match
// the pasted token's own lifetime, per Roadmap Phase 10.
func tokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("not a JWT (expected 3 dot-separated parts, got %d)", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse JWT claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf(`JWT has no "exp" claim`)
	}
	return time.Unix(claims.Exp, 0), nil
}
