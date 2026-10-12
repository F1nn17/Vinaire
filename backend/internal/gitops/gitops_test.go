package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"

	"github.com/f1nn17/vinaire/backend/internal/clusters"
)

// ---- fakes -------------------------------------------------------------------

type fakeStore struct {
	mu        sync.Mutex
	docs      map[string][]byte
	encrypted bool
}

func newFakeStore() *fakeStore { return &fakeStore{docs: map[string][]byte{}, encrypted: true} }

func (f *fakeStore) Load(context.Context) ([]clusters.StoredCluster, error) { return nil, nil }
func (f *fakeStore) Save(context.Context, []clusters.StoredCluster) error   { return nil }
func (f *fakeStore) Kind() string                                           { return "fake" }
func (f *fakeStore) Encrypted() bool                                        { return f.encrypted }
func (f *fakeStore) LoadDoc(_ context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.docs[name], nil
}
func (f *fakeStore) SaveDoc(_ context.Context, name string, plain []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[name] = append([]byte(nil), plain...)
	return nil
}

type fakeRegistry map[string]*clusters.Entry

func (r fakeRegistry) Get(name string) (*clusters.Entry, bool) { e, ok := r[name]; return e, ok }

func newRegistry(names ...string) fakeRegistry {
	r := fakeRegistry{}
	for _, n := range names {
		r[n] = &clusters.Entry{Name: n, RESTConfig: &rest.Config{Host: "https://" + n + ".example"}}
	}
	return r
}

type fakeGit struct {
	mu       sync.Mutex
	commit   string
	files    map[string][]byte
	fetchErr error
	checkErr error
	fetches  int
}

func (g *fakeGit) Check(context.Context, Source) error { return g.checkErr }
func (g *fakeGit) Fetch(context.Context, Source) (*Snapshot, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fetches++
	if g.fetchErr != nil {
		return nil, g.fetchErr
	}
	files := map[string][]byte{}
	for k, v := range g.files {
		files[k] = v
	}
	return &Snapshot{Commit: g.commit, Files: files}, nil
}

// fakeCluster is a tiny stand-in for the cluster an Applier talks to.
type fakeCluster struct {
	mu        sync.Mutex
	live      map[string]string // object key -> a "revision" of its content
	applies   int
	refreshes int
	failKinds map[string]string // kind -> error to return from Apply
	crdSeen   bool              // set once a CRD was applied; Widget needs it and a Refresh
	refreshed bool
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{live: map[string]string{}, failKinds: map[string]string{}}
}

func content(o *unstructured.Unstructured) string {
	b, _ := json.Marshal(o.Object)
	return string(b)
}

type fakeApplier struct{ c *fakeCluster }

func (a fakeApplier) Check(_ context.Context, o *unstructured.Unstructured) ObjectStatus {
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	st := ObjectStatus{ObjectRef: refOf(o)}
	cur, ok := a.c.live[objectKey(o)]
	switch {
	case !ok:
		st.State = StateMissing
	case cur != content(o):
		st.State = StateOutOfSync
	default:
		st.State = StateSynced
	}
	return st
}

func (a fakeApplier) Apply(_ context.Context, o *unstructured.Unstructured) error {
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	if msg := a.c.failKinds[o.GetKind()]; msg != "" {
		return errors.New(msg)
	}
	if o.GetKind() == "Widget" && !(a.c.crdSeen && a.c.refreshed) {
		return errors.New(`no matches for kind "Widget"`)
	}
	if o.GetKind() == "CustomResourceDefinition" {
		a.c.crdSeen = true
	}
	a.c.live[objectKey(o)] = content(o)
	a.c.applies++
	return nil
}

func (a fakeApplier) Refresh() {
	a.c.mu.Lock()
	defer a.c.mu.Unlock()
	a.c.refreshes++
	a.c.refreshed = true
}

type harness struct {
	m     *Manager
	git   *fakeGit
	cl    *fakeCluster
	store *fakeStore
	audit []map[string]any
	admin bool
	idErr error
}

