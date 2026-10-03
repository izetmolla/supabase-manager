package proxymanager

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

const projectDomainsKey = "project_domains:"

// projectDomainServices are the project services whose public URL can be picked; "site" is the
// app in front of the project (auth.site_url) and may use any host.
var projectDomainServices = []string{"api", "studio", "mail", "site"}

// ProjectDomain is a public URL of a proxy host.
type ProjectDomain struct {
	Service  string `json:"service,omitempty"`
	HostID   uint   `json:"host_id"`
	HostName string `json:"host_name"`
	Domain   string `json:"domain"`
	URL      string `json:"url"`
}

type ProjectDomains struct {
	// Available are the domains of hosts routing to a service of this project.
	Available []ProjectDomain `json:"available"`
	// Sites are the domains of every host, candidates for the Site URL.
	Sites []ProjectDomain `json:"sites"`
	// Selected maps a service (api, studio, mail, site) to the chosen URL; missing means local.
	Selected map[string]string `json:"selected"`
}

// hostURLs returns the public URL of each concrete domain of a host, using the scheme and
// port of the first enabled instance serving it.
func (s *Service) hostURLs(h *ProxyHost, instances map[uint]*ProxyInstance) []ProjectDomain {
	var in *ProxyInstance
	for _, id := range h.InstanceIDs {
		if i := instances[id]; i != nil && i.Enabled {
			in = i
			break
		}
	}
	if in == nil {
		return nil
	}
	scheme, port, def := "http", in.HTTPPort, 80
	if h.TLS.Mode != "" && h.TLS.Mode != TLSNone && in.HTTPSPort > 0 {
		scheme, port, def = "https", in.HTTPSPort, 443
	}
	if port == 0 {
		return nil
	}
	var out []ProjectDomain
	for _, d := range h.Domains {
		d = strings.TrimSpace(d)
		if d == "" || strings.ContainsAny(d, "*~^") {
			continue
		}
		u := scheme + "://" + d
		if port != def {
			u += ":" + strconv.Itoa(port)
		}
		out = append(out, ProjectDomain{HostID: h.ID, HostName: h.Name, Domain: d, URL: u})
	}
	return out
}

// hostProjectServices lists the services of project that a host routes to.
func hostProjectServices(h *ProxyHost, upstreams map[uint]*ProxyUpstream, project string) []string {
	ids := []uint{h.UpstreamID}
	for _, r := range h.Routes {
		ids = append(ids, r.UpstreamID)
	}
	var out []string
	for _, id := range ids {
		u := upstreams[id]
		if u == nil {
			continue
		}
		for _, t := range u.Targets {
			if t.Kind == "project" && t.Project == project && !slices.Contains(out, t.Service) {
				out = append(out, t.Service)
			}
		}
	}
	return out
}

func (s *Service) GetProjectDomains(project string) (*ProjectDomains, error) {
	var hosts []ProxyHost
	var ups []ProxyUpstream
	var insts []ProxyInstance
	if err := s.db.Where("enabled = ?", true).Order("id").Find(&hosts).Error; err != nil {
		return nil, err
	}
	s.db.Find(&ups)
	s.db.Find(&insts)
	upstreams := map[uint]*ProxyUpstream{}
	for i := range ups {
		upstreams[ups[i].ID] = &ups[i]
	}
	instances := map[uint]*ProxyInstance{}
	for i := range insts {
		instances[insts[i].ID] = &insts[i]
	}

	out := &ProjectDomains{Available: []ProjectDomain{}, Sites: []ProjectDomain{}, Selected: map[string]string{}}
	for i := range hosts {
		h := &hosts[i]
		urls := s.hostURLs(h, instances)
		if h.Kind == spec.HostProxy {
			out.Sites = append(out.Sites, urls...)
		}
		for _, svc := range hostProjectServices(h, upstreams, project) {
			for _, d := range urls {
				d.Service = svc
				out.Available = append(out.Available, d)
			}
		}
	}
	if _, err := s.settings.Get(projectDomainsKey+project, &out.Selected); err != nil {
		return nil, err
	}
	if out.Selected == nil {
		out.Selected = map[string]string{}
	}
	return out, nil
}

// SetProjectDomains stores the chosen URLs and points the project's auth at them: the API URL
// becomes auth.external_url and the site URL becomes auth.site_url (and an allowed redirect).
// It reports whether config.toml changed, which needs a project restart.
func (s *Service) SetProjectDomains(p *models.Project, selected map[string]string) (bool, error) {
	cur, err := s.GetProjectDomains(p.Slug)
	if err != nil {
		return false, err
	}
	clean := map[string]string{}
	for k, v := range selected {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !slices.Contains(projectDomainServices, k) {
			return false, invalid(fmt.Sprintf("unknown service %q", k))
		}
		pool := cur.Available
		if k == "site" {
			pool = cur.Sites
		}
		if !slices.ContainsFunc(pool, func(d ProjectDomain) bool { return d.URL == v && (k == "site" || d.Service == k) }) {
			return false, invalid(fmt.Sprintf("%s is not a proxy host of this project's %s", v, k))
		}
		clean[k] = v
	}

	f, err := s.projects.LoadConfig(p)
	if err != nil {
		return false, err
	}
	changed := false
	if clean["api"] != cur.Selected["api"] {
		ext := ""
		if clean["api"] != "" {
			ext = clean["api"] + "/auth/v1"
		}
		if err := f.Set("auth", "external_url", ext); err != nil {
			return false, err
		}
		changed = true
	}
	if site := clean["site"]; site != "" && site != cur.Selected["site"] {
		if err := f.Set("auth", "site_url", site); err != nil {
			return false, err
		}
		redirects := f.Strings("auth", "additional_redirect_urls")
		if !slices.Contains(redirects, site) {
			if err := f.Set("auth", "additional_redirect_urls", append(redirects, site)); err != nil {
				return false, err
			}
		}
		changed = true
	}
	if changed {
		if err := f.Save(); err != nil {
			return false, err
		}
	}
	return changed, s.settings.Put(projectDomainsKey+p.Slug, clean)
}
