// Package metricsstack describes the "metrics stack" Vinaire can install in one
// click (Roadmap Phase 37): Prometheus with kube-state-metrics and node-exporter,
// from the prometheus-community chart. It only *describes* what to install — the
// exact, pinned chart and the values to give it. The install itself goes through
// the Marketplace's Helm path, as the signed-in user, like any other chart (see
// AGENTS.md), so nothing here writes to a cluster. Package logging does the same
// for Loki and Alloy.
package metricsstack

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Pinned chart version. Deliberately not "latest": what gets installed is
// something that was tried, and an upgrade is a deliberate edit here. The chart
// comes from the prometheus-community Helm repository, which is in the default
// catalog.
const (
	Repo         = "prometheus-community"
	Chart        = "prometheus"
	ChartVersion = "29.36.0"

	DefaultNamespace = "monitoring"
	Release          = "prometheus"
	// ServerService is the Service the chart creates for the server when the
	// release is called Release.
	ServerService = Release + "-server"
)

// Options are what the user may choose; everything else is fixed.
type Options struct {
	Namespace    string
	RetentionDay int    // days kept
	StorageSize  string // PVC size, e.g. "10Gi"; "none" = no PVC (emptyDir)
	StorageClass string // empty = the cluster default
}

var (
	nsRe      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	sizeRe    = regexp.MustCompile(`^[1-9][0-9]{0,4}(Mi|Gi|Ti)$`)
	classRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	ErrOption = errors.New("invalid option")
)

// Normalize fills defaults and validates, so a value that reaches the values
// template is one of a few safe shapes (no YAML injection through an option).
func (o Options) Normalize() (Options, error) {
	if o.Namespace == "" {
		o.Namespace = DefaultNamespace
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
	return o, nil
}

// Values are the values for the chart: Prometheus server with its default
// kubelet/cAdvisor scrape jobs, plus kube-state-metrics and node-exporter — what
// Vinaire's CPU / memory / network / filesystem charts read — without Alertmanager
// or the Pushgateway. A larger cluster that wants Grafana, the operator and alert
// rules should install kube-prometheus-stack from the Marketplace instead.
func Values(o Options) string {
	persist := "enabled: true\n    size: " + o.StorageSize
	if o.StorageSize == "none" {
		persist = "enabled: false"
	}
	class := ""
	if o.StorageClass != "" && o.StorageSize != "none" {
		class = "\n    storageClass: " + o.StorageClass
	}
	return fmt.Sprintf(`# Installed by Vinaire — Prometheus for a small cluster.
alertmanager:
  enabled: false
prometheus-pushgateway:
  enabled: false
kube-state-metrics:
  enabled: true
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
    %s%s
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      memory: 1Gi
`, o.RetentionDay, persist, class)
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
	return []InstallStep{{
		Title: "Prometheus — scrapes and stores the metrics (with kube-state-metrics and node-exporter)",
		Repo:  Repo, Chart: Chart, Version: ChartVersion,
		Namespace: o.Namespace, Release: Release, Values: Values(o),
		CreateNamespace: true, Wait: true,
	}}
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
