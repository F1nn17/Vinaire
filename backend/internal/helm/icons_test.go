package helm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIconURLOnlyAcceptsPlainHTTPS(t *testing.T) {
	r := Repo{Name: "r", URL: "https://charts.example.com/repo"}
	cases := map[string]string{
		"https://cdn.example.com/a.png":      "https://cdn.example.com/a.png",
		"a.png":                              "https://charts.example.com/repo/a.png",
		"http://cdn.example.com/a.png":       "",
		"https://user:pw@cdn.example.com/a":  "",
		"file:///etc/passwd":                 "",
		"":                                   "",
		"data:image/svg+xml;base64,PHN2Zz4=": "",
	}
	for in, want := range cases {
		if got := iconURL(r, in); got != want {
			t.Errorf("iconURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSniffIconChecksTheContentNotTheHeader(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if ct, ok := sniffIcon(png); !ok || ct != "image/png" {
		t.Errorf("png: %q %v", ct, ok)
	}
	if _, ok := sniffIcon([]byte("<svg xmlns='http://www.w3.org/2000/svg'><circle r='1'/></svg>")); !ok {
		t.Error("a plain svg was refused")
	}
	for _, bad := range []string{
		"<svg><script>alert(1)</script></svg>",
		"<svg onload=alert(1)></svg>",
		"<svg><foreignObject><iframe/></foreignObject></svg>",
		"<html><body>not an image</body></html>",
		"MZ\x90\x00 an executable",
	} {
		if _, ok := sniffIcon([]byte(bad)); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestIconIsFetchedFromTheIndexURLAndCached(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/index.yaml", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`apiVersion: v1
entries:
  withicon: [{name: withicon, version: 1.0.0, icon: ` + base + `/icon.png, urls: [x.tgz]}]
  noicon:   [{name: noicon, version: 1.0.0, urls: [x.tgz]}]
  badicon:  [{name: badicon, version: 1.0.0, icon: ` + base + `/page.html, urls: [x.tgz]}]
`))
	})
	mux.HandleFunc("/icon.png", func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
	})
	mux.HandleFunc("/page.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png") // lies
		_, _ = w.Write([]byte("<html>not an image</html>"))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	base = srv.URL
	c := newCatalog(Config{Repos: []Repo{{Name: "fake", URL: srv.URL}}}, srv.Client())

	for i := 0; i < 3; i++ {
		ic, err := c.Icon(context.Background(), "fake", "withicon")
		if err != nil || ic.ContentType != "image/png" {
			t.Fatalf("icon: %+v, %v", ic, err)
		}
	}
	if hits != 1 {
		t.Errorf("the icon host was hit %d times, want 1 (cached)", hits)
	}
	for _, name := range []string{"noicon", "badicon"} {
		if _, err := c.Icon(context.Background(), "fake", name); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
	}
	if _, err := c.Icon(context.Background(), "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo: %v", err)
	}
}
