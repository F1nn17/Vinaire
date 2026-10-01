package provision

import (
	"strings"
)

// Roadmap Phase 30 handles SSH credentials and a cluster join token — the most
// sensitive data in the whole app. Two defenses against them leaking:
//
//   - Secret never prints its value: formatting it with %v/%+v/%#v, logging it
//     with slog, or marshaling it to JSON/text all give "[REDACTED]". Decoding
//     JSON *into* a Secret still works (it's just a string underneath).
//   - Redactor scrubs every known secret out of any text that is about to be
//     stored or streamed (remote command output, error messages).

const redacted = "[REDACTED]"

// Secret is a string that refuses to be printed.
type Secret string

func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Reveal returns the real value. Call sites using it are the only places a
// secret is allowed to leave its wrapper — keep them few and audit them.
func (s Secret) Reveal() string { return string(s) }

// Redactor replaces known secret values in text.
type Redactor struct {
	secrets []string
}

// NewRedactor collects the non-trivial secrets to scrub. Very short values are
// skipped: replacing "ab" everywhere would mangle ordinary output and protects
// nothing.
func NewRedactor(secrets ...Secret) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		r.Add(s)
	}
	return r
}

// Add registers another secret (e.g. a token learned mid-run).
func (r *Redactor) Add(s Secret) {
	v := s.Reveal()
	if len(strings.TrimSpace(v)) >= 4 {
		r.secrets = append(r.secrets, v)
	}
	// A multi-line secret (a PEM private key) can show up one line at a time.
	if strings.Contains(v, "\n") {
		for _, line := range strings.Split(v, "\n") {
			if line = strings.TrimSpace(line); len(line) >= 8 {
				r.secrets = append(r.secrets, line)
			}
		}
	}
}

// Redact returns s with every registered secret replaced.
func (r *Redactor) Redact(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, redacted)
	}
	return s
}
