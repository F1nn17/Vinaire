// Roadmap Phase 110: shared sessions between two Vinaire replicas, over the REAL Kubernetes API (real latency, real
// resourceVersion conflicts) — what the in-memory tests of internal/authn can only imitate. Two SharedStores, each with its
// own client, share one Secret like two pods do. The revoke bound stated in docs/security (5 s) is measured here.
//
//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/f1nn17/vinaire/backend/internal/authn"
	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// unique per run: a namespace deleted by the previous run may still be terminating
var haNamespace = fmt.Sprintf("ha-sessions-ci-%d", time.Now().Unix()%1000000)

// flakyKV is a replica whose deletes the API server "refuses" on demand (a network or API failure at the wrong moment).
type flakyKV struct {
	authn.KV
	failRemove bool
}

func (f *flakyKV) Remove(ctx context.Context, key string) error {
	if f.failRemove {
		return errors.New("api unreachable")
	}
	return f.KV.Remove(ctx, key)
}

func haClient(t *testing.T) kubernetes.Interface {
	t.Helper()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestTwoReplicasShareSessionsOverTheRealAPI(t *testing.T) {
	ctx := context.Background()
	admin := haClient(t)
	if _, err := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: haNamespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace: %v", err)
	}
	t.Cleanup(func() { _ = admin.CoreV1().Namespaces().Delete(ctx, haNamespace, metav1.DeleteOptions{}) })

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	cipher, err := clusters.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	kvA := &flakyKV{KV: clusters.NewSessionBackend(haClient(t), haNamespace)}
	kvB := &flakyKV{KV: clusters.NewSessionBackend(haClient(t), haNamespace)}
	a, b := authn.NewSharedStore(kvA, cipher), authn.NewSharedStore(kvB, cipher)

	login := func(store *authn.SharedStore) string {
		id := authn.NewSessionID()
		if err := store.Create(&authn.Session{ID: id, K8sToken: "tok", Username: "ha-user", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("login: %v", err)
		}
		return id
	}
	waitUntil := func(limit time.Duration, cond func() bool) time.Duration {
		start := time.Now()
		for !cond() {
			if time.Since(start) > limit {
				return -1
			}
			time.Sleep(100 * time.Millisecond)
		}
		return time.Since(start)
	}

	// a session just created on the other replica is found by a forced reload, which is rate-limited (missRefreshGap, 0.5 s)
	knows := func(s *authn.SharedStore, id string) bool {
		return waitUntil(5*time.Second, func() bool { _, ok := s.Get(id); return ok }) >= 0
	}

	t.Run("a login on one replica is accepted by the other", func(t *testing.T) {
		id := login(a)
		if !knows(b, id) {
			t.Fatal("the second replica does not know a session created on the first")
		}
	})

	t.Run("a revoke reaches the other replica within the stated bound", func(t *testing.T) {
		id := login(a)
		if !knows(b, id) { // B has loaded the shared view
			t.Fatal("B does not know the session")
		}
		var ref string
		for _, s := range a.List() {
			if s.Ref == authn.Ref(id) {
				ref = s.Ref
			}
		}
		if ok, err := a.Revoke(ref); !ok || err != nil {
			t.Fatalf("revoke: %v %v", ok, err)
		}
		took := waitUntil(10*time.Second, func() bool { _, ok := b.Get(id); return !ok })
		if took < 0 {
			t.Fatal("the revoked session still works on the other replica after 10 s")
		}
		if took > 5*time.Second {
			t.Fatalf("the revoke took %v to reach the other replica; docs/security states at most 5 s", took)
		}
		t.Logf("revoke reached the other replica in %v", took)
	})

	t.Run("a sign-out is refused while the API fails, and works after", func(t *testing.T) {
		id := login(a)
		if !knows(b, id) {
			t.Fatal("B does not know the session")
		}
		kvB.failRemove = true
		if err := b.DeleteStrict(id); !errors.Is(err, authn.ErrStoreUnavailable) {
			t.Fatalf("sign-out during an API failure: %v", err)
		}
		if _, ok := a.Get(id); !ok {
			t.Fatal("the session vanished although the deletion was not saved")
		}
		kvB.failRemove = false
		if err := b.DeleteStrict(id); err != nil {
			t.Fatalf("the retry: %v", err)
		}
		if took := waitUntil(10*time.Second, func() bool { _, ok := a.Get(id); return !ok }); took < 0 || took > 5*time.Second {
			t.Fatalf("after a confirmed sign-out the first replica still honoured the session for %v", took)
		}
	})

	t.Run("a renewal that finishes after a revoke does not bring the session back", func(t *testing.T) {
		id := login(a)
		if !knows(b, id) {
			t.Fatal("B does not know the session")
		}
		s, _ := b.Get(id)
		if err := a.DeleteStrict(id); err != nil {
			t.Fatal(err)
		}
		renewed := *s
		renewed.ID = id
		renewed.K8sToken = "renewed"
		renewed.ExpiresAt = time.Now().Add(2 * time.Hour)
		if err := b.Update(&renewed); err != nil && !errors.Is(err, authn.ErrNoSession) {
			t.Fatalf("update: %v", err)
		}
		time.Sleep(4 * time.Second) // longer than the cache
		if _, ok := a.Get(id); ok {
			t.Fatal("a revoked session came back after a late renewal")
		}
		if _, ok := b.Get(id); ok {
			t.Fatal("a revoked session came back on the renewing replica")
		}
	})
}
