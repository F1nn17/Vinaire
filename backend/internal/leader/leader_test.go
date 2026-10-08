package leader

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestOnlyOneReplicaLeadsAndAnotherTakesOver(t *testing.T) {
	leaseDuration, renewDeadline, retryPeriod = 1500*time.Millisecond, 1000*time.Millisecond, 100*time.Millisecond
	cs := fake.NewSimpleClientset()
	ctxA, stopA := context.WithCancel(context.Background())
	ctxB, stopB := context.WithCancel(context.Background())
	defer stopB()
	a := Run(ctxA, cs, "vinaire", "replica-a")
	b := Run(ctxB, cs, "vinaire", "replica-b")

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for i := 0; i < 100; i++ {
			if cond() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	waitFor("a leader", func() bool { return a.IsLeader() != b.IsLeader() })
	for i := 0; i < 5; i++ { // never both
		if a.IsLeader() && b.IsLeader() {
			t.Fatal("two leaders at once")
		}
		time.Sleep(50 * time.Millisecond)
	}
	first, second := a, b
	stopFirst := stopA
	if b.IsLeader() {
		first, second, stopFirst = b, a, stopB
	}
	stopFirst() // the leader goes away (releases the lease)
	waitFor("a takeover", func() bool { return second.IsLeader() })
	if first.IsLeader() {
		t.Error("the stopped replica still thinks it leads")
	}
}

func TestNoElectorMeansLeader(t *testing.T) {
	var e *Elector
	if !e.IsLeader() {
		t.Error("with no election every replica is the leader")
	}
}
