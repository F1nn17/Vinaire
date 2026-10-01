package clusters

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Roadmap Phase 34, local discovery: for Vinaire running directly on a home
// machine or home server (outside any cluster), find other clusters on this
// machine — a second k3s, k3d and kind clusters, minikube — from what the
// Vinaire process can already see.
//
// This is read-only and strictly non-escalating: it reads kubeconfig files the
// process may open, never invokes sudo or changes permissions, and never
// touches the Docker socket (k3d/kind write their own kubeconfig contexts, so
// they are found that way). It never returns a credential — candidates carry
// names, server URLs, a type and "what kind of credential / is it cluster
// admin", nothing more — and adding one extracts only the chosen context.

// Candidate is a cluster found on this machine.
type Candidate struct {
	ID           string `json:"id"`
	Name         string `json:"name"` // suggested registry name
	Source       string `json:"source"`
	Context      string `json:"context"`
	Server       string `json:"server"`
	Type         string `json:"type"`
	Reachable    bool   `json:"reachable"`
	Version      string `json:"version,omitempty"`
	Credential   string `json:"credential"`
	ClusterAdmin *bool  `json:"clusterAdmin"` // nil = couldn't tell
	AlreadyAdded bool   `json:"alreadyAdded"`
	// Problem says why a candidate can't be added (e.g. an exec plugin).
	Problem string `json:"problem,omitempty"`
}

// UnreadableFile is a kubeconfig that exists but this process may not open.
type UnreadableFile struct {
	Path string `json:"path"`
	Hint string `json:"hint"`
}

// ListeningAPI is a Kubernetes API answering on a local port for which no
// credentials were found.
type ListeningAPI struct {
	Address string `json:"address"`
	Note    string `json:"note"`
}

// DiscoverResult is what GET /api/v1/clusters/discover returns.
type DiscoverResult struct {
	Available     bool             `json:"available"`
	Reason        string           `json:"reason,omitempty"`
	Candidates    []Candidate      `json:"candidates"`
	Unreadable    []UnreadableFile `json:"unreadable"`
	ListeningAPIs []ListeningAPI   `json:"listeningApis"`
	SearchedPaths []string         `json:"searchedPaths"`
}

// DiscoverOptions says where to look and what is already registered.
type DiscoverOptions struct {
	ExtraPaths []string
	Registered []*Entry
}

const k3sKubeconfig = "/etc/rancher/k3s/k3s.yaml"

// wellKnownLocalPorts are probed for a kube-apiserver: k3s/kubeadm/minikube's
// 6443, MicroK8s's 16443. A fixed list — never user-supplied.
var wellKnownLocalPorts = []string{"127.0.0.1:6443", "127.0.0.1:16443"}

func searchPaths(extra []string) []string {
	var paths []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		for _, q := range paths {
			if q == p {
				return
			}
		}
		paths = append(paths, p)
	}
	for _, p := range filepath.SplitList(os.Getenv("KUBECONFIG")) {
		add(p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".kube", "config"))
	}
	add(k3sKubeconfig)
	for _, p := range extra {
		add(p)
	}
	return paths
}

func candidateID(path, context string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + context))
	return hex.EncodeToString(sum[:8])
}

// inCluster reports whether this process is a pod, where the host's files and
// loopback are out of reach.
func inCluster() bool { return os.Getenv("KUBERNETES_SERVICE_HOST") != "" }

// Discover scans for clusters on this machine.
func Discover(ctx context.Context, opts DiscoverOptions) DiscoverResult {
	res := DiscoverResult{Candidates: []Candidate{}, Unreadable: []UnreadableFile{}, ListeningAPIs: []ListeningAPI{}}
	if inCluster() {
		res.Reason = "Vinaire is running inside a cluster as a pod, so it can't see this machine's kubeconfig files or ports. Run Vinaire directly on the machine (or mount a kubeconfig into it) to use local discovery."
		return res
	}
	res.Available = true
	res.SearchedPaths = searchPaths(opts.ExtraPaths)

	// Identities / URLs of what's already registered, so a second address for
	// the same cluster isn't offered as a new one.
	registeredIDs := map[string]bool{}
	registeredHosts := map[string]bool{}
	for _, e := range opts.Registered {
		registeredHosts[hostKey(e.RESTConfig.Host)] = true
		if id, ok := Identity(ctx, e.Clientset); ok {
			registeredIDs[id] = true
		}
	}

	var cands []Candidate
	for _, path := range res.SearchedPaths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			res.Unreadable = append(res.Unreadable, UnreadableFile{Path: path, Hint: unreadableHint(path)})
			continue
		}
		f.Close()
		cfg, err := clientcmd.LoadFromFile(path)
		if err != nil {
			continue
		}
		for ctxName := range cfg.Contexts {
			cands = append(cands, Candidate{
				ID: candidateID(path, ctxName), Name: ctxName, Source: path, Context: ctxName,
				Type: TypeUnknown, Credential: CredNone,
			})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Name < cands[j].Name })

	// Probe candidates concurrently but boundedly.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range cands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			probeCandidate(ctx, &cands[i], registeredIDs, registeredHosts)
		}()
	}
	wg.Wait()
	res.Candidates = append(res.Candidates, cands...)

	shown := map[string]bool{}
	for _, c := range res.Candidates {
		shown[hostKey(c.Server)] = true
	}
	for _, addr := range wellKnownLocalPorts {
		if shown[addr] || registeredHosts[addr] {
			continue
		}
		if note, ok := probeLocalAPI(ctx, addr); ok {
			res.ListeningAPIs = append(res.ListeningAPIs, ListeningAPI{Address: addr, Note: note})
		}
	}
	return res
}

