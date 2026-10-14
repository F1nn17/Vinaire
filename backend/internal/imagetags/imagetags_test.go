package imagetags

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAcceptsOnlyKnownRegistriesAndPlainNames(t *testing.T) {
	for in, want := range map[string]string{
		"postgres":                  "docker.io/library/postgres",
		"grafana/grafana":           "docker.io/grafana/grafana",
		"docker.io/library/redis":   "docker.io/library/redis",
		"quay.io/keycloak/keycloak": "quay.io/keycloak/keycloak",
		"quay.io/minio/aistor/minio": "quay.io/minio/aistor/minio",
	} {
		reg, repo, err := parse(in)
		if err != nil || reg+"/"+repo != want {
			t.Errorf("parse(%q) = %q %q %v, want %s", in, reg, repo, err, want)
		}
	}
	if _, _, err := parse("ghcr.io/owner/app"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ghcr.io: %v", err)
	}
	if _, _, err := parse("169.254.169.254/x/y"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("an address as the registry must be refused, got %v", err)
	}
	for _, bad := range []string{"", "a/b/c", "docker.io/a/b/c", "quay.io/a/b/c/d", "UPPER", "../etc", "a b", "postgres?x=1", "x/y@sha256"} {
		if _, _, err := parse(bad); err == nil {
			t.Errorf("parse(%q) should fail", bad)
		}
	}
}

func TestOrderPutsVersionsFirstAndNewestNumberUp(t *testing.T) {
	got := order([]Tag{
		{Name: "17-alpine", Updated: "2025-02"}, {Name: "9.6"}, {Name: "latest"}, {Name: "17.2"},
		{Name: "17"}, {Name: "sha256-0123.sig"}, {Name: "bad tag"}, {Name: "16.10"}, {Name: "17.2"},
	})
	want := []string{"17.2", "17", "16.10", "9.6", "latest", "17-alpine"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

func TestHubIsAskedAtItsOwnAddressAndAnswersAreCached(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v2/repositories/library/postgres/tags" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"next":"https://evil.example/x","results":[{"name":"17","last_updated":"x"},{"name":"16"}]}`))
	}))
	defer srv.Close()
	l := New()
	l.client = srv.Client()
	l.hub = srv.URL
	for i := 0; i < 2; i++ {
		res, err := l.Tags(context.Background(), "postgres")
		if err != nil || len(res.Tags) != 2 || res.Tags[0].Name != "17" {
			t.Fatalf("%v %v", res, err)
		}
	}
	if calls != 1 {
		t.Errorf("registry was asked %d times; a foreign next link must not be followed and the answer cached", calls)
	}
}
