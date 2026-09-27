// Package integration holds Roadmap Phase 23's integration test suite —
// real HTTP requests against a real, already-running Vinaire backend
// (started separately, see .github/workflows/ci.yml) pointed at a real,
// disposable k3d cluster with testdata/manifests.yaml applied to it. This
// deliberately never mocks client-go: the whole point is catching
// regressions a mocked-Kubernetes unit test can't (a wrong namespace, a
// broken selector match, a field that stopped round-tripping).
//
// Excluded from every normal `go build`/`go test ./...` by the build tag
// below — run explicitly via `go test -tags=integration ./integration/...`
// once VINAIRE_BASE_URL (default http://localhost:8080) is reachable and
// AUTH_MODE=disabled on that backend (no login step needed for this
// suite — Phase 10's auth flow itself isn't what's under test here).
//
//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shiraku/vinaire/backend/internal/api"
)

// testNamespace/testDeployment etc. must match testdata/manifests.yaml
// exactly — every test below asserts against these specific names, not
// just "some pod exists somewhere".
const (
	testNamespace   = "vinaire-ci-test"
	testDeployment  = "demo-deployment"
	testStatefulSet = "demo-statefulset"
	testDaemonSet   = "demo-daemonset"
	testJob         = "demo-job"
	testConfigMap   = "demo-configmap"
	testSecret      = "demo-secret"
	testPVC         = "demo-pvc"
	testIngress     = "demo-ingress"
)

func baseURL() string {
	if v := os.Getenv("VINAIRE_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

// get GETs path against the backend under test and decodes the JSON body
// into out. Fails the test immediately on a transport error or non-200.
func get(t *testing.T, path string, out any) {
	t.Helper()
	url := baseURL() + path
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body: %s", url, resp.StatusCode, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			t.Fatalf("GET %s: decode response: %v, body: %s", url, err, body)
		}
	}
}

// post POSTs an empty body to path and returns the raw status code — used
// for the one write action this suite exercises (restart).
func post(t *testing.T, path string) int {
	t.Helper()
	url := baseURL() + path
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// eventually polls check every 500ms until it returns true or timeout
// elapses, then fails the test — for assertions that depend on the
// cluster reaching a state asynchronously (a Deployment's pod actually
// scheduling and going Ready, a Job completing).
func eventually(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !check() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

func TestHealthz(t *testing.T) {
	var out struct {
		Status string `json:"status"`
	}
	get(t, "/healthz", &out)
	if out.Status != "ok" {
		t.Errorf("expected status ok, got %q", out.Status)
	}
}

func findRow(rows []api.WorkloadRow, name string) *api.WorkloadRow {
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}

func TestPodsListAndNamespaceFilter(t *testing.T) {
	var rows []api.WorkloadRow
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/pods?namespace="+testNamespace, &rows)
		for _, r := range rows {
			if r.Namespace == testNamespace {
				return true
			}
		}
		return false
	})

	// This is the DoD's own "wrong namespace" regression: every row this
	// endpoint returns, when filtered, must actually belong to the
	// requested namespace — not silently include (or omit) pods from
	// elsewhere in the cluster (e.g. kube-system, always present on any
	// real k3d cluster).
	for _, r := range rows {
		if r.Namespace != testNamespace {
			t.Errorf("namespace filter leaked a pod from %q: %s", r.Namespace, r.Name)
		}
	}

	var allRows []api.WorkloadRow
	get(t, "/api/v1/pods", &allRows)
	sawOtherNamespace := false
	for _, r := range allRows {
		if r.Namespace == "kube-system" {
			sawOtherNamespace = true
			break
		}
	}
	if !sawOtherNamespace {
		t.Error("expected the unfiltered pod list to include kube-system pods (sanity check that filtering isn't a no-op)")
	}
}

func TestDeploymentsList(t *testing.T) {
	var rows []api.WorkloadRow
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/deployments?namespace="+testNamespace, &rows)
		row := findRow(rows, testDeployment)
		return row != nil && row.StatusColor == "ok"
	})
}

func TestStatefulSetsList(t *testing.T) {
	var rows []api.WorkloadRow
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/statefulsets?namespace="+testNamespace, &rows)
		return findRow(rows, testStatefulSet) != nil
	})
}

func TestDaemonSetsList(t *testing.T) {
	var rows []api.WorkloadRow
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/daemonsets?namespace="+testNamespace, &rows)
		return findRow(rows, testDaemonSet) != nil
	})
}

func TestJobsList(t *testing.T) {
	var rows []api.WorkloadRow
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/jobs?namespace="+testNamespace, &rows)
		return findRow(rows, testJob) != nil
	})
}

func TestConfigMapsList(t *testing.T) {
	var rows []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	get(t, "/api/v1/configmaps?namespace="+testNamespace, &rows)
	found := false
	for _, r := range rows {
		if r.Name == testConfigMap {
			found = true
		}
	}
	if !found {
		t.Errorf("expected configmap %q in namespace %q", testConfigMap, testNamespace)
	}
}

