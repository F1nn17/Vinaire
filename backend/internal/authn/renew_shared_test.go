package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Roadmap Phase 95: two replicas that share their sessions must not both present one refresh token.

// Claim and Release for the fake store of the shared-session tests, honouring expiry as the Secret-backed one does.
// failClaim: only the claim fails — reading the sessions still works
func (f *fakeKV) setFailClaim(v bool) { f.mu.Lock(); f.failClaim = v; f.mu.Unlock() }

func (f *fakeKV) Claim(_ context.Context, key, holder string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail || f.failClaim {
		return false, errors.New("api down")
	}
	if cur, ok := f.data[key]; ok {
		h, e, _ := strings.Cut(string(cur), "|")
		var until int64
		_, _ = fmt.Sscanf(e, "%d", &until)
		if h != holder && time.UnixMilli(until).After(time.Now()) {
			return false, nil
		}
	}
	f.data[key] = []byte(fmt.Sprintf("%s|%d", holder, time.Now().Add(ttl).UnixMilli()))
	return true, nil
}

func (f *fakeKV) Release(_ context.Context, key, holder string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, ok := f.data[key]; ok && strings.HasPrefix(string(cur), holder+"|") {
		delete(f.data, key)
	}
	return nil
}

type replicas struct {
	a, b             *Manager
	kv               *fakeKV
	ctl              *providerControl
	cookie           *http.Cookie
	id               string
	brokerA, brokerB *renewingBroker
}

// newReplicas: two Managers, one Keycloak, one shared store; the person signs in on A.
func newReplicas(t *testing.T) *replicas {
	t.Helper()
	fast(t)
	p, ctl := renewalProvider(t)
	ctl.set(func(c *providerControl) { c.strict = true }) // a spent refresh token is refused, as Keycloak does
	kv := newFakeKV()
	r := &replicas{kv: kv, ctl: ctl, brokerA: &renewingBroker{}, brokerB: &renewingBroker{}}
	mk := func(b *renewingBroker) *Manager {
		m := newTokenManager(t)
		m.SetIdentityBroker(b)
		m.UseStore(NewSharedStore(kv, fakeSealer{}))
		if err := m.ConfigureOIDC(context.Background(), http.DefaultClient, p.URL+"/realms/vinaire", "vinaire", "s", "https://v/cb", true); err != nil {
			t.Fatal(err)
		}
		return m
	}
	r.a, r.b = mk(r.brokerA), mk(r.brokerB)
	w := signIn(t, r.a)
	if w.Code != http.StatusFound {
		t.Fatalf("sign-in: %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			r.cookie, r.id = c, c.Value
		}
	}
	if r.cookie == nil {
		t.Fatal("no session cookie")
	}
	return r
}

func (r *replicas) request() *http.Request {
	q := httptest.NewRequest("GET", "/api/v1/pods", nil)
	q.AddCookie(r.cookie)
	return q
}

// due makes the credential run out in a minute, as stored for every replica.
func (r *replicas) due(t *testing.T) {
	t.Helper()
	cur, ok := r.a.Sessions.Get(r.id)
	if !ok {
		t.Fatal("no session")
	}
	cp := *cur
	cp.TokenExpiresAt = time.Now().Add(time.Minute)
	if err := r.a.Sessions.Update(&cp); err != nil {
		t.Fatal(err)
	}
}

