package clusters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiraku/vinaire/backend/internal/netguard"
)

func newKey(t *testing.T) string {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return encodeKey(k)
}

// The registry's encryption key can be rotated without losing it: with the new
// key current and the old one as "previous", the first load re-encrypts in place.
func TestKeyRotationReencryptsInPlace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	oldKey, newKey1 := newKey(t), newKey(t)

	old, err := NewFileStore(dir, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	list := []StoredCluster{{Name: "prod", Server: "https://prod.example", Token: "very-secret-token"}}
	if err := old.Save(ctx, list); err != nil {
		t.Fatal(err)
	}
	if err := old.SaveDoc(ctx, "gitops", []byte(`{"tok":"doc-secret"}`)); err != nil {
		t.Fatal(err)
	}

	// rotating without telling Vinaire the old key fails cleanly — it must not
	// return garbage or an empty registry
	lost, _ := NewFileStore(dir, newKey1)
	got, lostErr := lost.Load(ctx)
	if lostErr == nil || len(got) != 0 {
		t.Fatalf("the wrong key must fail to decrypt, got %v, %v", got, lostErr)
	}
	if !strings.Contains(lostErr.Error(), "CLUSTER_STORE_KEY_PREVIOUS") {
		t.Errorf("the error should tell the operator about the previous key: %v", lostErr)
	}

	// with the old key listed as previous, the registry opens and is re-sealed
	rotated, err := NewFileStore(dir, newKey1, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err = rotated.Load(ctx)
	if err != nil || len(got) != 1 || got[0].Token != "very-secret-token" {
		t.Fatalf("after rotation: %v, %v", got, err)
	}
	if doc, err := rotated.LoadDoc(ctx, "gitops"); err != nil || !bytes.Contains(doc, []byte("doc-secret")) {
		t.Fatalf("document after rotation: %q, %v", doc, err)
	}

	// now the NEW key alone is enough — everything was re-encrypted in place
	only, _ := NewFileStore(dir, newKey1)
	if got, err := only.Load(ctx); err != nil || len(got) != 1 {
		t.Errorf("not re-encrypted with the new key: %v, %v", got, err)
	}
	if doc, err := only.LoadDoc(ctx, "gitops"); err != nil || len(doc) == 0 {
		t.Errorf("document not re-encrypted: %q, %v", doc, err)
	}
	// and the old key no longer opens it
	stale, _ := NewFileStore(dir, oldKey)
	if _, err := stale.Load(ctx); err == nil {
		t.Error("the old key still opens the registry after rotation")
	}

	// a storage dump holds no secret in the clear
	for _, name := range []string{"clusters.enc", "gitops.enc"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("very-secret-token")) || bytes.Contains(raw, []byte("doc-secret")) || !IsSealed(raw) {
			t.Errorf("%s is readable in the clear", name)
		}
	}
}

func TestRotationRejectsBadPreviousKeys(t *testing.T) {
	if _, err := NewFileStore(t.TempDir(), newKey(t), "not-a-key"); err == nil {
		t.Error("an invalid previous key was accepted")
	}
	if keys, err := ParseKeys(" , "); err != nil || len(keys) != 0 {
		t.Errorf("blank list: %v, %v", keys, err)
	}
}

func TestCheckServer(t *testing.T) {
	ok := []string{"https://10.0.0.5:6443", "https://k8s.example.com", "https://127.0.0.1:6443", "https://[::1]:6443"}
	bad := []string{
		"http://10.0.0.5:6443", "ftp://x", "https://169.254.169.254", "https://169.254.1.1:443",
		"https://0.0.0.0", "https://224.0.0.1", "https://[fe80::1]", "", "not a url",
	}
	for _, s := range ok {
		if err := CheckServer(s); err != nil {
			t.Errorf("%s was refused: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := CheckServer(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

// A hostname that resolves to the metadata address can't slip past CheckServer
// (which only sees the URL), so the dialler itself refuses.
func TestRegisteredClusterDialsRefuseMetadata(t *testing.T) {
	entry, err := EntryFromStored(StoredCluster{Name: "x", Server: "https://cluster.example", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.RESTConfig.Dial == nil {
		t.Fatal("registered clusters must carry the guarded dialler")
	}
	for _, addr := range []string{"169.254.169.254:443", "224.0.0.1:443", "[fe80::1]:443"} {
		if _, err := entry.RESTConfig.Dial(context.Background(), "tcp", addr); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("dial %s: %v", addr, err)
		}
	}
	if _, err := netguard.DialContext(false)(context.Background(), "tcp", "127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("loopback with the strict dialler: %v", err)
	}
}
