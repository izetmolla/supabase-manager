package proxymanager

import (
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

func TestBootstrap(t *testing.T) {
	s := newTestService(t)
	s.opts.Bootstrap = &Bootstrap{Kind: spec.KindNginx, HTTPPort: 8081, HTTPSPort: 8444}
	id, err := s.bootstrap()
	if err != nil || id == 0 {
		t.Fatalf("bootstrap = %d, %v", id, err)
	}
	in, _ := s.GetInstance(id)
	if in.HTTPPort != 8081 || in.HTTPSPort != 8444 || !in.TLSALPN || in.Image != s.DefaultImage(spec.KindNginx) {
		t.Fatalf("instance = %+v", in)
	}
	var st Settings
	if ok, _ := s.settings.Get(settingsKey, &st); !ok || !st.Enabled {
		t.Fatal("not enabled")
	}
	if id, _ := s.bootstrap(); id != 0 {
		t.Fatal("a second instance was created")
	}
}

func TestGetProjectDomains(t *testing.T) {
	s := newTestService(t)
	in := &ProxyInstance{Name: "edge", Kind: spec.KindNginx, Image: "x", HTTPPort: 80, HTTPSPort: 8443, Enabled: true}
	s.db.Create(in)
	api := &ProxyUpstream{Name: "api", Algorithm: "round_robin", Scheme: "http", Targets: []Target{{Kind: "project", Project: "demo", Service: "api"}}}
	studio := &ProxyUpstream{Name: "studio", Algorithm: "round_robin", Scheme: "http", Targets: []Target{{Kind: "project", Project: "demo", Service: "studio"}}}
	app := &ProxyUpstream{Name: "app", Algorithm: "round_robin", Scheme: "http", Targets: []Target{{Kind: "static", Address: "127.0.0.1", Port: 3000}}}
	s.db.Create(api)
	s.db.Create(studio)
	s.db.Create(app)
	s.db.Create(&ProxyHost{Name: "api", Kind: spec.HostProxy, Domains: []string{"api.example.com", "*.example.com"}, UpstreamID: api.ID,
		Routes: []Route{{UpstreamID: studio.ID}}, InstanceIDs: []uint{in.ID}, TLS: TLSOptions{Mode: TLSAuto}, Enabled: true})
	s.db.Create(&ProxyHost{Name: "app", Kind: spec.HostProxy, Domains: []string{"app.example.com"}, UpstreamID: app.ID, InstanceIDs: []uint{in.ID}, Enabled: true})

	d, err := s.GetProjectDomains("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Available) != 2 || d.Available[0].Service != "api" || d.Available[0].URL != "https://api.example.com:8443" || d.Available[1].Service != "studio" {
		t.Fatalf("available = %+v", d.Available)
	}
	if len(d.Sites) != 2 || d.Sites[1].URL != "http://app.example.com" {
		t.Fatalf("sites = %+v", d.Sites)
	}
	if other, _ := s.GetProjectDomains("other"); len(other.Available) != 0 {
		t.Fatalf("other project got %+v", other.Available)
	}
}
