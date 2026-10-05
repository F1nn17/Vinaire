package helm

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Chart icons for the Marketplace. A chart's `icon` is a URL some third party
// hosts, and the UI's Content-Security-Policy deliberately only allows images
// from Vinaire's own origin — so the browser never contacts those hosts (and they
// never see who is browsing). The backend fetches the icon instead, and:
//
//   - the URL is the one the repository's own index names for that chart — a request
//     can name a repository and a chart, never a URL;
//   - it goes through the same guarded client as every catalog fetch (https only,
//     no loopback/link-local/private-metadata addresses, no redirect out of https);
//   - size is capped and the *content* is checked to be an image (PNG, JPEG, GIF,
//     WebP, or an SVG with no script), not trusted from the Content-Type header;
//   - it is served `nosniff` and sandboxed, from a cache, so repeated views cost the
//     remote host one request per day at most.

const (
	maxIconBytes   = 256 << 10
	iconTTL        = 24 * time.Hour
	iconMissTTL    = 15 * time.Minute
	maxCachedIcons = 512
)

// Icon is a fetched chart icon.
type Icon struct {
	Data        []byte
	ContentType string
}

type iconEntry struct {
	icon *Icon // nil: this chart has no (usable) icon
	at   time.Time
}

type iconCache struct {
	mu      sync.Mutex
	entries map[string]iconEntry
}

// iconURL is the (absolute, https) URL the index names for a chart, or "".
func iconURL(r Repo, rawIcon string) string {
	rawIcon = strings.TrimSpace(rawIcon)
	if rawIcon == "" {
		return ""
	}
	base, err := url.Parse(strings.TrimRight(r.URL, "/") + "/")
	if err != nil {
		return ""
	}
	ref, err := url.Parse(rawIcon)
	if err != nil {
		return ""
	}
	u := base.ResolveReference(ref)
	if u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}

// sniffIcon decides whether data is an acceptable image and returns its type.
func sniffIcon(data []byte) (string, bool) {
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return ct, true
	}
	// SVG isn't recognised by DetectContentType; accept it only when it looks like
	// an SVG document and carries nothing executable.
	head := strings.ToLower(string(data[:min(len(data), 2048)]))
	if !strings.Contains(head, "<svg") {
		return "", false
	}
	low := strings.ToLower(string(data))
	for _, bad := range []string{"<script", "javascript:", "<foreignobject", "onload=", "onerror=", "<iframe", "<!entity"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	return "image/svg+xml", true
}

// Icon returns the icon of a chart in a configured repository, or ErrNotFound
// when it has none (or it isn't a usable image).
func (c *Catalog) Icon(ctx context.Context, repoName, chartName string) (*Icon, error) {
	key := repoName + "/" + chartName
	c.icons.mu.Lock()
	if c.icons.entries == nil {
		c.icons.entries = map[string]iconEntry{}
	}
	if e, ok := c.icons.entries[key]; ok {
		ttl := iconTTL
		if e.icon == nil {
			ttl = iconMissTTL
		}
		if c.now().Sub(e.at) < ttl {
			c.icons.mu.Unlock()
			if e.icon == nil {
				return nil, fmt.Errorf("icon of %s: %w", key, ErrNotFound)
			}
			return e.icon, nil
		}
	}
	c.icons.mu.Unlock()

	r, cv, _, err := c.lookup(ctx, repoName, chartName, "")
	if err != nil {
		return nil, err
	}
	icon := c.fetchIcon(ctx, iconURL(r, cv.Icon))
	c.icons.mu.Lock()
	if len(c.icons.entries) >= maxCachedIcons {
		c.icons.entries = map[string]iconEntry{} // simple bound: start over
	}
	c.icons.entries[key] = iconEntry{icon: icon, at: c.now()}
	c.icons.mu.Unlock()
	if icon == nil {
		return nil, fmt.Errorf("icon of %s: %w", key, ErrNotFound)
	}
	return icon, nil
}

func (c *Catalog) fetchIcon(ctx context.Context, rawURL string) *Icon {
	if rawURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := c.fetchLimited(ctx, rawURL, maxIconBytes)
	if err != nil || len(data) == 0 {
		return nil
	}
	ct, ok := sniffIcon(bytes.TrimSpace(data))
	if !ok {
		return nil
	}
	return &Icon{Data: data, ContentType: ct}
}
