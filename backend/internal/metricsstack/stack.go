// Package metricsstack describes the "metrics stack" Vinaire can install in one
// click (Roadmap Phase 37). It only *describes* what to install — the exact,
// pinned charts and the values to give them. The install itself goes through the
// Marketplace's Helm path, as the signed-in user, like any other chart (see
// AGENTS.md), so nothing here writes to a cluster. Package logging does the same
// for Loki and Alloy.
//
// Three profiles:
//
//   - lite: metrics-server only — the current CPU/memory values, no history.
//   - standard (default): Prometheus with kube-state-metrics and node-exporter
//     (optionally Alertmanager) from the prometheus-community/prometheus chart.
//   - full: kube-prometheus-stack — the operator, Prometheus, Alertmanager, the
//     exporters and the default alert rules. Grafana stays off: its chart's default
//     admin password is well known and Vinaire doesn't use Grafana.
package metricsstack

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Profile selects what gets installed.
type Profile string

const (
	ProfileLite     Profile = "lite"
	ProfileStandard Profile = "standard"
	ProfileFull     Profile = "full"
)

// Pinned chart versions. Deliberately not "latest": what gets installed is
// something that was tried, and an upgrade is a deliberate edit here. The repos
// are in the default catalog.
const (
	// standard
	Repo         = "prometheus-community"
	Chart        = "prometheus"
	ChartVersion = "29.36.0"
	Release      = "prometheus"
	// ServerService is the Service the chart creates for the server when the
	// release is called Release.
	ServerService = Release + "-server"

	// full
	StackChart        = "kube-prometheus-stack"
	StackChartVersion = "92.1.0"
	StackRelease      = "kube-prometheus-stack"
	StackService      = StackRelease + "-prometheus"

	// lite
	LiteRepo         = "metrics-server"
	LiteChart        = "metrics-server"
	LiteChartVersion = "3.14.0"
	LiteRelease      = "metrics-server"

	DefaultNamespace = "monitoring"
)

// Options are what the user may choose; everything else is fixed.
type Options struct {
	Profile      Profile
	Namespace    string
	RetentionDay int    // days kept
	StorageSize  string // PVC size, e.g. "10Gi"; "none" = no PVC (emptyDir)
	StorageClass string // empty = the cluster default
	// NodeSelector pins the server (and Alertmanager) to nodes carrying this label,
	// "key=value"; empty = anywhere. Exporters that must see every node are never pinned.
	NodeSelector string
	// Alertmanager adds Alertmanager to the standard profile (the full profile has it).
	Alertmanager bool
}

var (
	nsRe      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	sizeRe    = regexp.MustCompile(`^[1-9][0-9]{0,4}(Mi|Gi|Ti)$`)
	classRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	selKeyRe  = regexp.MustCompile(`^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$`)
	selValRe  = regexp.MustCompile(`^([A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?)?$`)
	ErrOption = errors.New("invalid option")
)

// Normalize fills defaults and validates, so a value that reaches the values
// template is one of a few safe shapes (no YAML injection through an option).
func (o Options) Normalize() (Options, error) {
	switch o.Profile {
	case "":
		o.Profile = ProfileStandard
	case ProfileLite, ProfileStandard, ProfileFull:
	default:
		return o, fmt.Errorf("%w: profile must be lite, standard or full", ErrOption)
	}
	if o.Namespace == "" {
		o.Namespace = DefaultNamespace
		if o.Profile == ProfileLite {
			o.Namespace = "kube-system"
		}
	}
	if !nsRe.MatchString(o.Namespace) {
		return o, fmt.Errorf("%w: namespace", ErrOption)
	}
	if o.RetentionDay == 0 {
		o.RetentionDay = 7
	}
	if o.RetentionDay < 1 || o.RetentionDay > 365 {
		return o, fmt.Errorf("%w: retention must be 1–365 days", ErrOption)
	}
	if o.StorageSize == "" {
		o.StorageSize = "10Gi"
	}
	if o.StorageSize != "none" && !sizeRe.MatchString(o.StorageSize) {
		return o, fmt.Errorf("%w: storage size must look like 10Gi", ErrOption)
	}
	if o.StorageClass != "" && !classRe.MatchString(o.StorageClass) {
		return o, fmt.Errorf("%w: storage class", ErrOption)
	}
	if o.NodeSelector != "" {
		k, v, ok := strings.Cut(o.NodeSelector, "=")
		if !ok || !selKeyRe.MatchString(k) || !selValRe.MatchString(v) {
			return o, fmt.Errorf("%w: node selector must look like key=value", ErrOption)
		}
	}
	return o, nil
}

