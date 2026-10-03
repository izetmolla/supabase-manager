package proxymanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestListImageTags(t *testing.T) {
	var calls int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/acme/proxy-nginx/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"next":"","results":[
			{"name":"latest","last_updated":"2026-10-01T00:00:00Z","full_size":100},
			{"name":"1.2.0","last_updated":"2026-09-01T00:00:00Z"},
			{"name":"e355e37-dirty","last_updated":"2026-10-02T00:00:00Z"},
			{"name":"1.10.0","last_updated":"2026-09-20T00:00:00Z"},
			{"name":"old-build","last_updated":"2026-01-01T00:00:00Z"}
		]}`))
	}))
	defer hub.Close()
	prev := hubTagsAPI
	hubTagsAPI = hub.URL + "/"
	defer func() { hubTagsAPI = prev }()

	s := &Service{opts: Options{ImageRepository: "acme/proxy", ImageTag: "1.10.0"}}
	out, err := s.ListImageTags(context.Background(), "nginx", true)
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, tg := range out.Tags {
		tags = append(tags, tg.Tag)
	}
	if want := []string{"1.10.0", "1.2.0", "latest", "e355e37-dirty", "old-build"}; !slices.Equal(tags, want) {
		t.Fatalf("order = %v, want %v", tags, want)
	}
	if out.Default != "acme/proxy-nginx:1.10.0" || out.Tags[0].Image != "acme/proxy-nginx:1.10.0" || !out.Tags[0].Release || !out.Tags[0].Remote {
		t.Fatalf("unexpected result: %+v", out)
	}

	// Cached for the next call, and a repository missing on Docker Hub is reported, not fatal.
	if _, err := s.ListImageTags(context.Background(), "nginx", false); err != nil || calls != 1 {
		t.Fatalf("cache not used: calls=%d err=%v", calls, err)
	}
	missing, err := s.ListImageTags(context.Background(), "traefik", true)
	if err != nil || missing.HubError == "" || len(missing.Tags) != 0 {
		t.Fatalf("missing repo: %+v, %v", missing, err)
	}
	if _, err := s.ListImageTags(context.Background(), "apache", false); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestCheckUpdateNewRelease(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"next":"","results":[{"name":"latest"},{"name":"1.2.0"},{"name":"1.10.0"},{"name":"1.9.9"}]}`))
	}))
	defer hub.Close()
	prev := hubTagsAPI
	hubTagsAPI = hub.URL + "/"
	defer func() { hubTagsAPI = prev }()

	s := &Service{}
	u := s.checkUpdate(context.Background(), &ProxyInstance{ID: 1, Image: "acme/proxy-nginx:1.2.0"}, true)
	if !u.Available || u.Reason != UpdateNewRelease || u.Target != "acme/proxy-nginx:1.10.0" {
		t.Fatalf("unexpected update: %+v", u)
	}
	if u := s.checkUpdate(context.Background(), &ProxyInstance{Image: "ghcr.io/acme/proxy:1.0.0"}, false); u.Available || u.Error == "" {
		t.Fatalf("non-Hub image: %+v", u)
	}
	if repo, tag := splitImage("localhost:5000/proxy"); repo != "localhost:5000/proxy" || tag != "latest" {
		t.Fatalf("splitImage = %s %s", repo, tag)
	}
	if got := digestOf([]string{"docker.io/acme/p@sha256:a", "other/p@sha256:b"}, "acme/p"); !slices.Equal(got, []string{"sha256:a"}) {
		t.Fatalf("digestOf = %v", got)
	}
}

func TestOnDockerHub(t *testing.T) {
	for repo, want := range map[string]bool{
		"nginx": true, "izetmolla/supabase-manager-proxy-nginx": true,
		"ghcr.io/acme/proxy": false, "localhost:5000/proxy": false, "registry.local/proxy": false,
	} {
		if got := onDockerHub(repo); got != want {
			t.Errorf("onDockerHub(%q) = %v, want %v", repo, got, want)
		}
	}
}
