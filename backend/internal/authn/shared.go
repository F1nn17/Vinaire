package authn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// A session store shared by several replicas (Roadmap Phase 48). Each session is one
// key in a Secret of Vinaire's own namespace, written and removed with a merge patch of
// that key alone — so two replicas never overwrite each other's logins — and sealed with
// the registry's key: the Secret holds no readable token. The key is the SHA-256 of the
// session id, never the id, so a copy of the Secret doesn't give anyone a cookie.

// Sealer encrypts and decrypts the stored sessions (clusters.Cipher).
type Sealer interface {
	Seal(plain []byte) []byte
	Open(sealed []byte) ([]byte, error)
}

// KV is the shared place the sessions live: one Secret, one key per session.
type KV interface {
	// Snapshot returns every key (sealed values).
	Snapshot(ctx context.Context) (map[string][]byte, error)
	// Put and Remove touch exactly one key.
	Put(ctx context.Context, key string, sealed []byte) error
	// PutIfPresent is Put that never creates: it writes only if the key is in the store at that moment (a version check
	// on the Secret, so a key removed between the read and the write is not brought back) and says whether it was.
	// A session that was revoked while it was being renewed must stay revoked. (Roadmap Phase 100)
	PutIfPresent(ctx context.Context, key string, sealed []byte) (present bool, err error)
	Remove(ctx context.Context, key string) error
	// Claim takes the lease named key for holder until ttl has passed, and says whether it is held now: false when someone
	// else's unexpired lease is there. It is atomic (a version check on the Secret), which Put is not. Release gives it
	// back, only if holder still holds it. (Roadmap Phase 95)
	Claim(ctx context.Context, key, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, key, holder string) error
}

// ErrStoreUnavailable is returned when the shared store can't be written to.
var ErrStoreUnavailable = errors.New("the shared session store is unavailable")

// maxSharedSessions keeps the Secret well under Kubernetes' 1 MiB limit (an OIDC
// ID token with its sealing overhead is a few KiB).
const maxSharedSessions = 200

// vars so tests can shorten them
var (
	snapshotTTL    = 3 * time.Second        // how stale a replica's view of the others' logins may be
	missRefreshGap = 500 * time.Millisecond // a cookie we don't know forces one look, at most this often
)

type storedSession struct {
	Token    string `json:"t"`
	Username string `json:"u,omitempty"`
	K8sUser  string `json:"k,omitempty"`
	Expires  int64  `json:"e"`
	// renewal (Phase 84); the whole blob is sealed
	RefreshToken string `json:"r,omitempty"`
	Subject      string `json:"s,omitempty"`
	Issuer       string `json:"i,omitempty"`
	TokenExpires int64  `json:"te,omitempty"`
	Validated    int64  `json:"va,omitempty"`
}

// SharedStore implements Store on top of KV.
type SharedStore struct {
	kv     KV
	sealer Sealer
	holder string // this process, for the renewal leases

	mu       sync.Mutex
	snap     map[string]*Session // by key
	snapAt   time.Time
	forcedAt time.Time
	warnedAt time.Time
}

// NewSharedStore builds the store; call StartJanitor to let it tidy up.
func NewSharedStore(kv KV, sealer Sealer) *SharedStore {
	return &SharedStore{kv: kv, sealer: sealer, snap: map[string]*Session{}, holder: NewSessionID()[:16]}
}

func storeKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "s-" + hex.EncodeToString(sum[:])
}

// Ref is what the sessions list shows and what revoking takes: a short fingerprint
// of the session, from which the id (the cookie) can't be recovered.
func Ref(id string) string { return storeKey(id)[2:18] }

func (s *SharedStore) decode(sealed []byte, now time.Time) (*Session, bool) {
	plain, err := s.sealer.Open(sealed)
	if err != nil {
		return nil, false
	}
	var st storedSession
	if json.Unmarshal(plain, &st) != nil || st.Token == "" {
		return nil, false
	}
	exp := time.Unix(st.Expires, 0)
	if !exp.After(now) {
		return nil, false
	}
	out := &Session{K8sToken: st.Token, Username: st.Username, KubernetesUser: st.K8sUser, ExpiresAt: exp,
		RefreshToken: st.RefreshToken, Subject: st.Subject, Issuer: st.Issuer}
	if st.TokenExpires > 0 {
		out.TokenExpiresAt = time.Unix(st.TokenExpires, 0)
	}
	if st.Validated > 0 {
		out.ValidatedAt = time.Unix(st.Validated, 0)
	}
	return out, true
}

