package clusters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// serviceAccountNamespaceFile is where every pod's own namespace is
// automatically mounted — the standard way to learn "what namespace am I
// running in" from inside a pod, without a POD_NAMESPACE env var.
const serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// clustersSecretName holds every dynamically-added cluster's connection
// details (cluster management via the web UI) — deliberately a Secret of
// its own, separate from anything else this app reads. Access to it is
// scoped by a dedicated, narrow Role bound only in this app's own
// namespace (see AGENTS.md's core rule) — never the broad, cluster-wide
// read-only ClusterRole every *browsed* cluster's resources go through.
const clustersSecretName = "vinaire-clusters"
const clustersSecretKey = "clusters.json"

// StoredCluster is one dynamically-added cluster's connection details, as
// persisted in the clustersSecretName Secret.
type StoredCluster struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Token  string `json:"token"`
	CAData string `json:"caData,omitempty"`
}

// Store persists dynamically-added clusters as a single Secret in this
// backend's own namespace, written with the backend's own in-cluster
// identity — never the logged-in caller's token, since this is Vinaire
// managing its own configuration, not browsing a target cluster's
// resources (the same "backend-owned infrastructure, not per-caller"
// split the durable audit log, Roadmap Phase 17, already uses).
//
// Store is only ever obtained via NewStore, which reports ok=false when
// the backend isn't running in-cluster — there's nowhere sensible to
// persist to otherwise, so cluster management is an explicit "not
// available outside a cluster" feature, not a broken one, matching this
// codebase's metrics-server/Gateway API/Prometheus "may not exist" pattern.
type Store struct {
	clientset *kubernetes.Clientset
	namespace string
}

// NewStore builds a Store from the pod's own in-cluster identity.
func NewStore() (*Store, bool) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, false
	}
	namespaceBytes, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return nil, false
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, false
	}
	return &Store{clientset: clientset, namespace: strings.TrimSpace(string(namespaceBytes))}, true
}

// Load reads every persisted cluster. A missing Secret (nothing added yet)
// returns an empty, non-nil slice — not an error.
func (s *Store) Load(ctx context.Context) ([]StoredCluster, error) {
	secret, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, clustersSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return []StoredCluster{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s secret: %w", clustersSecretName, err)
	}

	stored := []StoredCluster{}
	if data := secret.Data[clustersSecretKey]; len(data) > 0 {
		if err := json.Unmarshal(data, &stored); err != nil {
			return nil, fmt.Errorf("parse %s secret: %w", clustersSecretName, err)
		}
	}
	return stored, nil
}

// Save persists the full current list, overwriting whatever was there —
// callers load, modify in memory, then save the whole list back. There are
// only ever a handful of clusters, so this is simpler and safer than a
// per-cluster key scheme with partial-update races.
func (s *Store) Save(ctx context.Context, list []StoredCluster) error {
	data, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("marshal clusters: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: clustersSecretName, Namespace: s.namespace},
		Data:       map[string][]byte{clustersSecretKey: data},
	}

	_, err = s.clientset.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		_, err = s.clientset.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("save %s secret: %w", clustersSecretName, err)
	}
	return nil
}

// EntryFromStored builds an Entry (the dynamic-add path) directly from a
// {server, token, caData} triple — unlike main.go's newClusterEntry, this
// bypasses kubeconfig entirely, since a user submits these fields directly
// through the cluster-management UI, not a kubeconfig context name. An
// empty CAData means Insecure (skip TLS verification) — a real, visible
// tradeoff the frontend surfaces explicitly rather than silently accepting.
func EntryFromStored(sc StoredCluster) (*Entry, error) {
	restConfig := &rest.Config{
		Host:        sc.Server,
		BearerToken: sc.Token,
	}
	if sc.CAData != "" {
		restConfig.TLSClientConfig = rest.TLSClientConfig{CAData: []byte(sc.CAData)}
	} else {
		restConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build clientset for %s: %w", sc.Name, err)
	}

	entry := &Entry{Name: sc.Name, Clientset: clientset, RESTConfig: restConfig, Removable: true}
	if mc, err := metricsclientset.NewForConfig(restConfig); err == nil {
		entry.MetricsClientset = mc
	}
	if gc, err := gatewayclientset.NewForConfig(restConfig); err == nil {
		entry.GatewayClientset = gc
	}
	return entry, nil
}

// VerifyConnection does a minimal real API call to confirm entry's
// credentials actually work, before persisting them — adding a cluster via
// the web UI should fail loudly and immediately on bad credentials, not
// silently show up broken later in every page that tries to use it.
func VerifyConnection(entry *Entry) error {
	if _, err := entry.Clientset.Discovery().ServerVersion(); err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	return nil
}
