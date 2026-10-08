// Package keyprovider keeps the master key of Vinaire's encrypted store outside the
// cluster that Vinaire runs in (Roadmap Phase 49).
//
// The store (registry of clusters, GitOps and sign-in settings, shared sessions) is
// sealed with a 32-byte data key. By default that key sits in a Secret next to the data,
// which protects a leaked backup or a stray `cat`, but not someone who can read every
// Secret of Vinaire's namespace. With a provider the data key is stored only *wrapped*
// by a key that never leaves an external service — HashiCorp Vault's (or OpenBao's)
// Transit engine — so reading the Secrets gets the attacker a blob that is useless
// without being able to ask that service, which has its own access control and audit
// log. Vinaire asks it once, at start, to unwrap the data key.
package keyprovider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Provider wraps and unwraps the store's data key.
type Provider interface {
	// Name is recorded next to the wrapped key, so a store wrapped by one provider
	// isn't silently opened with another.
	Name() string
	// Wrap returns an opaque text form of dek that only Unwrap can reverse.
	Wrap(ctx context.Context, dek []byte) (string, error)
	Unwrap(ctx context.Context, wrapped string) ([]byte, error)
}

// VaultConfig describes a Transit key. Everything comes from the operator's
// configuration (environment), never from a request.
type VaultConfig struct {
	Addr         string // https://vault.example.com:8200
	Namespace    string // Vault Enterprise / HCP namespace, optional
	TransitMount string // default "transit"
	KeyName      string // default "vinaire"

	// Authentication: a static token (Token or TokenFile), or Kubernetes auth with
	// the pod's ServiceAccount token (K8sRole non-empty).
	Token     string
	TokenFile string
	K8sRole   string
	K8sMount  string // default "kubernetes"
	JWTFile   string // default the pod's ServiceAccount token

	// AllowInsecure permits http:// (a local dev Vault). Never on a real deployment.
	AllowInsecure bool
	HTTP          *http.Client
}

// ErrNotConfigured is returned by FromEnv when no provider was asked for.
var ErrNotConfigured = errors.New("no key provider configured")

// FromEnv builds the provider KEY_PROVIDER asks for: "" or "local" = none (the data key
// stays in a Secret or a file), "vault" = Transit.
func FromEnv() (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KEY_PROVIDER"))) {
	case "", "local":
		return nil, ErrNotConfigured
	case "vault", "openbao":
		return NewVault(VaultConfig{
			Addr:          os.Getenv("VAULT_ADDR"),
			Namespace:     os.Getenv("VAULT_NAMESPACE"),
			TransitMount:  os.Getenv("VAULT_TRANSIT_MOUNT"),
			KeyName:       os.Getenv("VAULT_TRANSIT_KEY"),
			Token:         os.Getenv("VAULT_TOKEN"),
			TokenFile:     os.Getenv("VAULT_TOKEN_FILE"),
			K8sRole:       os.Getenv("VAULT_K8S_ROLE"),
			K8sMount:      os.Getenv("VAULT_K8S_MOUNT"),
			AllowInsecure: os.Getenv("VAULT_ALLOW_INSECURE") == "true",
		})
	}
	return nil, fmt.Errorf("unknown KEY_PROVIDER %q (want local or vault)", os.Getenv("KEY_PROVIDER"))
}

type vault struct {
	cfg VaultConfig
	hc  *http.Client

	mu        sync.Mutex
	token     string
	tokenTill time.Time
}

// NewVault checks the configuration and returns the provider (it doesn't contact Vault yet).
func NewVault(cfg VaultConfig) (Provider, error) {
	u, err := url.Parse(cfg.Addr)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, errors.New("VAULT_ADDR must be a URL like https://vault.example.com:8200")
	}
	if u.Scheme == "http" && !cfg.AllowInsecure {
		return nil, errors.New("VAULT_ADDR is http:// — the data key would travel unencrypted; use https (VAULT_ALLOW_INSECURE=true is for local development only)")
	}
	if cfg.TransitMount == "" {
		cfg.TransitMount = "transit"
	}
	if cfg.KeyName == "" {
		cfg.KeyName = "vinaire"
	}
	if cfg.K8sMount == "" {
		cfg.K8sMount = "kubernetes"
	}
	if cfg.JWTFile == "" {
		cfg.JWTFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}
	for field, v := range map[string]string{"VAULT_TRANSIT_MOUNT": cfg.TransitMount, "VAULT_TRANSIT_KEY": cfg.KeyName, "VAULT_K8S_MOUNT": cfg.K8sMount} {
		if !pathPart.MatchString(v) {
			return nil, fmt.Errorf("%s may only hold letters, digits, '-', '_' and '/'", field)
		}
	}
	if cfg.Token == "" && cfg.TokenFile == "" && cfg.K8sRole == "" {
		return nil, errors.New("KEY_PROVIDER=vault needs a way to sign in: VAULT_TOKEN / VAULT_TOKEN_FILE, or VAULT_K8S_ROLE for Kubernetes auth")
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &vault{cfg: cfg, hc: hc}, nil
}

