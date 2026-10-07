package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shiraku/vinaire/backend/internal/audit"
)

// The audit viewer: the same records Vinaire writes for every mutating action,
// searchable. Who did what, to which object, when — never the secret itself (the
// writers already leave those out). It is a read, but of every user's activity, so
// it is gated like a change to Vinaire's own configuration: a permission on the
// virtual resource audit.vinaire.io, asked of the cluster as the caller.

const (
	defaultAuditLimit = 200
	maxAuditLimit     = 1000
)

// parseAuditTime accepts an RFC 3339 time or a relative duration ("90m", "24h",
// "7d") meaning that long ago.
func parseAuditTime(v string, now time.Time) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	if strings.HasSuffix(v, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(v, "d")); err == nil && n >= 0 && n <= 3650 {
			return now.Add(-time.Duration(n) * 24 * time.Hour), true
		}
		return time.Time{}, false
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return now.Add(-d), true
	}
	return time.Time{}, false
}

// AuditLog handles GET /api/v1/audit?action=&identity=&namespace=&name=&q=&since=&until=&limit=.
func (h *Handler) AuditLog(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "audit", "get") {
		return
	}
	if !h.Audit.Available() {
		respondJSON(w, map[string]any{"available": false, "entries": []audit.Entry{}, "actions": []string{}})
		return
	}
	q := r.URL.Query()
	now := time.Now().UTC()
	since, ok1 := parseAuditTime(q.Get("since"), now)
	until, ok2 := parseAuditTime(q.Get("until"), now)
	if !ok1 || !ok2 {
		respondError(w, http.StatusBadRequest, `since/until must be an RFC 3339 time or a duration such as "90m", "24h", "7d"`)
		return
	}
	limit := defaultAuditLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			respondError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, maxAuditLimit)
	}
	for _, k := range []string{"action", "identity", "namespace", "name", "q"} {
		if len(q.Get(k)) > 200 {
			respondError(w, http.StatusBadRequest, k+" is too long")
			return
		}
	}
	res, err := h.Audit.Query(audit.Filter{
		Action: q.Get("action"), Identity: q.Get("identity"), Namespace: q.Get("namespace"),
		Name: q.Get("name"), Text: q.Get("q"), Since: since, Until: until,
	}, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "couldn't read the audit log")
		return
	}
	respondJSON(w, map[string]any{
		"available": true, "entries": res.Entries, "truncated": res.Truncated,
		"actions": res.Actions, "scanned": res.Scanned, "oldest": res.Oldest,
	})
}
