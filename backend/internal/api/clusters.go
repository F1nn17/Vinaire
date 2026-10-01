package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/shiraku/vinaire/backend/internal/clusters"
)

// ClusterRow matches the frontend's Cluster type (frontend/src/lib/types.ts).
type ClusterRow struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	// Removable is false for the env-var-configured cluster(s) — the
	// settings UI shouldn't offer to remove those (see clusters.Entry).
	Removable bool `json:"removable"`
	// Type (Roadmap Phase 34): k3s | k3d | kind | minikube | microk8s |
	// kubeadm | managed | unknown. Detected, and correctable by the user.
	Type string `json:"type"`
	// CanAddNode is whether Phase 30's "Add node" makes sense for this type.
	CanAddNode bool `json:"canAddNode"`
	// Status is "connected" or "unreachable" (a cached probe of /version).
	Status string `json:"status"`
}

// ClusterManagement describes the registry's persistence.
type ClusterManagement struct {
	Available bool   `json:"available"`
	Store     string `json:"store,omitempty"` // "secret" | "file"
	Encrypted bool   `json:"encrypted"`
	Reason    string `json:"reason,omitempty"`
}

var (
	statusMu    sync.Mutex
	statusCache = map[string]statusEntry{}
)

type statusEntry struct {
	status string
	at     time.Time
}

const statusTTL = 15 * time.Second

// clusterStatus probes a cluster's /version, cached briefly so the list
// endpoint (hit on every page load) doesn't hammer every cluster.
var (
	redetectMu   sync.Mutex
	redetectLast = map[string]time.Time{}
)

// redetectType retries type detection for a reachable cluster whose type is
// still unknown — startup detection can run while a cluster is briefly down, and
// would otherwise never run again. Throttled per cluster.
func redetectType(name string, entry *clusters.Entry) {
	redetectMu.Lock()
	if t, ok := redetectLast[name]; ok && time.Since(t) < 5*time.Minute {
		redetectMu.Unlock()
		return
	}
	redetectLast[name] = time.Now()
	redetectMu.Unlock()

	if t := clusters.DetectType(context.Background(), entry.Clientset, name); t != clusters.TypeUnknown && entry.Type() == clusters.TypeUnknown {
		entry.SetType(t)
	}
}

func newClientset(cfg *rest.Config) (*kubernetes.Clientset, error) {
	return kubernetes.NewForConfig(cfg)
}

func clusterStatus(name string, entry *clusters.Entry) string {
	statusMu.Lock()
	if c, ok := statusCache[name]; ok && time.Since(c.at) < statusTTL {
		statusMu.Unlock()
		return c.status
	}
	statusMu.Unlock()

	status := "connected"
	cfg := *entry.RESTConfig
	cfg.Timeout = 2 * time.Second
	if cs, err := newClientset(&cfg); err != nil {
		status = "unreachable"
	} else if _, err := cs.Discovery().ServerVersion(); err != nil {
		status = "unreachable"
	}
	statusMu.Lock()
	statusCache[name] = statusEntry{status, time.Now()}
	statusMu.Unlock()
	return status
}

// ListClusters handles GET /api/v1/clusters (Roadmap Phase 11) — every
// kubeconfig context this backend is configured to talk to, in
// configuration order, plus any cluster added dynamically through the
// cluster-management UI. Named ListClusters (not Clusters) because Handler
// already has a Clusters field for the registry itself.
func (h *Handler) ListClusters(w http.ResponseWriter, r *http.Request) {
	names := h.Clusters.Names()
	def := h.Clusters.Default()

	rows := make([]ClusterRow, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		entry, _ := h.Clusters.Get(name)
		rows[i] = ClusterRow{
			Name: name, Default: def != nil && def.Name == name,
			Removable: entry != nil && entry.Removable,
			Type:      clusters.TypeUnknown, CanAddNode: true, Status: "unreachable",
		}
		if entry == nil {
			continue
		}
		rows[i].Type = entry.Type()
		rows[i].CanAddNode = clusters.SupportsAddNode(rows[i].Type)
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows[i].Status = clusterStatus(name, entry)
			if rows[i].Status == "connected" && rows[i].Type == clusters.TypeUnknown {
				go redetectType(name, entry)
			}
		}()
	}
	wg.Wait()
	respondJSON(w, rows)
}