// selector renders the node selector as a YAML map body ("key: \"value\""), indented.
func (o Options) selector(indent string) string {
	if o.NodeSelector == "" {
		return ""
	}
	k, v, _ := strings.Cut(o.NodeSelector, "=")
	return fmt.Sprintf("\n%snodeSelector:\n%s  %q: %q", indent, indent, k, v)
}

func (o Options) volume() (size, class string, on bool) {
	if o.StorageSize == "none" {
		return "", "", false
	}
	return o.StorageSize, o.StorageClass, true
}

// Values are the values for the chosen profile's chart.
func Values(o Options) string {
	switch o.Profile {
	case ProfileLite:
		return liteValues(o)
	case ProfileFull:
		return fullValues(o)
	}
	return standardValues(o)
}

// standardValues: Prometheus server with its default kubelet/cAdvisor scrape jobs,
// plus kube-state-metrics and node-exporter — what Vinaire's CPU / memory / network /
// filesystem charts read.
func standardValues(o Options) string {
	persist := "enabled: false"
	if size, class, on := o.volume(); on {
		persist = "enabled: true\n    size: " + size
		if class != "" {
			persist += "\n    storageClass: " + class
		}
	}
	am := "enabled: false"
	if o.Alertmanager {
		am = "enabled: true\n  persistence:\n    enabled: false\n  resources:\n    requests:\n      cpu: 10m\n      memory: 32Mi\n    limits:\n      memory: 128Mi" + o.selector("  ")
	}
	return fmt.Sprintf(`# Installed by Vinaire — Prometheus for a small cluster.
alertmanager:
  %s
prometheus-pushgateway:
  enabled: false
kube-state-metrics:
  enabled: true%s
prometheus-node-exporter:
  enabled: true
  resources:
    requests:
      cpu: 20m
      memory: 32Mi
    limits:
      memory: 128Mi
server:
  retention: %dd
  persistentVolume:
    %s
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      memory: 1Gi%s
`, am, o.selector("  "), o.RetentionDay, persist, o.selector("  "))
}

// fullValues: kube-prometheus-stack. The control-plane component monitors are off —
// on k3s, k3d and most managed clusters those endpoints aren't reachable and would
// only raise permanent false alerts — and so is Grafana (see the package comment).
func fullValues(o Options) string {
	storage := ""
	if size, class, on := o.volume(); on {
		cl := ""
		if class != "" {
			cl = "\n          storageClassName: " + class
		}
		storage = fmt.Sprintf(`
    storageSpec:
      volumeClaimTemplate:
        spec:%s
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: %s`, cl, size)
	}
	return fmt.Sprintf(`# Installed by Vinaire — kube-prometheus-stack (no Grafana).
grafana:
  enabled: false
kubeEtcd:
  enabled: false
kubeControllerManager:
  enabled: false
kubeScheduler:
  enabled: false
kubeProxy:
  enabled: false
alertmanager:
  enabled: true
  alertmanagerSpec:
    resources:
      requests:
        cpu: 10m
        memory: 32Mi
      limits:
        memory: 128Mi%s
prometheus:
  prometheusSpec:
    retention: %dd
    resources:
      requests:
        cpu: 200m
        memory: 512Mi
      limits:
        memory: 2Gi%s%s
`, o.selector("    "), o.RetentionDay, storage, o.selector("    "))
}

func liteValues(o Options) string {
	return fmt.Sprintf(`# Installed by Vinaire — metrics-server.
resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    memory: 200Mi%s
`, o.selector(""))
}

