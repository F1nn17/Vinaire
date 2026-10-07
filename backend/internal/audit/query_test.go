package audit

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func line(t time.Time, action, ns, name, who string, detail map[string]any) string {
	b, _ := json.Marshal(Entry{Time: t, Action: action, Namespace: ns, Name: name, Identity: who, Detail: detail})
	return string(b) + "\n"
}

func TestQueryReadsLiveAndRotatedFilesNewestFirstAndFilters(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "audit.log")
	now := time.Now().UTC()

	// an older, gzip-compressed backup as lumberjack leaves it
	gzPath := filepath.Join(dir, "audit-2026-01-01T00-00-00.000.log.gz")
	f, _ := os.Create(gzPath)
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte(line(now.Add(-48*time.Hour), "restart-deployment", "shop", "web", "alice", nil)))
	_ = gz.Close()
	_ = f.Close()
	_ = os.Chtimes(gzPath, now.Add(-48*time.Hour), now.Add(-48*time.Hour))

	body := line(now.Add(-2*time.Hour), "delete-pod", "shop", "web-1", "bob", nil) +
		line(now.Add(-1*time.Hour), "helm-install", "db", "pg", "alice", map[string]any{"chart": "postgresql"}) +
		"not json at all\n" +
		line(now.Add(-30*time.Minute), "restart-deployment", "db", "pg", "carol", nil)
	if err := os.WriteFile(live, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// unrelated files in the same directory are ignored
	_ = os.WriteFile(filepath.Join(dir, "other.log"), []byte(line(now, "secret-thing", "", "", "x", nil)), 0o600)

	l := &Logger{path: live}
	all, err := l.Query(Filter{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 4 {
		t.Fatalf("got %d entries, want 4 (live + gz backup, junk line and other files skipped)", len(all.Entries))
	}
	for i := 1; i < len(all.Entries); i++ {
		if all.Entries[i].Time.After(all.Entries[i-1].Time) {
			t.Error("entries are not newest first")
		}
	}
	for _, e := range all.Entries {
		if e.Action == "secret-thing" {
			t.Error("read a file that isn't the audit log")
		}
	}
	if len(all.Actions) != 3 {
		t.Errorf("actions = %v", all.Actions)
	}

	check := func(name string, f Filter, want int) {
		t.Helper()
		r, _ := l.Query(f, 100)
		if len(r.Entries) != want {
			t.Errorf("%s: %d entries, want %d", name, len(r.Entries), want)
		}
	}
	check("action", Filter{Action: "restart-deployment"}, 2)
	check("identity substring, case-insensitive", Filter{Identity: "ALI"}, 2)
	check("namespace", Filter{Namespace: "db"}, 2)
	check("name substring", Filter{Name: "web"}, 2)
	check("since", Filter{Since: now.Add(-3 * time.Hour)}, 3)
	check("until", Filter{Until: now.Add(-90 * time.Minute)}, 2)
	check("free text reaches the detail", Filter{Text: "postgresql"}, 1)
	check("combined", Filter{Action: "restart-deployment", Namespace: "db"}, 1)
	none, _ := l.Query(Filter{Text: "no-such-thing"}, 10)
	if b, _ := json.Marshal(none); !strings.Contains(string(b), `"entries":[]`) {
		t.Errorf("an empty result must marshal as [], got %s", b)
	}

	r, _ := l.Query(Filter{}, 2)
	if len(r.Entries) != 2 || !r.Truncated {
		t.Errorf("limit: %d entries, truncated=%v", len(r.Entries), r.Truncated)
	}
	if r.Entries[0].Identity != "carol" {
		t.Errorf("limit must keep the newest, got %q first", r.Entries[0].Identity)
	}
}

func TestQueryWithoutALogIsEmptyNotAnError(t *testing.T) {
	var none *Logger
	if none.Available() {
		t.Error("a nil logger reported available")
	}
	r, err := none.Query(Filter{}, 10)
	if err != nil || len(r.Entries) != 0 {
		t.Errorf("nil logger: %v %v", r, err)
	}
	l := &Logger{path: filepath.Join(t.TempDir(), "missing", "audit.log")}
	if r, err := l.Query(Filter{}, 10); err != nil || len(r.Entries) != 0 {
		t.Errorf("missing dir: %v %v", r, err)
	}
}
