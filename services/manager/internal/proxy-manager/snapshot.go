package proxymanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/ports"
	"github.com/supabase-manager/manager/internal/projects"
	"github.com/supabase-manager/manager/internal/proxy-manager/render/nginx"
	"github.com/supabase-manager/manager/internal/proxy-manager/render/traefik"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

// projectServicePorts maps the services offered as upstream targets to their config.toml port.
var projectServicePorts = map[string]func(ports.Ports) int{
	"api":       func(p ports.Ports) int { return p.API },
	"studio":    func(p ports.Ports) int { return p.Studio },
	"mail":      func(p ports.Ports) int { return p.SMTP },
	"db":        func(p ports.Ports) int { return p.DB },
	"pooler":    func(p ports.Ports) int { return p.Pooler },
	"analytics": func(p ports.Ports) int { return p.Analytics },
}

var projectServiceLabels = map[string]string{
	"api": "API gateway (Kong)", "studio": "Studio", "mail": "Mailpit",
	"db": "Postgres", "pooler": "Connection pooler", "analytics": "Analytics",
}

// ProjectService is a project service that can be picked as an upstream target.
type ProjectService struct {
	Project     string `json:"project"`
	ProjectName string `json:"project_name"`
	Service     string `json:"service"`
	Label       string `json:"label"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Status      string `json:"status"`
}

func (s *Service) ProjectServices() ([]ProjectService, error) {
	list, err := s.projects.List()
	if err != nil {
		return nil, err
	}
	keys := []string{"api", "studio", "mail", "db", "pooler", "analytics"}
	var out []ProjectService
	for i := range list {
		p := &list[i]
		f, err := s.projects.LoadConfig(p)
		if err != nil {
			continue
		}
		pt := f.Settings().Ports
		for _, k := range keys {
			out = append(out, ProjectService{
				Project: p.Slug, ProjectName: p.Name, Service: k, Label: projectServiceLabels[k],
				Host: projects.ConnectHost(p), Port: projectServicePorts[k](pt), Status: p.Status,
			})
		}
	}
	return out, nil
}

// resolver resolves project-service targets, caching config.toml reads for one build.
type resolver struct {
	s     *Service
	cache map[string]*resolved
}

type resolved struct {
	host  string
	ports ports.Ports
	err   error
}

func (r *resolver) target(t Target) (spec.Target, error) {
	w := t.Weight
	if w == 0 {
		w = 1
	}
	if t.Kind != "project" {
		return spec.Target{Host: t.Address, Port: t.Port, Weight: w, Backup: t.Backup}, nil
	}
	if r.cache == nil {
		r.cache = map[string]*resolved{}
	}
	rs := r.cache[t.Project]
	if rs == nil {
		rs = &resolved{}
		p, err := r.s.projects.Get(t.Project)
		if err == nil {
			cfg, cerr := r.s.projects.LoadConfig(p)
			if cerr != nil {
				err = cerr
			} else {
				rs.ports = cfg.Settings().Ports
				rs.host = projects.ConnectHost(p)
			}
		}
		rs.err = err
		r.cache[t.Project] = rs
	}
	if rs.err != nil {
		return spec.Target{}, fmt.Errorf("project %s: %w", t.Project, rs.err)
	}
	fn := projectServicePorts[t.Service]
	if fn == nil {
		return spec.Target{}, fmt.Errorf("unknown service %q", t.Service)
	}
	return spec.Target{Host: rs.host, Port: fn(rs.ports), Weight: w, Backup: t.Backup}, nil
}

func (r *resolver) upstream(u *ProxyUpstream) (*spec.Upstream, []string) {
	out := &spec.Upstream{
		ID: u.ID, Name: u.Name, Algorithm: u.Algorithm, Scheme: u.Scheme,
		TLSSkipVerify: u.TLSSkipVerify, Sticky: u.Sticky, Health: u.Health,
	}
	var warnings []string
	for _, t := range u.Targets {
		rt, err := r.target(t)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("upstream %q: target skipped: %v", u.Name, err))
			continue
		}
		out.Targets = append(out.Targets, rt)
	}
	return out, warnings
}

func (s *Service) specInstance(in *ProxyInstance) spec.Instance {
	return spec.Instance{
		ID: in.ID, Name: in.Name, Kind: in.Kind, BindIP: in.BindIP,
		HTTPPort: in.HTTPPort, HTTPSPort: in.HTTPSPort, AdminPort: in.AdminPort, TLSALPN: in.TLSALPN,
		ManagerURL:     s.opts.ManagerURL,
		ALPNSolverAddr: s.opts.ALPNAddr,
	}
}

// buildState resolves everything an instance serves. Only enabled hosts and streams assigned to
// the instance are included.
func (s *Service) buildState(in *ProxyInstance) (*spec.State, []string, error) {
	st := &spec.State{
		Instance:    s.specInstance(in),
		Upstreams:   map[uint]*spec.Upstream{},
		Certs:       map[uint]*spec.Cert{},
		AccessLists: map[uint]*spec.AccessList{},
	}
	var warnings []string
	var hosts []ProxyHost
	if err := s.db.Where("enabled = ?", true).Order("id").Find(&hosts).Error; err != nil {
		return nil, nil, err
	}
	var streams []ProxyStream
	if err := s.db.Where("enabled = ?", true).Order("listen_port").Find(&streams).Error; err != nil {
		return nil, nil, err
	}

	upIDs := map[uint]bool{}
	aclIDs := map[uint]bool{}
	certIDs := map[uint]bool{}
	for _, h := range hosts {
		if !slices.Contains(h.InstanceIDs, in.ID) {
			continue
		}
		sh := spec.Host{
			ID: h.ID, Kind: h.Kind, Domains: h.Domains, Upstream: h.UpstreamID,
			ForceHTTPS: h.TLS.ForceHTTPS, HSTS: h.TLS.HSTS, HSTSSubdomains: h.TLS.HSTSSubdomains, HTTP2: h.TLS.HTTP2,
			WebSocket: h.Options.WebSocket, ConnectTimeout: h.Options.ConnectTimeout, ReadTimeout: h.Options.ReadTimeout,
			MaxBodyMB: h.Options.MaxBodyMB, AccessList: h.Security.AccessListID, IPRules: h.Security.IPRules,
			RateLimit: h.Security.RateLimit, RequestHeaders: h.Headers.Request, ResponseHeaders: h.Headers.Response,
			CORS: h.Headers.CORS, RedirectURL: h.Redirect.URL, RedirectCode: h.Redirect.Code,
			PreservePath: h.Redirect.PreservePath, ErrorCode: h.ErrorPage.Code, ErrorBody: h.ErrorPage.Body,
			RawNginx: h.RawNginx, RawTraefik: h.RawTraefik,
		}
		if h.TLS.Mode != TLSNone {
			sh.CertID = h.TLS.CertificateID
			certIDs[sh.CertID] = true
		}
		upIDs[h.UpstreamID] = true
		aclIDs[h.Security.AccessListID] = true
		for _, r := range h.Routes {
			sh.Routes = append(sh.Routes, spec.Route{
				ID: r.ID, PathType: r.PathType, Path: r.Path, Headers: r.Headers, Upstream: r.UpstreamID,
				StripPrefix: r.StripPrefix, RewriteRegex: r.RewriteRegex, RewriteReplacement: r.RewriteReplacement,
				AccessList: r.AccessListID, RequestHeaders: r.RequestHeaders, ResponseHeaders: r.ResponseHeaders,
			})
			upIDs[r.UpstreamID] = true
			aclIDs[r.AccessListID] = true
		}
		st.Hosts = append(st.Hosts, sh)
	}
	for _, x := range streams {
		if !slices.Contains(x.InstanceIDs, in.ID) {
			continue
		}
		st.Streams = append(st.Streams, spec.Stream{ID: x.ID, Protocol: x.Protocol, ListenPort: x.ListenPort, Upstream: x.UpstreamID})
		upIDs[x.UpstreamID] = true
	}

	r := &resolver{s: s}
	for id := range upIDs {
		if id == 0 {
			continue
		}
		var u ProxyUpstream
		if err := s.db.First(&u, id).Error; err != nil {
			continue
		}
		su, w := r.upstream(&u)
		warnings = append(warnings, w...)
		st.Upstreams[id] = su
	}
	for id := range aclIDs {
		if id == 0 {
			continue
		}
		var a AccessList
		if err := s.db.First(&a, id).Error; err != nil {
			continue
		}
		sa := &spec.AccessList{ID: a.ID, Allow: a.Allow, Deny: a.Deny, Satisfy: a.Satisfy}
		for _, u := range a.Users {
			sa.Users = append(sa.Users, spec.AccessUser{Username: u.Username, Hash: u.Hash})
		}
		st.AccessLists[id] = sa
	}
	for id := range certIDs {
		if id == 0 {
			continue
		}
		var c Certificate
		if err := s.db.First(&c, id).Error; err != nil || c.CertPEM == "" {
			continue
		}
		key := s.decrypt(c.KeyEncrypted)
		if key == "" {
			warnings = append(warnings, fmt.Sprintf("certificate %q: the private key cannot be decrypted", c.Name))
			continue
		}
		if c.NotAfter != nil && c.NotAfter.Before(time.Now()) {
			warnings = append(warnings, fmt.Sprintf("certificate %q expired on %s", c.Name, c.NotAfter.Format("2006-01-02")))
		}
		st.Certs[id] = &spec.Cert{ID: c.ID, Domains: c.Domains, CertPEM: c.CertPEM, KeyPEM: key}
	}
	return st, warnings, nil
}

// Render produces an instance's configuration from the current drafts.
func (s *Service) Render(in *ProxyInstance) (*spec.Output, error) {
	st, warnings, err := s.buildState(in)
	if err != nil {
		return nil, err
	}
	var out *spec.Output
	if in.Kind == spec.KindTraefik {
		out = traefik.Render(st)
	} else {
		out = nginx.Render(st)
	}
	out.Warnings = append(warnings, out.Warnings...)
	return out, nil
}

func checksum(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write([]byte(files[n]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// pending reports whether the drafts differ from the deployed configuration.
func (s *Service) pending(in *ProxyInstance) bool {
	if !in.Enabled {
		return false
	}
	out, err := s.Render(in)
	if err != nil {
		return false
	}
	return checksum(out.Files) != in.DeployedChecksum
}

// PreviewFile is one rendered file as shown in the UI.
type PreviewFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Deployed string `json:"deployed"`
	Status   string `json:"status"` // added | removed | changed | same
	Secret   bool   `json:"secret"`
}

// Preview is the rendered configuration of an instance compared with the deployed revision.
type Preview struct {
	InstanceID uint          `json:"instance_id"`
	Kind       string        `json:"kind"`
	Checksum   string        `json:"checksum"`
	Pending    bool          `json:"pending"`
	Files      []PreviewFile `json:"files"`
	Warnings   []string      `json:"warnings"`
}

func (s *Service) redact(in *ProxyInstance, path, content string, secret bool) string {
	if secret {
		if content == "" {
			return ""
		}
		return fmt.Sprintf("# private key (%d bytes), not shown", len(content))
	}
	if tok := s.decrypt(in.TokenEncrypted); tok != "" {
		content = strings.ReplaceAll(content, tok, "<token>")
	}
	return content
}

func (s *Service) Preview(id uint) (*Preview, error) {
	in, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	out, err := s.Render(in)
	if err != nil {
		return nil, err
	}
	deployed := map[string]string{}
	if in.DeployedRevisionID != 0 {
		if files, err := s.revisionFiles(in.DeployedRevisionID); err == nil {
			deployed = files
		}
	}
	p := &Preview{InstanceID: in.ID, Kind: in.Kind, Checksum: checksum(out.Files), Warnings: out.Warnings}
	p.Pending = in.Enabled && p.Checksum != in.DeployedChecksum
	paths := map[string]bool{}
	for k := range out.Files {
		paths[k] = true
	}
	for k := range deployed {
		paths[k] = true
	}
	names := make([]string, 0, len(paths))
	for k := range paths {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return fileOrder(names[i]) < fileOrder(names[j]) })
	for _, n := range names {
		secret := out.Secret[n] || strings.HasSuffix(n, ".key")
		cur, okCur := out.Files[n]
		dep, okDep := deployed[n]
		status := "same"
		switch {
		case okCur && !okDep:
			status = "added"
		case !okCur && okDep:
			status = "removed"
		case cur != dep:
			status = "changed"
		}
		p.Files = append(p.Files, PreviewFile{
			Path: n, Content: s.redact(in, n, cur, secret), Deployed: s.redact(in, n, dep, secret),
			Status: status, Secret: secret,
		})
	}
	return p, nil
}

// fileOrder lists main config first, then hosts, upstreams, streams, and keys last.
func fileOrder(p string) string {
	switch {
	case p == "nginx.conf" || p == "traefik.yml":
		return "0" + p
	case p == traefik.RoutingFile:
		return "1" + p
	case strings.HasPrefix(p, "hosts/"):
		return "2" + p
	case strings.HasPrefix(p, "certs/") || strings.HasPrefix(p, "htpasswd/"):
		return "9" + p
	}
	return "5" + p
}
