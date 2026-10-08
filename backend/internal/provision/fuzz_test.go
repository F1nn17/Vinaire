package provision

import (
	"strings"
	"testing"
)

// Roadmap Phase 50: what a user types ends up in a shell script that runs as root on
// another machine, so these validators are fuzzed against that one property.

const shellDanger = "'\"\\$`;&|<>(){}[]*?!#~ \t\r\n"

// unquote undoes shQuote the way a POSIX shell would (single quotes, and \' outside them).
func unquote(s string) (string, bool) {
	var out strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case in && c == '\'':
			in = false
		case in:
			out.WriteByte(c)
		case c == '\'':
			in = true
		case c == '\\' && i+1 < len(s) && s[i+1] == '\'':
			out.WriteByte('\'')
			i++
		default:
			return "", false // anything else outside quotes would be interpreted by the shell
		}
	}
	return out.String(), !in
}

func FuzzShQuote(f *testing.F) {
	for _, s := range []string{"", "plain", "it's", "'; rm -rf /; '", "a\nb", "$(id)", "`id`", "\\'", "''"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := unquote(shQuote(s))
		if !ok || got != s {
			t.Fatalf("shQuote(%q) = %q does not round-trip (%q, %v)", s, shQuote(s), got, ok)
		}
	})
}

func FuzzProvisionValidators(f *testing.F) {
	for _, s := range []string{"node-1", "v1.35.5+k3s1", "https://10.0.0.1:6443", "K10abc::server:xyz", "x; reboot", "a b", "$(id)", "", "../../etc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if ValidateNodeName(s) == nil && strings.ContainsAny(s, shellDanger) {
			t.Fatalf("node name %q accepted", s)
		}
		if ValidateVersion(s) == nil && strings.ContainsAny(s, strings.NewReplacer("+", "").Replace(shellDanger)) {
			t.Fatalf("version %q accepted", s)
		}
		if ValidateServerURL(s) == nil && (strings.ContainsAny(s, "'\"\\$`;&|<>(){}!# \t\r\n") || !strings.HasPrefix(s, "https://")) {
			t.Fatalf("server URL %q accepted", s)
		}
		if ValidateToken(Secret(s)) == nil && strings.ContainsAny(s, shellDanger) {
			t.Fatalf("a token with shell characters was accepted")
		}
		k := Kubeadm{}
		if ep, err := k.JoinEndpoint("", Secret(s)); err == nil {
			if !strings.HasPrefix(ep, "https://") || strings.ContainsAny(ep, "'\"\\$`;&|<>(){}!# \t\r\n") {
				t.Fatalf("join endpoint %q from %q", ep, s)
			}
		}
	})
}
