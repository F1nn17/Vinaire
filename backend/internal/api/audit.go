package api

import (
	"log/slog"
	"net/http"

	"github.com/f1nn17/vinaire/backend/internal/audit"
	"github.com/f1nn17/vinaire/backend/internal/authn"
)

// auditLog records one audited action — every Phase 9 write action
// (restart/delete/rollback) and Phase 6's secret reveal call this exactly
// once. It always logs to stdout via slog (unchanged since Phase 9/6) and,
// since Phase 17, also durably to h.Audit if configured, so the trail
// survives a pod restart. "who" is the caller's logged-in identity
// (best-effort username in OIDC mode, empty in token mode — a pasted
// ServiceAccount token carries no human username) plus their source
// address; extra key/value pairs add action-specific context (e.g.
// rollback's target revision, or reveal's key name).
func (h *Handler) auditLog(r *http.Request, action, namespace, name string, extra ...any) {
	username := ""
	if sess, ok := authn.FromContext(r.Context()); ok {
		username = sess.Username
	}

	args := []any{"action", action, "namespace", namespace, "name", name, "user", username, "remote_addr", r.RemoteAddr}
	args = append(args, extra...)
	slog.Warn("audited action", args...)

	if h.Audit != nil {
		h.Audit.Write(audit.Entry{
			Action:     action,
			Namespace:  namespace,
			Name:       name,
			Identity:   username,
			RemoteAddr: r.RemoteAddr,
			Detail:     extraToDetail(extra),
		})
	}
}

// extraToDetail folds auditLog's variadic key/value extras into a map for
// the durable JSON record.
func extraToDetail(extra []any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	m := make(map[string]any, len(extra)/2)
	for i := 0; i+1 < len(extra); i += 2 {
		key, ok := extra[i].(string)
		if !ok {
			continue
		}
		m[key] = extra[i+1]
	}
	return m
}
