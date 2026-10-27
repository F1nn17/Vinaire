package authn

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// the shared Secret, as two replicas see it
type fakeKV struct {
	mu   sync.Mutex
	data map[string][]byte
	fail bool
	// failClaim makes only Claim fail (Phase 95)
	failClaim bool
}

func newFakeKV() *fakeKV { return &fakeKV{data: map[string][]byte{}} }

func (f *fakeKV) Snapshot(context.Context) (map[string][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("api down")
	}
	out := map[string][]byte{}
	for k, v := range f.data {
		out[k] = append([]byte(nil), v...)
	}
	return out, nil
}
func (f *fakeKV) Put(_ context.Context, k string, v []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("api down")
	}
	f.data[k] = append([]byte(nil), v...)
	return nil
}
func (f *fakeKV) PutIfPresent(_ context.Context, k string, v []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return false, errors.New("api down")
	}
	if _, ok := f.data[k]; !ok {
		return false, nil
	}
	f.data[k] = append([]byte(nil), v...)
	return true, nil
}
func (f *fakeKV) Remove(_ context.Context, k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, k)
	return nil
}

// a recognisable "encryption": the plain text is not in the output
type fakeSealer struct{}

func (fakeSealer) Seal(p []byte) []byte {
	out := make([]byte, len(p))
	for i, b := range p {
		out[i] = b ^ 0x5a
	}
	return append([]byte("sealed:"), out...)
}
func (fakeSealer) Open(s []byte) ([]byte, error) {
	if !bytes.HasPrefix(s, []byte("sealed:")) {
		return nil, errors.New("not sealed with this key")
	}
	s = s[len("sealed:"):]
	out := make([]byte, len(s))
	for i, b := range s {
		out[i] = b ^ 0x5a
	}
	return out, nil
}

func fast(t *testing.T) {
	a, b := snapshotTTL, missRefreshGap
	snapshotTTL, missRefreshGap = 0, 0
	t.Cleanup(func() { snapshotTTL, missRefreshGap = a, b })
}

func TestTwoReplicasShareLogins(t *testing.T) {
	fast(t)
	kv := newFakeKV()
	a, b := NewSharedStore(kv, fakeSealer{}), NewSharedStore(kv, fakeSealer{})
	id := NewSessionID()
	if err := a.Create(&Session{ID: id, K8sToken: "TOKEN-SECRET-VALUE", Username: "alice", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// logged in on A, the next request lands on B
	got, ok := b.Get(id)
	if !ok || got.K8sToken != "TOKEN-SECRET-VALUE" || got.Username != "alice" || got.ID != id {
		t.Fatalf("replica B: %+v %v", got, ok)
	}
	// a restart: a new replica with an empty memory still knows the login
	if _, ok := NewSharedStore(kv, fakeSealer{}).Get(id); !ok {
		t.Error("the login didn't survive a new process")
	}
	// the Secret holds neither the token nor the cookie
	for k, v := range kv.data {
		if strings.Contains(k, id) || bytes.Contains(v, []byte("TOKEN-SECRET-VALUE")) || bytes.Contains(v, []byte("alice")) {
			t.Errorf("the shared store holds readable data under %q", k)
		}
	}
	// signing out on B ends it on A
	b.Delete(id)
	if _, ok := a.Get(id); ok {
		t.Error("the session outlived its logout on another replica")
	}
	if _, ok := a.Get("junk-cookie"); ok {
		t.Error("an unknown cookie was accepted")
	}
}

func TestSharedStoreExpiryCapAndFailures(t *testing.T) {
	fast(t)
	kv := newFakeKV()
	s := NewSharedStore(kv, fakeSealer{})
	old := NewSessionID()
	_ = s.Create(&Session{ID: old, K8sToken: "t", ExpiresAt: time.Now().Add(-time.Minute)})
	if _, ok := s.Get(old); ok {
		t.Error("an expired session was returned")
	}
	for i := 0; i < maxSharedSessions; i++ {
		if err := s.Create(&Session{ID: NewSessionID(), K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
	}
	if err := s.Create(&Session{ID: NewSessionID(), K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)}); !errors.Is(err, ErrTooManySessions) {
		t.Errorf("past the cap: %v", err)
	}
	kv.fail = true
	if err := s.Create(&Session{ID: NewSessionID(), K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)}); !errors.Is(err, ErrTooManySessions) && !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("store down: %v", err)
	}
}

func TestStoreDownKeepsTheLastView(t *testing.T) {
	fast(t)
	kv := newFakeKV()
	s := NewSharedStore(kv, fakeSealer{})
	id := NewSessionID()
	_ = s.Create(&Session{ID: id, K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)})
	kv.fail = true
	if _, ok := s.Get(id); !ok {
		t.Error("a store outage logged everyone out")
	}
	other := NewSharedStore(kv, fakeSealer{})
	if err := other.Create(&Session{ID: NewSessionID(), K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)}); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("a login while the store is down must fail loudly, got %v", err)
	}
}

func TestListAndRevokeNeverExposeTheSession(t *testing.T) {
	fast(t)
	kv := newFakeKV()
	a, b := NewSharedStore(kv, fakeSealer{}), NewSharedStore(kv, fakeSealer{})
	id := NewSessionID()
	_ = a.Create(&Session{ID: id, K8sToken: "TOKEN-X", Username: "bob", ExpiresAt: time.Now().Add(time.Hour)})
	list := b.List()
	if len(list) != 1 || list[0].Username != "bob" || list[0].Ref != Ref(id) {
		t.Fatalf("list: %+v", list)
	}
	if strings.Contains(list[0].Ref+list[0].Owner, id) || strings.Contains(list[0].Owner, "TOKEN-X") {
		t.Error("the list leaks the id or the token")
	}
	if b.Revoke("short") {
		t.Error("a too-short reference matched")
	}
	if !b.Revoke(list[0].Ref) {
		t.Fatal("revoke found nothing")
	}
	if _, ok := a.Get(id); ok {
		t.Error("a revoked session still works on another replica")
	}
}

func TestMemoryStoreListsAndRevokes(t *testing.T) {
	m := NewMemoryStore()
	id := NewSessionID()
	_ = m.Create(&Session{ID: id, K8sToken: "t", ExpiresAt: time.Now().Add(time.Hour)})
	l := m.List()
	if len(l) != 1 || l[0].Ref != Ref(id) {
		t.Fatalf("%+v", l)
	}
	if !m.Revoke(l[0].Ref) {
		t.Error("revoke")
	}
	if _, ok := m.Get(id); ok {
		t.Error("still there")
	}
}
