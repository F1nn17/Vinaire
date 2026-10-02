package clusters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
const (
	clustersSecretName = "vinaire-clusters"
	clustersSecretKey  = "clusters.json"
	storeKeySecretName = "vinaire-clusters-key"
	storeKeySecretKey  = "key"
	fileStoreName      = "clusters.enc"
	fileStoreKeyName   = "store.key"
)

// Origin records how a cluster was added.
const (
	OriginToken      = "token"      // Phase 24: server + bearer token
	OriginKubeconfig = "kubeconfig" // pasted/uploaded kubeconfig
	OriginDiscovered = "discovered" // found on this machine
	OriginSSHImport  = "ssh-import" // kubeconfig read off a server over SSH
	OriginSSHCreate  = "ssh-create" // cluster Vinaire installed over SSH
)

// StoredCluster is one dynamically-added cluster's connection details, as
// persisted (encrypted) by a Store. Phase 24 clusters carry Server/Token/CAData;
// Phase 34 clusters carry Kubeconfig — a single-context, fully inline
// kubeconfig (no file references, no exec plugins) — which also supports the
// client-certificate credentials k3s uses.
type StoredCluster struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Token  string `json:"token,omitempty"`
	CAData string `json:"caData,omitempty"`

	Kubeconfig string `json:"kubeconfig,omitempty"`
	Type       string `json:"type,omitempty"`
	Origin     string `json:"origin,omitempty"`
	// SSHHostKey is the SSH host key fingerprint the user confirmed when the
	// cluster was added over SSH, kept so the trust decision is on record.
	SSHHostKey string `json:"sshHostKey,omitempty"`
}

// Store persists the dynamically-added clusters. Callers load, modify in
// memory, then save the whole list back — there are only ever a handful, so
// this beats a per-cluster scheme with partial-update races.
type Store interface {
	Load(ctx context.Context) ([]StoredCluster, error)
	Save(ctx context.Context, list []StoredCluster) error
	// Kind says where the data lives ("secret" or "file"); Encrypted whether
	// it is sealed at rest (false only for an in-cluster install that hasn't
	// been granted access to the key Secret — see NewSecretStore).
	Kind() string
	Encrypted() bool
}

// DocStore keeps named, sealed documents next to the cluster registry (Roadmap
// Phase 29's GitOps configuration, which holds git and ServiceAccount tokens).
// Both stores implement it; the document is sealed with the same key as the
// registry. Encrypted() on the Store says whether that is really so — a caller
// that stores credentials must refuse to when it is not.
type DocStore interface {
	Store
	// LoadDoc returns the plaintext of the named document, or nil if it doesn't
	// exist yet.
	LoadDoc(ctx context.Context, name string) ([]byte, error)
	SaveDoc(ctx context.Context, name string, plain []byte) error
}

func marshalList(c *Cipher, list []StoredCluster) ([]byte, error) {
	data, err := json.Marshal(list)
	if err != nil {
		return nil, fmt.Errorf("marshal clusters: %w", err)
	}
	if c != nil {
		data = c.Seal(data)
	}
	return data, nil
}

func unmarshalList(c *Cipher, data []byte) ([]StoredCluster, error) {
	stored := []StoredCluster{}
	if len(data) == 0 {
		return stored, nil
	}
	if IsSealed(data) && c == nil {
		return nil, errors.New("the stored clusters are encrypted but no key is available")
	}
	if c != nil {
		var err error
		if data, err = c.Open(data); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("parse stored clusters: %w", err)
	}
	return stored, nil
}

// --- Secret store (running in-cluster) ---

// SecretStore persists as a Secret in this backend's own namespace, written
// with the backend's own in-cluster identity — never the logged-in caller's
// token, since this is Vinaire managing its own configuration, not browsing a
// target cluster's resources.
type SecretStore struct {
	clientset *kubernetes.Clientset
	namespace string
	cipher    *Cipher
}

