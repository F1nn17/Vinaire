package api

import (
	"testing"
	"time"
)

func TestParseAuditTime(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	good := map[string]time.Time{
		"":                     {},
		"90m":                  now.Add(-90 * time.Minute),
		"24h":                  now.Add(-24 * time.Hour),
		"7d":                   now.Add(-7 * 24 * time.Hour),
		"2026-10-01T00:00:00Z": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range good {
		got, ok := parseAuditTime(in, now)
		if !ok || !got.Equal(want) {
			t.Errorf("%q = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"yesterday", "-5h", "7dd", "99999d", "1 hour"} {
		if _, ok := parseAuditTime(bad, now); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
