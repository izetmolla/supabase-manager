package proxymanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/supabase-manager/manager/internal/proxy-manager/acme"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"gorm.io/gorm"
)

var (
	reDomain     = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	reHeaderName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	reImage      = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*$`)
	reUsername   = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
	reHostname   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	reEnvKey     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// ErrNotFound is returned for unknown ids.
var ErrNotFound = errors.New("not found")

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

func validPort(p int, allowZero bool) bool {
	return (allowZero && p == 0) || (p >= 1 && p <= 65535)
}

func validCIDR(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// safeValue rejects characters that would break out of a quoted nginx string or a Traefik
// rule; generated configuration must not be injectable through form fields.
func safeValue(field, v string) error {
	if strings.ContainsAny(v, "\n\r`") {
		return invalid(field + " must not contain line breaks or backticks")
	}
	return nil
}

func validPath(field, pathType, p string) error {
	if p == "" {
		return invalid(field + " is required")
	}
	if strings.ContainsAny(p, " \t\n\r;{}`\"'") {
		return invalid(field + " must not contain spaces, quotes, braces or semicolons")
	}
	switch pathType {
	case spec.PathRegex:
		if _, err := regexp.Compile(p); err != nil {
			return invalid(field + " is not a valid regular expression")
		}
	default:
		if !strings.HasPrefix(p, "/") {
			return invalid(field + " must start with /")
		}
	}
	return nil
}

func validHeaders(field string, hs []spec.Header) error {
	for _, h := range hs {
		if !reHeaderName.MatchString(h.Name) {
			return invalid(fmt.Sprintf("%s: %q is not a valid header name", field, h.Name))
		}
		if err := safeValue(field, h.Value); err != nil {
			return err
		}
	}
	return nil
}