func TestTwoReplicasRenewingTheSameSessionAtOnceRotateTheRefreshTokenOnlyOnce(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	var wg sync.WaitGroup
	var denied atomic.Int32
	for i := 0; i < 12; i++ {
		m := r.a
		if i%2 == 1 {
			m = r.b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := m.Authenticate(r.request()); !ok {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()
	if denied.Load() != 0 {
		t.Fatalf("%d requests were refused — a replica presented a spent refresh token and ended the session", denied.Load())
	}
	if got := r.ctl.refreshCalls.Load(); got != 1 {
		t.Fatalf("the provider was asked %d times, want once (a rotated refresh token must not be used twice)", got)
	}
	if got := r.brokerA.renewals.Load() + r.brokerB.renewals.Load(); got != 1 {
		t.Fatalf("%d credentials were minted, want one", got)
	}
	// the other replica uses the renewed session and doesn't renew again
	if s, ok := r.b.Authenticate(r.request()); !ok || s.RefreshToken != "rt-2" {
		t.Fatalf("replica B: %v %+v", ok, s)
	}
	if r.ctl.refreshCalls.Load() != 1 {
		t.Fatal("renewed again")
	}
}

func TestAReplicaThatFindsTheSessionAlreadyRenewedUsesItInsteadOfRenewingAgain(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	// B's view is the old one (it read the store before A renewed); A renews through its own request
	stale, _ := r.b.Sessions.Get(r.id)
	if _, ok := r.a.Authenticate(r.request()); !ok {
		t.Fatal("A's renewal failed")
	}
	out, ok := r.b.renewIfDue(context.Background(), stale)
	if !ok || out.RefreshToken != "rt-2" {
		t.Fatalf("B did not pick up A's renewal: %v %+v", ok, out)
	}
	if r.ctl.refreshCalls.Load() != 1 {
		t.Fatalf("the provider was asked %d times, want once", r.ctl.refreshCalls.Load())
	}
}

func TestAReplicaWaitsForTheOneThatHoldsTheLeaseAndDoesNotTouchTheRefreshToken(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	a := r.a.Sessions.(*SharedStore)
	release, got, err := a.AcquireRenewal(r.id, time.Minute) // replica A is "in the middle of" a renewal
	if err != nil || !got {
		t.Fatalf("lease: %v %v", got, err)
	}
	old := renewWait
	renewWait = 400 * time.Millisecond
	t.Cleanup(func() { renewWait = old })
	start := time.Now()
	if _, ok := r.b.Authenticate(r.request()); !ok {
		t.Fatal("B ended a session that another replica is renewing")
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Errorf("B didn't wait for the lease holder (%s)", time.Since(start))
	}
	if r.ctl.refreshCalls.Load() != 0 {
		t.Fatalf("B presented the refresh token while A held the lease (%d calls)", r.ctl.refreshCalls.Load())
	}
	release()
}

func TestALeaseThatNobodyReleasesLapsesAndTheNextReplicaRenews(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	a := r.a.Sessions.(*SharedStore)
	if _, got, _ := a.AcquireRenewal(r.id, 150*time.Millisecond); !got { // a replica that died holding it
		t.Fatal("lease")
	}
	oldWait, oldStep := renewWait, renewWaitStep
	renewWait, renewWaitStep = 50*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { renewWait, renewWaitStep = oldWait, oldStep })
	// B waits a little, gives up and carries on with the credential it has
	if _, ok := r.b.Authenticate(r.request()); !ok {
		t.Fatal("the session ended while waiting")
	}
	time.Sleep(200 * time.Millisecond) // the lease has lapsed
	if s, ok := r.b.Authenticate(r.request()); !ok || r.ctl.refreshCalls.Load() != 1 || s.RefreshToken != "rt-2" {
		t.Fatalf("B did not renew once the lease lapsed: %v %d %+v", ok, r.ctl.refreshCalls.Load(), s)
	}
}

func TestWhenTheLeaseCannotBeTakenTheRefreshTokenIsNotRotated(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	r.kv.setFailClaim(true)
	// the credential still has a minute: the request goes on with it, and nothing is presented to the provider
	if _, ok := r.b.Authenticate(r.request()); !ok {
		t.Fatal("the session ended because the store could not give a lease")
	}
	if r.ctl.refreshCalls.Load() != 0 {
		t.Fatalf("the refresh token was presented although nobody could be told to leave it alone (%d calls)", r.ctl.refreshCalls.Load())
	}
	// once the store answers again the renewal goes through
	r.kv.setFailClaim(false)
	if s, ok := r.b.Authenticate(r.request()); !ok || r.ctl.refreshCalls.Load() != 1 || s.RefreshToken != "rt-2" {
		t.Fatalf("after the store came back: %v %d %+v", ok, r.ctl.refreshCalls.Load(), s)
	}
}

// Roadmap Phase 100: an administrator ends a session on one replica while another is renewing it.
func TestRevokeDuringRenewalMustNotRestoreTheSession(t *testing.T) {
	r := newReplicas(t)
	r.due(t)
	revoked := false
	hook := func() { // replica B's administrator ends the session while A is between the provider and the save
		if revoked {
			return
		}
		revoked = true
		if ok, err := r.b.Sessions.(*SharedStore).Revoke(Ref(r.id)); !ok || err != nil {
			t.Error("the revoke found no session")
		}
	}
	r.brokerA.onRenew.Store(&hook)
	if s, ok := r.a.Authenticate(r.request()); ok {
		t.Fatalf("the request that was being renewed was served by a revoked session: %+v", s)
	}
	for _, m := range []*Manager{r.a, r.b} {
		if _, ok := m.Authenticate(r.request()); ok {
			t.Fatal("a replica still accepts the revoked session — the renewal brought it back")
		}
	}
	for k := range r.kv.data {
		if strings.HasPrefix(k, "s-") {
			t.Fatalf("the revoked session is in the store again: %s", k)
		}
	}
}

// Roadmap Phase 112: the API becomes unreachable from the renewing replica AFTER the provider has rotated the refresh
// token and before the renewed session is saved. The stored copy still holds the spent token, so unless the new one is
// saved the next renewal (on any replica) is refused by a provider that rotates, and the person must sign in again.
func partitionAfterRotation(t *testing.T, heal time.Duration) (*replicas, bool) {
	t.Helper()
	old := renewSaveBackoff
	renewSaveBackoff = []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond, 160 * time.Millisecond}
	t.Cleanup(func() { renewSaveBackoff = old })
	r := newReplicas(t)
	r.due(t)
	hook := func() {
		r.kv.mu.Lock()
		r.kv.fail = true
		r.kv.mu.Unlock()
		if heal > 0 {
			time.AfterFunc(heal, func() { r.kv.mu.Lock(); r.kv.fail = false; r.kv.mu.Unlock() })
		}
	}
	r.brokerA.onRenew.Store(&hook)
	r.a.Authenticate(r.request())
	r.brokerA.onRenew.Store(nil)
	r.kv.mu.Lock()
	r.kv.fail = false
	r.kv.mu.Unlock()
	r.due(t) // make the next replica find it due again, whatever was stored
	_, ok := r.b.Authenticate(r.request())
	return r, ok
}

func TestABriefPartitionAfterTheProviderRotatedTheTokenDoesNotEndTheSession(t *testing.T) {
	// the API is back within the renewal's retries: the rotated refresh token is saved, the other replica carries on
	r, okB := partitionAfterRotation(t, 100*time.Millisecond)
	if !okB {
		t.Fatalf("a blip of the API ended the session (provider asked %d times)", r.ctl.refreshCalls.Load())
	}
}

func TestALongPartitionAfterTheProviderRotatedTheTokenEndsTheSessionAndNothingWorse(t *testing.T) {
	// not healed within the retries: the rotated token is lost with the replica's memory. This pins the known cost
	// (the person signs in again) so a change to it is a decision: the session is ended, never kept on a spent token.
	r, okB := partitionAfterRotation(t, 0)
	if okB {
		t.Fatal("the other replica served a session whose refresh token the provider had already rotated away")
	}
	if _, ok := r.a.Sessions.Get(r.id); ok {
		t.Fatal("the ended session is still in the store")
	}
}

// Roadmap Phase 114: the renewal lease must outlive the WORST renewal, the save of the rotated refresh token with its retries
// included. Otherwise a replica whose save hangs is still working when its lease lapses, a second replica takes it and
// presents the refresh token the provider has just rotated away.
func TestTheRenewalLeaseOutlivesTheWorstRenewal(t *testing.T) {
	if got := renewLeaseTTL(); got < 2*renewTimeout+renewSaveWorstCase() {
		t.Fatalf("lease %s is shorter than the provider call, the credential call and a save that takes %s", got, renewSaveWorstCase())
	}
	// the retries themselves can never take longer than the budget says (the production values, not the shortened ones)
	if renewSaveBackoffTotal() > renewSaveBudget {
		t.Fatalf("the save backoffs add up to %s, more than the budget %s — the budget would cut them off", renewSaveBackoffTotal(), renewSaveBudget)
	}
}

func renewSaveBackoffTotal() time.Duration {
	var d time.Duration
	for _, w := range renewSaveBackoff {
		d += w
	}
	return d
}

func TestASaveThatHangsIsCutOffByTheBudgetAndNeverOutlivesTheLease(t *testing.T) {
	defer func(a, b time.Duration, c []time.Duration) { renewSaveBudget, storeCallTimeout, renewSaveBackoff = a, b, c }(renewSaveBudget, storeCallTimeout, renewSaveBackoff)
	renewSaveBudget, storeCallTimeout = 300*time.Millisecond, 100*time.Millisecond
	renewSaveBackoff = []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond}
	kv := newFakeKV()
	kv.failPut, kv.hangPut = true, 100*time.Millisecond
	m := newTokenManager(t)
	m.UseStore(NewSharedStore(kv, fakeSealer{}))
	s := &Session{ID: NewSessionID(), K8sToken: "t", Username: "u", ExpiresAt: time.Now().Add(time.Hour)}
	m.Sessions.(*SharedStore).snap[storeKey(s.ID)] = s
	start := time.Now()
	if err := m.saveRenewed(s); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("saveRenewed: %v", err)
	}
	if took := time.Since(start); took > renewSaveWorstCase() {
		t.Fatalf("the save took %s, longer than its stated worst case %s", took, renewSaveWorstCase())
	}
	if kv.putCalls >= 7 {
		t.Fatalf("%d attempts: the budget did not cut the retries off", kv.putCalls)
	}
}

