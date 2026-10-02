package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// A local repository, read through go-git's file transport (which needs the git
// binary's upload-pack; the test is skipped where there is none).
func makeRepo(t *testing.T) (dir string, repo *git.Repository, commit func(msg string, files map[string]string) plumbing.Hash) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	dir = t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	commit = func(msg string, files map[string]string) plumbing.Hash {
		for name, body := range files {
			full := filepath.Join(dir, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(full), 0o755)
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := wt.Add(filepath.ToSlash(name)); err != nil {
				t.Fatal(err)
			}
		}
		h, err := wt.Commit(msg, &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@example.com", When: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	return dir, repo, commit
}

func TestGitFetcherReadsManifests(t *testing.T) {
	dir, repo, commit := makeRepo(t)
	first := commit("one", map[string]string{
		"k8s/cm.yaml":        cmYAML,
		"k8s/ns.yml":         nsYAML,
		"k8s/notes.txt":      "not a manifest",
		"k8s/sub/deep.yaml":  "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: deep}\n",
		"k8s/.hidden/x.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: hidden}\n",
		"other/o.yaml":       "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: other}\n",
	})
	repo.CreateTag("v1", first, nil)
	repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("release"), first))
	commit("two", map[string]string{"k8s/cm.yaml": strings.Replace(cmYAML, "v1", "v2", 1)})

	f := GitFetcher{}
	src := Source{RepoURL: dir, Path: "k8s"}
	ctx := context.Background()

	if err := f.Check(ctx, src); err != nil {
		t.Fatalf("Check: %v", err)
	}
	snap, err := f.Fetch(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 2 || snap.Files["k8s/cm.yaml"] == nil || snap.Files["k8s/ns.yml"] == nil {
		t.Errorf("non-recursive files = %v", keys(snap.Files))
	}
	head, _ := repo.Head()
	if snap.Commit != head.Hash().String() {
		t.Errorf("commit %s, want the default branch's %s", snap.Commit, head.Hash())
	}

	src.Recurse = true
	if snap, _ = f.Fetch(ctx, src); len(snap.Files) != 3 || snap.Files["k8s/sub/deep.yaml"] == nil {
		t.Errorf("recursive files = %v (hidden directories must be skipped)", keys(snap.Files))
	}

	// a tag and a branch name both work as a revision
	for _, rev := range []string{"v1", "release"} {
		src.Revision = rev
		snap, err := f.Fetch(ctx, src)
		if err != nil || snap.Commit != first.String() || !strings.Contains(string(snap.Files["k8s/cm.yaml"]), "k: v1") {
			t.Errorf("revision %q: %v %+v", rev, err, snap)
		}
		if err := f.Check(ctx, src); err != nil {
			t.Errorf("Check %q: %v", rev, err)
		}
	}

	src.Revision = "nope"
	if err := f.Check(ctx, src); err == nil || !strings.Contains(err.Error(), "no branch or tag") {
		t.Errorf("Check of a missing revision: %v", err)
	}
	if _, err := f.Fetch(ctx, src); err == nil {
		t.Error("Fetch of a missing revision succeeded")
	}
	src.Revision, src.Path = "", "missing/dir"
	if _, err := f.Fetch(ctx, src); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("a missing path: %v", err)
	}
	if _, err := f.Fetch(ctx, Source{RepoURL: filepath.Join(dir, "nowhere")}); err == nil {
		t.Error("a missing repository succeeded")
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestReadManifestFilesLimitsAndContainment(t *testing.T) {
	put := func(fs interface {
		Create(string) (interface {
			Write([]byte) (int, error)
			Close() error
		}, error)
	}, p, body string) {
	}
	_ = put

	fs := memfs.New()
	write := func(p, body string) {
		f, err := fs.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(body))
		f.Close()
	}
	write("a/ok.yaml", cmYAML)
	write("a/big.yaml", strings.Repeat("x", maxFileBytes+1))

	if _, err := readManifestFiles(fs, "a", false); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("an oversize file: %v", err)
	}
	// a path can't climb out of the repository
	write("secret.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: secret}\n")
	files, err := readManifestFiles(fs, "../../", false)
	if err == nil {
		for name := range files {
			if !strings.HasPrefix(name, ".") && name != "secret.yaml" {
				t.Errorf("unexpected file %q", name)
			}
		}
	}

	many := memfs.New()
	for i := 0; i <= maxFiles; i++ {
		f, _ := many.Create(filepath.ToSlash(filepath.Join("d", strings.Repeat("a", 1)+itoa(i)+".yaml")))
		f.Write([]byte(cmYAML))
		f.Close()
	}
	if _, err := readManifestFiles(many, "d", false); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many files: %v", err)
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{digits[i%10]}, b...)
	}
	return string(b)
}
