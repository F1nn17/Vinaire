package helm

import (
	"errors"
	"strings"
	"testing"
)

// the defaults of prometheus-postgres-exporter (the part that matters here)
func pgDefaults() map[string]any {
	return map[string]any{
		"config": map[string]any{
			"datasource": map[string]any{
				"host":           "",
				"user":           "postgres",
				"password":       nil,
				"passwordFile":   "",
				"passwordSecret": map[string]any{},
			},
			"datasourceSecret": map[string]any{},
		},
		"serviceMonitor": map[string]any{"enabled": false},
	}
}

func TestARequiredValueIsDescribedNotJustReported(t *testing.T) {
	err := errors.New("execution error at (prometheus-postgres-exporter/templates/secrets.yaml:2:9): config.datasource.password is required when not using config.datasource.passwordSecret, config.datasource.passwordFile, or config.datasourceSecret")
	p := classifyRenderError(err, pgDefaults())
	if !p.Required || p.Template != "prometheus-postgres-exporter/templates/secrets.yaml:2:9" {
		t.Fatalf("not recognised as the chart's own requirement: %+v", p)
	}
	got := map[string]ValueNeed{}
	for _, n := range p.Needs {
		got[n.Path] = n
	}
	if len(got) != 4 {
		t.Fatalf("expected the four values the message names, got %+v", p.Needs)
	}
	if n := got["config.datasource.password"]; n.Kind != "string" || !n.Sensitive || n.SecretRef {
		t.Errorf("the password is a sensitive plain value: %+v", n)
	}
	if n := got["config.datasource.passwordSecret"]; n.Kind != "object" || !n.SecretRef || !n.Guessed || strings.Join(n.Children, ",") != "name,key" {
		t.Errorf("an empty object named …Secret is a secret reference with name and key: %+v", n)
	}
	if n := got["config.datasource.passwordFile"]; n.Sensitive || n.SecretRef {
		t.Errorf("a file path is neither a password nor a Secret: %+v", n)
	}
	if n := got["config.datasourceSecret"]; !n.SecretRef {
		t.Errorf("datasourceSecret is a Secret reference: %+v", n)
	}
	// the errors callers already test for still hold, and so does the sentence
	if !errors.Is(p, ErrRender) || !errors.Is(p, ErrInvalid) {
		t.Error("a render problem must still be ErrInvalid and ErrRender")
	}
	if !strings.Contains(p.Error(), "the chart doesn't render with these values") {
		t.Errorf("message changed: %s", p.Error())
	}
}

func TestValuesPrefixAndUnknownNames(t *testing.T) {
	defaults := map[string]any{"auth": map[string]any{"token": ""}, "image": map[string]any{"tag": "1"}}
	// Helm's own wording of a bare `required "…"` names the value as .Values.x.y; names that are not
	// in the chart's defaults (a file name, a domain, a made-up value) are never offered
	p := classifyRenderError(errors.New(`execution error at (c/templates/a.yaml:3:4): A valid .Values.auth.token is required, see values.yaml or example.com/docs`), defaults)
	if len(p.Needs) != 1 || p.Needs[0].Path != "auth.token" || !p.Needs[0].Sensitive {
		t.Fatalf("got %+v", p.Needs)
	}
}

// the three shapes real charts use (found by rendering the catalog's charts with their defaults)
func TestRealWorldMessages(t *testing.T) {
	defaults := map[string]any{
		"cluster": map[string]any{"name": ""},
		"loki":    map[string]any{"storage": map[string]any{"bucketNames": map[string]any{}}},
		"tls":     map[string]any{"routerCertAndPKsecretName": ""},
	}
	// a YAML example in the message (grafana/k8s-monitoring)
	p := classifyRenderError(errors.New("execution error at (k8s-monitoring/templates/validations.yaml:1:2): A Cluster name is required!\nPlease set:\ncluster:\n  name: my-cluster-name"), defaults)
	if !p.Required || len(p.Needs) != 1 || p.Needs[0].Path != "cluster.name" || p.Needs[0].Kind != "string" {
		t.Errorf("snippet: %+v", p)
	}
	// a dotted path the defaults leave out, inside a map they have (grafana/loki)
	p = classifyRenderError(errors.New("execution error at (loki/templates/validate.yaml:5:3): Please define loki.storage.bucketNames.chunks"), defaults)
	if !p.Required || len(p.Needs) != 1 || p.Needs[0].Path != "loki.storage.bucketNames.chunks" {
		t.Errorf("new key in a known map: %+v", p)
	}
	// a `fail` after a lookup of something in the cluster is no requirement: a client-side render
	// has no cluster, so the lookup finds nothing even when the Secret is there
	p = classifyRenderError(errors.New("execution error at (c/templates/x.yaml:1:1): tls.routerCertAndPKsecretName: secret 'x-router-tls' not found in namespace 'default'"), defaults)
	if p.Required {
		t.Errorf("a lookup artifact must not block the install: %+v", p)
	}
	// words with no parent in the defaults are never offered
	p = classifyRenderError(errors.New("execution error at (c/t.yaml:1:1): see docs.example.com or notes.txt and set Note: later"), defaults)
	if len(p.Needs) != 0 {
		t.Errorf("offered %+v", p.Needs)
	}
}

func TestOtherRenderErrorsAreNotRequirements(t *testing.T) {
	for _, msg := range []string{
		`template: c/templates/x.yaml:12:5: executing "c/templates/x.yaml" at <.Values.a.b>: nil pointer evaluating interface {}.b`,
		`YAML parse error on c/templates/x.yaml: error converting YAML to JSON`,
		``,
	} {
		p := classifyRenderError(errors.New(msg), pgDefaults())
		if p.Required || len(p.Needs) != 0 {
			t.Errorf("%q wrongly treated as a missing value: %+v", msg, p)
		}
	}
}

func TestTheNumberOfNeedsIsBounded(t *testing.T) {
	defaults := map[string]any{}
	var names []string
	for i := 0; i < 30; i++ {
		k := "k" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		defaults[k] = map[string]any{"x": ""}
		names = append(names, k+".x")
	}
	p := classifyRenderError(errors.New("execution error at (c/t.yaml:1:1): need "+strings.Join(names, ", ")), defaults)
	if len(p.Needs) != maxNeeds {
		t.Errorf("got %d needs", len(p.Needs))
	}
}

// What the parser must never do with the chart author's text: panic, or offer a value that is not in
// the chart's own defaults.
func FuzzClassifyRenderError(f *testing.F) {
	f.Add("execution error at (a/b.yaml:1:2): config.datasource.password is required")
	f.Add("execution error at (): ")
	f.Add("a.b.c .Values.x..y ....")
	defaults := pgDefaults()
	f.Fuzz(func(t *testing.T, msg string) {
		p := classifyRenderError(errors.New(msg), defaults)
		if len(p.Needs) > maxNeeds {
			t.Fatalf("too many needs: %d", len(p.Needs))
		}
		for _, n := range p.Needs {
			if _, ok := resolveNeed(defaults, strings.Split(n.Path, ".")); !ok {
				t.Fatalf("offered %q, which is neither in the chart's defaults nor a new key in one of its maps", n.Path)
			}
		}
	})
}
