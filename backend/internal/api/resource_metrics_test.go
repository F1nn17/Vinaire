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
			if strings.Count(q.promql, " or ") != 3 {
				t.Errorf("%s: want four alternatives joined by or: %q", typ, q.promql)
			}
		}
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
