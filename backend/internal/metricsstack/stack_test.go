package metricsstack

import (
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestNormalizeDefaultsAndRejectsInjection(t *testing.T) {
	o, err := Options{}.Normalize()
	if err != nil || o.Namespace != "monitoring" || o.RetentionDay != 7 || o.StorageSize != "10Gi" {
		t.Fatalf("defaults: %+v %v", o, err)
	}
	for _, bad := range []Options{
		{Namespace: "Bad_NS"},
		{Namespace: "ok", RetentionDay: 400},
		{Namespace: "ok", StorageSize: "10Gi\nserver: {}"},
		{Namespace: "ok", StorageClass: "x\ny: z"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestValuesReflectOptions(t *testing.T) {
	o, _ := Options{RetentionDay: 14, StorageSize: "20Gi", StorageClass: "fast"}.Normalize()
	v := Values(o)
	for _, want := range []string{"retention: 14d", "size: 20Gi", "storageClass: fast", "kube-state-metrics:\n  enabled: true"} {
		if !strings.Contains(v, want) {
			t.Errorf("values lack %q:\n%s", want, v)
		}
	}
	none, _ := Options{StorageSize: "none", StorageClass: "fast"}.Normalize()
	if v := Values(none); !strings.Contains(v, "enabled: false") || strings.Contains(v, "storageClass") {
		t.Errorf("no-volume values wrong:\n%s", v)
	}
}

func TestPlanIsPinned(t *testing.T) {
	steps := Plan(Options{Namespace: "monitoring"})
	if len(steps) != 1 || steps[0].Version == "" || steps[0].Version == "latest" || steps[0].Release != Release || !steps[0].Wait {
		t.Errorf("plan: %+v", steps)
	}
}

func TestIsPrometheusService(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		ip     string
		ports  []int32
		want   int32
		ok     bool
	}{
		{"community server", map[string]string{"app.kubernetes.io/name": "prometheus", "app.kubernetes.io/component": "server"}, "10.0.0.1", []int32{80}, 80, true},
		{"kube-prometheus-stack", map[string]string{"app": "kube-prometheus-stack-prometheus"}, "10.0.0.2", []int32{9090, 8080}, 9090, true},
		{"headless", map[string]string{"app.kubernetes.io/name": "prometheus", "app.kubernetes.io/component": "server"}, "None", []int32{80}, 0, false},
		{"alertmanager", map[string]string{"app.kubernetes.io/name": "alertmanager"}, "10.0.0.3", []int32{9093}, 0, false},
		{"pushgateway", map[string]string{"app.kubernetes.io/name": "prometheus-pushgateway"}, "10.0.0.4", []int32{9091}, 0, false},
		{"stack alertmanager", map[string]string{"app": "kube-prometheus-stack-alertmanager"}, "10.0.0.5", []int32{9093}, 0, false},
		{"vmsingle", map[string]string{"app.kubernetes.io/name": "vmsingle"}, "10.0.0.6", []int32{8429}, 8429, true},
	}
	for _, c := range cases {
		port, ok := IsPrometheusService(c.labels, c.ip, c.ports)
		if ok != c.ok || port != c.want {
			t.Errorf("%s: got %d,%v want %d,%v", c.name, port, ok, c.want, c.ok)
		}
	}
}

// The values are YAML built from options; a wrong indent once made every install
// fail at "values are not valid YAML". Parse what each profile and option
// combination produces, and check the options land where the chart reads them.
func TestValuesAreValidYAML(t *testing.T) {
	for _, prof := range []Profile{ProfileLite, ProfileStandard, ProfileFull} {
		for _, o := range []Options{
			{}, {StorageSize: "none"}, {StorageClass: "fast", RetentionDay: 30},
			{StorageSize: "500Mi", Namespace: "obs", NodeSelector: "disktype=ssd"},
			{Alertmanager: true, NodeSelector: "node-role.kubernetes.io/monitoring="},
		} {
			o.Profile = prof
			o, err := o.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if err := yaml.Unmarshal([]byte(Values(o)), &v); err != nil {
				t.Fatalf("%s %+v: %v\n%s", prof, o, err, Values(o))
			}
			sel := map[string]any(nil)
			if o.NodeSelector != "" {
				k, val, _ := strings.Cut(o.NodeSelector, "=")
				sel = map[string]any{k: val}
			}
			switch prof {
			case ProfileStandard:
				pv := v["server"].(map[string]any)["persistentVolume"].(map[string]any)
				if (o.StorageSize == "none") == pv["enabled"].(bool) {
					t.Errorf("%+v: persistentVolume.enabled = %v", o, pv["enabled"])
				}
				if o.StorageSize != "none" && pv["size"] != o.StorageSize {
					t.Errorf("%+v: size = %v", o, pv["size"])
				}
				if o.StorageClass != "" && o.StorageSize != "none" && pv["storageClass"] != o.StorageClass {
					t.Errorf("%+v: storageClass = %v", o, pv["storageClass"])
				}
				if v["alertmanager"].(map[string]any)["enabled"] != o.Alertmanager {
					t.Errorf("%+v: alertmanager.enabled", o)
				}
				if sel != nil {
					got := v["server"].(map[string]any)["nodeSelector"].(map[string]any)
					if got[firstKey(sel)] != sel[firstKey(sel)] {
						t.Errorf("%+v: server.nodeSelector = %v", o, got)
					}
				}
			case ProfileFull:
				spec := v["prometheus"].(map[string]any)["prometheusSpec"].(map[string]any)
				if spec["retention"] != fmt.Sprintf("%dd", o.RetentionDay) {
					t.Errorf("%+v: retention = %v", o, spec["retention"])
				}
				if _, has := spec["storageSpec"]; has == (o.StorageSize == "none") {
					t.Errorf("%+v: storageSpec present = %v", o, has)
				}
				if v["grafana"].(map[string]any)["enabled"] != false {
					t.Error("Grafana must stay off (default admin password)")
				}
				if sel != nil && spec["nodeSelector"] == nil {
					t.Errorf("%+v: no nodeSelector on Prometheus", o)
				}
			case ProfileLite:
				if sel != nil && v["nodeSelector"] == nil {
					t.Errorf("%+v: no nodeSelector", o)
				}
			}
		}
	}
}

func firstKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return ""
}

func TestProfilesAndSelectorValidation(t *testing.T) {
	for _, bad := range []Options{
		{Profile: "huge"},
		{NodeSelector: "nokey"},
		{NodeSelector: "a=b\nc: d"},
		{NodeSelector: `a="x"`},
		{NodeSelector: "=v"},
	} {
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	for _, p := range []Profile{ProfileLite, ProfileStandard, ProfileFull} {
		o, _ := Options{Profile: p}.Normalize()
		steps := Plan(o)
		if len(steps) != 1 || steps[0].Release != ReleaseOf(p) || steps[0].Version == "" || steps[0].Version == "latest" {
			t.Errorf("%s plan: %+v", p, steps)
		}
	}
	if o, _ := (Options{Profile: ProfileLite}).Normalize(); o.Namespace != "kube-system" {
		t.Errorf("lite goes to kube-system, got %s", o.Namespace)
	}
}

func TestAlertmanagerAndManagedDetection(t *testing.T) {
	if p, ok := IsAlertmanagerService(map[string]string{"app.kubernetes.io/name": "alertmanager"}, "10.0.0.1", []int32{9093}); !ok || p != 9093 {
		t.Error("community alertmanager")
	}
	if _, ok := IsAlertmanagerService(map[string]string{"app": "kube-prometheus-stack-alertmanager"}, "10.0.0.1", []int32{9093, 8080}); !ok {
		t.Error("stack alertmanager")
	}
	if _, ok := IsAlertmanagerService(map[string]string{"app.kubernetes.io/name": "alertmanager"}, "None", []int32{9093}); ok {
		t.Error("headless alertmanager")
	}
	if ProfileOf(ServerService) != ProfileStandard || ProfileOf(StackService) != ProfileFull || ProfileOf("other") != "" {
		t.Error("ProfileOf")
	}
}