// ClusterManagementInfo handles GET /api/v1/clusters/management.
func (h *Handler) ClusterManagementInfo(w http.ResponseWriter, r *http.Request) {
	if h.ClusterStore == nil {
		respondJSON(w, ClusterManagement{Reason: "Cluster management needs somewhere to keep the encrypted registry: run Vinaire in a cluster, or set VINAIRE_DATA_DIR."})
		return
	}
	respondJSON(w, ClusterManagement{Available: true, Store: h.ClusterStore.Kind(), Encrypted: h.ClusterStore.Encrypted()})
}

var clusterNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

// regError is a registration failure with the HTTP status it deserves.
type regError struct {
	status int
	msg    string
}

func (e *regError) Error() string { return e.msg }

func respondRegError(w http.ResponseWriter, err error) {
	var re *regError
	if errors.As(err, &re) {
		respondError(w, re.status, re.msg)
		return
	}
	respondError(w, http.StatusInternalServerError, err.Error())
}

// explainConnectError turns the commonest "I can't connect" failure into
// advice: k3s issues its certificate without the machine's public address
// unless told to (--tls-san), so connecting by that address fails TLS.
func explainConnectError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "x509") {
		return msg + " — the cluster's TLS certificate doesn't cover the address Vinaire used. For k3s that means the certificate was issued without that address (k3s needs --tls-san <address>); use an address the certificate does cover (the \"server URL\" field), or recreate the server with --tls-san."
	}
	return msg
}

// registerCluster is the one path every way of adding a cluster ends in: check
// the name, prove the credentials work, work out the type, refuse a cluster
// that's already registered under another name, persist it (sealed), make it
// live, and audit it. Nothing is stored or registered until the connection has
// actually been made.
func (h *Handler) registerCluster(ctx context.Context, r *http.Request, sc clusters.StoredCluster, hint string) (*clusters.Entry, error) {
	if h.ClusterStore == nil {
		return nil, &regError{http.StatusServiceUnavailable, "cluster management is unavailable here: run Vinaire in a cluster, or set VINAIRE_DATA_DIR so there is somewhere to keep the encrypted registry"}
	}
	sc.Name = strings.TrimSpace(sc.Name)
	if !clusterNameRe.MatchString(sc.Name) {
		return nil, &regError{http.StatusBadRequest, "cluster name must be 1-63 letters, digits, '.', '_' or '-'"}
	}
	if _, exists := h.Clusters.Get(sc.Name); exists {
		return nil, &regError{http.StatusConflict, "a cluster named " + sc.Name + " is already configured"}
	}

	entry, err := clusters.EntryFromStored(sc)
	if err != nil {
		return nil, &regError{http.StatusBadRequest, "failed to build a client for this cluster: " + err.Error()}
	}
	if err := clusters.VerifyConnection(entry); err != nil {
		return nil, &regError{http.StatusBadRequest, "could not connect to this cluster: " + explainConnectError(err)}
	}

	if id, ok := clusters.Identity(ctx, entry.Clientset); ok {
		for _, name := range h.Clusters.Names() {
			if other, ok := h.Clusters.Get(name); ok {
				if oid, ok := clusters.Identity(ctx, other.Clientset); ok && oid == id {
					return nil, &regError{http.StatusConflict, "this cluster is already registered as " + name}
				}
			}
		}
	}

	if sc.Type == "" || !clusters.ValidType(sc.Type) {
		sc.Type = clusters.DetectType(ctx, entry.Clientset, hint)
	}
	entry.SetType(sc.Type)

	stored, err := h.ClusterStore.Load(ctx)
	if err != nil {
		return nil, &regError{http.StatusInternalServerError, "failed to load existing clusters: " + err.Error()}
	}
	if err := h.ClusterStore.Save(ctx, append(stored, sc)); err != nil {
		return nil, &regError{http.StatusInternalServerError, "failed to save cluster: " + err.Error()}
	}

	h.Clusters.AddDynamic(entry)
	// Server, type and origin — never a credential.
	h.auditLog(r, "add_cluster", "", sc.Name, "server", sc.Server, "type", sc.Type, "origin", sc.Origin)
	return entry, nil
}

func clusterRowFor(entry *clusters.Entry) ClusterRow {
	t := entry.Type()
	return ClusterRow{Name: entry.Name, Removable: entry.Removable, Type: t, CanAddNode: clusters.SupportsAddNode(t), Status: "connected"}
}

// addClusterRequest is the cluster-management UI's "add a cluster" form:
// name + server URL + a bearer token (typically a ServiceAccount token
// from that cluster, the same shape Vinaire's own token-mode login already
// asks users to paste — see AGENTS.md's ClusterRole example). caData is an
// optional base64-free PEM CA certificate; omitting it means the
// connection skips TLS certificate verification, a real tradeoff the
// frontend must surface explicitly rather than silently accept.
type addClusterRequest struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Token  string `json:"token"`
	CAData string `json:"caData"`
}