func hostKey(server string) string {
	u, err := url.Parse(server)
	if err != nil {
		return server
	}
	return u.Host
}

func unreadableHint(path string) string {
	if path == k3sKubeconfig {
		return "k3s keeps its admin kubeconfig readable by root only, and Vinaire doesn't escalate on its own. Copy it somewhere Vinaire's user can read — e.g. `sudo cp " + k3sKubeconfig + " ~/.kube/k3s.yaml && sudo chown $USER ~/.kube/k3s.yaml` — then add that path to DISCOVERY_KUBECONFIG_PATHS (or KUBECONFIG) and rescan."
	}
	return "This file exists but Vinaire's user can't read it. Fix its permissions (or copy it somewhere readable) and rescan."
}

// probeCandidate fills in everything knowable about one candidate. It never
// returns an error: a failure is recorded on the candidate itself.
func probeCandidate(ctx context.Context, c *Candidate, registeredIDs, registeredHosts map[string]bool) {
	info, err := ParseKubeconfigFile(c.Source, ParseOptions{AllowLocalFiles: true, Context: c.Context})
	if err != nil {
		c.Problem = err.Error()
		return
	}
	c.Server, c.Credential = info.Server, info.CredentialKind
	if registeredHosts[hostKey(info.Server)] {
		c.AlreadyAdded = true
	}

	restConfig, err := RESTConfigFromKubeconfig(info.Kubeconfig)
	if err != nil {
		c.Problem = err.Error()
		return
	}
	restConfig.Timeout = 4 * time.Second
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		c.Problem = err.Error()
		return
	}
	v, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return // unreachable: Reachable stays false
	}
	c.Reachable, c.Version = true, v.GitVersion
	c.Type = DetectType(ctx, clientset, c.Context)
	if admin, ok := IsClusterAdmin(ctx, clientset); ok {
		c.ClusterAdmin = &admin
	}
	if id, ok := Identity(ctx, clientset); ok && registeredIDs[id] {
		c.AlreadyAdded = true
	}
}

// probeLocalAPI checks whether addr answers like a Kubernetes API server.
func probeLocalAPI(ctx context.Context, addr string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", false
	}
	conn.Close()
	client := &http.Client{
		Timeout:   1500 * time.Millisecond,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // identity probe only; nothing is sent or trusted
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr+"/version", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var body struct {
		GitVersion string `json:"gitVersion"`
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&body)
	switch {
	case body.GitVersion != "":
		return "A Kubernetes API (" + body.GitVersion + ") is answering here but no kubeconfig for it was found. Add it with \"Import via kubeconfig\" once you have one.", true
	case body.Kind == "Status" && body.APIVersion == "v1":
		return "A Kubernetes API is answering here but needs credentials, and no kubeconfig for it was found. Add it with \"Import via kubeconfig\" once you have one.", true
	}
	return "", false
}

// ErrNoSuchCandidate is returned when a discovered cluster id no longer matches.
var ErrNoSuchCandidate = errors.New("that cluster is no longer found on this machine — rescan")

// ExtractCandidate re-resolves a candidate id (never a path from the client —
// the id must match what a fresh scan of the known locations produces) and
// returns ONLY that context's normalized, fully inline kubeconfig.
func ExtractCandidate(id string, extraPaths []string) (KubeconfigInfo, string, error) {
	if inCluster() {
		return KubeconfigInfo{}, "", errors.New("local discovery isn't available inside a cluster")
	}
	for _, path := range searchPaths(extraPaths) {
		cfg, err := clientcmd.LoadFromFile(path)
		if err != nil {
			continue
		}
		for ctxName := range cfg.Contexts {
			if candidateID(path, ctxName) != id {
				continue
			}
			info, err := ParseKubeconfigFile(path, ParseOptions{AllowLocalFiles: true, Context: ctxName})
			if err != nil {
				return KubeconfigInfo{}, "", fmt.Errorf("can't add %s: %w", ctxName, err)
			}
			return info, ctxName, nil
		}
	}
	return KubeconfigInfo{}, "", ErrNoSuchCandidate
}