func newHarness(t *testing.T, files map[string]string) *harness {
	t.Helper()
	h := &harness{git: &fakeGit{commit: "c0ffee1", files: map[string][]byte{}}, cl: newFakeCluster(), store: newFakeStore()}
	for k, v := range files {
		h.git.files[k] = []byte(v)
	}
	h.m = NewManager(h.store, newRegistry("dev", "prod"), h.git, func(action, name, identity string, detail map[string]any) {
		d := map[string]any{"action": action, "name": name, "identity": identity}
		for k, v := range detail {
			d[k] = v
		}
		h.audit = append(h.audit, d)
	})
	h.m.newApplier = func(*rest.Config, Source) (Applier, error) { return fakeApplier{h.cl}, nil }
	h.m.identify = func(context.Context, *clusters.Entry, string) (string, bool, error) {
		return "system:serviceaccount:apps:deployer", h.admin, h.idErr
	}
	// no m.Start: no background workers, the tests drive reconcile themselves
	return h
}

func (h *harness) add(t *testing.T, mutate func(*AddRequest)) SourceView {
	t.Helper()
	if err := h.m.SetMode(context.Background(), "dev", ModeNative); err != nil {
		t.Fatal(err)
	}
	req := AddRequest{Name: "apps", Cluster: "dev", RepoURL: "https://git.example/org/repo.git", Path: "k8s", AutoSync: true, ApplyToken: "sa-token-SECRET", GitToken: "git-token-SECRET"}
	if mutate != nil {
		mutate(&req)
	}
	v, err := h.m.AddSource(context.Background(), req, "alice")
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	return v
}

func (h *harness) rt(t *testing.T, id string) *runtime {
	t.Helper()
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	rt := h.m.srcs[id]
	if rt == nil {
		t.Fatal("no such source")
	}
	return rt
}

func (h *harness) reconcile(t *testing.T, id, manualBy string) SourceView {
	t.Helper()
	rt := h.rt(t, id)
	h.m.reconcile(context.Background(), rt, manualBy)
	return rt.view()
}

const cmYAML = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app-config\n  namespace: web\ndata:\n  k: v1\n"
const nsYAML = "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: web\n"

// ---- manifests -----------------------------------------------------------------

func TestParseManifests(t *testing.T) {
	files := map[string][]byte{
		"k8s/b.yaml": []byte("---\n" + cmYAML + "---\n# only a comment\n---\n" + nsYAML),
		"k8s/a.json": []byte(`{"apiVersion":"v1","kind":"Service","metadata":{"name":"svc","namespace":"web"}}`),
		"k8s/c.yaml": []byte("apiVersion: v1\nkind: List\nitems:\n- apiVersion: v1\n  kind: ConfigMap\n  metadata: {name: one}\n- apiVersion: v1\n  kind: ConfigMap\n  metadata: {name: two}\n"),
		"k8s/d.yaml": []byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"),
	}
	objs, err := ParseManifests(files)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, o := range objs {
		kinds = append(kinds, o.GetKind()+"/"+o.GetName())
	}
	// namespaces first, CRDs second, the rest in file order
	want := "Namespace/web CustomResourceDefinition/widgets.example.com Service/svc ConfigMap/app-config ConfigMap/one ConfigMap/two"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("order:\n got  %s\n want %s", got, want)
	}
}

