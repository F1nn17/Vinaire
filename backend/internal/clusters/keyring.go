package clusters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/f1nn17/vinaire/backend/internal/keyprovider"
)

// Roadmap Phase 49: where the store's data key lives. Without a provider it is the
// raw key in a Secret or a file (as before). With one (Vault Transit) only the key
// *wrapped* by the provider is stored, and the raw key exists only in Vinaire's memory.

// StoreOptions are the key-related settings both stores take.
type StoreOptions struct {
	EnvKey   string   // CLUSTER_STORE_KEY
	Previous []string // CLUSTER_STORE_KEY_PREVIOUS
	// Provider keeps the data key outside the cluster; nil = the key sits in a Secret/file.
	Provider keyprovider.Provider
	// Rewrap re-encrypts the wrapped key under the provider key's newest version at start
	// (set it once after rotating the provider's key).
	Rewrap bool
}

// ErrKeyUnavailable means the store exists but its key can't be obtained with the
// configuration given (wrapped by a provider that isn't configured, or by another one).
// Starting anyway would mean running with an empty or foreign registry, so it is fatal.
var ErrKeyUnavailable = errors.New("the store's key can't be obtained with this configuration")

// keyBackend is where the (wrapped or raw) data key is kept.
type keyBackend interface {
	// read returns whichever forms exist; empty values mean "not stored".
	read(ctx context.Context) (raw []byte, wrapped, provider string, err error)
	// write stores the key; an empty raw removes the raw form (only after the wrapped
	// form is safely written).
	write(ctx context.Context, raw []byte, wrapped, provider string) error
}

// resolveDataKey returns the 32-byte key to seal the store with, creating or migrating
// it as needed. It never falls back to storing a key in the clear when a provider is
// configured: failing to reach the provider is an error.
func resolveDataKey(ctx context.Context, kb keyBackend, prov keyprovider.Provider, rewrap bool) ([]byte, error) {
	raw, wrapped, pname, err := kb.read(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case wrapped != "":
		if prov == nil {
			return nil, fmt.Errorf("%w: it is wrapped by %q, but KEY_PROVIDER isn't set — set it back, or restore the store from a backup", ErrKeyUnavailable, pname)
		}
		if pname != prov.Name() {
			return nil, fmt.Errorf("%w: it was wrapped by %q but the configured provider is %q", ErrKeyUnavailable, pname, prov.Name())
		}
		dek, err := prov.Unwrap(ctx, wrapped)
		if err != nil {
			return nil, fmt.Errorf("unwrap the store key with %s: %w", pname, err)
		}
		if len(raw) > 0 { // a raw copy left over from a half-finished migration
			if werr := kb.write(ctx, nil, wrapped, pname); werr != nil {
				slog.Warn("key provider: couldn't remove the leftover raw key", "error", werr)
			}
		}
		if rewrap {
			if rw, ok := prov.(keyprovider.Rewrapper); ok {
				if nw, err := rw.Rewrap(ctx, wrapped); err != nil {
					slog.Warn("key provider: couldn't rewrap the store key under the newest key version", "error", err)
				} else if err := kb.write(ctx, nil, nw, pname); err != nil {
					slog.Warn("key provider: couldn't save the rewrapped store key", "error", err)
				} else {
					slog.Info("key provider: the store key was rewrapped under the newest version of the provider key")
				}
			}
		}
		return dek, nil
	case len(raw) == 32:
		if prov == nil {
			return raw, nil
		}
		// moving to a provider: the same key, now wrapped; the data needs no re-encryption
		w, err := prov.Wrap(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("wrap the existing store key with %s: %w", prov.Name(), err)
		}
		if err := kb.write(ctx, nil, w, prov.Name()); err != nil {
			return nil, fmt.Errorf("save the wrapped store key: %w", err)
		}
		slog.Info("key provider: the store key is now kept only wrapped by " + prov.Name() + "; the raw copy was removed")
		return raw, nil
	case len(raw) != 0:
		return nil, errors.New("the stored key isn't 32 bytes")
	}
	dek, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	if prov == nil {
		return dek, kb.write(ctx, dek, "", "")
	}
	w, err := prov.Wrap(ctx, dek)
	if err != nil {
		return nil, fmt.Errorf("wrap a new store key with %s: %w", prov.Name(), err)
	}
	return dek, kb.write(ctx, nil, w, prov.Name())
}

