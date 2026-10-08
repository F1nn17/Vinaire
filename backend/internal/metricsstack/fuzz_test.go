package metricsstack

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// Roadmap Phase 50: whatever a user types into the install form, the values handed to
// Helm stay a YAML document of the expected shape — an option can't add a key.
func FuzzOptionsNeverInjectYAML(f *testing.F) {
	f.Add("lite", "monitoring", "10Gi", "fast", "disktype=ssd", 7, true)
	f.Add("full", "obs", "none", "", "", 30, false)
	f.Add("standard", "x\ny: z", "1Gi\nfoo: bar", "c\n  d: e", "a=b\nc: d", 400, true)
	f.Add("", "", "", "", "k=v: w", 0, false)
	f.Fuzz(func(t *testing.T, profile, ns, size, class, sel string, days int, am bool) {
		o, err := Options{Profile: Profile(profile), Namespace: ns, StorageSize: size, StorageClass: class, NodeSelector: sel, RetentionDay: days, Alertmanager: am}.Normalize()
		if err != nil {
			return
		}
		var v map[string]any
		if err := yaml.Unmarshal([]byte(Values(o)), &v); err != nil {
			t.Fatalf("not valid YAML for %+v: %v\n%s", o, err, Values(o))
		}
		allowed := map[Profile][]string{
			ProfileStandard: {"alertmanager", "prometheus-pushgateway", "kube-state-metrics", "prometheus-node-exporter", "server"},
			ProfileFull:     {"grafana", "kubeEtcd", "kubeControllerManager", "kubeScheduler", "kubeProxy", "alertmanager", "prometheus"},
			ProfileLite:     {"resources", "nodeSelector"},
		}[o.Profile]
		for k := range v {
			ok := false
			for _, a := range allowed {
				ok = ok || a == k
			}
			if !ok {
				t.Fatalf("an option added the top-level key %q (%+v)", k, o)
			}
		}
		if o.NodeSelector != "" && !strings.Contains(Values(o), `"`) {
			t.Fatalf("the node selector isn't quoted in the values")
		}
		if !nsRe.MatchString(o.Namespace) {
			t.Fatalf("namespace %q accepted", o.Namespace)
		}
	})
}