func TestParseManifestsRejects(t *testing.T) {
	cases := map[string]string{
		"no kind":       "metadata:\n  name: x\n",
		"no name":       "apiVersion: v1\nkind: ConfigMap\nmetadata: {}\n",
		"generateName":  "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  generateName: x-\n",
		"bad yaml":      "apiVersion: [unclosed\n",
		"duplicate":     cmYAML + "---\n" + cmYAML,
		"not an object": "- a\n- b\n",
	}
	for name, doc := range cases {
		if _, err := ParseManifests(map[string][]byte{"x.yaml": []byte(doc)}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// the same object in two files is also a duplicate
	if _, err := ParseManifests(map[string][]byte{"a.yaml": []byte(cmYAML), "b.yaml": []byte(cmYAML)}); err == nil {
		t.Error("an object defined in two files was accepted")
	}
}

// ---- validation ----------------------------------------------------------------

func TestValidate(t *testing.T) {
	ok := AddRequest{Name: "apps", Cluster: "dev", RepoURL: "https://github.com/o/r", ApplyToken: "t"}
	if r := ok; validate(&r) != nil || r.IntervalSeconds != defaultInterval {
		t.Fatalf("a valid request was rejected: %v", validate(&r))
	}
	bad := map[string]func(*AddRequest){
		"http url":        func(r *AddRequest) { r.RepoURL = "http://github.com/o/r" },
		"file url":        func(r *AddRequest) { r.RepoURL = "file:///etc" },
		"ssh url":         func(r *AddRequest) { r.RepoURL = "ssh://git@github.com/o/r" },
		"credentials":     func(r *AddRequest) { r.RepoURL = "https://user:pw@github.com/o/r" },
		"query":           func(r *AddRequest) { r.RepoURL = "https://github.com/o/r?x=1" },
		"no host":         func(r *AddRequest) { r.RepoURL = "https://" },
		"dash revision":   func(r *AddRequest) { r.Revision = "--upload-pack=x" },
		"dotdot revision": func(r *AddRequest) { r.Revision = "a..b" },
		"path traversal":  func(r *AddRequest) { r.Path = "../../etc" },
		"bad namespace":   func(r *AddRequest) { r.Namespace = "Bad_NS" },
		"short interval":  func(r *AddRequest) { r.IntervalSeconds = 5 },
		"long interval":   func(r *AddRequest) { r.IntervalSeconds = 99999 },
		"no token":        func(r *AddRequest) { r.ApplyToken = " " },
		"empty name":      func(r *AddRequest) { r.Name = "" },
		"weird name":      func(r *AddRequest) { r.Name = "a;rm -rf" },
		"bad git user":    func(r *AddRequest) { r.GitUsername = "a b" },
	}
	for name, mutate := range bad {
		r := ok
		mutate(&r)
		if err := validate(&r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// plain http is only for local development, and only when asked for
	AllowInsecureGit = true
	defer func() { AllowInsecureGit = false }()
	r := ok
	r.RepoURL = "http://127.0.0.1:9000/repo.git"
	if err := validate(&r); err != nil {
		t.Errorf("dev flag: %v", err)
	}
}

// ---- reconcile -----------------------------------------------------------------

func TestAutoSyncAppliesAndStaysQuiet(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/ns.yaml": nsYAML, "k8s/cm.yaml": cmYAML})
	v := h.add(t, nil)

	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseSynced || got.Status.Revision != "c0ffee1" || got.Status.Applied != "c0ffee1" || got.Status.SyncedAt == nil {
		t.Fatalf("after first poll: %+v", got.Status)
	}
	if h.cl.applies != 2 || len(got.Status.Objects) != 2 {
		t.Fatalf("applied %d objects, status lists %d", h.cl.applies, len(got.Status.Objects))
	}
	if len(h.audit) != 1 || h.audit[0]["action"] != "gitops-apply" || h.audit[0]["applied"] != 2 {
		t.Fatalf("audit = %v", h.audit)
	}

	// nothing changed: a second poll applies and audits nothing
	h.reconcile(t, v.ID, "")
	if h.cl.applies != 2 || len(h.audit) != 1 {
		t.Errorf("a no-op poll wrote: applies=%d audits=%d", h.cl.applies, len(h.audit))
	}

	// a new commit changes the ConfigMap: only that object is applied again
	h.git.files["k8s/cm.yaml"] = []byte(strings.Replace(cmYAML, "v1", "v2", 1))
	h.git.commit = "c0ffee2"
	got = h.reconcile(t, v.ID, "")
	if h.cl.applies != 3 || got.Status.Revision != "c0ffee2" || got.Status.Phase != PhaseSynced {
		t.Errorf("after a new commit: applies=%d status=%+v", h.cl.applies, got.Status)
	}
}

func TestDriftIsRevertedWhenAutoSync(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	v := h.add(t, nil)
	h.reconcile(t, v.ID, "")
	for k := range h.cl.live {
		h.cl.live[k] = "someone edited this by hand"
	}
	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseSynced || h.cl.applies != 2 {
		t.Errorf("drift not corrected: applies=%d %+v", h.cl.applies, got.Status)
	}
}

func TestWithoutAutoSyncOnlySyncNowApplies(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	v := h.add(t, func(r *AddRequest) { r.AutoSync = false })

	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseOutOfSync || h.cl.applies != 0 || got.Status.Objects[0].State != StateMissing {
		t.Fatalf("a scheduled poll must only report: applies=%d %+v", h.cl.applies, got.Status)
	}
	if !strings.Contains(got.Status.Message, "auto-sync is off") {
		t.Errorf("message = %q", got.Status.Message)
	}
	got = h.reconcile(t, v.ID, "alice")
	if got.Status.Phase != PhaseSynced || h.cl.applies != 1 {
		t.Fatalf("Sync now should apply: applies=%d %+v", h.cl.applies, got.Status)
	}
	if h.audit[len(h.audit)-1]["identity"] != "alice" || h.audit[len(h.audit)-1]["manual"] != true {
		t.Errorf("a manual sync must be audited as the person who clicked: %v", h.audit)
	}
}

func TestApplyFailureIsReported(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML, "k8s/ns.yaml": nsYAML})
	v := h.add(t, nil)
	h.cl.failKinds["ConfigMap"] = `configmaps "app-config" is forbidden: User "sa" cannot create resource "configmaps"`
	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseError || !strings.Contains(got.Status.Message, "1 of 2") {
		t.Fatalf("status = %+v", got.Status)
	}
	var found bool
	for _, o := range got.Status.Objects {
		if o.Kind == "ConfigMap" && o.State == StateError && strings.Contains(o.Message, "forbidden") {
			found = true
		}
	}
	if !found {
		t.Errorf("the RBAC refusal wasn't attached to its object: %+v", got.Status.Objects)
	}
	if last := h.audit[len(h.audit)-1]; last["failed"] != 1 || last["applied"] != 1 {
		t.Errorf("audit = %v", last)
	}
}

func TestCustomResourceFollowsItsCRDInOneSync(t *testing.T) {
	crd := "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: widgets.example.com\n"
	widget := "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: w1\n  namespace: web\n"
	// the Widget file sorts before the CRD file, but the CRD must still go first
	h := newHarness(t, map[string]string{"k8s/a-widget.yaml": widget, "k8s/z-crd.yaml": crd})
	v := h.add(t, nil)
	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseSynced || h.cl.refreshes == 0 {
		t.Fatalf("status = %+v (refreshes=%d)", got.Status, h.cl.refreshes)
	}
}

func TestGitAndManifestErrorsKeepTheLastObjects(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	v := h.add(t, nil)
	h.reconcile(t, v.ID, "")

	h.git.fetchErr = errors.New("the repository needs a (valid) access token")
	got := h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseError || !strings.HasPrefix(got.Status.Message, "git: ") || len(got.Status.Objects) != 1 {
		t.Errorf("git failure: %+v", got.Status)
	}
	h.git.fetchErr = nil
	h.git.files["k8s/cm.yaml"] = []byte("not: a manifest")
	got = h.reconcile(t, v.ID, "")
	if got.Status.Phase != PhaseError || !strings.HasPrefix(got.Status.Message, "manifests: ") {
		t.Errorf("manifest failure: %+v", got.Status)
	}
}

func TestOrphansAreListedButNeverDeleted(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML, "k8s/ns.yaml": nsYAML})
	v := h.add(t, nil)
	h.reconcile(t, v.ID, "")
	delete(h.git.files, "k8s/cm.yaml")
	got := h.reconcile(t, v.ID, "")
	if len(got.Status.Orphans) != 1 || got.Status.Orphans[0].Name != "app-config" {
		t.Fatalf("orphans = %+v", got.Status.Orphans)
	}
	if _, still := h.cl.live["v1 ConfigMap web/app-config"]; !still {
		t.Error("an object removed from git was deleted from the cluster — native mode must never prune")
	}
}

