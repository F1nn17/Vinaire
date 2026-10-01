// Package authn implements Roadmap Phase 10's authentication: the browser
// never holds a raw Kubernetes token, only an opaque session-ID cookie.
// The server-side Session behind that cookie carries the real K8s bearer
// token (pasted directly in "token" mode, or the ID token from a Keycloak
// login in "oidc" mode) and is what every handler actually authenticates
// to the Kubernetes API with — see internal/clusters.Entry.ClientsetForToken.
package authn

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Session is one logged-in user's server-side state.
type Session struct {
	ID        string
	K8sToken  string
	Username  string // best-effort: OIDC's preferred_username, empty in token mode
	ExpiresAt time.Time
}

// OwnerKey is a stable identifier for "who is this" that is safe to compare and
// store (it never contains the token itself) — used to scope long-running jobs
// (Roadmap Phase 30) to the identity that started them.
func (s *Session) OwnerKey() string {
	if s.Username != "" {
		return "user:" + s.Username
	}
	sum := sha256.Sum256([]byte(s.K8sToken))
	return "token:" + hex.EncodeToString(sum[:8])
}

// Store is a server-side session store. MemoryStore is the only
// implementation for now — swappable for Redis later (per Roadmap
// Phase 10) without any caller needing to change.
type Store interface {
	Create(sess *Session)
	Get(id string) (*Session, bool)
	Delete(id string)
}

// MemoryStore is a process-local Store. Sessions are lost on restart —
// acceptable for now; there is no persistence requirement in Phase 10.
type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]*Session)}
}

func (s *MemoryStore) Create(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

// Get returns the session for id, transparently deleting and reporting
// "not found" if it has expired — callers never see a stale session.
func (s *MemoryStore) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, id)
		return nil, false
	}
	return sess, true
}

func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}
