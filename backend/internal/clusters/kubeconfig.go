package clusters

import (
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/shiraku/vinaire/backend/internal/netguard"
	"net"
	"net/url"
	"os"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Roadmap Phase 34: turning a kubeconfig someone hands us (pasted, uploaded,
// read off a server, or found on this machine) into a registered cluster.
//
// A kubeconfig is not just data: it can name a command to run for credentials
// (`exec`, `auth-provider`) and point at files to read (`client-key`,
// `tokenFile`). Honouring those would let a pasted kubeconfig execute programs
// as the Vinaire process or read arbitrary local files. So ParseKubeconfig
// accepts only the safe subset — inline certificate/token data — and refuses
// the rest, and it extracts exactly ONE context (never the whole file, which
// may hold credentials for clusters the user didn't pick).

// Credential kinds reported to the UI.
const (
	CredClientCert = "client-certificate"
	CredToken      = "token"
	CredNone       = "none"
)

// KubeconfigInfo describes the single context ParseKubeconfig extracted. It
// never carries a credential value except inside Kubeconfig (the normalized
// kubeconfig, which goes straight to the encrypted store).
type KubeconfigInfo struct {
	Context        string
	Server         string
	CredentialKind string
	InsecureTLS    bool
	// Kubeconfig is a minimal, fully inline, single-context kubeconfig.
	Kubeconfig []byte
}

// ParseOptions controls what a kubeconfig may reference.
type ParseOptions struct {
	// AllowLocalFiles lets certificate/key/token *file paths* be read and
	// inlined. Only for kubeconfig files already on this machine (discovery) —
	// never for text a user pasted or a remote server returned.
	AllowLocalFiles bool
	// Context picks a context; empty uses current-context, or the only one.
	Context string
	// ServerOverride replaces the cluster's server URL (e.g. k3s.yaml's
	// 127.0.0.1 with the machine's real address).
	ServerOverride string
}

// checkServer rejects a server URL that is not https, or that points at
// addresses with no business being a Kubernetes API (link-local — cloud
// metadata — unspecified, multicast).
func CheckServer(server string) error {
	return checkServer(server)
}

func checkServer(server string) error {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return errors.New("the cluster has no valid server URL")
	}
	if u.Scheme != "https" {
		return errors.New("the cluster server URL must be https")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("the server address %s isn't allowed", ip)
		}
	}
	return nil
}

// ParseKubeconfig validates data and returns the chosen context, normalized.
func ParseKubeconfig(data []byte, opts ParseOptions) (KubeconfigInfo, error) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return KubeconfigInfo{}, fmt.Errorf("couldn't parse the kubeconfig: %w", err)
	}
	return normalize(cfg, opts)
}

// ParseKubeconfigFile is ParseKubeconfig for a file on this machine, so
// relative file references resolve against its directory.
func ParseKubeconfigFile(path string, opts ParseOptions) (KubeconfigInfo, error) {
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return KubeconfigInfo{}, err
	}
	// LoadFromFile leaves relative file references as written; make them
	// relative to this file, as kubectl does.
	if err := clientcmd.ResolveLocalPaths(cfg); err != nil {
		return KubeconfigInfo{}, err
	}
	return normalize(cfg, opts)
}