// ---- source management -----------------------------------------------------------

func TestAddSourceChecks(t *testing.T) {
	ctx := context.Background()
	req := AddRequest{Name: "apps", Cluster: "dev", RepoURL: "https://git.example/r", ApplyToken: "tok"}

	h := newHarness(t, nil)
	if _, err := h.m.AddSource(ctx, req, "a"); !errors.Is(err, ErrConflict) {
		t.Errorf("a source on a cluster that isn't in native mode: %v", err)
	}
	h.m.SetMode(ctx, "dev", ModeNative)
	if _, err := h.m.AddSource(ctx, AddRequest{Name: "x", Cluster: "ghost", RepoURL: req.RepoURL, ApplyToken: "t"}, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cluster: %v", err)
	}

	h.idErr = errors.New("unauthorized")
	if _, err := h.m.AddSource(ctx, req, "a"); !errors.Is(err, ErrInvalid) || len(h.m.Sources("dev")) != 0 {
		t.Errorf("a rejected ServiceAccount token must store nothing: %v", err)
	}
	h.idErr = nil
	h.git.checkErr = errors.New("repository not found")
	if _, err := h.m.AddSource(ctx, req, "a"); !errors.Is(err, ErrInvalid) || len(h.m.Sources("dev")) != 0 {
		t.Errorf("an unreachable repository must store nothing: %v", err)
	}
	h.git.checkErr = nil

	h.admin = true
	v, err := h.m.AddSource(ctx, req, "a")
	if err != nil {
		t.Fatal(err)
	}
	if v.ApplyAs != "system:serviceaccount:apps:deployer" || len(v.Warnings) != 1 || !strings.Contains(v.Warnings[0], "cluster-admin") {
		t.Errorf("view = %+v", v)
	}
	if _, err := h.m.AddSource(ctx, req, "a"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: %v", err)
	}
}