// AddCluster handles POST /api/v1/clusters — cluster management via the
// web UI, server + bearer token form. Validates the submitted credentials
// against a real API call before persisting or registering anything.
func (h *Handler) AddCluster(w http.ResponseWriter, r *http.Request) {
	var req addClusterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Server = strings.TrimSpace(req.Server)
	req.Token = strings.TrimSpace(req.Token)
	req.CAData = strings.TrimSpace(req.CAData)
	if strings.TrimSpace(req.Name) == "" || req.Server == "" || req.Token == "" {
		respondError(w, http.StatusBadRequest, "name, server, and token are all required")
		return
	}
	entry, err := h.registerCluster(r.Context(), r, clusters.StoredCluster{
		Name: req.Name, Server: req.Server, Token: req.Token, CAData: req.CAData, Origin: clusters.OriginToken,
	}, "")
	if err != nil {
		respondRegError(w, err)
		return
	}
	respondJSON(w, clusterRowFor(entry))
}

// --- kubeconfig import ---

const maxKubeconfigBytes = 1 << 20

type kubeconfigRequest struct {
	Name       string `json:"name"`
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
	Type       string `json:"type"`
}

// KubeconfigPreview describes a pasted kubeconfig WITHOUT any credential, so
// the UI can ask which context to use and warn about what it holds.
type KubeconfigPreview struct {
	Contexts       []string `json:"contexts"`
	Current        string   `json:"current"`
	Selected       string   `json:"selected"`
	Server         string   `json:"server"`
	CredentialKind string   `json:"credentialKind"`
	InsecureTLS    bool     `json:"insecureTls"`
	Problem        string   `json:"problem,omitempty"`
}

func decodeKubeconfigRequest(w http.ResponseWriter, r *http.Request) (kubeconfigRequest, bool) {
	var req kubeconfigRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxKubeconfigBytes+4096)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body (a kubeconfig must be under 1 MiB)")
		return req, false
	}
	if strings.TrimSpace(req.Kubeconfig) == "" {
		respondError(w, http.StatusBadRequest, "paste or upload a kubeconfig")
		return req, false
	}
	return req, true
}

// PreviewKubeconfig handles POST /api/v1/clusters/import-kubeconfig/preview.
func (h *Handler) PreviewKubeconfig(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeKubeconfigRequest(w, r)
	if !ok {
		return
	}
	cfg, err := clientcmd.Load([]byte(req.Kubeconfig))
	if err != nil {
		respondError(w, http.StatusBadRequest, "couldn't parse the kubeconfig: "+err.Error())
		return
	}
	p := KubeconfigPreview{Current: cfg.CurrentContext, Contexts: []string{}}
	for name := range cfg.Contexts {
		p.Contexts = append(p.Contexts, name)
	}
	sort.Strings(p.Contexts)
	if info, err := clusters.ParseKubeconfig([]byte(req.Kubeconfig), clusters.ParseOptions{Context: req.Context}); err != nil {
		p.Problem = err.Error()
	} else {
		p.Selected, p.Server, p.CredentialKind, p.InsecureTLS = info.Context, info.Server, info.CredentialKind, info.InsecureTLS
	}
	respondJSON(w, p)
}

// ImportKubeconfig handles POST /api/v1/clusters/import-kubeconfig — add a
// cluster from a kubeconfig with no SSH involved; works with any
// distribution. Only the chosen context is kept, only inline credentials are
// accepted, and exec/auth-provider plugins are refused (they'd run commands
// on the Vinaire host).
func (h *Handler) ImportKubeconfig(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeKubeconfigRequest(w, r)
	if !ok {
		return
	}
	info, err := clusters.ParseKubeconfig([]byte(req.Kubeconfig), clusters.ParseOptions{Context: req.Context})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = info.Context
	}
	entry, err := h.registerCluster(r.Context(), r, clusters.StoredCluster{
		Name: name, Server: info.Server, Kubeconfig: string(info.Kubeconfig), Type: req.Type, Origin: clusters.OriginKubeconfig,
	}, info.Context)
	if err != nil {
		respondRegError(w, err)
		return
	}
	respondJSON(w, clusterRowFor(entry))
}

// --- local discovery ---