func normalize(cfg *clientcmdapi.Config, opts ParseOptions) (KubeconfigInfo, error) {
	ctxName := opts.Context
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}
	if ctxName == "" && len(cfg.Contexts) == 1 {
		for n := range cfg.Contexts {
			ctxName = n
		}
	}
	if ctxName == "" {
		return KubeconfigInfo{}, errors.New("the kubeconfig has several contexts and none is selected — pick one")
	}
	kctx, ok := cfg.Contexts[ctxName]
	if !ok {
		return KubeconfigInfo{}, fmt.Errorf("the kubeconfig has no context %q", ctxName)
	}
	cluster, ok := cfg.Clusters[kctx.Cluster]
	if !ok || cluster == nil {
		return KubeconfigInfo{}, fmt.Errorf("context %q refers to a cluster that isn't defined", ctxName)
	}
	user := cfg.AuthInfos[kctx.AuthInfo]
	if user == nil {
		user = &clientcmdapi.AuthInfo{}
	}

	// Refuse anything that would run a program or reach outside the document.
	switch {
	case user.Exec != nil:
		return KubeconfigInfo{}, errors.New("this context gets its credentials from an exec plugin, which would run a command on the Vinaire host — not supported; use a token or a client certificate instead")
	case user.AuthProvider != nil:
		return KubeconfigInfo{}, errors.New("this context uses an auth-provider plugin — not supported; use a token or a client certificate instead")
	case user.Username != "" || user.Password != "":
		return KubeconfigInfo{}, errors.New("basic-auth credentials aren't supported")
	case cluster.ProxyURL != "":
		return KubeconfigInfo{}, errors.New("a proxy-url in the kubeconfig isn't supported")
	}

	cluster = cluster.DeepCopy()
	user = user.DeepCopy()
	if err := inlineFiles(cluster, user, opts.AllowLocalFiles); err != nil {
		return KubeconfigInfo{}, err
	}
	if opts.ServerOverride != "" {
		cluster.Server = opts.ServerOverride
	}
	if err := checkServer(cluster.Server); err != nil {
		return KubeconfigInfo{}, err
	}

	cred := CredNone
	switch {
	case len(user.ClientCertificateData) > 0 && len(user.ClientKeyData) > 0:
		cred = CredClientCert
	case user.Token != "":
		cred = CredToken
	}

	out := clientcmdapi.NewConfig()
	out.Clusters["cluster"] = cluster
	out.AuthInfos["user"] = user
	out.Contexts["context"] = &clientcmdapi.Context{Cluster: "cluster", AuthInfo: "user"}
	out.CurrentContext = "context"
	normalized, err := clientcmd.Write(*out)
	if err != nil {
		return KubeconfigInfo{}, fmt.Errorf("couldn't re-encode the kubeconfig: %w", err)
	}
	return KubeconfigInfo{
		Context: ctxName, Server: cluster.Server, CredentialKind: cred,
		InsecureTLS: cluster.InsecureSkipTLSVerify, Kubeconfig: normalized,
	}, nil
}

// inlineFiles makes every credential field inline data. For untrusted input
// (allowFiles=false) any file reference is an error; for a local file the
// referenced files are read in.
func inlineFiles(cluster *clientcmdapi.Cluster, user *clientcmdapi.AuthInfo, allowFiles bool) error {
	read := func(path string, dst *[]byte, what string) error {
		if path == "" {
			return nil
		}
		if !allowFiles {
			return fmt.Errorf("the kubeconfig refers to a %s file (%s); paste a kubeconfig with the certificate data inline instead", what, "file reference not allowed")
		}
		b, err := os.ReadFile(path) // #nosec G304 -- a kubeconfig path from this machine's own discovery list, never from a request
		if err != nil {
			return fmt.Errorf("couldn't read the %s file: %w", what, err)
		}
		*dst = b
		return nil
	}
	if err := read(cluster.CertificateAuthority, &cluster.CertificateAuthorityData, "certificate-authority"); err != nil {
		return err
	}
	cluster.CertificateAuthority = ""
	if err := read(user.ClientCertificate, &user.ClientCertificateData, "client-certificate"); err != nil {
		return err
	}
	user.ClientCertificate = ""
	if err := read(user.ClientKey, &user.ClientKeyData, "client-key"); err != nil {
		return err
	}
	user.ClientKey = ""
	if user.TokenFile != "" {
		var tok []byte
		if err := read(user.TokenFile, &tok, "token"); err != nil {
			return err
		}
		user.Token = strings.TrimSpace(string(tok))
		user.TokenFile = ""
	}
	return nil
}

// RESTConfigFromKubeconfig builds a REST config from a (normalized) kubeconfig.
func RESTConfigFromKubeconfig(data []byte) (*rest.Config, error) {
	cfg, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, err
	}
	// Defence in depth: a stored kubeconfig never needs these, so make sure a
	// tampered one can't smuggle a command in.
	cfg.ExecProvider = nil
	cfg.AuthProvider = nil
	guardDial(cfg)
	return cfg, nil
}

// guardDial makes every connection to a registered cluster refuse link-local
// (cloud metadata), multicast and unspecified addresses — checked on the address
// actually dialled, so a hostname that resolves there (or is rebound to it) is
// refused too. Loopback and private ranges stay allowed: that is where clusters
// live (k3d, kind, a LAN).
func guardDial(cfg *rest.Config) {
	if cfg.Dial == nil {
		cfg.Dial = netguard.DialContext(true)
	}
}

func encodeKey(key []byte) string { return base64.StdEncoding.EncodeToString(key) }