func TestSecretsNeverLeaveTheManager(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	v := h.add(t, nil)
	h.reconcile(t, v.ID, "")

	out, _ := json.Marshal(h.m.Sources("dev"))
	rt := h.rt(t, v.ID)
	everything := string(out) + fmt.Sprintf("%v %+v %#v %s", rt.src, rt.src, rt.src, rt.src.ApplyToken) + fmt.Sprint(h.audit)
	srcJSON, _ := json.Marshal(rt.src)
	everything += string(srcJSON)
	for _, secret := range []string{"sa-token-SECRET", "git-token-SECRET"} {
		if strings.Contains(everything, secret) {
			t.Errorf("%q leaked through a view, a format verb, JSON or the audit trail", secret)
		}
	}
	if !v.HasGitToken {
		t.Error("the view should say a git token is set")
	}
}

func TestPersistenceAndEncryptionRule(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	v := h.add(t, nil)

	// a second manager over the same store gets the source (and its tokens) back
	m2 := NewManager(h.store, newRegistry("dev"), h.git, nil)
	if err := m2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m2.mu.Lock()
		for _, rt := range m2.srcs {
			m2.stopLocked(rt)
		}
		m2.mu.Unlock()
	})
	got := m2.Sources("dev")
	if len(got) != 1 || got[0].ID != v.ID || m2.Mode("dev") != ModeNative {
		t.Fatalf("not restored: %+v mode=%s", got, m2.Mode("dev"))
	}
	if m2.srcs[v.ID].src.ApplyToken.Reveal() != "sa-token-SECRET" {
		t.Error("the apply token wasn't restored")
	}

	// a store that can't encrypt must not be given credentials
	h2 := newHarness(t, nil)
	h2.store.encrypted = false
	h2.m.SetMode(context.Background(), "dev", ModeNative)
	_, err := h2.m.AddSource(context.Background(), AddRequest{Name: "a", Cluster: "dev", RepoURL: "https://git.example/r", ApplyToken: "tok"}, "a")
	if !errors.Is(err, ErrUnencrypted) || len(h2.m.Sources("dev")) != 0 || len(h2.store.docs["gitops"]) != 0 && strings.Contains(string(h2.store.docs["gitops"]), "tok") {
		t.Errorf("credentials were stored without encryption: %v", err)
	}
}

func TestModeGatingAndRemoval(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	if h.m.Mode("dev") != ModeOff {
		t.Error("the default mode must be off")
	}
	if err := h.m.SetMode(ctx, "dev", Mode("bogus")); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad mode: %v", err)
	}
	if err := h.m.SetMode(ctx, "ghost", ModeNative); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown cluster: %v", err)
	}
	v := h.add(t, nil)
	if _, err := h.m.SyncNow(v.ID, "alice"); !errors.Is(err, ErrConflict) {
		t.Errorf("Sync now with no worker running: %v", err) // the harness runs no workers
	}
	if _, err := h.m.SyncNow("nope", "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown source: %v", err)
	}

	h.m.SetMode(ctx, "dev", ModeOff)
	if len(h.m.Sources("dev")) != 1 {
		t.Error("switching a cluster to off must keep its sources configured")
	}
	if _, err := h.m.RemoveSource(ctx, v.ID); err != nil || len(h.m.Sources("dev")) != 0 {
		t.Errorf("remove: %v", err)
	}
	if _, err := h.m.RemoveSource(ctx, v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v", err)
	}

	h.m.SetMode(ctx, "dev", ModeNative)
	h.m.AddSource(ctx, AddRequest{Name: "again", Cluster: "dev", RepoURL: "https://git.example/r", ApplyToken: "t"}, "a")
	h.m.ForgetCluster(ctx, "dev")
	if len(h.m.Sources("dev")) != 0 || h.m.Mode("dev") != ModeOff {
		t.Error("ForgetCluster left configuration behind")
	}
}

func TestWorkerSyncsInTheBackground(t *testing.T) {
	h := newHarness(t, map[string]string{"k8s/cm.yaml": cmYAML})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	v := h.add(t, func(r *AddRequest) { r.IntervalSeconds = 30 })

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := h.m.Source(v.ID); got.Status.Phase == PhaseSynced {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, _ := h.m.Source(v.ID)
	t.Fatalf("the worker never synced: %+v", got.Status)
}
