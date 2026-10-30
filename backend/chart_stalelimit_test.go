package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/f1nn17/vinaire/backend/internal/config"
)

// Roadmap Phase 114: a value Helm accepts for sessions.staleLimit must be one the backend accepts too — otherwise the install
// succeeds and the Deployment crash-loops on "SESSION_STALE_LIMIT=5s is shorter than 10s". The schema may be stricter than the
// backend (it takes only plain seconds, minutes and hours), never looser.
var staleLimitCases = []struct {
	value       string
	helmAccepts bool
}{
	{"", true}, {"0", true}, {"10s", true}, {"90s", true}, {"1m", true}, {"5m", true}, {"1h", true}, {"1m30s", true}, {"1h30m", true},
	{"5s", false}, {"9s", false}, {"0s", false}, {"1s", false}, {"60sec", false}, {"1 minute", false}, {"abc", false}, {"-1m", false},
	{"500ms", false}, {"1.5m", false}, {"01m", false},
}

func staleLimitPattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	raw, err := os.ReadFile("../charts/vinaire/values.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Sessions struct {
				Properties struct {
					StaleLimit struct {
						Pattern string `json:"pattern"`
					} `json:"staleLimit"`
				} `json:"properties"`
			} `json:"sessions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(schema.Properties.Sessions.Properties.StaleLimit.Pattern)
}

func TestTheSchemaNeverAcceptsAStaleLimitTheBackendRefuses(t *testing.T) {
	re := staleLimitPattern(t)
	for _, c := range staleLimitCases {
		if got := re.MatchString(c.value); got != c.helmAccepts {
			t.Errorf("schema on %q: accepts=%v, want %v", c.value, got, c.helmAccepts)
		}
		if re.MatchString(c.value) {
			if _, err := config.ParseStaleLimit(c.value); err != nil {
				t.Errorf("the schema accepts %q but the backend refuses it: %v", c.value, err)
			}
		}
	}
}

// The same, through helm itself (the real gate: it must stop before anything reaches the cluster). Skipped without helm.
func TestHelmRefusesAStaleLimitTheBackendWouldRefuse(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is not installed")
	}
	for _, c := range staleLimitCases {
		args := []string{"template", "v", "../charts/vinaire", "-n", "v", "--set", "sessions.store=secret", "--set-string", "sessions.staleLimit=" + c.value}
		out, err := exec.Command("helm", args...).CombinedOutput()
		if (err == nil) != c.helmAccepts {
			t.Errorf("helm on staleLimit=%q: accepted=%v, want %v: %s", c.value, err == nil, c.helmAccepts, out)
		}
		if err == nil {
			if _, perr := config.ParseStaleLimit(c.value); perr != nil {
				t.Errorf("helm accepts %q but the backend refuses it: %v", c.value, perr)
			}
		}
	}
}