// refresh reloads the shared view if it is older than max (a failed reload keeps the
// old view and tries again soon, so the store being down can't turn every request into
// an API call).
func (s *SharedStore) refresh(max time.Duration) {
	s.mu.Lock()
	if time.Since(s.snapAt) < max {
		s.mu.Unlock()
		return
	}
	s.snapAt = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := s.kv.Snapshot(ctx)
	if err != nil {
		s.mu.Lock()
		if time.Since(s.warnedAt) > time.Minute {
			s.warnedAt = time.Now()
			slog.Warn("shared session store: couldn't read it — using the last view", "error", err)
		}
		s.mu.Unlock()
		return
	}
	now := time.Now()
	next := make(map[string]*Session, len(raw))
	for k, v := range raw {
		if !strings.HasPrefix(k, "s-") {
			continue
		}
		if sess, ok := s.decode(v, now); ok {
			next[k] = sess
		}
	}
	s.mu.Lock()
	s.snap = next
	s.mu.Unlock()
}

func (s *SharedStore) Get(id string) (*Session, bool) {
	key := storeKey(id)
	s.refresh(snapshotTTL)
	if sess, ok := s.lookup(key, id); ok {
		return sess, true
	}
	// a login on another replica may be newer than our view — look once, not for every junk cookie
	s.mu.Lock()
	again := time.Since(s.forcedAt) > missRefreshGap
	if again {
		s.forcedAt = time.Now()
	}
	s.mu.Unlock()
	if !again {
		return nil, false
	}
	s.refresh(0)
	return s.lookup(key, id)
}

func (s *SharedStore) lookup(key, id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.snap[key]
	if !ok || time.Now().After(sess.ExpiresAt) {
		return nil, false
	}
	out := *sess
	out.ID = id
	return &out, true
}

func encodeSession(sess *Session) ([]byte, error) {
	st := storedSession{Token: sess.K8sToken, Username: sess.Username, K8sUser: sess.KubernetesUser, Expires: sess.ExpiresAt.Unix(),
		RefreshToken: sess.RefreshToken, Subject: sess.Subject, Issuer: sess.Issuer}
	if !sess.TokenExpiresAt.IsZero() {
		st.TokenExpires = sess.TokenExpiresAt.Unix()
	}
	if !sess.ValidatedAt.IsZero() {
		st.Validated = sess.ValidatedAt.Unix()
	}
	return json.Marshal(st) // #nosec G117 -- the blob is sealed by the caller (Sealer) before it reaches the Secret; it is never returned or logged
}

// Update writes a renewed session over the old one — the same key, so every replica finds it on its next
// look at the store.
func (s *SharedStore) Update(sess *Session) error {
	key := storeKey(sess.ID)
	blob, err := encodeSession(sess)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// The store decides, not this replica's snapshot: another replica may have revoked the session a moment ago.
	present, err := s.kv.PutIfPresent(ctx, key, s.sealer.Seal(blob))
	if err != nil {
		slog.Warn("shared session store: couldn't save a renewed session", "error", err)
		return ErrStoreUnavailable
	}
	if !present {
		s.mu.Lock()
		delete(s.snap, key)
		s.mu.Unlock()
		return ErrNoSession
	}
	stored := *sess
	stored.ID = ""
	s.mu.Lock()
	s.snap[key] = &stored
	s.mu.Unlock()
	return nil
}

func (s *SharedStore) Create(sess *Session) error {
	s.refresh(snapshotTTL)
	s.mu.Lock()
	live := 0
	now := time.Now()
	for _, v := range s.snap {
		if v.ExpiresAt.After(now) {
			live++
		}
	}
	s.mu.Unlock()
	if live >= maxSharedSessions {
		return ErrTooManySessions
	}
	blob, err := encodeSession(sess)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	key := storeKey(sess.ID)
	if err := s.kv.Put(ctx, key, s.sealer.Seal(blob)); err != nil {
		slog.Warn("shared session store: couldn't save a login", "error", err)
		return ErrStoreUnavailable
	}
	stored := *sess
	stored.ID = ""
	s.mu.Lock()
	s.snap[key] = &stored
	s.mu.Unlock()
	return nil
}