// InstallStep is one Helm release, in the shape the Marketplace's install job takes.
type InstallStep struct {
	Title           string `json:"title"`
	Repo            string `json:"repo"`
	Chart           string `json:"chart"`
	Version         string `json:"version"`
	Namespace       string `json:"namespace"`
	Release         string `json:"release"`
	Values          string `json:"values"`
	CreateNamespace bool   `json:"createNamespace"`
	Wait            bool   `json:"wait"`
}

// Plan is the install for the options.
func Plan(o Options) []InstallStep {
	switch o.Profile {
	case ProfileLite:
		return []InstallStep{{
			Title: "metrics-server — the current CPU and memory of nodes and pods",
			Repo:  LiteRepo, Chart: LiteChart, Version: LiteChartVersion,
			Namespace: o.Namespace, Release: LiteRelease, Values: Values(o),
			CreateNamespace: true, Wait: true,
		}}
	case ProfileFull:
		return []InstallStep{{
			Title: "kube-prometheus-stack — operator, Prometheus, Alertmanager, exporters and alert rules",
			Repo:  Repo, Chart: StackChart, Version: StackChartVersion,
			Namespace: o.Namespace, Release: StackRelease, Values: Values(o),
			CreateNamespace: true, Wait: true,
		}}
	}
	title := "Prometheus — scrapes and stores the metrics (with kube-state-metrics and node-exporter)"
	if o.Alertmanager {
		title = "Prometheus — scrapes and stores the metrics (with kube-state-metrics, node-exporter and Alertmanager)"
	}
	return []InstallStep{{
		Title: title,
		Repo:  Repo, Chart: Chart, Version: ChartVersion,
		Namespace: o.Namespace, Release: Release, Values: Values(o),
		CreateNamespace: true, Wait: true,
	}}
}

// ProfileOf says which profile of ours a Prometheus Service belongs to, "" if it
// isn't one Vinaire installed.
func ProfileOf(service string) Profile {
	switch service {
	case ServerService:
		return ProfileStandard
	case StackService:
		return ProfileFull
	}
	return ""
}

// ReleaseOf is the Helm release a profile installs.
func ReleaseOf(p Profile) string {
	switch p {
	case ProfileFull:
		return StackRelease
	case ProfileLite:
		return LiteRelease
	}
	return Release
}

// IsPrometheusService recognises the Service a Prometheus install exposes for
// queries — the community chart's server, kube-prometheus-stack's, or a
// VictoriaMetrics single-node — returning the port to query. Not headless; never
// the Alertmanager, Pushgateway or an exporter.
func IsPrometheusService(labels map[string]string, clusterIP string, ports []int32) (int32, bool) {
	if clusterIP == "None" {
		return 0, false
	}
	name := labels["app.kubernetes.io/name"]
	switch {
	case name == "prometheus" && labels["app.kubernetes.io/component"] == "server":
		return pickPort(ports, 80, 9090)
	case strings.HasSuffix(labels["app"], "-prometheus") && !strings.Contains(labels["app"], "alertmanager"):
		return pickPort(ports, 9090)
	case name == "prometheus" && labels["app.kubernetes.io/component"] == "":
		return pickPort(ports, 9090)
	case name == "vmsingle":
		return pickPort(ports, 8429)
	}
	return 0, false
}

// IsAlertmanagerService recognises the Service an Alertmanager exposes for its API
// (the community subchart's or kube-prometheus-stack's), not the headless one.
func IsAlertmanagerService(labels map[string]string, clusterIP string, ports []int32) (int32, bool) {
	if clusterIP == "None" {
		return 0, false
	}
	if labels["app.kubernetes.io/name"] == "alertmanager" || strings.HasSuffix(labels["app"], "-alertmanager") {
		return pickPort(ports, 9093, 80)
	}
	return 0, false
}

func pickPort(have []int32, want ...int32) (int32, bool) {
	for _, w := range want {
		for _, p := range have {
			if p == w {
				return p, true
			}
		}
	}
	return 0, false
}

// IsKubeStateMetrics / IsNodeExporter recognise the two exporters by label.
func IsKubeStateMetrics(labels map[string]string) bool {
	return labels["app.kubernetes.io/name"] == "kube-state-metrics"
}

func IsNodeExporter(labels map[string]string) bool {
	n := labels["app.kubernetes.io/name"]
	return n == "prometheus-node-exporter" || n == "node-exporter"
}