func TestAReplicaCannotTakeTheLeaseWhileTheOtherIsStillTryingToSaveTheRotatedToken(t *testing.T) {
	defer func(a, b, c, d, e, f time.Duration, g []time.Duration) {
		renewSaveBudget, storeCallTimeout, renewTimeout, renewLeaseMargin, renewWait, renewWaitStep, renewSaveBackoff = a, b, c, d, e, f, g
	}(renewSaveBudget, storeCallTimeout, renewTimeout, renewLeaseMargin, renewWait, renewWaitStep, renewSaveBackoff)
	renewSaveBudget, storeCallTimeout, renewTimeout, renewLeaseMargin = 600*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond, 0
	renewWait, renewWaitStep = 40*time.Millisecond, 10*time.Millisecond
	renewSaveBackoff = []time.Duration{50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond, 50 * time.Millisecond}
	r := newReplicas(t)
	r.due(t)
	r.kv.mu.Lock()
	r.kv.failPut, r.kv.hangPut = true, 100*time.Millisecond // A's saves hang, then fail; everything else works
	r.kv.mu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); r.a.Authenticate(r.request()) }()
	// B starts only once A holds the lease (otherwise B could win the race and the roles would be swapped)
	for i := 0; ; i++ {
		r.kv.mu.Lock()
		_, held := r.kv.data[leaseKey(r.id)]
		r.kv.mu.Unlock()
		if held {
			break
		}
		if i > 500 {
			t.Fatal("A never took the lease")
		}
		time.Sleep(2 * time.Millisecond)
	}
	// B asks again and again while A is still trying to save: it must neither take the lease nor touch the refresh token
	deadline := time.After(5 * time.Second)
	for running := true; running; {
		select {
		case <-done:
			running = false
		case <-deadline:
			t.Fatal("A's renewal never finished")
		default:
			r.b.Authenticate(r.request())
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := r.ctl.refreshCalls.Load(); got != 1 {
		t.Fatalf("the provider was asked %d times: a second replica presented the refresh token the first had just rotated", got)
	}
}
