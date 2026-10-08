package clusters

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// a provider that "wraps" by reversing and tagging; it can be switched off
type fakeProvider struct {
	name     string
	down     bool
	unwraps  int
	rewraps  int
	lastWrap string
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Wrap(_ context.Context, dek []byte) (string, error) {
	if f.down {
		return "", errors.New("provider down")
	}
	r := make([]byte, len(dek))
	for i := range dek {
		r[len(dek)-1-i] = dek[i] ^ 0xa5
	}
	f.lastWrap = "fake:v1:" + base64.StdEncoding.EncodeToString(r)
	return f.lastWrap, nil
}
func (f *fakeProvider) Unwrap(_ context.Context, w string) ([]byte, error) {
	f.unwraps++
	if f.down {
		return nil, errors.New("provider down")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimPrefix(w, "fake:v2:"), "fake:v1:"))
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(raw))
	for i := range raw {
		out[len(raw)-1-i] = raw[i] ^ 0xa5
	}
	return out, nil
}
func (f *fakeProvider) Rewrap(_ context.Context, w string) (string, error) {
	f.rewraps++
	return strings.Replace(w, "fake:v1:", "fake:v2:", 1), nil
}

func sample() []StoredCluster {
	return []StoredCluster{{Name: "prod", Server: "https://10.0.0.1:6443", Token: "PROD-TOKEN-VALUE", CAData: "ca", Origin: OriginToken}}
}

func TestFileStoreKeyIsOnlyKeptWrappedWithAProvider(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	prov := &fakeProvider{name: "fake:test"}
	st, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, sample()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store.key")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a raw key file exists next to the data")
	}
	wrapped, err := os.ReadFile(filepath.Join(dir, "store.key.wrapped"))
	if err != nil || !bytes.Contains(wrapped, []byte("fake:v1:")) {
		t.Fatalf("wrapped key file: %v %s", err, wrapped)
	}
	// a restart: the key comes back through the provider
	st2, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st2.Load(ctx); err != nil || len(got) != 1 || got[0].Token != "PROD-TOKEN-VALUE" {
		t.Fatalf("after restart: %v %v", got, err)
	}
	// without the provider the data cannot be opened — and nothing falls back to a fresh key
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{}); err == nil || !strings.Contains(err.Error(), "wrapped") {
		t.Errorf("no provider: %v", err)
	}
	// the wrong provider
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: &fakeProvider{name: "fake:other"}}); err == nil {
		t.Error("another provider was accepted")
	}
	// the provider is down: an error, not a store on a new key
	prov.down = true
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov}); err == nil {
		t.Error("a store was opened while the provider was down")
	}
	// the environment key and a provider are two ways of doing one thing
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov, EnvKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))}); err == nil {
		t.Error("CLUSTER_STORE_KEY together with a provider was accepted")
	}
}

func TestMovingAnExistingStoreToAProviderKeepsTheData(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	plain, err := NewFileStoreOpts(ctx, dir, StoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = plain.Save(ctx, sample())
	if _, err := os.Stat(filepath.Join(dir, "store.key")); err != nil {
		t.Fatal("expected a raw key file before the move")
	}
	prov := &fakeProvider{name: "fake:test"}
	moved, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := moved.Load(ctx); err != nil || len(got) != 1 || got[0].Token != "PROD-TOKEN-VALUE" {
		t.Fatalf("data after the move: %v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "store.key")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the raw key file wasn't removed after wrapping")
	}
}

func TestRewrapMovesTheWrappedKeyToTheNewVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	prov := &fakeProvider{name: "fake:test"}
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov, Rewrap: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "store.key.wrapped"))
	if prov.rewraps != 1 || !bytes.Contains(b, []byte("fake:v2:")) {
		t.Errorf("rewrap: %d %s", prov.rewraps, b)
	}
	if _, err := NewFileStoreOpts(ctx, dir, StoreOptions{Provider: prov}); err != nil {
		t.Errorf("the rewrapped key must still open: %v", err)
	}
}

func TestSecretKeyIsWrappedAndTheRawCopyRemoved(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	s := &SecretStore{clientset: cs, namespace: "vinaire"}

	// an install from before the provider: a raw key in the Secret
	rawKey, _ := s.resolveKey(ctx, StoreOptions{})
	sec, _ := cs.CoreV1().Secrets("vinaire").Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if !bytes.Equal(sec.Data["key"], rawKey) {
		t.Fatal("expected the raw key in the Secret")
	}

	prov := &fakeProvider{name: "fake:test"}
	got, err := s.resolveKey(ctx, StoreOptions{Provider: prov})
	if err != nil || !bytes.Equal(got, rawKey) {
		t.Fatalf("migration must keep the same key: %v", err)
	}
	sec, _ = cs.CoreV1().Secrets("vinaire").Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if _, has := sec.Data["key"]; has || string(sec.Data["provider"]) != "fake:test" || !strings.HasPrefix(string(sec.Data["wrapped"]), "fake:v1:") {
		t.Fatalf("Secret after migration: %v", sec.Data)
	}
	for _, v := range sec.Data {
		if bytes.Contains(v, rawKey) {
			t.Error("the raw key is still in the Secret")
		}
	}
	// the next start unwraps it
	again, err := s.resolveKey(ctx, StoreOptions{Provider: prov})
	if err != nil || !bytes.Equal(again, rawKey) {
		t.Fatalf("second start: %v", err)
	}
	// a provider outage is an error
	prov.down = true
	if _, err := s.resolveKey(ctx, StoreOptions{Provider: prov}); err == nil {
		t.Error("an outage must not produce a key")
	}
	if _, err := s.resolveKey(ctx, StoreOptions{EnvKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), Provider: prov}); err == nil {
		t.Error("env key and provider together accepted")
	}
}

func TestFreshSecretWithAProviderNeverHoldsARawKey(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewSimpleClientset()
	s := &SecretStore{clientset: cs, namespace: "vinaire"}
	prov := &fakeProvider{name: "fake:test"}
	dek, err := s.resolveKey(ctx, StoreOptions{Provider: prov})
	if err != nil || len(dek) != 32 {
		t.Fatal(err)
	}
	sec, _ := cs.CoreV1().Secrets("vinaire").Get(ctx, storeKeySecretName, metav1.GetOptions{})
	if _, has := sec.Data["key"]; has {
		t.Error("a new install with a provider stored a raw key")
	}
	for _, v := range sec.Data {
		if bytes.Contains(v, dek) {
			t.Error("the key appears in the Secret")
		}
	}
}