func (v *vault) Name() string { return "vault:" + v.cfg.TransitMount + "/" + v.cfg.KeyName }

func (v *vault) call(ctx context.Context, method, path string, body any, out any, auth bool) (int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(v.cfg.Addr, "/")+"/v1/"+path, rd)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if v.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.cfg.Namespace)
	}
	if auth {
		t, err := v.clientToken(ctx, false)
		if err != nil {
			return 0, err
		}
		req.Header.Set("X-Vault-Token", t)
	}
	resp, err := v.hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("reach Vault: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		// Vault's error text names what is wrong (permission denied, missing key); it
		// never echoes our request body.
		var e struct {
			Errors []string `json:"errors"`
		}
		_ = json.Unmarshal(raw, &e)
		return resp.StatusCode, fmt.Errorf("Vault answered %d: %s", resp.StatusCode, strings.Join(e.Errors, "; "))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("unexpected answer from Vault: %w", err)
		}
	}
	return resp.StatusCode, nil
}

var pathPart = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)

// clientToken returns a Vault token: the static one, or one from Kubernetes auth
// (cached until shortly before it expires; renew forces a new login).
func (v *vault) clientToken(ctx context.Context, renew bool) (string, error) {
	if v.cfg.K8sRole == "" {
		if v.cfg.Token != "" {
			return v.cfg.Token, nil
		}
		b, err := os.ReadFile(v.cfg.TokenFile) // #nosec G304 -- operator-configured path
		if err != nil {
			return "", fmt.Errorf("read VAULT_TOKEN_FILE: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	v.mu.Lock()
	if !renew && v.token != "" && time.Now().Before(v.tokenTill) {
		t := v.token
		v.mu.Unlock()
		return t, nil
	}
	v.mu.Unlock()

	jwt, err := os.ReadFile(v.cfg.JWTFile) // #nosec G304 -- the pod's own ServiceAccount token
	if err != nil {
		return "", fmt.Errorf("read the ServiceAccount token for Vault's Kubernetes auth: %w", err)
	}
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if _, err := v.call(ctx, http.MethodPost, "auth/"+v.cfg.K8sMount+"/login",
		map[string]string{"role": v.cfg.K8sRole, "jwt": strings.TrimSpace(string(jwt))}, &out, false); err != nil {
		return "", fmt.Errorf("Vault Kubernetes login: %w", err)
	}
	if out.Auth.ClientToken == "" {
		return "", errors.New("Vault Kubernetes login returned no token")
	}
	ttl := time.Duration(out.Auth.LeaseDuration) * time.Second
	if ttl <= 0 || ttl > time.Hour {
		ttl = time.Hour
	}
	v.mu.Lock()
	v.token, v.tokenTill = out.Auth.ClientToken, time.Now().Add(ttl*8/10)
	t := v.token
	v.mu.Unlock()
	return t, nil
}

func (v *vault) Wrap(ctx context.Context, dek []byte) (string, error) {
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	_, err := v.call(ctx, http.MethodPost, v.cfg.TransitMount+"/encrypt/"+v.cfg.KeyName,
		map[string]string{"plaintext": base64.StdEncoding.EncodeToString(dek)}, &out, true)
	if err != nil {
		return "", err
	}
	if out.Data.Ciphertext == "" {
		return "", errors.New("Vault returned no ciphertext")
	}
	return out.Data.Ciphertext, nil
}

func (v *vault) Unwrap(ctx context.Context, wrapped string) ([]byte, error) {
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	_, err := v.call(ctx, http.MethodPost, v.cfg.TransitMount+"/decrypt/"+v.cfg.KeyName,
		map[string]string{"ciphertext": wrapped}, &out, true)
	if err != nil {
		return nil, err
	}
	dek, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil || len(dek) != 32 {
		return nil, errors.New("Vault returned something that isn't a 32-byte data key")
	}
	return dek, nil
}

// Rewrapper is implemented by providers that can re-encrypt a wrapped key under the
// newest version of their key without the key itself passing through Vinaire.
type Rewrapper interface {
	Rewrap(ctx context.Context, wrapped string) (string, error)
}

// Rewrap moves a wrapped data key to the current version of the Transit key (after
// `vault write -f transit/keys/vinaire/rotate`).
func (v *vault) Rewrap(ctx context.Context, wrapped string) (string, error) {
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if _, err := v.call(ctx, http.MethodPost, v.cfg.TransitMount+"/rewrap/"+v.cfg.KeyName, map[string]string{"ciphertext": wrapped}, &out, true); err != nil {
		return "", err
	}
	if out.Data.Ciphertext == "" {
		return "", errors.New("Vault returned no ciphertext")
	}
	return out.Data.Ciphertext, nil
}