// NewSecretStore builds a SecretStore from the pod's own in-cluster identity.
// The encryption key comes from envKey (CLUSTER_STORE_KEY) if set, otherwise
// from — and on first run generated into — the Secret vinaire-clusters-key.
// If neither works (typically an older install whose Role predates that
// Secret) the store still works but writes unencrypted, as Phase 24 did, and
// says so loudly; Encrypted() lets the UI warn too.
func NewSecretStore(ctx context.Context, envKey string) (*SecretStore, bool) {
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
	s := &SecretStore{clientset: clientset, namespace: strings.TrimSpace(string(namespaceBytes))}

	key, err := s.resolveKey(ctx, envKey)
	if err != nil {
		slog.Warn("cluster store: no encryption key available — added clusters will be stored UNENCRYPTED. Set CLUSTER_STORE_KEY, or let Vinaire manage the Secret "+storeKeySecretName+" (grant it get/create in the cluster-manager Role)", "error", err)
		return s, true
	}
	if s.cipher, err = NewCipher(key); err != nil {
		slog.Warn("cluster store: invalid encryption key — storing unencrypted", "error", err)
	}
	return s, true
}

func (s *SecretStore) resolveKey(ctx context.Context, envKey string) ([]byte, error) {
	if envKey != "" {
		return ParseKey(envKey)
	}
	secrets := s.clientset.CoreV1().Secrets(s.namespace)
	sec, err := secrets.Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if err == nil {
		if k := sec.Data[storeKeySecretKey]; len(k) == 32 {
			return k, nil
		}
		return nil, errors.New("the " + storeKeySecretName + " Secret doesn't hold a 32-byte key")
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	_, err = secrets.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: storeKeySecretName, Namespace: s.namespace},
		Data:       map[string][]byte{storeKeySecretKey: key},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (s *SecretStore) Kind() string    { return "secret" }
func (s *SecretStore) Encrypted() bool { return s.cipher != nil }

// Load reads every persisted cluster. A missing Secret (nothing added yet)
// returns an empty, non-nil slice — not an error.
func (s *SecretStore) Load(ctx context.Context) ([]StoredCluster, error) {
	secret, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, clustersSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return []StoredCluster{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s secret: %w", clustersSecretName, err)
	}
	return unmarshalList(s.cipher, secret.Data[clustersSecretKey])
}

func (s *SecretStore) Save(ctx context.Context, list []StoredCluster) error {
	data, err := marshalList(s.cipher, list)
	if err != nil {
		return err
	}
	return s.putKey(ctx, clustersSecretKey, data)
}

// putKey writes one key of the registry Secret, leaving the others (the
// registry list and every document live side by side in it) untouched.
func (s *SecretStore) putKey(ctx context.Context, key string, data []byte) error {
	secrets := s.clientset.CoreV1().Secrets(s.namespace)
	for attempt := 0; attempt < 3; attempt++ {
		secret, err := secrets.Get(ctx, clustersSecretName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, err = secrets.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: clustersSecretName, Namespace: s.namespace},
				Data:       map[string][]byte{key: data},
			}, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				continue // someone created it between our Get and Create
			}
		} else if err == nil {
			if secret.Data == nil {
				secret.Data = map[string][]byte{}
			}
			secret.Data[key] = data
			_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
			if apierrors.IsConflict(err) {
				continue
			}
		}
		if err != nil {
			return fmt.Errorf("save %s secret: %w", clustersSecretName, err)
		}
		return nil
	}
	return fmt.Errorf("save %s secret: too many concurrent updates", clustersSecretName)
}

// LoadDoc / SaveDoc: documents are stored under "doc-<name>" in the same Secret.
func (s *SecretStore) LoadDoc(ctx context.Context, name string) ([]byte, error) {
	secret, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, clustersSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get %s secret: %w", clustersSecretName, err)
	}
	return openDoc(s.cipher, secret.Data["doc-"+name])
}

func (s *SecretStore) SaveDoc(ctx context.Context, name string, plain []byte) error {
	data := plain
	if s.cipher != nil {
		data = s.cipher.Seal(plain)
	}
	return s.putKey(ctx, "doc-"+name, data)
}

// openDoc unseals a stored document (nil in, nil out).
func openDoc(c *Cipher, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if IsSealed(data) {
		if c == nil {
			return nil, errors.New("the stored document is encrypted but no key is available")
		}
		return c.Open(data)
	}
	return data, nil
}

// --- File store (running outside a cluster, e.g. a home server) ---

// FileStore persists to <dir>/clusters.enc (mode 0600, directory 0700),
// sealed with a key from envKey (CLUSTER_STORE_KEY) or, absent that, a
// randomly generated <dir>/store.key (0600) — comparable to an SSH private key
// sitting in ~/.ssh.
type FileStore struct {
	dir    string
	cipher *Cipher
}

