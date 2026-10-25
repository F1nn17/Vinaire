package clusters

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Roadmap Phase 95: the renewal lease of the shared session store.

func backend(cs *fake.Clientset) *SessionBackend {
	return &SessionBackend{s: &SecretStore{clientset: cs, namespace: "vinaire"}}
}

func TestOnlyOneHolderGetsALeaseUntilItIsReleasedOrLapses(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	a, b := backend(cs), backend(cs)

	if ok, err := a.Claim(ctx, "l-x", "replica-a", time.Minute); err != nil || !ok { // there is no Secret yet
		t.Fatalf("first claim: %v %v", ok, err)
	}
	if ok, _ := b.Claim(ctx, "l-x", "replica-b", time.Minute); ok {
		t.Fatal("two replicas hold the same lease")
	}
	if ok, _ := a.Claim(ctx, "l-x", "replica-a", time.Minute); !ok {
		t.Fatal("the holder can extend its own lease")
	}
	// someone else's Release doesn't free it
	if err := b.Release(ctx, "l-x", "replica-b"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.Claim(ctx, "l-x", "replica-b", time.Minute); ok {
		t.Fatal("a lease was freed by a replica that doesn't hold it")
	}
	if err := a.Release(ctx, "l-x", "replica-a"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.Claim(ctx, "l-x", "replica-b", time.Minute); !ok {
		t.Fatal("a released lease can't be taken")
	}
	// a lease whose holder died (it was never released) lapses
	if ok, _ := a.Claim(ctx, "l-y", "replica-a", -time.Second); !ok {
		t.Fatal("claim with an already-past expiry")
	}
	if ok, _ := b.Claim(ctx, "l-y", "replica-b", time.Minute); !ok {
		t.Fatal("an expired lease still blocks")
	}
}

func TestALeaseNeverDisturbsTheSessionsInTheSameSecret(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	a := backend(cs)
	if err := a.Put(ctx, "s-aaa", []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	if ok, _ := a.Claim(ctx, "l-aaa", "r", time.Minute); !ok {
		t.Fatal("claim")
	}
	snap, _ := a.Snapshot(ctx)
	if string(snap["s-aaa"]) != "sealed" {
		t.Fatalf("a session was changed by a lease: %v", snap)
	}
	if err := a.Release(ctx, "l-aaa", "r"); err != nil {
		t.Fatal(err)
	}
	snap, _ = a.Snapshot(ctx)
	if _, has := snap["l-aaa"]; has || string(snap["s-aaa"]) != "sealed" {
		t.Fatalf("after release: %v", snap)
	}
}

// The API server refuses a patch that carries a stale resourceVersion with a Conflict; the claim reads again and tries
// once more — and, when the other replica took the lease in between, gives up.
func TestAConflictIsReadAgainAndTheLeaseThatWasTakenInTheMeantimeIsRespected(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	a := backend(cs)
	if err := a.Put(ctx, "s-aaa", []byte("sealed")); err != nil {
		t.Fatal(err)
	}
	conflicts := 0
	gvr := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	cs.PrependReactor("patch", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts > 0 {
			return false, nil, nil
		}
		conflicts++
		// the other replica wins the race: its lease is written (straight into the tracker — a reactor must not call
		// the client, which holds the same lock), then our patch is refused
		obj, err := cs.Tracker().Get(gvr, "vinaire", sessionsSecretName)
		if err != nil {
			t.Error(err)
			return false, nil, nil
		}
		sec := obj.(*corev1.Secret).DeepCopy()
		sec.Data["l-race"] = []byte(fmt.Sprintf("replica-b|%d", time.Now().Add(time.Minute).Unix()))
		if err := cs.Tracker().Update(gvr, sec, "vinaire"); err != nil {
			t.Error(err)
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, sessionsSecretName, fmt.Errorf("the object has been modified"))
	})
	if ok, err := a.Claim(ctx, "l-race", "replica-a", time.Minute); err != nil || ok {
		t.Fatalf("the lease the other replica took in the meantime was taken over: %v %v", ok, err)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts: %d", conflicts)
	}
}