// TestSecretsListNeverLeaksValues is the core-rule assertion for Secrets:
// the list endpoint must be metadata-only — no decoded value anywhere in
// the response body, regardless of what field names change under it.
func TestSecretsListNeverLeaksValues(t *testing.T) {
	var body []byte
	resp, err := http.Get(baseURL() + "/api/v1/secrets?namespace=" + testNamespace)
	if err != nil {
		t.Fatalf("GET secrets: %v", err)
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET secrets: status %d, body: %s", resp.StatusCode, body)
	}

	var rows []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode secrets response: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Name == testSecret {
			found = true
		}
	}
	if !found {
		t.Errorf("expected secret %q in namespace %q", testSecret, testNamespace)
	}
	if bytesContain(body, "super-secret-value") {
		t.Error("secrets list response leaked the plaintext secret value — must be metadata-only")
	}
}

func bytesContain(haystack []byte, needle string) bool {
	return len(needle) > 0 && stringIndex(string(haystack), needle) >= 0
}

func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestNodesList(t *testing.T) {
	var rows []api.NodeRow
	get(t, "/api/v1/nodes", &rows)
	if len(rows) == 0 {
		t.Error("expected at least one node")
	}
}

func TestStorageList(t *testing.T) {
	var rows []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	get(t, "/api/v1/pvcs?namespace="+testNamespace, &rows)
	found := false
	for _, r := range rows {
		if r.Name == testPVC {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PVC %q in namespace %q", testPVC, testNamespace)
	}
}

func TestIngressesList(t *testing.T) {
	var rows []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	get(t, "/api/v1/ingresses?namespace="+testNamespace, &rows)
	found := false
	for _, r := range rows {
		if r.Name == testIngress {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ingress %q in namespace %q", testIngress, testNamespace)
	}
}

func TestSearch(t *testing.T) {
	var results []api.SearchResult
	eventually(t, 30*time.Second, func() bool {
		get(t, "/api/v1/search?q=demo-deployment", &results)
		return len(results) > 0
	})
	found := false
	for _, r := range results {
		if r.Name == testDeployment {
			found = true
		}
	}
	if !found {
		t.Errorf("expected search for %q to return it", testDeployment)
	}
}

func TestCapacity(t *testing.T) {
	var resp api.CapacityResponse
	get(t, "/api/v1/capacity", &resp)
	if len(resp.Nodes) == 0 {
		t.Error("expected at least one node in capacity response")
	}
	if resp.Cluster.Pods.Total == "" {
		t.Error("expected a non-empty cluster pod capacity total")
	}
}

func TestDashboardSummary(t *testing.T) {
	var resp api.DashboardSummaryResponse
	get(t, "/api/v1/dashboard/summary", &resp)
	found := false
	for _, ns := range resp.Namespaces {
		if ns.Name == testNamespace {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dashboard summary's namespace breakdown to include %q", testNamespace)
	}
}

func TestPodDetail(t *testing.T) {
	var rows []api.WorkloadRow
	var podName string
	eventually(t, 60*time.Second, func() bool {
		get(t, "/api/v1/pods?namespace="+testNamespace, &rows)
		for _, r := range rows {
			if strings.HasPrefix(r.Name, testDeployment+"-") {
				podName = r.Name
				return true
			}
		}
		return false
	})
	if podName == "" {
		t.Fatal("no pod found to fetch detail for")
	}

	var detail struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	get(t, fmt.Sprintf("/api/v1/pods/%s/%s", testNamespace, podName), &detail)
	if detail.Name != podName || detail.Namespace != testNamespace {
		t.Errorf("pod detail mismatch: got %s/%s, want %s/%s", detail.Namespace, detail.Name, testNamespace, podName)
	}
}

func TestDeploymentDetailAndRestart(t *testing.T) {
	var detail struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Pods      []any  `json:"pods"`
	}
	get(t, fmt.Sprintf("/api/v1/deployments/%s/%s", testNamespace, testDeployment), &detail)
	if detail.Name != testDeployment {
		t.Fatalf("expected deployment name %q, got %q", testDeployment, detail.Name)
	}

	// The one write action this suite exercises end-to-end — restart is
	// the least destructive of Phase 9's three (a rolling patch, not a
	// delete), and this is a disposable CI-only cluster.
	status := post(t, fmt.Sprintf("/api/v1/deployments/%s/%s/restart", testNamespace, testDeployment))
	if status != http.StatusOK && status != http.StatusNoContent {
		t.Errorf("restart: expected 200/204, got %d", status)
	}
}

func TestTopology(t *testing.T) {
	var resp struct {
		Nodes []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"nodes"`
	}
	get(t, "/api/v1/topology?namespace="+testNamespace, &resp)
	found := false
	for _, n := range resp.Nodes {
		if n.Kind == "deployment" && n.Name == testDeployment {
			found = true
		}
	}
	if !found {
		t.Errorf("expected topology graph for %q to include the %q deployment node", testNamespace, testDeployment)
	}
}
