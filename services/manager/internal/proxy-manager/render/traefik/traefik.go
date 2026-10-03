// Package traefik renders a proxy state into Traefik v3 configuration.
//
// Traefik gets the static configuration (traefik.yml: entrypoints and providers) and two files
// in the directory watched by its file provider: the routing configuration (RoutingFile) and
// the certificates (dynamic/tls.yml). All of them are JSON, which YAML parsers accept as well.
package traefik

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

const (
	// ConfigDir is the config volume mount inside the container.
	ConfigDir = "/etc/traefik/sm"
	// AccessLogFile is the JSON access log inside the container.
	AccessLogFile = "/var/log/traefik/access.log"
	// RoutingFile holds routers, services and middlewares.
	RoutingFile = "dynamic/routing.yml"

	acmePriority = 1000000000
	routeBase    = 100000
	managerSvc   = "sm-manager"
)

type obj = map[string]any

// Render returns the configuration files for one Traefik instance.
func Render(st *spec.State) *spec.Output {
	out := spec.NewOutput()
	r := &renderer{st: st, out: out, http: newSection(), tcp: newSection(), udp: newSection(), transports: obj{}}
	out.Files["traefik.yml"] = r.static()
	r.dynamic()
	out.Files[RoutingFile] = r.dynamicJSON()
	out.Files["dynamic/tls.yml"] = r.tlsFile()
	for id, c := range st.Certs {
		out.Files[fmt.Sprintf("certs/%d.crt", id)] = c.CertPEM
		key := fmt.Sprintf("certs/%d.key", id)
		out.Files[key] = c.KeyPEM
		out.Secret[key] = true
	}
	if st.Instance.TLSALPN {
		out.Warn("TLS-ALPN-01 needs nginx; Traefik cannot forward ACME TLS challenges, use HTTP-01 or DNS-01 for hosts on this instance")
	}
	return out
}

type section struct {
	routers, services, middlewares obj
}

func newSection() *section { return &section{routers: obj{}, services: obj{}, middlewares: obj{}} }

type renderer struct {
	st             *spec.State
	out            *spec.Output
	http, tcp, udp *section
	transports     obj
	raw            []obj
}

func pretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b) + "\n"
}

func (r *renderer) addr(port int, suffix string) string {
	ip := r.st.Instance.BindIP
	if ip == "0.0.0.0" {
		ip = ""
	}
	if strings.Contains(ip, ":") {
		ip = "[" + ip + "]"
	}
	return ip + ":" + strconv.Itoa(port) + suffix
}

func (r *renderer) static() string {
	in := r.st.Instance
	eps := obj{}
	timeouts := obj{"respondingTimeouts": obj{"readTimeout": "600s", "idleTimeout": "180s"}}
	if in.HTTPPort > 0 {
		eps["web"] = obj{"address": r.addr(in.HTTPPort, ""), "transport": timeouts}
	}
	if in.HTTPSPort > 0 {
		eps["websecure"] = obj{"address": r.addr(in.HTTPSPort, ""), "transport": timeouts}
	}
	eps["traefik"] = obj{"address": "127.0.0.1:" + strconv.Itoa(in.AdminPort)}
	for _, s := range r.st.Streams {
		if s.Protocol == "udp" {
			eps[fmt.Sprintf("udp-%d", s.ListenPort)] = obj{"address": r.addr(s.ListenPort, "/udp")}
		} else {
			eps[fmt.Sprintf("tcp-%d", s.ListenPort)] = obj{"address": r.addr(s.ListenPort, "")}
		}
	}
	cfg := obj{
		"global":      obj{"checkNewVersion": false, "sendAnonymousUsage": false},
		"entryPoints": eps,
		"providers": obj{
			"file": obj{"directory": ConfigDir + "/dynamic", "watch": true},
		},
		"api":  obj{"insecure": true, "dashboard": false},
		"ping": obj{"entryPoint": "traefik"},
		"log":  obj{"level": "INFO"},
		"accessLog": obj{
			"filePath": AccessLogFile,
			"format":   "json",
			"fields":   obj{"defaultMode": "keep", "headers": obj{"defaultMode": "drop", "names": obj{"User-Agent": "keep", "Referer": "keep"}}},
		},
	}
	return pretty(cfg)
}