// NewFileStore opens (creating if needed) the data directory.
func NewFileStore(dir, envKey string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	var key []byte
	var err error
	if envKey != "" {
		key, err = ParseKey(envKey)
	} else {
		key, err = loadOrCreateKeyFile(filepath.Join(dir, fileStoreKeyName))
	}
	if err != nil {
		return nil, err
	}
	c, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &FileStore{dir: dir, cipher: c}, nil
}

func loadOrCreateKeyFile(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		key, perr := ParseKey(string(b))
		if perr != nil {
			return nil, fmt.Errorf("%s is not a valid key file: %w", path, perr)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create key file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(encodeKey(key) + "\n"); err != nil {
		return nil, err
	}
	return key, nil
}

func (f *FileStore) Kind() string    { return "file" }
func (f *FileStore) Encrypted() bool { return true }

func (f *FileStore) Load(_ context.Context) ([]StoredCluster, error) {
	data, err := os.ReadFile(filepath.Join(f.dir, fileStoreName))
	if errors.Is(err, os.ErrNotExist) {
		return []StoredCluster{}, nil
	}
	if err != nil {
		return nil, err
	}
	return unmarshalList(f.cipher, data)
}

// Save writes atomically (temp file + rename) so a crash never leaves a
// half-written registry.
func (f *FileStore) Save(_ context.Context, list []StoredCluster) error {
	data, err := marshalList(f.cipher, list)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, fileStoreName+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		// Windows has no POSIX modes; ignore there, enforce everywhere else.
		slog.Debug("cluster store: chmod not supported", "error", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(f.dir, fileStoreName))
}

// EntryFromStored builds a live Entry from persisted details: a stored
// kubeconfig (Phase 34) or a {server, token, caData} triple (Phase 24). An
// empty CAData on the latter means Insecure (skip TLS verification) — a real,
// visible tradeoff the frontend surfaces explicitly rather than silently
// accepting.
func EntryFromStored(sc StoredCluster) (*Entry, error) {
	var restConfig *rest.Config
	if sc.Kubeconfig != "" {
		var err error
		if restConfig, err = RESTConfigFromKubeconfig([]byte(sc.Kubeconfig)); err != nil {
			return nil, fmt.Errorf("stored kubeconfig for %s: %w", sc.Name, err)
		}
	} else {
		restConfig = &rest.Config{Host: sc.Server, BearerToken: sc.Token}
		if sc.CAData != "" {
			restConfig.TLSClientConfig = rest.TLSClientConfig{CAData: []byte(sc.CAData)}
		} else {
			restConfig.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
		}
	}
	return newEntry(sc.Name, restConfig, sc.Type, true)
}

// newEntry builds an Entry's clients from a REST config.
func newEntry(name string, restConfig *rest.Config, clusterType string, removable bool) (*Entry, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build clientset for %s: %w", name, err)
	}
	entry := &Entry{Name: name, Clientset: clientset, RESTConfig: restConfig, Removable: removable}
	entry.SetType(clusterType)
	if mc, err := metricsclientset.NewForConfig(restConfig); err == nil {
		entry.MetricsClientset = mc
	}
	if gc, err := gatewayclientset.NewForConfig(restConfig); err == nil {
		entry.GatewayClientset = gc
	}
	return entry, nil
}

// EntryFromRESTConfig builds a removable Entry straight from a REST config.
func EntryFromRESTConfig(name string, restConfig *rest.Config, clusterType string) (*Entry, error) {
	return newEntry(name, restConfig, clusterType, true)
}

// VerifyConnection does a minimal real API call to confirm entry's
// credentials actually work, before persisting them — adding a cluster via
// the web UI should fail loudly and immediately on bad credentials, not
// silently show up broken in every page that tries to use it.
func VerifyConnection(entry *Entry) error {
	if _, err := entry.Clientset.Discovery().ServerVersion(); err != nil {
		return fmt.Errorf("connect to cluster: %w", err)
	}
	return nil
}

// LoadDoc / SaveDoc: a document is <dir>/<name>.enc, sealed like clusters.enc.
func (f *FileStore) LoadDoc(_ context.Context, name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(f.dir, name+".enc"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return openDoc(f.cipher, data)
}

func (f *FileStore) SaveDoc(_ context.Context, name string, plain []byte) error {
	tmp, err := os.CreateTemp(f.dir, name+".enc.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		slog.Debug("doc store: chmod not supported", "error", err)
	}
	if _, err := tmp.Write(f.cipher.Seal(plain)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(f.dir, name+".enc"))
}