// Delete is a person signing out: this replica forgets the session at once and the Secret is cleaned best-effort (the
// janitor removes what is left at expiry). An administrator's revoke is stricter — see Revoke.
func (s *SharedStore) Delete(id string) {
	key := storeKey(id)
	s.mu.Lock()
	delete(s.snap, key)
	s.mu.Unlock()
	if err := s.remove(key); err != nil {
		slog.Warn("shared session store: couldn't remove a session", "error", err)
	}
}

// DeleteStrict ends a session for every replica and returns ErrStoreUnavailable if the Secret did not take the change;
// the session then stays valid (and known here), so signing out can be tried again.
func (s *SharedStore) DeleteStrict(id string) error {
	key := storeKey(id)
	if err := s.remove(key); err != nil {
		slog.Warn("shared session store: couldn't end a session at sign-out", "error", err)
		return ErrStoreUnavailable
	}
	s.mu.Lock()
	delete(s.snap, key)
	s.mu.Unlock()
	return nil
}

// remove deletes the key from the shared Secret and returns the error if that did not happen.
func (s *SharedStore) remove(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return s.kv.Remove(ctx, key)
}

// SessionInfo describes one session for the administrator's list. It never carries the
// id or the token.
type SessionInfo struct {
	Ref       string    `json:"ref"`
	Owner     string    `json:"owner"`
	Username  string    `json:"username,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *SharedStore) List() []SessionInfo {
	s.refresh(snapshotTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]SessionInfo, 0, len(s.snap))
	for k, v := range s.snap {
		if v.ExpiresAt.After(now) {
			out = append(out, SessionInfo{Ref: k[2:18], Owner: v.OwnerKey(), Username: v.Username, ExpiresAt: v.ExpiresAt})
		}
	}
	return out
}

// Revoke ends the session for every replica. It reports success only once the deletion is saved in the shared Secret:
// when the API refuses, the session stays (here too) and ErrStoreUnavailable is returned, so nobody is told a session
// is gone while another replica can still honour it.
func (s *SharedStore) Revoke(ref string) (bool, error) {
	s.refresh(0)
	s.mu.Lock()
	var key string
	for k := range s.snap {
		if len(ref) >= 8 && strings.HasPrefix(k[2:], ref) {
			key = k
			break
		}
	}
	s.mu.Unlock()
	if key == "" {
		return false, nil
	}
	if err := s.remove(key); err != nil {
		slog.Warn("shared session store: couldn't revoke a session", "error", err)
		return false, ErrStoreUnavailable
	}
	s.mu.Lock()
	delete(s.snap, key)
	s.mu.Unlock()
	return true, nil
}

// StartJanitor removes expired sessions from the shared Secret every minute (any
// replica may; removing a key twice is harmless), and any value that no longer opens
// with the current key — it can never be used again.
func (s *SharedStore) StartJanitor(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			c, cancel := context.WithTimeout(ctx, 15*time.Second)
			raw, err := s.kv.Snapshot(c)
			if err != nil {
				cancel()
				continue
			}
			now := time.Now()
			for k, v := range raw {
				if !strings.HasPrefix(k, "s-") {
					continue
				}
				if _, ok := s.decode(v, now); !ok {
					_ = s.kv.Remove(c, k)
				}
			}
			cancel()
		}
	}()
}

// A renewal of one session must not run on two replicas at once (Roadmap Phase 95): the identity provider rotates the
// refresh token on every use and treats a second use of the spent one as theft — it can end the person's session at the
// provider itself. Inside one process a singleflight prevents it; between replicas this lease does. It is a key of the
// same Secret, "l-<hash of the session id>", written with a version check, so the one that gets it is the only one that
// may present the refresh token.

func leaseKey(id string) string { return "l-" + storeKey(id)[2:] }

// AcquireRenewal tries to become the only replica renewing this session. release must be called when done.
func (s *SharedStore) AcquireRenewal(id string, ttl time.Duration) (release func(), acquired bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ok, err := s.kv.Claim(ctx, leaseKey(id), s.holder, ttl)
	if err != nil || !ok {
		return func() {}, false, err
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := s.kv.Release(ctx, leaseKey(id), s.holder); err != nil {
			slog.Warn("shared session store: couldn't release a renewal lease (it expires by itself)", "error", err)
		}
	}, true, nil
}

// Fresh is the session as the store holds it now, not as this replica last saw it.
func (s *SharedStore) Fresh(id string) (*Session, bool) {
	s.refresh(0)
	return s.lookup(storeKey(id), id)
}