// DiscoverClusters handles GET /api/v1/clusters/discover.
func (h *Handler) DiscoverClusters(w http.ResponseWriter, r *http.Request) {
	var registered []*clusters.Entry
	for _, name := range h.Clusters.Names() {
		if e, ok := h.Clusters.Get(name); ok {
			registered = append(registered, e)
		}
	}
	respondJSON(w, clusters.Discover(r.Context(), clusters.DiscoverOptions{ExtraPaths: h.DiscoveryPaths, Registered: registered}))
}

type addDiscoveredRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// AddDiscoveredCluster handles POST /api/v1/clusters/add-discovered. The client
// sends only the candidate id from a discovery scan — never a file path — and
// the server re-resolves it against the fixed list of locations it scans.
func (h *Handler) AddDiscoveredCluster(w http.ResponseWriter, r *http.Request) {
	var req addDiscoveredRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.ID == "" {
		respondError(w, http.StatusBadRequest, `request body must be {"id": "<discovered cluster id>"}`)
		return
	}
	info, ctxName, err := clusters.ExtractCandidate(req.ID, h.DiscoveryPaths)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, clusters.ErrNoSuchCandidate) {
			status = http.StatusNotFound
		}
		respondError(w, status, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = ctxName
	}
	entry, err := h.registerCluster(r.Context(), r, clusters.StoredCluster{
		Name: name, Server: info.Server, Kubeconfig: string(info.Kubeconfig), Type: req.Type, Origin: clusters.OriginDiscovered,
	}, ctxName)
	if err != nil {
		respondRegError(w, err)
		return
	}
	respondJSON(w, clusterRowFor(entry))
}

// --- type override ---

type setTypeRequest struct {
	Type string `json:"type"`
}

// SetClusterType handles PUT /api/v1/clusters/{name}/type — correct a wrong
// detection. Persisted for clusters added through the UI; for an
// env-configured cluster it lasts until the next restart.
func (h *Handler) SetClusterType(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	entry, ok := h.Clusters.Get(name)
	if !ok {
		respondError(w, http.StatusNotFound, "no such cluster")
		return
	}
	var req setTypeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || !clusters.ValidType(req.Type) {
		respondError(w, http.StatusBadRequest, "type must be one of k3s, k3d, kind, minikube, microk8s, kubeadm, managed, unknown")
		return
	}

	persisted := false
	if entry.Removable && h.ClusterStore != nil {
		stored, err := h.ClusterStore.Load(r.Context())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to load existing clusters: "+err.Error())
			return
		}
		for i := range stored {
			if stored[i].Name == name {
				stored[i].Type = req.Type
				persisted = true
			}
		}
		if persisted {
			if err := h.ClusterStore.Save(r.Context(), stored); err != nil {
				respondError(w, http.StatusInternalServerError, "failed to save clusters: "+err.Error())
				return
			}
		}
	}
	entry.SetType(req.Type)
	h.auditLog(r, "set_cluster_type", "", name, "type", req.Type)
	respondJSON(w, map[string]any{"status": "ok", "persisted": persisted})
}

// RemoveCluster handles DELETE /api/v1/clusters/{name} — the counterpart
// to AddCluster. Removes only Vinaire's own registration of the cluster; the
// cluster itself is never touched. Refuses to remove anything not added
// dynamically, since those would just reappear on the next restart from their
// env var anyway.
func (h *Handler) RemoveCluster(w http.ResponseWriter, r *http.Request) {
	if h.ClusterStore == nil {
		respondError(w, http.StatusServiceUnavailable, "cluster management is unavailable here")
		return
	}

	name := chi.URLParam(r, "name")
	entry, exists := h.Clusters.Get(name)
	if !exists {
		respondError(w, http.StatusNotFound, "no such cluster")
		return
	}
	if !entry.Removable {
		respondError(w, http.StatusBadRequest, "this cluster is configured via an environment variable and can't be removed here")
		return
	}

	stored, err := h.ClusterStore.Load(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load existing clusters: "+err.Error())
		return
	}
	kept := make([]clusters.StoredCluster, 0, len(stored))
	for _, sc := range stored {
		if sc.Name != name {
			kept = append(kept, sc)
		}
	}
	if err := h.ClusterStore.Save(r.Context(), kept); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save clusters: "+err.Error())
		return
	}

	h.Clusters.Remove(name)
	statusMu.Lock()
	delete(statusCache, name)
	statusMu.Unlock()
	h.auditLog(r, "remove_cluster", "", name)
	respondJSON(w, map[string]string{"status": "ok"})
}
