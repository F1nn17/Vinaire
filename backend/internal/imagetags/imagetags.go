// Package imagetags lists the tags of a container image, so the "install from an image" page can
// offer them in a list instead of asking the user to guess a version. Only two registries are
// asked, at fixed addresses (Docker Hub and Quay) — a request names an image, never a URL — through
// the guarded HTTP client, anonymously, with the answer size capped and cached for a few minutes.
package imagetags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/f1nn17/vinaire/backend/internal/netguard"
)

const (
	hubBase   = "https://hub.docker.com"
	quayBase  = "https://quay.io"
	maxBody   = 4 << 20
	cacheTTL  = 10 * time.Minute
	maxCache  = 200
	maxReturn = 60
)

var (
	segment = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	tagName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	version = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+)*$`)
)

// ErrUnsupported: the image lives in a registry this package does not ask.
var ErrUnsupported = errors.New("tags are only listed for Docker Hub and quay.io images")

// Tag is one tag and when it was last pushed (empty when the registry doesn't say).
type Tag struct {
	Name    string `json:"name"`
	Updated string `json:"updated,omitempty"`
}

// Result is what the endpoint returns.
type Result struct {
	Registry string `json:"registry"`
	Tags     []Tag  `json:"tags"`
}

type cached struct {
	at  time.Time
	res Result
}

// Lookup asks the registries; use New.
type Lookup struct {
	client   *http.Client
	hub      string
	quay     string
	now      func() time.Time
	mu       sync.Mutex
	cache    map[string]cached
	inflight map[string]chan struct{}
}

func New() *Lookup {
	return &Lookup{
		client: netguard.Client(netguard.Options{Timeout: 10 * time.Second}),
		hub:    hubBase, quay: quayBase, now: time.Now,
		cache: map[string]cached{}, inflight: map[string]chan struct{}{},
	}
}

// parse turns "postgres", "grafana/grafana" or "quay.io/keycloak/keycloak" into a registry and a
// repository path, refusing anything else.
func parse(image string) (registry, repo string, err error) {
	parts := strings.Split(image, "/")
	if strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost" {
		switch parts[0] {
		case "quay.io":
			registry, parts = "quay.io", parts[1:]
		case "docker.io", "index.docker.io":
			registry, parts = "docker.io", parts[1:]
		default:
			return "", "", ErrUnsupported
		}
	} else {
		registry = "docker.io"
	}
	if len(parts) == 1 && registry == "docker.io" {
		parts = []string{"library", parts[0]}
	}
	// Docker Hub names are owner/name; Quay allows repositories nested one level deeper
	// (quay.io/minio/aistor/minio).
	if len(parts) != 2 && !(registry == "quay.io" && len(parts) == 3) {
		return "", "", fmt.Errorf("not an image name: %q", image)
	}
	for _, p := range parts {
		if !segment.MatchString(p) {
			return "", "", fmt.Errorf("not an image name: %q", image)
		}
	}
	return registry, strings.Join(parts, "/"), nil
}

// Tags returns the tags of image, newest versions first.
func (l *Lookup) Tags(ctx context.Context, image string) (Result, error) {
	registry, repo, err := parse(image)
	if err != nil {
		return Result{}, err
	}
	key := registry + "/" + repo
	for {
		l.mu.Lock()
		if c, ok := l.cache[key]; ok && l.now().Sub(c.at) < cacheTTL {
			l.mu.Unlock()
			return c.res, nil
		}
		if wait, ok := l.inflight[key]; ok {
			l.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
		}
		done := make(chan struct{})
		l.inflight[key] = done
		l.mu.Unlock()

		var tags []Tag
		if registry == "quay.io" {
			tags, err = l.fromQuay(ctx, repo)
		} else {
			tags, err = l.fromHub(ctx, repo)
		}

		res := Result{Registry: registry, Tags: order(tags)}
		l.mu.Lock()
		delete(l.inflight, key)
		close(done)
		if err == nil {
			if len(l.cache) >= maxCache {
				l.cache = map[string]cached{}
			}
			l.cache[key] = cached{at: l.now(), res: res}
		}
		l.mu.Unlock()
		if err != nil {
			return Result{}, err
		}
		return res, nil
	}
}

func (l *Lookup) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("registry unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("no such image")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out)
}

func (l *Lookup) fromHub(ctx context.Context, repo string) ([]Tag, error) {
	var tags []Tag
	next := fmt.Sprintf("%s/v2/repositories/%s/tags?page_size=100&ordering=last_updated", l.hub, repo)
	for page := 0; page < 3 && next != ""; page++ {
		var body struct {
			Next    string `json:"next"`
			Results []struct {
				Name        string `json:"name"`
				LastUpdated string `json:"last_updated"`
			} `json:"results"`
		}
		if err := l.get(ctx, next, &body); err != nil {
			if len(tags) > 0 {
				break
			}
			return nil, err
		}
		for _, r := range body.Results {
			tags = append(tags, Tag{Name: r.Name, Updated: r.LastUpdated})
		}
		// follow only a link that stays on the registry's own address
		next = ""
		if strings.HasPrefix(body.Next, l.hub+"/") {
			next = body.Next
		}
	}
	return tags, nil
}

func (l *Lookup) fromQuay(ctx context.Context, repo string) ([]Tag, error) {
	var body struct {
		Tags []struct {
			Name    string `json:"name"`
			StartTS int64  `json:"start_ts"`
		} `json:"tags"`
	}
	if err := l.get(ctx, fmt.Sprintf("%s/api/v1/repository/%s/tag/?limit=100&onlyActiveTags=true", l.quay, repo), &body); err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, len(body.Tags))
	for _, t := range body.Tags {
		tags = append(tags, Tag{Name: t.Name, Updated: time.Unix(t.StartTS, 0).UTC().Format(time.RFC3339)})
	}
	return tags, nil
}

// order puts plain versions first (newest number first), then "latest", then the rest by recency,
// and drops names that could not be a tag.
func order(in []Tag) []Tag {
	seen := map[string]bool{}
	var versions, rest []Tag
	var latest *Tag
	for i := range in {
		t := in[i]
		// cosign keeps signatures and attestations as tags of their own (sha256-<digest>.sig): not versions
		if !tagName.MatchString(t.Name) || seen[t.Name] || strings.HasPrefix(t.Name, "sha256-") {
			continue
		}
		seen[t.Name] = true
		switch {
		case t.Name == "latest":
			latest = &t
		case version.MatchString(t.Name):
			versions = append(versions, t)
		default:
			rest = append(rest, t)
		}
	}
	sort.SliceStable(versions, func(i, j int) bool { return less(versions[j].Name, versions[i].Name) })
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].Updated > rest[j].Updated })
	out := append([]Tag{}, versions...)
	if latest != nil {
		out = append(out, *latest)
	}
	out = append(out, rest...)
	if len(out) > maxReturn {
		out = out[:maxReturn]
	}
	return out
}

// less compares two dotted versions numerically.
func less(a, b string) bool {
	as, bs := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}
