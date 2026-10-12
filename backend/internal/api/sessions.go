package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/f1nn17/vinaire/backend/internal/authn"
)

// The sessions list (Roadmap Phase 48): who is signed in to this Vinaire, and a way to
// end a session — for an administrator who lost a laptop, or is removing someone's
// access. It shows who and until when; it never carries the cookie or the Kubernetes
// token behind a session. Both calls need a right on the virtual resource
// auth.vinaire.io, like the rest of Vinaire's own configuration.

type sessionRow struct {
	Ref       string    `json:"ref"`
	Owner     string    `json:"owner"`
	Username  string    `json:"username,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	Current   bool      `json:"current"`
}

// Sessions handles GET /api/v1/auth/sessions.
func (h *Handler) Sessions(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "get") {
		return
	}
	lister, ok := h.Auth.Sessions.(authn.Lister)
	if !ok || h.Auth.Mode == authn.ModeDisabled {
		respondJSON(w, map[string]any{"sessions": []sessionRow{}, "shared": false})
		return
	}
	cur := ""
	if sess, ok := authn.FromContext(r.Context()); ok {
		cur = authn.Ref(sess.ID)
	}
	_, shared := h.Auth.Sessions.(*authn.SharedStore)
	rows := []sessionRow{}
	for _, s := range lister.List() {
		rows = append(rows, sessionRow{Ref: s.Ref, Owner: s.Owner, Username: s.Username, ExpiresAt: s.ExpiresAt, Current: s.Ref == cur})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ExpiresAt.After(rows[j].ExpiresAt) })
	respondJSON(w, map[string]any{"sessions": rows, "shared": shared})
}

// RevokeSession handles DELETE /api/v1/auth/sessions/{ref}.
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	if !h.requireVinaireRight(w, r, "auth", "delete") {
		return
	}
	lister, ok := h.Auth.Sessions.(authn.Lister)
	if !ok || h.Auth.Mode == authn.ModeDisabled {
		respondError(w, http.StatusConflict, "there are no sessions to end")
		return
	}
	ref := chi.URLParam(r, "ref")
	owner := ""
	for _, s := range lister.List() {
		if s.Ref == ref {
			owner = s.Owner
		}
	}
	if len(ref) < 8 || len(ref) > 16 || !lister.Revoke(ref) {
		respondError(w, http.StatusNotFound, "no such session")
		return
	}
	h.auditLog(r, "session-revoke", "", ref, "owner", owner)
	respondJSON(w, map[string]string{"status": "ok"})
}
