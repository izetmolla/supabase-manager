package proxymanager

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/supabase-manager/manager/internal/proxy-manager/acme"
)

const panelUpstreamName = "Supabase Manager"

// PanelHostInput publishes the manager itself through the proxies.
type PanelHostInput struct {
	// Domain is a domain name or the server's public IP address.
	Domain string `json:"domain"`
	// Email registers a Let's Encrypt account for HTTPS when no default ACME account exists.
	Email string `json:"email"`
	// NoTLS serves the panel over HTTP only.
	NoTLS bool `json:"no_tls"`
}

// PanelHostResult describes the host serving the panel.
type PanelHostResult struct {
	Host    *ProxyHost `json:"host"`
	Created bool       `json:"created"`
	URL     string     `json:"url"`
}

// EnsurePanelHost creates a proxy host forwarding Domain to the manager on every enabled
// instance. An existing host for the domain is kept as it is. With TLS, the certificate is
// created but not issued; ApplyPanelHost issues it after the deploy.
func (s *Service) EnsurePanelHost(in PanelHostInput) (*PanelHostResult, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	domains, err := normalizeDomains([]string{in.Domain})
	if err != nil {
		return nil, err
	}
	domain := domains[0]
	if strings.HasPrefix(domain, "*.") {
		return nil, invalid("the panel needs a single domain, not a wildcard")
	}

	var hosts []ProxyHost
	s.db.Order("id").Find(&hosts)
	for i := range hosts {
		if slices.Contains(hosts[i].Domains, domain) {
			return &PanelHostResult{Host: &hosts[i], URL: s.panelURL(&hosts[i], domain)}, nil
		}
	}

	var instances []ProxyInstance
	s.db.Where("enabled = ?", true).Order("id").Find(&instances)
	if len(instances) == 0 {
		return nil, invalid("there is no enabled proxy instance; create one in Proxy Manager > Instances")
	}
	ids := make([]uint, 0, len(instances))
	https := false
	for _, inst := range instances {
		ids = append(ids, inst.ID)
		https = https || inst.HTTPSPort > 0
	}

	up, err := s.panelUpstream()
	if err != nil {
		return nil, err
	}
	h := &ProxyHost{
		Name: "Supabase Manager", Domains: []string{domain}, UpstreamID: up.ID, InstanceIDs: ids,
		Options: HostOptions{WebSocket: true, ReadTimeout: 3600},
		Enabled: true, Notes: "Supabase Manager panel (created by the installer or the CLI)",
	}
	if https && !in.NoTLS && net.ParseIP(domain) == nil {
		if err := s.ensureDefaultAccount(in.Email); err != nil {
			return nil, err
		}
		h.TLS = TLSOptions{Mode: TLSAuto, ForceHTTPS: true, HTTP2: true}
	}
	if err := s.normalizeHost(h); err != nil {
		return nil, err
	}
	if h.TLS.Mode == TLSAuto {
		cert, err := s.autoCertificate(h.Domains)
		if err != nil {
			return nil, err
		}
		h.TLS.CertificateID = cert.ID
	}
	if err := s.saveRow(h, true); err != nil {
		return nil, err
	}
	return &PanelHostResult{Host: h, Created: true, URL: s.panelURL(h, domain)}, nil
}

// ApplyPanelHost deploys the instances serving the panel host that have pending changes and,
// for HTTPS, issues the certificate (which redeploys the instances once it is stored).
func (s *Service) ApplyPanelHost(ctx context.Context, res *PanelHostResult, logf func(string)) error {
	if res.Created {
		logf(fmt.Sprintf("Created proxy host %q for %s", res.Host.Name, strings.Join(res.Host.Domains, ", ")))
	} else {
		logf(fmt.Sprintf("Proxy host %q already serves %s; keeping it", res.Host.Name, strings.Join(res.Host.Domains, ", ")))
	}
	for _, id := range res.Host.InstanceIDs {
		inst, err := s.GetInstance(id)
		if err != nil || !s.pending(inst) {
			continue
		}
		logf(fmt.Sprintf("== deploying %s (%s)", inst.Name, inst.Kind))
		if _, err := s.Deploy(ctx, id, 0, RevisionKindDeploy, "Supabase Manager panel host", logf); err != nil {
			return fmt.Errorf("deploy %s: %w", inst.Name, err)
		}
	}
	if res.Host.TLS.Mode == TLSAuto && res.Host.TLS.CertificateID != 0 {
		cert, err := s.GetCertificate(res.Host.TLS.CertificateID)
		if err == nil && cert.Status != CertValid {
			logf("== issuing the certificate for " + strings.Join(cert.Domains, ", "))
			if err := s.issue(ctx, cert.ID, 0, logf); err != nil {
				return fmt.Errorf("certificate: %w (retry in Proxy Manager > Certificates once DNS points to this server)", err)
			}
		}
	}
	return nil
}

// panelUpstream returns the upstream pointing at the manager, creating it when missing.
func (s *Service) panelUpstream() (*ProxyUpstream, error) {
	host, port, err := net.SplitHostPort(strings.TrimPrefix(ManagerURLFromAddr(s.opts.ManagerAddr), "http://"))
	if err != nil {
		return nil, err
	}
	p, _ := strconv.Atoi(port)
	var up ProxyUpstream
	if s.db.Where("name = ?", panelUpstreamName).Limit(1).Find(&up).RowsAffected > 0 {
		return &up, nil
	}
	up = ProxyUpstream{Name: panelUpstreamName, Targets: []Target{{Kind: "static", Address: host, Port: p, Weight: 1}}}
	if err := s.SaveUpstream(&up); err != nil {
		return nil, err
	}
	return &up, nil
}

// ensureDefaultAccount registers a Let's Encrypt account with email unless a default ACME
// account already exists.
func (s *Service) ensureDefaultAccount(email string) error {
	var n int64
	s.db.Model(&AcmeAccount{}).Where("is_default = ?", true).Count(&n)
	if n > 0 {
		return nil
	}
	if strings.TrimSpace(email) == "" {
		return invalid("HTTPS needs an email for the Let's Encrypt account (or turn TLS off)")
	}
	_, err := s.SaveAcmeAccount(0, AcmeAccountInput{Name: "Let's Encrypt", Email: email, DirectoryURL: acme.Directories[0].URL, IsDefault: true})
	return err
}

// panelURL is the address of domain on the first instance serving h.
func (s *Service) panelURL(h *ProxyHost, domain string) string {
	scheme, port, def := "http", 80, 80
	var inst ProxyInstance
	if len(h.InstanceIDs) > 0 {
		s.db.Limit(1).Find(&inst, h.InstanceIDs[0])
		port = inst.HTTPPort
	}
	if h.TLS.Mode != TLSNone && h.TLS.Mode != "" && inst.HTTPSPort > 0 {
		scheme, port, def = "https", inst.HTTPSPort, 443
	}
	if port == 0 || port == def {
		return scheme + "://" + domain
	}
	return scheme + "://" + net.JoinHostPort(domain, strconv.Itoa(port))
}