func (r *renderer) tlsFile() string {
	ids := make([]uint, 0, len(r.st.Certs))
	for id := range r.st.Certs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	certs := make([]obj, 0, len(ids))
	for _, id := range ids {
		certs = append(certs, obj{
			"certFile": fmt.Sprintf("%s/certs/%d.crt", ConfigDir, id),
			"keyFile":  fmt.Sprintf("%s/certs/%d.key", ConfigDir, id),
		})
	}
	cfg := obj{"tls": obj{
		"certificates": certs,
		"options":      obj{"default": obj{"minVersion": "VersionTLS12", "sniStrict": false}},
	}}
	return pretty(cfg)
}

func hostRule(domains []string) string {
	parts := make([]string, 0, len(domains))
	for _, d := range domains {
		if rest, ok := strings.CutPrefix(d, "*."); ok {
			parts = append(parts, "HostRegexp(`^[^.]+\\."+regexp.QuoteMeta(rest)+"$`)")
		} else {
			parts = append(parts, "Host(`"+d+"`)")
		}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

func routeRule(host string, rt spec.Route) string {
	var rule strings.Builder
	rule.WriteString(host)
	switch rt.PathType {
	case spec.PathExact:
		rule.WriteString(" && Path(`" + rt.Path + "`)")
	case spec.PathRegex:
		rule.WriteString(" && PathRegexp(`" + rt.Path + "`)")
	default:
		if rt.Path != "" && rt.Path != "/" {
			rule.WriteString(" && PathPrefix(`" + rt.Path + "`)")
		}
	}
	for _, h := range rt.Headers {
		if v, ok := strings.CutPrefix(h.Value, "~"); ok {
			rule.WriteString(" && HeaderRegexp(`" + h.Name + "`, `" + v + "`)")
		} else {
			rule.WriteString(" && Header(`" + h.Name + "`, `" + h.Value + "`)")
		}
	}
	return rule.String()
}

func (r *renderer) managerService() string {
	if _, ok := r.http.services[managerSvc]; !ok {
		r.http.services[managerSvc] = obj{"loadBalancer": obj{"servers": []obj{{"url": r.st.Instance.ManagerURL}}}}
	}
	return managerSvc
}

func (r *renderer) dynamic() {
	in := r.st.Instance
	if in.HTTPPort > 0 {
		r.http.routers["sm-acme"] = obj{
			"entryPoints": []string{"web"},
			"rule":        "PathPrefix(`/.well-known/acme-challenge/`)",
			"priority":    acmePriority,
			"service":     r.managerService(),
		}
	}
	for _, h := range r.st.Hosts {
		r.host(h)
	}
	for _, s := range r.st.Streams {
		r.stream(s)
	}
}

func (r *renderer) dynamicJSON() string {
	cfg := obj{}
	put := func(name string, s *section, transports obj) {
		m := obj{}
		if len(s.routers) > 0 {
			m["routers"] = s.routers
		}
		if len(s.services) > 0 {
			m["services"] = s.services
		}
		if len(s.middlewares) > 0 {
			m["middlewares"] = s.middlewares
		}
		if len(transports) > 0 {
			m["serversTransports"] = transports
		}
		if len(m) > 0 {
			cfg[name] = m
		}
	}
	put("http", r.http, r.transports)
	put("tcp", r.tcp, nil)
	put("udp", r.udp, nil)
	for _, raw := range r.raw {
		merge(cfg, raw)
	}
	return pretty(cfg)
}

// merge deep-merges src into dst; src wins for non-object values.
func merge(dst, src obj) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func (r *renderer) aclMiddlewares(aclID uint, label string) []string {
	al := r.st.AccessLists[aclID]
	if al == nil {
		return nil
	}
	var names []string
	if len(al.Deny) > 0 {
		r.out.Warn(fmt.Sprintf("%s: Traefik has no IP deny rules, only the allow rules of the access list apply", label))
	}
	if len(al.Allow) > 0 {
		n := fmt.Sprintf("acl%d-ip", al.ID)
		r.http.middlewares[n] = obj{"ipAllowList": obj{"sourceRange": al.Allow}}
		names = append(names, n)
	}
	if len(al.Users) > 0 {
		if al.Satisfy == "any" && len(al.Allow) > 0 {
			r.out.Warn(fmt.Sprintf("%s: Traefik requires both the IP allow list and the password (satisfy any is not supported)", label))
		}
		users := make([]string, 0, len(al.Users))
		for _, u := range al.Users {
			users = append(users, u.Username+":"+u.Hash)
		}
		n := fmt.Sprintf("acl%d-auth", al.ID)
		r.http.middlewares[n] = obj{"basicAuth": obj{"users": users, "removeHeader": true, "realm": "Restricted"}}
		names = append(names, n)
	}
	return names
}

func headerMap(hs []spec.Header) obj {
	m := obj{}
	for _, h := range hs {
		m[h.Name] = h.Value
	}
	return m
}

// service returns the name of the Traefik service for an upstream, optionally with a host's
// servers transport (timeouts are a transport setting in Traefik).
func (r *renderer) service(uid uint, transport string) string {
	u := r.st.Upstreams[uid]
	if u == nil {
		return ""
	}
	name := fmt.Sprintf("u%d", uid)
	if transport != "" {
		name = transport + "-" + name
	}
	if _, ok := r.http.services[name]; ok {
		return name
	}
	if transport == "" && (u.Scheme == "https" && u.TLSSkipVerify) {
		transport = fmt.Sprintf("u%d-transport", uid)
		r.transports[transport] = obj{"insecureSkipVerify": true}
	} else if transport != "" && u.Scheme == "https" && u.TLSSkipVerify {
		if t, ok := r.transports[transport].(obj); ok {
			t["insecureSkipVerify"] = true
		}
	}
	switch u.Algorithm {
	case spec.AlgoLeastConn:
		r.out.Warn(fmt.Sprintf("upstream %q: Traefik balances with weighted round robin instead of least connections", u.Name))
	case spec.AlgoIPHash:
		if !u.Sticky {
			r.out.Warn(fmt.Sprintf("upstream %q: Traefik has no IP hash, a sticky cookie is used instead", u.Name))
		}
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	stickyCookie := obj{}
	if u.Sticky || u.Algorithm == spec.AlgoIPHash {
		stickyCookie = obj{"cookie": obj{"name": fmt.Sprintf("sm_u%d", uid), "httpOnly": true, "secure": false}}
	}
	lb := func(targets []spec.Target, sticky bool) obj {
		servers := make([]obj, 0, len(targets))
		for _, t := range targets {
			host := t.Host
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			servers = append(servers, obj{"url": fmt.Sprintf("%s://%s:%d", scheme, host, t.Port)})
		}
		if len(servers) == 0 {
			servers = append(servers, obj{"url": scheme + "://127.0.0.1:9"})
		}
		m := obj{"servers": servers, "passHostHeader": true}
		if u.Health.Enabled {
			hc := obj{"path": orDefault(u.Health.Path, "/"), "interval": secs(u.Health.Interval, 10), "timeout": secs(u.Health.Timeout, 3)}
			if u.Health.ExpectStatus > 0 {
				hc["status"] = u.Health.ExpectStatus
			}
			m["healthCheck"] = hc
		}
		if sticky && len(stickyCookie) > 0 {
			m["sticky"] = stickyCookie
		}
		if transport != "" {
			m["serversTransport"] = transport
		}
		return obj{"loadBalancer": m}
	}
	var primary, backup []spec.Target
	weighted := false
	for _, t := range u.Targets {
		if t.Backup {
			backup = append(backup, t)
		} else {
			primary = append(primary, t)
			if t.Weight > 1 {
				weighted = true
			}
		}
	}
	if len(u.Targets) == 0 {
		r.out.Warn(fmt.Sprintf("upstream %q has no reachable targets", u.Name))
	}
	build := func(base string, targets []spec.Target) {
		if !weighted || len(targets) < 2 {
			r.http.services[base] = lb(targets, true)
			return
		}
		children := make([]obj, 0, len(targets))
		for i, t := range targets {
			child := fmt.Sprintf("%s-t%d", base, i)
			r.http.services[child] = lb([]spec.Target{t}, false)
			w := max(t.Weight, 1)
			children = append(children, obj{"name": child, "weight": w})
		}
		w := obj{"services": children}
		if len(stickyCookie) > 0 {
			w["sticky"] = stickyCookie
		}
		if u.Health.Enabled {
			w["healthCheck"] = obj{}
		}
		r.http.services[base] = obj{"weighted": w}
	}
	if len(backup) > 0 && len(primary) > 0 {
		build(name+"-primary", primary)
		r.http.services[name+"-backup"] = lb(backup, true)
		r.http.services[name] = obj{"failover": obj{"service": name + "-primary", "fallback": name + "-backup"}}
		if !u.Health.Enabled {
			r.out.Warn(fmt.Sprintf("upstream %q: backup targets need a health check to take over in Traefik", u.Name))
		}
	} else {
		build(name, u.Targets)
	}
	return name
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func secs(n, def int) string {
	if n <= 0 {
		n = def
	}
	return strconv.Itoa(n) + "s"
}

func (r *renderer) host(h spec.Host) {
	in := r.st.Instance
	label := "host " + strings.Join(h.Domains, ", ")
	hr := hostRule(h.Domains)
	cert := r.st.Certs[h.CertID]
	tls := cert != nil && in.HTTPSPort > 0
	if h.CertID != 0 && cert == nil {
		r.out.Warn(label + ": the certificate is not issued yet, the host is served over HTTP only")
	}
	prefix := fmt.Sprintf("h%d", h.ID)

	type ep struct {
		name string
		tls  bool
	}
	var eps []ep
	if in.HTTPPort > 0 {
		if tls && h.ForceHTTPS {
			mw := prefix + "-https"
			red := obj{"scheme": "https", "permanent": true}
			if in.HTTPSPort != 443 {
				red["port"] = strconv.Itoa(in.HTTPSPort)
			}
			r.http.middlewares[mw] = obj{"redirectScheme": red}
			r.http.routers[prefix+"-redirect"] = obj{
				"entryPoints": []string{"web"}, "rule": hr, "priority": 1,
				"middlewares": []string{mw}, "service": "noop@internal",
			}
		} else {
			eps = append(eps, ep{"web", false})
		}
	}
	if tls {
		eps = append(eps, ep{"websecure", true})
	}

	if h.RawTraefik != "" {
		var raw obj
		if err := json.Unmarshal([]byte(h.RawTraefik), &raw); err != nil {
			r.out.Warn(label + ": the custom Traefik JSON is invalid and was ignored: " + err.Error())
		} else {
			r.raw = append(r.raw, raw)
		}
	}

	addRouter := func(name string, e ep, rule string, priority int, mws []string, svc string) {
		ro := obj{"entryPoints": []string{e.name}, "rule": rule, "priority": priority, "service": svc}
		if len(mws) > 0 {
			ro["middlewares"] = mws
		}
		if e.tls {
			ro["tls"] = obj{}
		}
		r.http.routers[name+"-"+e.name] = ro
	}

	switch h.Kind {
	case spec.HostRedirect:
		mw := prefix + "-redirect-to"
		repl := h.RedirectURL
		if h.PreservePath {
			repl = strings.TrimRight(repl, "/") + "${1}"
		}
		r.http.middlewares[mw] = obj{"redirectRegex": obj{
			"regex": "^https?://[^/]+(/.*)?$", "replacement": repl,
			"permanent": h.RedirectCode == 0 || h.RedirectCode == 301 || h.RedirectCode == 308,
		}}
		for _, e := range eps {
			addRouter(prefix, e, hr, 1, []string{mw}, "noop@internal")
		}
		return
	case spec.HostError:
		mw := prefix + "-error-page"
		r.http.middlewares[mw] = obj{"replacePath": obj{"path": fmt.Sprintf("/.well-known/sm-proxy/error/%d", h.ID)}}
		svc := r.managerService()
		for _, e := range eps {
			addRouter(prefix, e, hr, 1, []string{mw}, svc)
		}
		return
	}

	// Host-wide middlewares, in order: IP rules, access list, rate limit, body size, headers.
	var common []string
	var allow []string
	hasDeny := false
	for _, ru := range h.IPRules {
		if ru.Action == "deny" {
			hasDeny = true
		} else {
			allow = append(allow, ru.CIDR)
		}
	}
	if hasDeny {
		r.out.Warn(label + ": Traefik has no IP deny rules, only allow rules apply")
	}
	if len(allow) > 0 {
		n := prefix + "-ip"
		r.http.middlewares[n] = obj{"ipAllowList": obj{"sourceRange": allow}}
		common = append(common, n)
	}
	hostACL := r.aclMiddlewares(h.AccessList, label)
	if h.RateLimit.Enabled && h.RateLimit.RPS > 0 {
		n := prefix + "-ratelimit"
		burst := max(h.RateLimit.Burst, 1)
		r.http.middlewares[n] = obj{"rateLimit": obj{"average": h.RateLimit.RPS, "burst": burst, "period": "1s"}}
		common = append(common, n)
	}
	if h.MaxBodyMB > 0 {
		n := prefix + "-body"
		r.http.middlewares[n] = obj{"buffering": obj{"maxRequestBodyBytes": int64(h.MaxBodyMB) << 20}}
		common = append(common, n)
	}
	headers := obj{}
	if len(h.RequestHeaders) > 0 {
		headers["customRequestHeaders"] = headerMap(h.RequestHeaders)
	}
	if len(h.ResponseHeaders) > 0 {
		headers["customResponseHeaders"] = headerMap(h.ResponseHeaders)
	}
	if h.CORS.Enabled {
		origins := h.CORS.Origins
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		headers["accessControlAllowOriginList"] = origins
		headers["accessControlAllowMethods"] = splitList(orDefault(h.CORS.Methods, "GET, POST, PUT, PATCH, DELETE, OPTIONS"))
		headers["accessControlAllowHeaders"] = splitList(orDefault(h.CORS.Headers, "Authorization, Content-Type, apikey, x-client-info"))
		headers["accessControlAllowCredentials"] = h.CORS.Credentials
		if h.CORS.MaxAge > 0 {
			headers["accessControlMaxAge"] = h.CORS.MaxAge
		}
		headers["addVaryHeader"] = true
	}
	if len(headers) > 0 {
		n := prefix + "-headers"
		r.http.middlewares[n] = obj{"headers": headers}
		common = append(common, n)
	}
	var hsts string
	if tls && h.HSTS {
		hsts = prefix + "-hsts"
		r.http.middlewares[hsts] = obj{"headers": obj{"stsSeconds": 31536000, "stsIncludeSubdomains": h.HSTSSubdomains}}
	}

	transport := ""
	if h.ConnectTimeout > 0 || h.ReadTimeout > 0 {
		transport = prefix + "-transport"
		ft := obj{}
		if h.ConnectTimeout > 0 {
			ft["dialTimeout"] = secs(h.ConnectTimeout, 30)
		}
		if h.ReadTimeout > 0 {
			ft["responseHeaderTimeout"] = secs(h.ReadTimeout, 60)
		}
		r.transports[transport] = obj{"forwardingTimeouts": ft}
	}

	upstream := func(id uint) string {
		if id != 0 && r.st.Upstreams[id] != nil {
			return r.service(id, transport)
		}
		if r.st.Upstreams[h.Upstream] != nil {
			return r.service(h.Upstream, transport)
		}
		return ""
	}

	chain := func(e ep, acl []string, extra []string) []string {
		mws := append([]string{}, common[:min(len(common), ipCount(allow))]...)
		mws = append(mws, acl...)
		mws = append(mws, common[min(len(common), ipCount(allow)):]...)
		mws = append(mws, extra...)
		if e.tls && hsts != "" {
			mws = append(mws, hsts)
		}
		return mws
	}

	n := len(h.Routes)
	for i, rt := range h.Routes {
		svc := upstream(rt.Upstream)
		if svc == "" {
			r.out.Warn(fmt.Sprintf("%s: route %s has no upstream and is skipped", label, rt.Path))
			continue
		}
		rp := fmt.Sprintf("%s-r%d", prefix, i)
		var extra []string
		if rt.StripPrefix && (rt.PathType == "" || rt.PathType == spec.PathPrefix) && rt.Path != "/" && rt.Path != "" {
			mw := rp + "-strip"
			r.http.middlewares[mw] = obj{"stripPrefix": obj{"prefixes": []string{strings.TrimRight(rt.Path, "/")}}}
			extra = append(extra, mw)
		}
		if rt.RewriteRegex != "" {
			mw := rp + "-rewrite"
			r.http.middlewares[mw] = obj{"replacePathRegex": obj{"regex": rt.RewriteRegex, "replacement": rt.RewriteReplacement}}
			extra = append(extra, mw)
		}
		if len(rt.RequestHeaders) > 0 || len(rt.ResponseHeaders) > 0 {
			mw := rp + "-headers"
			hd := obj{}
			if len(rt.RequestHeaders) > 0 {
				hd["customRequestHeaders"] = headerMap(rt.RequestHeaders)
			}
			if len(rt.ResponseHeaders) > 0 {
				hd["customResponseHeaders"] = headerMap(rt.ResponseHeaders)
			}
			r.http.middlewares[mw] = obj{"headers": hd}
			extra = append(extra, mw)
		}
		acl := hostACL
		if rt.AccessList != 0 {
			acl = r.aclMiddlewares(rt.AccessList, label)
		}
		for _, e := range eps {
			addRouter(rp, e, routeRule(hr, rt), routeBase+(n-i)*10, chain(e, acl, extra), svc)
		}
	}
	if svc := upstream(0); svc != "" {
		for _, e := range eps {
			addRouter(prefix+"-default", e, hr, 1, chain(e, hostACL, nil), svc)
		}
	} else if len(h.Routes) == 0 {
		r.out.Warn(label + ": no upstream configured")
	}
}

// ipCount keeps the host IP allow list first in the middleware chain.
func ipCount(allow []string) int {
	if len(allow) > 0 {
		return 1
	}
	return 0
}

func splitList(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (r *renderer) stream(s spec.Stream) {
	u := r.st.Upstreams[s.Upstream]
	if u == nil {
		r.out.Warn(fmt.Sprintf("stream on port %d has no upstream and is skipped", s.ListenPort))
		return
	}
	servers := make([]obj, 0, len(u.Targets))
	for _, t := range u.Targets {
		host := t.Host
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		sv := obj{"address": fmt.Sprintf("%s:%d", host, t.Port)}
		if t.Weight > 1 {
			sv["weight"] = t.Weight
		}
		servers = append(servers, sv)
	}
	name := fmt.Sprintf("s%d", s.ID)
	if s.Protocol == "udp" {
		r.udp.routers[name] = obj{"entryPoints": []string{fmt.Sprintf("udp-%d", s.ListenPort)}, "service": name}
		r.udp.services[name] = obj{"loadBalancer": obj{"servers": servers}}
		return
	}
	r.tcp.routers[name] = obj{"entryPoints": []string{fmt.Sprintf("tcp-%d", s.ListenPort)}, "rule": "HostSNI(`*`)", "service": name}
	r.tcp.services[name] = obj{"loadBalancer": obj{"servers": servers}}
}
