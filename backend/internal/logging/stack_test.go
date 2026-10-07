package logging

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestOptionsAreNormalizedAndValidated(t *testing.T) {
	o, err := Options{}.Normalize()
	if err != nil || o.Namespace != "logging" || o.RetentionDay != 7 || o.StorageSize != "10Gi" {
		t.Errorf("defaults: %+v %v", o, err)
	}
	for name, bad := range map[string]Options{
		"namespace with a dot":    {Namespace: "a.b"},
		"namespace traversal":     {Namespace: "../x"},
		"retention too long":      {RetentionDay: 4000},
		"negative retention":      {RetentionDay: -1},
		"size without a unit":     {StorageSize: "10"},
		"size with yaml injected": {StorageSize: "10Gi\nfoo: bar"},
		"class with a newline":    {StorageClass: "a\nb"},
		"class with spaces":       {StorageClass: "a b"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := (Options{StorageSize: "none"}).Normalize(); err != nil {
		t.Errorf("'none' (no volume) refused: %v", err)
	}
}

func TestGeneratedValuesAreValidYAMLWithTheChosenOptions(t *testing.T) {
	o, _ := Options{Namespace: "obs", RetentionDay: 14, StorageSize: "20Gi", StorageClass: "fast"}.Normalize()
	var loki map[string]any
	if err := yaml.Unmarshal([]byte(LokiValues(o)), &loki); err != nil {
		t.Fatalf("Loki values aren't YAML: %v\n%s", err, LokiValues(o))
	}
	if loki["deploymentMode"] != "SingleBinary" {
		t.Errorf("deploymentMode = %v", loki["deploymentMode"])
	}
	v := LokiValues(o)
	for _, want := range []string{"retention_period: 336h", "size: 20Gi", "storageClass: fast", "auth_enabled: false"} {
		if !strings.Contains(v, want) {
			t.Errorf("Loki values lack %q", want)
		}
	}
	noVol, _ := Options{StorageSize: "none"}.Normalize()
	var l2 map[string]any
	if err := yaml.Unmarshal([]byte(LokiValues(noVol)), &l2); err != nil || strings.Contains(LokiValues(noVol), "storageClass") {
		t.Errorf("no-volume values: %v", err)
	}

	var alloy map[string]any
	if err := yaml.Unmarshal([]byte(AlloyValues(o)), &alloy); err != nil {
		t.Fatalf("Alloy values aren't YAML: %v", err)
	}
	if !strings.Contains(AlloyValues(o), "http://loki.obs.svc.cluster.local:3100/loki/api/v1/push") {
		t.Error("Alloy doesn't push to the Loki in the chosen namespace")
	}
	for _, label := range []string{`"namespace"`, `"pod"`, `"container"`} {
		if !strings.Contains(AlloyValues(o), "target_label  = "+label) {
			t.Errorf("Alloy doesn't set the %s label the search needs", label)
		}
	}
}

func TestPlanOrderAndPins(t *testing.T) {
	o, _ := Options{}.Normalize()
	plan := Plan(o)
	if len(plan) != 2 || plan[0].Chart != "loki" || plan[1].Chart != "alloy" {
		t.Fatalf("plan = %+v (Loki must come first, Alloy needs somewhere to push)", plan)
	}
	for _, s := range plan {
		if s.Version == "" || s.Version == "latest" || s.Repo != "grafana" || !s.Wait {
			t.Errorf("step %s isn't pinned/waiting: %+v", s.Chart, s)
		}
	}
}

func TestRecognisingLokiAndCollectors(t *testing.T) {
	good := map[string]string{"app.kubernetes.io/name": "loki"}
	if p, ok := IsLokiService(good, "10.0.0.1", []int32{9095, 3100}); !ok || p != 3100 {
		t.Errorf("direct service: %v %v", p, ok)
	}
	if p, ok := IsLokiService(good, "10.0.0.1", []int32{80}); !ok || p != 80 {
		t.Errorf("gateway service: %v %v", p, ok)
	}
	if _, ok := IsLokiService(good, "None", []int32{3100}); ok {
		t.Error("a headless service was taken")
	}
	if _, ok := IsLokiService(map[string]string{"app.kubernetes.io/name": "loki", "app.kubernetes.io/component": "memberlist"}, "10.0.0.1", []int32{7946, 3100}); ok {
		t.Error("the memberlist service was taken")
	}
	if _, ok := IsLokiService(map[string]string{"app.kubernetes.io/name": "grafana"}, "10.0.0.1", []int32{3100}); ok {
		t.Error("a non-Loki service was taken")
	}
	if !IsCollector(map[string]string{"app.kubernetes.io/name": "promtail"}) || IsCollector(map[string]string{"app.kubernetes.io/name": "nginx"}) {
		t.Error("collector detection")
	}
}
