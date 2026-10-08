package clusters

import (
	"bytes"
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionBackendKeepsKeysIndependent(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	a := &SessionBackend{s: &SecretStore{clientset: cs, namespace: "vinaire"}}
	b := &SessionBackend{s: &SecretStore{clientset: cs, namespace: "vinaire"}}

	if snap, err := a.Snapshot(ctx); err != nil || len(snap) != 0 {
		t.Fatalf("empty: %v %v", snap, err)
	}
	// two replicas write different sessions; neither overwrites the other
	if err := a.Put(ctx, "s-aaa", []byte("sealed-a")); err != nil { // creates the Secret
		t.Fatal(err)
	}
	if err := b.Put(ctx, "s-bbb", []byte("sealed-b")); err != nil { // patches it
		t.Fatal(err)
	}
	snap, _ := a.Snapshot(ctx)
	if !bytes.Equal(snap["s-aaa"], []byte("sealed-a")) || !bytes.Equal(snap["s-bbb"], []byte("sealed-b")) {
		t.Fatalf("snapshot: %v", snap)
	}
	if err := b.Remove(ctx, "s-aaa"); err != nil {
		t.Fatal(err)
	}
	snap, _ = a.Snapshot(ctx)
	if _, has := snap["s-aaa"]; has || !bytes.Equal(snap["s-bbb"], []byte("sealed-b")) {
		t.Fatalf("after remove: %v", snap)
	}
	// removing from a Secret that doesn't exist yet is fine
	if err := (&SessionBackend{s: &SecretStore{clientset: fake.NewSimpleClientset(), namespace: "x"}}).Remove(ctx, "s-zzz"); err != nil {
		t.Errorf("remove with no Secret: %v", err)
	}
}

func TestSessionBackendRefusesWithoutAKey(t *testing.T) {
	if _, _, ok := (&SecretStore{}).SessionBackend(); ok {
		t.Error("sessions would be stored unsealed")
	}
}
