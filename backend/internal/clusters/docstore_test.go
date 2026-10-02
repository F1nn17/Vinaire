package clusters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreDocuments(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	var _ DocStore = fs
	ctx := context.Background()

	if got, err := fs.LoadDoc(ctx, "gitops"); err != nil || got != nil {
		t.Fatalf("a missing document should load as nil, got %q, %v", got, err)
	}
	secret := []byte(`{"applyToken":"super-secret-token"}`)
	if err := fs.SaveDoc(ctx, "gitops", secret); err != nil {
		t.Fatal(err)
	}
	got, err := fs.LoadDoc(ctx, "gitops")
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "gitops.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("super-secret-token")) || !IsSealed(raw) {
		t.Error("the document is not sealed on disk")
	}

	// the registry and a document live side by side without disturbing each other
	if err := fs.Save(ctx, []StoredCluster{{Name: "c1", Server: "https://c1"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := fs.LoadDoc(ctx, "gitops"); !bytes.Equal(got, secret) {
		t.Error("saving the registry changed the document")
	}
	if list, err := fs.Load(ctx); err != nil || len(list) != 1 {
		t.Errorf("registry = %v, %v", list, err)
	}

	// a different key can't read it
	other, _ := NewFileStore(t.TempDir(), "")
	if err := os.WriteFile(filepath.Join(other.dir, "gitops.enc"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := other.LoadDoc(ctx, "gitops"); err == nil {
		t.Error("a document sealed with another key was opened")
	}
}
