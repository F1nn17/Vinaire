package api

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// Roadmap Phase 61: a node's series name the node in different labels depending on how
// Prometheus scrapes the kubelet; the queries must try each.
func TestNodeQueriesTryEveryWayANodeIsNamed(t *testing.T) {
	for _, typ := range []string{"cpu", "memory", "network", "filesystem"} {
		plan, ok := planFor("nodes", "", "k3d-vinaire-dev-server-0", typ)
		if !ok {
			t.Fatalf("%s: no plan", typ)
		}
		for _, q := range plan.queries {
			for _, label := range []string{`node="k3d-vinaire-dev-server-0"`, `instance=~"k3d-vinaire-dev-server-0(:[0-9]+)?"`, `kubernetes_io_hostname="k3d-vinaire-dev-server-0"`, `nodename="k3d-vinaire-dev-server-0"`} {
				if !strings.Contains(q.promql, label) {
					t.Errorf("%s: %q lacks %s", typ, q.promql, label)
				}
			}
			// the root cgroup's four alternatives come first, then the pod-level fallback, which counts
			// only when the root has nothing (kube-prometheus-stack drops `id="/"`)
			root, rest, found := strings.Cut(q.promql, " or ((")
			if !found || strings.Count(root, " or ") != 3 || !strings.Contains(root, `id="/"`) {
				t.Errorf("%s: want four root alternatives, then the pod-level fallback: %q", typ, q.promql)
			}
			if !strings.Contains(rest, ") and on() absent(") || strings.Contains(rest, `id="/",`+"node=") && !strings.Contains(rest, "absent(") {
				t.Errorf("%s: the fallback must apply only when the root cgroup has no series: %q", typ, q.promql)
			}
			fb, _, _ := strings.Cut(rest, ") and on() absent(")
			if strings.Contains(fb, `id="/"`) {
				t.Errorf("%s: the fallback must not ask for the root cgroup: %q", typ, fb)
			}
		}
	}
}

// kube-prometheus-stack drops every cgroup series without a pod, so a node's and the cluster's usage
// is built from pods there: the fallback sums containers (CPU, memory) or pods (network, disk).
func TestNodeAndClusterQueriesFallBackToPodsWhenTheRootCgroupIsNotStored(t *testing.T) {
	for _, kind := range []string{"nodes", "cluster"} {
		for typ, want := range map[string]string{"cpu": `container!="",container!="POD"`, "memory": `container!="",container!="POD"`, "network": `pod!=""`, "filesystem": `pod!=""`} {
			plan, _ := planFor(kind, "", "n1", typ)
			for _, q := range plan.queries {
				_, rest, _ := strings.Cut(q.promql, " or ((")
				if !strings.Contains(rest, want) {
					t.Errorf("%s/%s: fallback lacks %s: %q", kind, typ, want, q.promql)
				}
			}
		}
	}
	// pods and workloads need no fallback
	if plan, _ := planFor("pods", "app", "web", "cpu"); strings.Contains(plan.queries[0].promql, "absent(") {
		t.Errorf("a pod query has no root cgroup to fall back from")
	}
}

func TestNodeNamesWithDotsAreQuotedInTheRegexAlternative(t *testing.T) {
	plan, _ := planFor("nodes", "", "worker-1.example.com", "cpu")
	if !strings.Contains(plan.queries[0].promql, `instance=~"worker-1\\.example\\.com(:[0-9]+)?"`) {
		t.Errorf("dots must not match any character: %q", plan.queries[0].promql)
	}
}

func TestPodAndDeploymentQueriesAreUnchanged(t *testing.T) {
	for _, kind := range []string{"pods", "deployments"} {
		plan, _ := planFor(kind, "app", "web", "cpu")
		if strings.Contains(plan.queries[0].promql, " or ") {
			t.Errorf("%s: a single selector needs no alternatives: %q", kind, plan.queries[0].promql)
		}
	}
}

func fetchReturning(body string, err error) promFetch {
	return func(context.Context, string, url.Values) ([]byte, error) { return []byte(body), err }
}

func TestExplainNoDataSaysWhatIsMissing(t *testing.T) {
	none := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	some := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"37"]}]}}`
	if got := explainNoData(context.Background(), fetchReturning(none, nil), "nodes"); !strings.Contains(got, "isn't scraping the kubelet") {
		t.Errorf("no container metrics at all: %q", got)
	}
	if got := explainNoData(context.Background(), fetchReturning(some, nil), "nodes"); !strings.Contains(got, "none it can tie to this node") {
		t.Errorf("metrics exist but not for this node: %q", got)
	}
	if got := explainNoData(context.Background(), fetchReturning(some, nil), "pods"); !strings.Contains(got, "may not be exported") {
		t.Errorf("a pod with no data keeps the general message: %q", got)
	}
	if got := explainNoData(context.Background(), fetchReturning("", errors.New("down")), "nodes"); !strings.Contains(got, "may not be exported") {
		t.Errorf("a failed probe falls back to the general message: %q", got)
	}
}

// Roadmap Phase 66: the pods of a StatefulSet, a DaemonSet and a Job are told apart by name.
func TestWorkloadMetricsMatchTheirOwnPodNames(t *testing.T) {
	cases := map[string]string{
		"statefulsets": `pod=~"^db-[0-9]+$"`,
		"daemonsets":   `pod=~"^db-[a-z0-9]{5}$"`,
		"jobs":         `pod=~"^db-[a-z0-9]{5}$"`,
	}
	for kind, want := range cases {
		plan, ok := planFor(kind, "app", "db", "cpu")
		if !ok {
			t.Fatalf("%s: no plan", kind)
		}
		q := plan.queries[0].promql
		if !strings.Contains(q, want) || !strings.Contains(q, `namespace="app"`) {
			t.Errorf("%s: %q lacks %s", kind, q, want)
		}
	}
	// another workload whose name merely starts with this one's must not match
	plan, _ := planFor("statefulsets", "app", "db", "cpu")
	if strings.Contains(plan.queries[0].promql, `db-.*`) {
		t.Errorf("too loose: %s", plan.queries[0].promql)
	}
}

func TestClusterPlanSumsTheRootCgroupOfEveryNode(t *testing.T) {
	for _, typ := range []string{"cpu", "memory", "network", "filesystem"} {
		plan, ok := planFor("cluster", "", "cluster", typ)
		if !ok || len(plan.queries) == 0 {
			t.Fatalf("%s: no plan", typ)
		}
		for _, q := range plan.queries {
			if !strings.Contains(q.promql, `id="/"`) || strings.Contains(q.promql, "node=") {
				t.Errorf("%s: %s must select every node's root cgroup and no single node", typ, q.promql)
			}
		}
	}
}

func TestPromInstantReadsASumAndRefusesNothingOrNaN(t *testing.T) {
	ok := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"123.5"]}]}}`
	if v, got := promInstant(context.Background(), fetchReturning(ok, nil), "sum(x)"); !got || v != 123.5 {
		t.Errorf("a sum: %v %v", v, got)
	}
	for name, body := range map[string]string{
		"nothing": `{"status":"success","data":{"resultType":"vector","result":[]}}`,
		"nan":     `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"NaN"]}]}}`,
		"error":   `{"status":"error"}`,
	} {
		if _, got := promInstant(context.Background(), fetchReturning(body, nil), "sum(x)"); got {
			t.Errorf("%s must not count as an answer", name)
		}
	}
	if _, got := promInstant(context.Background(), fetchReturning("", errors.New("down")), "sum(x)"); got {
		t.Error("a failing Prometheus is no answer")
	}
}
