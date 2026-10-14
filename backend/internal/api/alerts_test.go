package api

import (
	"testing"
	"time"
)

// Alertmanager keeps a resolved alert for a while; its endsAt is already past. The bell must not
// show it as if it were still firing.
func TestAnAlertThatHasEndedIsNotShown(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		endsAt string
		over   bool
	}{
		"ends in the future": {"2026-10-14T12:05:00Z", false},
		"ended a minute ago": {"2026-10-14T11:59:00Z", true},
		"ends right now":     {"2026-10-14T12:00:00Z", true},
		"no end given":       {"", false},
		"unreadable":         {"soon", false},
	} {
		if got := alertIsOver(alertmanagerAlert{EndsAt: c.endsAt}, now); got != c.over {
			t.Errorf("%s: %v, want %v", name, got, c.over)
		}
	}
}
