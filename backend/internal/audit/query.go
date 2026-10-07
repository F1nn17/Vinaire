package audit

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Reading the log back (for the audit viewer). The files are the ones Logger
// writes: the live file and its rotated backups next to it (lumberjack names them
// "<name>-<timestamp><ext>", optionally gzip-compressed). Only that directory and
// only that base name are read — nothing here comes from a request.

// Filter selects entries. Zero values mean "any".
type Filter struct {
	Action    string // exact
	Identity  string // substring, case-insensitive
	Namespace string // exact
	Name      string // substring, case-insensitive
	Text      string // substring of the whole record (detail included), case-insensitive
	Since     time.Time
	Until     time.Time
}

// Result is a page of entries, newest first.
type Result struct {
	Entries []Entry `json:"entries"`
	// Truncated: more matched than the limit allowed.
	Truncated bool `json:"truncated,omitempty"`
	// Actions are the distinct actions seen while scanning — for a filter menu.
	Actions []string `json:"actions"`
	// Scanned is how many records were looked at; Oldest the time of the oldest one.
	Scanned int       `json:"scanned"`
	Oldest  time.Time `json:"oldest,omitempty"`
}

const (
	// A query reads at most this many files and bytes, so a huge log can't make a
	// request expensive. The retention policy keeps at most 6 files of ~10 MB anyway.
	maxFiles     = 8
	maxScanBytes = 80 << 20
	keepMatches  = 20000
)

func (f Filter) matches(e Entry, raw []byte) bool {
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if f.Namespace != "" && e.Namespace != f.Namespace {
		return false
	}
	if f.Identity != "" && !strings.Contains(strings.ToLower(e.Identity), strings.ToLower(f.Identity)) {
		return false
	}
	if f.Name != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(f.Name)) {
		return false
	}
	if !f.Since.IsZero() && e.Time.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Time.After(f.Until) {
		return false
	}
	if f.Text != "" && !strings.Contains(strings.ToLower(string(raw)), strings.ToLower(f.Text)) {
		return false
	}
	return true
}

// logFiles lists the live file and its backups, newest first.
func (l *Logger) logFiles() []string {
	if l == nil || l.path == "" {
		return nil
	}
	dir, base := filepath.Dir(l.path), filepath.Base(l.path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type f struct {
		path string
		mod  time.Time
	}
	var files []f
	for _, e := range entries {
		n := e.Name()
		isLive := n == base
		isBackup := strings.HasPrefix(n, stem+"-") && (strings.HasSuffix(n, ext) || strings.HasSuffix(n, ext+".gz"))
		if e.IsDir() || (!isLive && !isBackup) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, f{filepath.Join(dir, n), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	out := make([]string, len(files))
	for i, x := range files {
		out[i] = x.path
	}
	return out
}

// Query returns up to limit matching entries, newest first.
func (l *Logger) Query(f Filter, limit int) (*Result, error) {
	res := &Result{Entries: []Entry{}, Actions: []string{}}
	seen := map[string]bool{}
	var budget int64 = maxScanBytes
	matched := []Entry{} // never nil: it is marshalled as [] not null
	for _, path := range l.logFiles() {
		entries, scanned, err := readFile(path, &budget, f, seen, res)
		if err != nil {
			continue // a half-rotated or unreadable file shouldn't hide the rest
		}
		res.Scanned += scanned
		matched = append(matched, entries...)
		if len(matched) > keepMatches { // bound memory: keep only the newest
			sort.SliceStable(matched, func(i, j int) bool { return matched[i].Time.After(matched[j].Time) })
			matched = matched[:keepMatches]
		}
		if budget <= 0 {
			break
		}
	}
	// files are newest-first but a file's lines are oldest-first: order everything
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Time.After(matched[j].Time) })
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
		res.Truncated = true
	}
	res.Entries = matched
	for a := range seen {
		res.Actions = append(res.Actions, a)
	}
	sort.Strings(res.Actions)
	return res, nil
}

func readFile(path string, budget *int64, f Filter, seen map[string]bool, res *Result) ([]Entry, int, error) {
	fh, err := os.Open(path) // #nosec G304 -- the path comes from the configured audit log's own directory listing, never from a request
	if err != nil {
		return nil, 0, err
	}
	defer fh.Close()
	var r io.Reader = fh
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(fh)
		if err != nil {
			return nil, 0, err
		}
		defer gz.Close()
		r = gz
	}
	sc := bufio.NewScanner(io.LimitReader(r, *budget))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var out []Entry
	n := 0
	for sc.Scan() {
		line := sc.Bytes()
		*budget -= int64(len(line)) + 1
		var e Entry
		if json.Unmarshal(line, &e) != nil || e.Action == "" {
			continue
		}
		n++
		seen[e.Action] = true
		if res.Oldest.IsZero() || e.Time.Before(res.Oldest) {
			res.Oldest = e.Time
		}
		if f.matches(e, line) {
			out = append(out, e)
		}
	}
	return out, n, sc.Err()
}

// Available reports whether there is a file-backed log to read.
func (l *Logger) Available() bool { return l != nil && l.path != "" }
