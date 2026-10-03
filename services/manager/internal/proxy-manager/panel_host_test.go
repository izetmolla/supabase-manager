package proxymanager

import (
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

func TestEnsurePanelHost(t *testing.T) {
	s := newTestService(t)
	s.opts.ManagerAddr = "0.0.0.0:9090"
	if _, err := s.EnsurePanelHost(PanelHostInput{Domain: "panel.example.com"}); err == nil {
		t.Fatal("expected an error without proxy instances")
	}
	s.db.Create(&ProxyInstance{Name: "default", Kind: spec.KindNginx, Image: "x", HTTPPort: 80, HTTPSPort: 8443, Enabled: true})

	if _, err := s.EnsurePanelHost(PanelHostInput{Domain: "panel.example.com"}); err == nil {
		t.Fatal("expected HTTPS to need an email when no ACME account exists")
	}
	res, err := s.EnsurePanelHost(PanelHostInput{Domain: "Panel.Example.com.", Email: "ops@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	h := res.Host
	if !res.Created || h.Domains[0] != "panel.example.com" || h.TLS.Mode != TLSAuto || h.TLS.CertificateID == 0 || !h.TLS.ForceHTTPS {
		t.Fatalf("host = %+v", h)
	}
	if res.URL != "https://panel.example.com:8443" {
		t.Fatalf("url = %s", res.URL)
	}
	up, _ := s.panelUpstream()
	if up.ID != h.UpstreamID || up.Targets[0].Address != "127.0.0.1" || up.Targets[0].Port != 9090 {
		t.Fatalf("upstream = %+v", up)
	}
	var acc AcmeAccount
	if s.db.Where("is_default = ?", true).First(&acc).Error != nil || acc.Email != "ops@example.com" {
		t.Fatalf("account = %+v", acc)
	}

	again, err := s.EnsurePanelHost(PanelHostInput{Domain: "panel.example.com"})
	if err != nil || again.Created || again.Host.ID != h.ID {
		t.Fatalf("second call = %+v, %v", again, err)
	}

	ip, err := s.EnsurePanelHost(PanelHostInput{Domain: "203.0.113.7"})
	if err != nil || !ip.Created || ip.Host.TLS.Mode != TLSNone || ip.URL != "http://203.0.113.7" || ip.Host.UpstreamID != up.ID {
		t.Fatalf("ip host = %+v, %v", ip, err)
	}
}