func normalizeDomains(ds []string) ([]string, error) {
	var out []string
	for _, d := range ds {
		d = strings.ToLower(strings.TrimSpace(d))
		d = strings.TrimSuffix(d, ".")
		if d == "" {
			continue
		}
		if !reDomain.MatchString(d) {
			return nil, invalid(fmt.Sprintf("%q is not a valid domain name", d))
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, invalid("at least one domain is required")
	}
	return out, nil
}

func (s *Service) instanceIDsExist(ids []uint) ([]uint, error) {
	var out []uint
	for _, id := range ids {
		if slices.Contains(out, id) {
			continue
		}
		var n int64
		s.db.Model(&ProxyInstance{}).Where("id = ?", id).Count(&n)
		if n == 0 {
			return nil, invalid(fmt.Sprintf("proxy instance %d does not exist", id))
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) exists(model any, id uint, what string) error {
	if id == 0 {
		return nil
	}
	var n int64
	s.db.Model(model).Where("id = ?", id).Count(&n)
	if n == 0 {
		return invalid(fmt.Sprintf("%s %d does not exist", what, id))
	}
	return nil
}

// ---- Instances ----

// InstanceView is an instance with its container state and deploy status.
type InstanceView struct {
	ProxyInstance
	Container   any       `json:"container"`
	Agent       AgentInfo `json:"agent"`
	InSync      bool      `json:"in_sync"`
	Pending     bool      `json:"pending"`
	HostCount   int       `json:"host_count"`
	StreamCount int       `json:"stream_count"`
}

func (s *Service) ListInstances(ctx context.Context) ([]InstanceView, error) {
	var list []ProxyInstance
	if err := s.db.Order("id").Find(&list).Error; err != nil {
		return nil, err
	}
	var hosts []ProxyHost
	var streams []ProxyStream
	s.db.Select("id", "instance_ids", "enabled").Find(&hosts)
	s.db.Select("id", "instance_ids", "enabled").Find(&streams)
	out := make([]InstanceView, 0, len(list))
	for i := range list {
		v := InstanceView{ProxyInstance: list[i]}
		if st, err := s.dc.ContainerState(ctx, list[i].ContainerName()); err == nil {
			v.Container = st
		}
		for _, h := range hosts {
			if slices.Contains(h.InstanceIDs, list[i].ID) {
				v.HostCount++
			}
		}
		for _, st := range streams {
			if slices.Contains(st.InstanceIDs, list[i].ID) {
				v.StreamCount++
			}
		}
		v.Pending = s.pending(&list[i])
		v.Agent = s.agents.info(list[i].ID)
		v.InSync = v.Agent.Connected && v.Agent.ConfigChecksum == list[i].DeployedChecksum
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) GetInstance(id uint) (*ProxyInstance, error) {
	var in ProxyInstance
	if err := s.db.First(&in, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &in, nil
}

func (s *Service) normalizeInstance(in *ProxyInstance) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return invalid("name is required")
	}
	switch in.Kind {
	case spec.KindNginx:
		if in.Image == "" {
			in.Image = s.DefaultImage(in.Kind)
		}
	case spec.KindTraefik:
		if in.Image == "" {
			in.Image = s.DefaultImage(in.Kind)
		}
		in.TLSALPN = false
	default:
		return invalid("kind must be nginx or traefik")
	}
	in.Image = strings.TrimSpace(in.Image)
	if !reImage.MatchString(in.Image) {
		return invalid("image is not a valid image reference")
	}
	if legacyImage(in.Image) {
		return invalid("the image must include the Supabase Manager proxy agent; use " + s.DefaultImage(in.Kind) + " or an image built from it")
	}
	in.BindIP = strings.TrimSpace(in.BindIP)
	if in.BindIP != "" && net.ParseIP(in.BindIP) == nil {
		return invalid("bind IP must be an IP address (or empty for all addresses)")
	}
	if !validPort(in.HTTPPort, true) || !validPort(in.HTTPSPort, true) {
		return invalid("ports must be between 1 and 65535")
	}
	if in.HTTPPort == 0 && in.HTTPSPort == 0 {
		return invalid("an instance needs an HTTP or an HTTPS port")
	}
	if in.HTTPPort != 0 && in.HTTPPort == in.HTTPSPort {
		return invalid("HTTP and HTTPS ports must differ")
	}
	if in.TLSALPN && in.HTTPSPort == 0 {
		return invalid("TLS-ALPN-01 needs an HTTPS port")
	}
	if in.Kind == spec.KindTraefik {
		if in.AdminPort == 0 {
			in.AdminPort = s.freeAdminPort(in.ID)
		}
		if !validPort(in.AdminPort, false) {
			return invalid("admin port must be between 1 and 65535")
		}
	} else {
		in.AdminPort = 0
	}
	return nil
}

func (s *Service) freeAdminPort(self uint) int {
	var list []ProxyInstance
	s.db.Where("id <> ?", self).Find(&list)
	used := map[int]bool{}
	for _, i := range list {
		used[i.AdminPort], used[i.HTTPPort], used[i.HTTPSPort] = true, true, true
	}
	for p := 8090; p < 8190; p++ {
		if !used[p] && p != s.managerPort() && !strings.HasSuffix(s.opts.AgentAddr, ":"+strconv.Itoa(p)) {
			return p
		}
	}
	return 8090
}

func (s *Service) CreateInstance(in *ProxyInstance) error {
	in.ID = 0
	if err := s.normalizeInstance(in); err != nil {
		return err
	}
	in.TokenEncrypted = s.encrypt(randomToken())
	in.DeployedRevisionID, in.DeployedChecksum, in.DeployedAt = 0, "", nil
	return s.db.Create(in).Error
}

func (s *Service) UpdateInstance(id uint, in *ProxyInstance) (*ProxyInstance, error) {
	cur, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	in.ID = id
	in.Kind = cur.Kind
	if err := s.normalizeInstance(in); err != nil {
		return nil, err
	}
	cur.Name, cur.Image, cur.BindIP, cur.HTTPPort, cur.HTTPSPort = in.Name, in.Image, in.BindIP, in.HTTPPort, in.HTTPSPort
	cur.AdminPort, cur.TLSALPN, cur.Enabled, cur.Notes = in.AdminPort, in.TLSALPN, in.Enabled, in.Notes
	return cur, s.saveRow(cur, false)
}

func (s *Service) DeleteInstance(ctx context.Context, id uint) error {
	in, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if err := s.removeRuntime(ctx, in); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var hosts []ProxyHost
		tx.Find(&hosts)
		for _, h := range hosts {
			if i := slices.Index(h.InstanceIDs, id); i >= 0 {
				h.InstanceIDs = slices.Delete(h.InstanceIDs, i, i+1)
				if err := tx.Model(&h).Update("instance_ids", h.InstanceIDs).Error; err != nil {
					return err
				}
			}
		}
		var streams []ProxyStream
		tx.Find(&streams)
		for _, st := range streams {
			if i := slices.Index(st.InstanceIDs, id); i >= 0 {
				st.InstanceIDs = slices.Delete(st.InstanceIDs, i, i+1)
				if err := tx.Model(&st).Update("instance_ids", st.InstanceIDs).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Where("instance_id = ?", id).Delete(&ConfigRevision{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ProxyInstance{}, id).Error
	})
}

// ---- Hosts ----

func (s *Service) ListHosts() ([]ProxyHost, error) {
	var list []ProxyHost
	return list, s.db.Order("id").Find(&list).Error
}

func (s *Service) GetHost(id uint) (*ProxyHost, error) {
	var h ProxyHost
	if err := s.db.First(&h, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &h, nil
}

func (s *Service) normalizeHost(h *ProxyHost) error {
	var err error
	if h.Domains, err = normalizeDomains(h.Domains); err != nil {
		return err
	}
	if h.Name = strings.TrimSpace(h.Name); h.Name == "" {
		h.Name = h.Domains[0]
	}
	if h.InstanceIDs, err = s.instanceIDsExist(h.InstanceIDs); err != nil {
		return err
	}
	switch h.Kind {
	case "", spec.HostProxy:
		h.Kind = spec.HostProxy
	case spec.HostRedirect:
		u, err := url.Parse(h.Redirect.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("redirect URL must be an absolute http(s) URL")
		}
		if err := safeValue("redirect URL", h.Redirect.URL); err != nil {
			return err
		}
		if strings.ContainsAny(h.Redirect.URL, " \"'$") {
			return invalid("redirect URL must not contain spaces, quotes or $")
		}
		if !slices.Contains([]int{0, 301, 302, 307, 308}, h.Redirect.Code) {
			return invalid("redirect code must be 301, 302, 307 or 308")
		}
	case spec.HostError:
		if h.ErrorPage.Code != 0 && (h.ErrorPage.Code < 200 || h.ErrorPage.Code > 599) {
			return invalid("error code must be between 200 and 599")
		}
		if len(h.ErrorPage.Body) > 64*1024 {
			return invalid("error page body is limited to 64 KB")
		}
	default:
		return invalid("kind must be proxy, redirect or error")
	}
	if err := s.exists(&ProxyUpstream{}, h.UpstreamID, "upstream"); err != nil {
		return err
	}
	switch h.TLS.Mode {
	case "", TLSNone:
		h.TLS.Mode, h.TLS.CertificateID = TLSNone, 0
	case TLSCertificate, TLSAuto:
		if err := s.exists(&Certificate{}, h.TLS.CertificateID, "certificate"); err != nil {
			return err
		}
	default:
		return invalid("TLS mode must be none, certificate or auto")
	}
	if h.TLS.Mode == TLSCertificate && h.TLS.CertificateID == 0 {
		return invalid("choose a certificate")
	}
	o := &h.Options
	if o.ConnectTimeout < 0 || o.ReadTimeout < 0 || o.ConnectTimeout > 3600 || o.ReadTimeout > 86400 {
		return invalid("timeouts must be between 0 and 86400 seconds")
	}
	if o.MaxBodyMB < -1 || o.MaxBodyMB > 100000 {
		return invalid("max body size must be -1 (unlimited), 0 (default) or a size in MB")
	}
	sec := &h.Security
	if err := s.exists(&AccessList{}, sec.AccessListID, "access list"); err != nil {
		return err
	}
	for _, r := range sec.IPRules {
		if r.Action != "allow" && r.Action != "deny" {
			return invalid("IP rule action must be allow or deny")
		}
		if !validCIDR(r.CIDR) {
			return invalid(fmt.Sprintf("%q is not an IP address or CIDR range", r.CIDR))
		}
	}
	if sec.RateLimit.Enabled && (sec.RateLimit.RPS < 1 || sec.RateLimit.RPS > 100000 || sec.RateLimit.Burst < 0) {
		return invalid("rate limit needs at least 1 request per second and a burst of 0 or more")
	}
	if err := validHeaders("request headers", h.Headers.Request); err != nil {
		return err
	}
	if err := validHeaders("response headers", h.Headers.Response); err != nil {
		return err
	}
	c := &h.Headers.CORS
	for _, o := range c.Origins {
		if strings.ContainsAny(o, " \"'`\n;") {
			return invalid(fmt.Sprintf("CORS origin %q is not valid", o))
		}
	}
	if err := safeValue("CORS methods", c.Methods); err != nil {
		return err
	}
	if err := safeValue("CORS headers", c.Headers); err != nil {
		return err
	}
	if strings.ContainsAny(c.Methods+c.Headers, "\";") {
		return invalid("CORS methods and headers must not contain quotes or semicolons")
	}
	var nextID uint
	for _, r := range h.Routes {
		nextID = max(nextID, r.ID)
	}
	for i := range h.Routes {
		r := &h.Routes[i]
		if r.ID == 0 {
			nextID++
			r.ID = nextID
		}
		if r.PathType == "" {
			r.PathType = spec.PathPrefix
		}
		if !slices.Contains([]string{spec.PathPrefix, spec.PathExact, spec.PathRegex}, r.PathType) {
			return invalid("route path type must be prefix, exact or regex")
		}
		if err := validPath(fmt.Sprintf("route %d path", i+1), r.PathType, r.Path); err != nil {
			return err
		}
		for _, hm := range r.Headers {
			if !reHeaderName.MatchString(hm.Name) {
				return invalid(fmt.Sprintf("route %d: %q is not a valid header name", i+1, hm.Name))
			}
			if err := safeValue("route header value", hm.Value); err != nil {
				return err
			}
			if strings.Contains(hm.Value, `"`) {
				return invalid("route header values must not contain quotes")
			}
		}
		if err := s.exists(&ProxyUpstream{}, r.UpstreamID, "upstream"); err != nil {
			return err
		}
		if err := s.exists(&AccessList{}, r.AccessListID, "access list"); err != nil {
			return err
		}
		if r.RewriteRegex != "" {
			if _, err := regexp.Compile(r.RewriteRegex); err != nil {
				return invalid(fmt.Sprintf("route %d: rewrite is not a valid regular expression", i+1))
			}
			if err := safeValue("rewrite", r.RewriteRegex+r.RewriteReplacement); err != nil {
				return err
			}
		}
		if err := validHeaders("route request headers", r.RequestHeaders); err != nil {
			return err
		}
		if err := validHeaders("route response headers", r.ResponseHeaders); err != nil {
			return err
		}
	}
	if h.RawTraefik = strings.TrimSpace(h.RawTraefik); h.RawTraefik != "" && !json.Valid([]byte(h.RawTraefik)) {
		return invalid("custom Traefik configuration must be a JSON object")
	}
	return nil
}

// HostSaveResult is a saved host and, for automatic TLS, the certificate job that was started.
type HostSaveResult struct {
	Host  *ProxyHost `json:"host"`
	JobID uint       `json:"job_id,omitempty"`
}

func (s *Service) SaveHost(h *ProxyHost, userID uint) (*HostSaveResult, error) {
	if h.ID != 0 {
		if _, err := s.GetHost(h.ID); err != nil {
			return nil, err
		}
	}
	if err := s.normalizeHost(h); err != nil {
		return nil, err
	}
	res := &HostSaveResult{Host: h}
	if h.TLS.Mode == TLSAuto && h.TLS.CertificateID == 0 {
		cert, err := s.autoCertificate(h.Domains)
		if err != nil {
			return nil, err
		}
		h.TLS.CertificateID = cert.ID
		if cert.Status != CertValid {
			job, err := s.IssueCertificate(cert.ID, userID)
			if err == nil {
				res.JobID = job.ID
			}
		}
	}
	if err := s.saveRow(h, h.ID == 0); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) DeleteHost(id uint) error {
	return s.db.Delete(&ProxyHost{}, id).Error
}

// ---- Upstreams ----

func (s *Service) ListUpstreams() ([]ProxyUpstream, error) {
	var list []ProxyUpstream
	return list, s.db.Order("name").Find(&list).Error
}

func (s *Service) SaveUpstream(u *ProxyUpstream) error {
	if u.ID != 0 {
		if err := s.exists(&ProxyUpstream{}, u.ID, "upstream"); err != nil {
			return ErrNotFound
		}
	}
	if u.Name = strings.TrimSpace(u.Name); u.Name == "" {
		return invalid("name is required")
	}
	if u.Algorithm == "" {
		u.Algorithm = spec.AlgoRoundRobin
	}
	if !slices.Contains([]string{spec.AlgoRoundRobin, spec.AlgoLeastConn, spec.AlgoIPHash}, u.Algorithm) {
		return invalid("algorithm must be round_robin, least_conn or ip_hash")
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return invalid("scheme must be http or https")
	}
	for i := range u.Targets {
		t := &u.Targets[i]
		if t.Weight < 0 || t.Weight > 1000 {
			return invalid("weights must be between 0 and 1000")
		}
		switch t.Kind {
		case "", "static":
			t.Kind = "static"
			t.Address = strings.TrimSpace(t.Address)
			if net.ParseIP(t.Address) == nil && !reHostname.MatchString(t.Address) {
				return invalid(fmt.Sprintf("%q is not a valid IP address or host name", t.Address))
			}
			if !validPort(t.Port, false) {
				return invalid("target ports must be between 1 and 65535")
			}
			t.Project, t.Service = "", ""
		case "project":
			if _, err := s.projects.Get(t.Project); err != nil {
				return invalid(fmt.Sprintf("project %q does not exist", t.Project))
			}
			if _, ok := projectServicePorts[t.Service]; !ok {
				return invalid(fmt.Sprintf("unknown project service %q", t.Service))
			}
			t.Address, t.Port = "", 0
		default:
			return invalid("target kind must be static or project")
		}
	}
	hc := &u.Health
	if hc.Enabled {
		if hc.Path == "" {
			hc.Path = "/"
		}
		if err := validPath("health check path", spec.PathPrefix, hc.Path); err != nil {
			return err
		}
		if hc.Interval < 0 || hc.Interval > 3600 || hc.Timeout < 0 || hc.Timeout > 300 {
			return invalid("health check interval or timeout is out of range")
		}
		if hc.ExpectStatus != 0 && (hc.ExpectStatus < 100 || hc.ExpectStatus > 599) {
			return invalid("expected status must be an HTTP status code")
		}
	}
	return s.saveRow(u, u.ID == 0)
}

func (s *Service) upstreamUsage(id uint) []string {
	var users []string
	var hosts []ProxyHost
	s.db.Find(&hosts)
	for _, h := range hosts {
		used := h.UpstreamID == id
		for _, r := range h.Routes {
			used = used || r.UpstreamID == id
		}
		if used {
			users = append(users, "host "+h.Name)
		}
	}
	var streams []ProxyStream
	s.db.Where("upstream_id = ?", id).Find(&streams)
	for _, st := range streams {
		users = append(users, fmt.Sprintf("stream %d/%s", st.ListenPort, st.Protocol))
	}
	return users
}

func (s *Service) DeleteUpstream(id uint) error {
	if users := s.upstreamUsage(id); len(users) > 0 {
		return invalid("the upstream is used by " + strings.Join(users, ", "))
	}
	return s.db.Delete(&ProxyUpstream{}, id).Error
}

// ---- Streams ----

func (s *Service) ListStreams() ([]ProxyStream, error) {
	var list []ProxyStream
	return list, s.db.Order("listen_port").Find(&list).Error
}

func (s *Service) SaveStream(st *ProxyStream) error {
	if st.ID != 0 {
		if err := s.exists(&ProxyStream{}, st.ID, "stream"); err != nil {
			return ErrNotFound
		}
	}
	if st.Protocol == "" {
		st.Protocol = "tcp"
	}
	if st.Protocol != "tcp" && st.Protocol != "udp" {
		return invalid("protocol must be tcp or udp")
	}
	if !validPort(st.ListenPort, false) {
		return invalid("listen port must be between 1 and 65535")
	}
	if st.UpstreamID == 0 {
		return invalid("choose an upstream")
	}
	if err := s.exists(&ProxyUpstream{}, st.UpstreamID, "upstream"); err != nil {
		return err
	}
	var err error
	if st.InstanceIDs, err = s.instanceIDsExist(st.InstanceIDs); err != nil {
		return err
	}
	st.Name = strings.TrimSpace(st.Name)
	return s.saveRow(st, st.ID == 0)
}

func (s *Service) DeleteStream(id uint) error {
	return s.db.Delete(&ProxyStream{}, id).Error
}

// ---- Access lists ----

// AccessUserInput sets a user; an empty password keeps the stored one.
type AccessUserInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AccessListInput struct {
	Name    string            `json:"name"`
	Users   []AccessUserInput `json:"users"`
	Allow   []string          `json:"allow"`
	Deny    []string          `json:"deny"`
	Satisfy string            `json:"satisfy"`
}

func publicAccessList(a AccessList) AccessList {
	users := make([]AccessUser, len(a.Users))
	for i, u := range a.Users {
		users[i] = AccessUser{Username: u.Username}
	}
	a.Users = users
	return a
}

func (s *Service) ListAccessLists() ([]AccessList, error) {
	var list []AccessList
	if err := s.db.Order("name").Find(&list).Error; err != nil {
		return nil, err
	}
	for i := range list {
		list[i] = publicAccessList(list[i])
	}
	return list, nil
}

func (s *Service) SaveAccessList(id uint, in AccessListInput) (*AccessList, error) {
	al := &AccessList{}
	if id != 0 {
		if err := s.db.First(al, id).Error; err != nil {
			return nil, notFound(err)
		}
	}
	if al.Name = strings.TrimSpace(in.Name); al.Name == "" {
		return nil, invalid("name is required")
	}
	if in.Satisfy == "" {
		in.Satisfy = "all"
	}
	if in.Satisfy != "any" && in.Satisfy != "all" {
		return nil, invalid("satisfy must be any or all")
	}
	al.Satisfy = in.Satisfy
	existing := map[string]string{}
	for _, u := range al.Users {
		existing[u.Username] = u.Hash
	}
	var users []AccessUser
	seen := map[string]bool{}
	for _, u := range in.Users {
		u.Username = strings.TrimSpace(u.Username)
		if !reUsername.MatchString(u.Username) {
			return nil, invalid(fmt.Sprintf("%q is not a valid user name (letters, digits, . _ @ -)", u.Username))
		}
		if seen[u.Username] {
			return nil, invalid("user " + u.Username + " is listed twice")
		}
		seen[u.Username] = true
		hash := existing[u.Username]
		if u.Password != "" {
			if len(u.Password) < 6 {
				return nil, invalid("passwords need at least 6 characters")
			}
			hash = apr1Hash(u.Password)
		}
		if hash == "" {
			return nil, invalid("set a password for " + u.Username)
		}
		users = append(users, AccessUser{Username: u.Username, Hash: hash})
	}
	clean := func(list []string) ([]string, error) {
		var out []string
		for _, c := range list {
			if c = strings.TrimSpace(c); c == "" {
				continue
			}
			if !validCIDR(c) {
				return nil, invalid(fmt.Sprintf("%q is not an IP address or CIDR range", c))
			}
			out = append(out, c)
		}
		return out, nil
	}
	var err error
	if al.Allow, err = clean(in.Allow); err != nil {
		return nil, err
	}
	if al.Deny, err = clean(in.Deny); err != nil {
		return nil, err
	}
	al.Users = users
	if err := s.saveRow(al, al.ID == 0); err != nil {
		return nil, err
	}
	pub := publicAccessList(*al)
	return &pub, nil
}

func (s *Service) DeleteAccessList(id uint) error {
	var hosts []ProxyHost
	s.db.Find(&hosts)
	for _, h := range hosts {
		used := h.Security.AccessListID == id
		for _, r := range h.Routes {
			used = used || r.AccessListID == id
		}
		if used {
			return invalid("the access list is used by host " + h.Name)
		}
	}
	return s.db.Delete(&AccessList{}, id).Error
}

// ---- ACME accounts ----

type AcmeAccountInput struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	DirectoryURL string `json:"directory_url"`
	EABKeyID     string `json:"eab_key_id"`
	EABHMAC      string `json:"eab_hmac"`
	IsDefault    bool   `json:"is_default"`
}

func (s *Service) ListAcmeAccounts() ([]AcmeAccount, error) {
	var list []AcmeAccount
	return list, s.db.Order("id").Find(&list).Error
}

func (s *Service) SaveAcmeAccount(id uint, in AcmeAccountInput) (*AcmeAccount, error) {
	a := &AcmeAccount{}
	if id != 0 {
		if err := s.db.First(a, id).Error; err != nil {
			return nil, notFound(err)
		}
	}
	in.Email = strings.TrimSpace(in.Email)
	if !strings.Contains(in.Email, "@") || strings.ContainsAny(in.Email, " \n") {
		return nil, invalid("a valid email address is required")
	}
	u, err := url.Parse(in.DirectoryURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, invalid("the directory URL must be an https URL")
	}
	changed := a.Email != in.Email || a.DirectoryURL != in.DirectoryURL || a.EABKeyID != in.EABKeyID || in.EABHMAC != ""
	a.Name = strings.TrimSpace(in.Name)
	if a.Name == "" {
		a.Name = in.Email
	}
	a.Email, a.DirectoryURL, a.EABKeyID = in.Email, in.DirectoryURL, strings.TrimSpace(in.EABKeyID)
	if in.EABHMAC != "" {
		a.EABHMACEncrypted = s.encrypt(strings.TrimSpace(in.EABHMAC))
	}
	if a.EABKeyID != "" && a.EABHMACEncrypted == "" {
		return nil, invalid("EAB needs both the key id and the HMAC key")
	}
	if changed {
		// A different CA or identity needs a new registration (the key is kept).
		a.Registration, a.Registered = "", false
	}
	var count int64
	s.db.Model(&AcmeAccount{}).Count(&count)
	a.IsDefault = in.IsDefault || count == 0 || (id != 0 && a.IsDefault && count == 1)
	return a, s.db.Transaction(func(tx *gorm.DB) error {
		q := tx
		if a.ID != 0 {
			q = tx.Omit("created_at")
		}
		if err := q.Save(a).Error; err != nil {
			return err
		}
		if a.IsDefault {
			return tx.Model(&AcmeAccount{}).Where("id <> ?", a.ID).Update("is_default", false).Error
		}
		return nil
	})
}

// RegisterAcmeAccount registers the account with its CA now (otherwise this happens on the
// first issuance).
func (s *Service) RegisterAcmeAccount(id uint) (*AcmeAccount, error) {
	a := &AcmeAccount{}
	if err := s.db.First(a, id).Error; err != nil {
		return nil, notFound(err)
	}
	acc := s.accountData(a)
	acc.Registration = nil
	err := s.ACME.Register(acc)
	s.storeAccount(a, acc, err)
	if err != nil {
		return a, invalid("registration failed: " + err.Error())
	}
	return a, nil
}

func (s *Service) DeleteAcmeAccount(id uint) error {
	var n int64
	s.db.Model(&Certificate{}).Where("acme_account_id = ?", id).Count(&n)
	if n > 0 {
		return invalid("the account is used by certificates")
	}
	return s.db.Delete(&AcmeAccount{}, id).Error
}

// ---- DNS providers ----

// DNSProviderView shows which credentials are set without revealing them.
type DNSProviderView struct {
	DNSProvider
	Keys []string `json:"keys"`
}

type DNSProviderInput struct {
	Name        string            `json:"name"`
	Code        string            `json:"code"`
	Credentials map[string]string `json:"credentials"`
}

func (s *Service) dnsCreds(p *DNSProvider) map[string]string {
	m := map[string]string{}
	if dec := s.decrypt(p.CredentialsEncrypted); dec != "" {
		_ = json.Unmarshal([]byte(dec), &m)
	}
	return m
}

func (s *Service) ListDNSProviders() ([]DNSProviderView, error) {
	var list []DNSProvider
	if err := s.db.Order("name").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]DNSProviderView, 0, len(list))
	for i := range list {
		keys := []string{}
		for k := range s.dnsCreds(&list[i]) {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		out = append(out, DNSProviderView{DNSProvider: list[i], Keys: keys})
	}
	return out, nil
}

func (s *Service) SaveDNSProvider(id uint, in DNSProviderInput) (*DNSProviderView, error) {
	p := &DNSProvider{}
	if id != 0 {
		if err := s.db.First(p, id).Error; err != nil {
			return nil, notFound(err)
		}
	}
	if !slices.ContainsFunc(acme.Catalog(), func(e acme.CatalogEntry) bool { return e.Code == in.Code }) {
		return nil, invalid("unknown DNS provider")
	}
	creds := map[string]string{}
	if p.Code == in.Code {
		creds = s.dnsCreds(p)
	}
	for k, v := range in.Credentials {
		k = strings.TrimSpace(k)
		if !reEnvKey.MatchString(k) || !acme.AllowedKey(in.Code, k) {
			return nil, invalid(fmt.Sprintf("%s is not a setting of this provider", k))
		}
		v = strings.TrimSpace(v)
		switch v {
		case "":
			continue // keep the stored value
		case "-":
			delete(creds, k)
		default:
			creds[k] = v
		}
	}
	if len(creds) == 0 {
		return nil, invalid("enter the provider credentials")
	}
	b, _ := json.Marshal(creds)
	p.Name = strings.TrimSpace(in.Name)
	if p.Name == "" {
		p.Name = in.Code
	}
	p.Code = in.Code
	p.CredentialsEncrypted = s.encrypt(string(b))
	if err := s.saveRow(p, p.ID == 0); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(creds))
	for k := range creds {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return &DNSProviderView{DNSProvider: *p, Keys: keys}, nil
}

func (s *Service) DeleteDNSProvider(id uint) error {
	var n int64
	s.db.Model(&Certificate{}).Where("dns_provider_id = ?", id).Count(&n)
	if n > 0 {
		return invalid("the DNS provider is used by certificates")
	}
	return s.db.Delete(&DNSProvider{}, id).Error
}