// ---- Secret ----

type secretKeyBackend struct{ s *SecretStore }

func (b secretKeyBackend) read(ctx context.Context) ([]byte, string, string, error) {
	sec, err := b.s.clientset.CoreV1().Secrets(b.s.namespace).Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", err
	}
	return sec.Data[storeKeySecretKey], string(sec.Data[storeKeyWrappedKey]), string(sec.Data[storeKeyProviderKey]), nil
}

func (b secretKeyBackend) write(ctx context.Context, raw []byte, wrapped, provider string) error {
	secrets := b.s.clientset.CoreV1().Secrets(b.s.namespace)
	sec, err := secrets.Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		data := map[string][]byte{}
		if len(raw) > 0 {
			data[storeKeySecretKey] = raw
		}
		if wrapped != "" {
			data[storeKeyWrappedKey], data[storeKeyProviderKey] = []byte(wrapped), []byte(provider)
		}
		_, err = secrets.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: storeKeySecretName, Namespace: b.s.namespace}, Data: data}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if sec.Data == nil {
		sec.Data = map[string][]byte{}
	}
	if wrapped != "" {
		sec.Data[storeKeyWrappedKey], sec.Data[storeKeyProviderKey] = []byte(wrapped), []byte(provider)
	}
	if len(raw) > 0 {
		sec.Data[storeKeySecretKey] = raw
	} else {
		delete(sec.Data, storeKeySecretKey)
	}
	_, err = secrets.Update(ctx, sec, metav1.UpdateOptions{})
	return err
}

// ---- file ----

type fileKeyBackend struct{ dir string }

type wrappedFile struct {
	Provider string `json:"provider"`
	Wrapped  string `json:"wrapped"`
}

func (b fileKeyBackend) rawPath() string { return filepath.Join(b.dir, fileStoreKeyName) }
func (b fileKeyBackend) wrappedPath() string {
	return filepath.Join(b.dir, fileStoreKeyName+".wrapped")
}

func (b fileKeyBackend) read(context.Context) ([]byte, string, string, error) {
	var raw []byte
	if data, err := os.ReadFile(b.rawPath()); err == nil { // #nosec G304 -- inside the data directory
		k, perr := ParseKey(string(data))
		if perr != nil {
			return nil, "", "", fmt.Errorf("%s is not a valid key file: %w", b.rawPath(), perr)
		}
		raw = k
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", "", err
	}
	var w wrappedFile
	if data, err := os.ReadFile(b.wrappedPath()); err == nil { // #nosec G304 -- inside the data directory
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, "", "", fmt.Errorf("%s is not a valid wrapped-key file: %w", b.wrappedPath(), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", "", err
	}
	return raw, w.Wrapped, w.Provider, nil
}

func (b fileKeyBackend) write(_ context.Context, raw []byte, wrapped, provider string) error {
	if wrapped != "" {
		data, _ := json.Marshal(wrappedFile{Provider: provider, Wrapped: wrapped})
		if err := os.WriteFile(b.wrappedPath(), data, 0o600); err != nil {
			return err
		}
	}
	if len(raw) > 0 {
		f, err := os.OpenFile(b.rawPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- inside the data directory
		if err != nil {
			return fmt.Errorf("create key file: %w", err)
		}
		defer f.Close()
		_, err = f.WriteString(encodeKey(raw) + "\n")
		return err
	}
	if err := os.Remove(b.rawPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
