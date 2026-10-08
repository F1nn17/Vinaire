package clusters

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// TokenExpiry reads the "exp" claim of a bearer token that is a JWT, without checking
// its signature (Vinaire only wants to warn the administrator before a cluster's
// credential runs out). ok is false for a token with no readable expiry — which for a
// ServiceAccount token means one that never expires.
func TokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// Credential health, shown next to each cluster the administrator added.
const (
	CredOK       = ""          // nothing to say
	CredNoExpiry = "no-expiry" // a bearer token that never expires: a leak is permanent
	CredExpiring = "expiring"  // runs out within a week
	CredExpired  = "expired"   // already refused by the cluster
)

// CredentialHealth classifies a cluster's bearer token. Clusters that authenticate with
// a client certificate (no bearer token) are not judged here.
func CredentialHealth(token string, now time.Time) (state string, expires *time.Time) {
	if token == "" {
		return CredOK, nil
	}
	exp, ok := TokenExpiry(token)
	switch {
	case !ok:
		return CredNoExpiry, nil
	case !exp.After(now):
		return CredExpired, &exp
	case exp.Before(now.Add(7 * 24 * time.Hour)):
		return CredExpiring, &exp
	}
	return CredOK, &exp
}
