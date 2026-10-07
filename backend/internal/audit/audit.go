// Package audit gives Vinaire's write actions (Roadmap Phase 9) and secret
// reveals (Phase 6) a durable log sink, alongside the structured stdout
// slog line they already write — Phase 17's own risk note: an unbounded
// log file on a long-running pod is a real risk, so this rotates.
package audit

import (
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Entry is one durable audit record.
type Entry struct {
	Time       time.Time      `json:"time"`
	Action     string         `json:"action"`
	Namespace  string         `json:"namespace"`
	Name       string         `json:"name"`
	Identity   string         `json:"identity"`
	RemoteAddr string         `json:"remoteAddr"`
	Detail     map[string]any `json:"detail,omitempty"`
}

// Logger appends audit Entries as JSON lines to a rotated log file.
type Logger struct {
	out  io.Writer
	path string // the live file, for reading the log back (query.go)
}

// Retention policy (Phase 17's own risk note: decide this explicitly
// rather than let the file grow unbounded). 10MB per file, keep 5 rotated
// backups compressed, and drop anything older than 30 days regardless of
// count — generous for a narrow, three-action write surface (Phase 9) plus
// secret reveals, while still bounding total disk use on a long-running pod.
const (
	maxSizeMB   = 10
	maxBackups  = 5
	maxAgeDays  = 30
	compressOld = true
)

// NewLogger opens (creating if needed) a rotated audit log at path.
func NewLogger(path string) *Logger {
	return &Logger{path: path, out: &lumberjack.Logger{
		Filename:   path,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   compressOld,
	}}
}

// Write appends one audit entry. A write failure is logged but never
// propagated — a durable-audit-log hiccup must not block the write action
// that triggered it from completing; the stdout slog line (written
// alongside every call to this) remains the fallback record.
func (l *Logger) Write(e Entry) {
	e.Time = time.Now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		slog.Error("audit: marshal entry failed", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := l.out.Write(data); err != nil {
		slog.Error("audit: write entry failed", "error", err)
	}
}
