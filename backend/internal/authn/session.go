// Package authn implements Roadmap Phase 10's authentication: the browser
// never holds a raw Kubernetes token, only an opaque session-ID cookie.
// The server-side Session behind that cookie carries the real K8s bearer
// token (pasted directly in "token" mode, or the ID token from a Keycloak
// login in "oidc" mode) and is what every handler actually authenticates
// to the Kubernetes API with — see internal/clusters.Entry.ClientsetForToken.
package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// NewSessionID returns 256 bits from crypto/rand, URL-safe. It is the only thing
// the browser holds, so it must be unguessable; a fresh one is issued at every
// login (never reusing whatever cookie the browser arrived with), which is what
// rules out session fixation.
func NewSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ErrTooManySessions is returned when the store is full.
var ErrTooManySessions = errors.New("too many active sessions")

// maxSessions bounds the in-memory store. Logins are validated against the
// cluster before a session exists, so this is defence in depth, not the first line.
const maxSessions = 5000

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
	Create(sess *Session) error
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

func (s *MemoryStore) Create(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxSessions {
		s.purgeLocked(time.Now())
		if len(s.sessions) >= maxSessions {
			return ErrTooManySessions
		}
	}
	s.sessions[sess.ID] = sess
	return nil
}

func (s *MemoryStore) purgeLocked(now time.Time) {
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

// StartJanitor removes expired sessions every minute until ctx ends. Get already
// refuses an expired session, but one that is never looked at again would sit in
// memory (holding a Kubernetes token) forever without this.
func (s *MemoryStore) StartJanitor(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.mu.Lock()
				s.purgeLocked(now)
				s.mu.Unlock()
			}
		}
	}()
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

// Lister is what a store offers the administrator's sessions list. Both stores do.
type Lister interface {
	List() []SessionInfo
	// Revoke ends the session with this Ref; false if there is none.
	Revoke(ref string) bool
}

func (s *MemoryStore) List() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]SessionInfo, 0, len(s.sessions))
	for id, sess := range s.sessions {
		if now.Before(sess.ExpiresAt) {
			out = append(out, SessionInfo{Ref: Ref(id), Owner: sess.OwnerKey(), Username: sess.Username, ExpiresAt: sess.ExpiresAt})
		}
	}
	return out
}

func (s *MemoryStore) Revoke(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.sessions {
		if len(ref) >= 8 && strings.HasPrefix(Ref(id), ref) {
			delete(s.sessions, id)
			return true
		}
	}
	return false
}
