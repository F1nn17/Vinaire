package gitops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Limits on what is read out of a repository, so a hostile or simply huge one
// can't exhaust memory: the clone is shallow (one commit) and kept in memory,
// and only manifest files under the source's path are read.
const (
	maxFiles     = 300
	maxFileBytes = 1 << 20
	maxTotal     = 8 << 20
	maxWalked    = 5000 // directory entries visited
)

// Snapshot is the manifests of one commit.
type Snapshot struct {
	Commit string
	Files  map[string][]byte // path within the repository -> content
}

// Fetcher reads manifests from git. The real one is GitFetcher; tests use a fake.
type Fetcher interface {
	// Check verifies the repository is reachable, the credentials work and the
	// revision exists, without downloading it.
	Check(ctx context.Context, src Source) error
	Fetch(ctx context.Context, src Source) (*Snapshot, error)
}

// GitFetcher is a go-git based Fetcher: in memory, nothing touches the disk and
// no git binary is needed.
type GitFetcher struct{}

var installOnce sync.Once

// InstallTransport routes go-git's https (and, only for local development,
// http) traffic through the guarded HTTP client, so a repository URL can't be
// used to make Vinaire talk to its own loopback or to the cloud metadata
// service. It is global to go-git and so is done once, at startup.
func InstallTransport(guarded, loopbackDev *http.Client) {
	installOnce.Do(func() {
		client.InstallProtocol("https", githttp.NewClient(guarded))
		if loopbackDev != nil {
			client.InstallProtocol("http", githttp.NewClient(loopbackDev))
		}
	})
}

func auth(src Source) transport.AuthMethod {
	if !src.GitToken.IsSet() {
		return nil
	}
	user := src.GitUsername
	if user == "" {
		user = "git" // GitHub, GitLab and Gitea accept any name next to a token
	}
	return &githttp.BasicAuth{Username: user, Password: src.GitToken.Reveal()}
}

// refCandidates: a revision may name a branch or a tag; the default branch is "".
func refCandidates(rev string) []plumbing.ReferenceName {
	if rev == "" || rev == "HEAD" {
		return []plumbing.ReferenceName{""}
	}
	return []plumbing.ReferenceName{plumbing.NewBranchReferenceName(rev), plumbing.NewTagReferenceName(rev)}
}

// Check lists the remote's references (git ls-remote).
func (GitFetcher) Check(ctx context.Context, src Source) error {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{src.RepoURL}})
	refs, err := rem.ListContext(ctx, &git.ListOptions{Auth: auth(src)})
	if err != nil {
		return friendlyGitError(err)
	}
	if src.Revision == "" || src.Revision == "HEAD" {
		return nil
	}
	for _, r := range refs {
		if n := r.Name(); n == plumbing.NewBranchReferenceName(src.Revision) || n == plumbing.NewTagReferenceName(src.Revision) {
			return nil
		}
	}
	return fmt.Errorf("the repository has no branch or tag named %q", src.Revision)
}

// Fetch clones the revision shallowly into memory and reads the manifest files.
func (GitFetcher) Fetch(ctx context.Context, src Source) (*Snapshot, error) {
	var lastErr error
	for _, ref := range refCandidates(src.Revision) {
		fs := memfs.New()
		repo, err := git.CloneContext(ctx, memory.NewStorage(), fs, &git.CloneOptions{
			URL: src.RepoURL, Auth: auth(src), ReferenceName: ref,
			SingleBranch: true, Depth: 1, Tags: git.NoTags,
		})
		if err != nil {
			lastErr = err
			if errors.Is(err, plumbing.ErrReferenceNotFound) || strings.Contains(err.Error(), "couldn't find remote ref") {
				continue // try the revision as a tag
			}
			return nil, friendlyGitError(err)
		}
		head, err := repo.Head()
		if err != nil {
			return nil, fmt.Errorf("reading the cloned revision: %w", err)
		}
		files, err := readManifestFiles(fs, src.Path, src.Recurse)
		if err != nil {
			return nil, err
		}
		return &Snapshot{Commit: head.Hash().String(), Files: files}, nil
	}
	return nil, friendlyGitError(lastErr)
}

// friendlyGitError turns go-git's terse errors into something a person can act on.
func friendlyGitError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed):
		return errors.New("the repository needs a (valid) access token")
	case errors.Is(err, transport.ErrRepositoryNotFound):
		return errors.New("repository not found — check the URL, and the token if it is private")
	case strings.Contains(err.Error(), "couldn't find remote ref"):
		return errors.New("the repository has no such branch or tag")
	}
	return err
}

var manifestExt = map[string]bool{".yaml": true, ".yml": true, ".json": true}

func readManifestFiles(fs billy.Filesystem, dir string, recurse bool) (map[string][]byte, error) {
	dir = path.Clean("/" + dir)[1:] // "" or a clean relative path, never ".." or absolute
	if dir == "" {
		dir = "."
	}
	if _, err := fs.Stat(dir); err != nil {
		return nil, fmt.Errorf("the path %q doesn't exist in the repository", dir)
	}
	files := map[string][]byte{}
	var total int
	var walked int
	var walk func(d string) error
	walk = func(d string) error {
		entries, err := fs.ReadDir(d)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if walked++; walked > maxWalked {
				return errors.New("the repository path has too many entries")
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue // .git, .github and other tooling directories
			}
			p := path.Join(d, name)
			if e.IsDir() {
				if recurse {
					if err := walk(p); err != nil {
						return err
					}
				}
				continue
			}
			if !manifestExt[strings.ToLower(path.Ext(name))] {
				continue
			}
			if e.Size() > maxFileBytes {
				return fmt.Errorf("%s is larger than %d KiB", p, maxFileBytes>>10)
			}
			f, err := fs.Open(p)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
			f.Close()
			if err != nil {
				return err
			}
			if total += len(data); total > maxTotal {
				return fmt.Errorf("the manifests are larger than %d MiB in total", maxTotal>>20)
			}
			if len(files) >= maxFiles {
				return fmt.Errorf("more than %d manifest files", maxFiles)
			}
			files[p] = data
		}
		return nil
	}
	if err := walk(dir); err != nil {
		return nil, err
	}
	return files, nil
}
