// Package logging describes the "logs stack" Vinaire can install in one click
// (Roadmap Phase 45): Loki (stores and searches logs) and Grafana Alloy (collects
// every pod's logs and ships them to Loki). It only *describes* what to install —
// the exact, pinned charts and the values to give them. The install itself goes
// through the Marketplace's Helm path, as the signed-in user, like any other chart
// (see AGENTS.md), so nothing here writes to a cluster.
package logging

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Pinned chart versions. Deliberately not "latest": what gets installed is
// something that was tried, and an upgrade is a deliberate edit here. Both come
// from the Grafana Helm repository, which is in the default catalog.
const (
	Repo        = "grafana"
	LokiChart   = "loki"
	LokiVersion = "7.3.0"
	// Alloy replaces Promtail, whose chart is deprecated.
	AlloyChart   = "alloy"
	AlloyVersion = "1.13.0"

	DefaultNamespace = "logging"
	LokiRelease      = "loki"
	AlloyRelease     = "alloy"
	LokiPort         = 3100
)

// Options are what the user may choose; everything else is fixed.
type Options struct {
	Namespace    string
	RetentionDay int    // days kept
	StorageSize  string // PVC size, e.g. "10Gi"; empty = no PVC (emptyDir)
	StorageClass string // empty = the cluster default
}

var (
	nsRe      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	sizeRe    = regexp.MustCompile(`^[1-9][0-9]{0,4}(Mi|Gi|Ti)$`)
	classRe   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	ErrOption = errors.New("invalid option")
)

// Normalize fills defaults and validates, so a value that reaches a template is
// one of a few safe shapes (no YAML or River injection through an option).
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

// LokiURL is where Alloy pushes, and where Vinaire finds Loki.
func (o Options) LokiService() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", LokiRelease, o.Namespace)
}

// LokiValues are the values for the Loki chart: the single-binary ("monolithic")
// mode — one pod, filesystem storage, no object store, no gateway, no caches, no
// canary — which is what a small or home cluster wants. A large cluster should use
// a distributed install of its own; this isn't that.
func LokiValues(o Options) string {
	persist := "enabled: true\n    size: " + o.StorageSize
	if o.StorageSize == "none" {
		persist = "enabled: false"
	}
	class := ""
	if o.StorageClass != "" && o.StorageSize != "none" {
		class = "\n    storageClass: " + o.StorageClass
	}
	return fmt.Sprintf(`# Installed by Vinaire — single-binary Loki for a small cluster.
deploymentMode: SingleBinary
loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1
  storage:
    type: filesystem
  schemaConfig:
    configs:
      - from: "2024-04-01"
        store: tsdb
        object_store: filesystem
        schema: v13
        index:
          prefix: loki_index_
          period: 24h
  limits_config:
    retention_period: %dh
    allow_structured_metadata: true
  compactor:
    retention_enabled: true
    delete_request_store: filesystem
singleBinary:
  replicas: 1
  persistence:
    %s%s
  resources:
    requests:
      cpu: 100m
      memory: 256Mi
    limits:
      memory: 1Gi
backend:
  replicas: 0
read:
  replicas: 0
write:
  replicas: 0
gateway:
  enabled: false
chunksCache:
  enabled: false
resultsCache:
  enabled: false
lokiCanary:
  enabled: false
test:
  enabled: false
minio:
  enabled: false
`, o.RetentionDay*24, persist, class)
}

// AlloyValues make Alloy (a DaemonSet) tail the logs of the pods on its own node
// through the Kubernetes API and push them to Loki, labelled namespace / pod /
// container / app — the labels Vinaire's log search expects.
func AlloyValues(o Options) string {
	return fmt.Sprintf(`# Installed by Vinaire — ships every pod's logs to Loki.
controller:
  type: daemonset
alloy:
  extraEnv:
    - name: NODE_NAME
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
  resources:
    requests:
      cpu: 50m
      memory: 128Mi
    limits:
      memory: 512Mi
  configMap:
    create: true
    content: |
      discovery.kubernetes "pods" {
        role = "pod"
        selectors {
          role  = "pod"
          field = "spec.nodeName=" + coalesce(env("NODE_NAME"), "")
        }
      }
      discovery.relabel "pods" {
        targets = discovery.kubernetes.pods.targets
        rule {
          source_labels = ["__meta_kubernetes_namespace"]
          target_label  = "namespace"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_name"]
          target_label  = "pod"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_container_name"]
          target_label  = "container"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_label_app_kubernetes_io_name"]
          target_label  = "app"
        }
      }
      loki.source.kubernetes "pods" {
        targets    = discovery.relabel.pods.output
        forward_to = [loki.write.default.receiver]
      }
      loki.write "default" {
        endpoint {
          url = "http://%s:%d/loki/api/v1/push"
        }
      }
`, o.LokiService(), LokiPort)
}

// InstallStep is one Helm release of the stack, in the shape the Marketplace's
// install job takes.
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

// Plan is the ordered installs for the options (Loki first: Alloy needs somewhere to push).
func Plan(o Options) []InstallStep {
	return []InstallStep{
		{Title: "Loki — stores and searches the logs", Repo: Repo, Chart: LokiChart, Version: LokiVersion,
			Namespace: o.Namespace, Release: LokiRelease, Values: LokiValues(o), CreateNamespace: true, Wait: true},
		{Title: "Alloy — collects every pod's logs", Repo: Repo, Chart: AlloyChart, Version: AlloyVersion,
			Namespace: o.Namespace, Release: AlloyRelease, Values: AlloyValues(o), CreateNamespace: true, Wait: true},
	}
}

// IsLokiService recognises the Service a Loki install exposes for queries: labelled
// as Loki, not headless, with a query port.
func IsLokiService(labels map[string]string, clusterIP string, ports []int32) (int32, bool) {
	if labels["app.kubernetes.io/name"] != "loki" || clusterIP == "None" {
		return 0, false
	}
	if strings.Contains(labels["app.kubernetes.io/component"], "memberlist") {
		return 0, false
	}
	for _, want := range []int32{LokiPort, 80} {
		for _, p := range ports {
			if p == want {
				return p, true
			}
		}
	}
	return 0, false
}

// IsCollector recognises a DaemonSet that ships pod logs.
func IsCollector(labels map[string]string) bool {
	switch labels["app.kubernetes.io/name"] {
	case "alloy", "promtail", "grafana-agent", "fluent-bit", "vector":
		return true
	}
	return false
}
