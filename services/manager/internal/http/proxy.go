package httpapi

import (
	"bufio"
	"bytes"
	"errors"
	"html"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/proxy"
	"github.com/supabase-manager/manager/internal/projects"
	"github.com/valyala/fasthttp"
)

// Every project's services are reachable on the manager's own port:
//
//	/proxy/<slug>/studio/...  entry point, redirects to /project/<slug>/...
//	/project/<slug>/...       Studio itself (plus its root-level assets and /api calls)
//	/proxy/<slug>/mail/...    Mailpit
//	/proxy/<slug>/api/...     the project API gateway (Kong)
//
// Studio is a Next.js build without a base path, so it cannot live under a prefix: its pages,
// chunks and API calls are absolute (/_next/..., /api/platform/...). Studio accepts any project
// ref in its URLs, so the manager slug is used as the ref and requests are attributed to a
// project by their path, their Referer, or the sticky sm_studio cookie, in that order.

const (
	proxyPrefix = "/proxy/"
	upstreamTTL = 10 * time.Second
)

var configureProxyOnce sync.Once

// configureProxy sets the process-wide policy of the Fiber proxy middleware. Private addresses
// must be allowed because every upstream is a local port; upstream URLs are only ever built
// from a project's network settings and config.toml ports, never from request data.
func configureProxy() {
	configureProxyOnce.Do(func() {
		policy := proxy.DefaultSecurityPolicy()
		policy.AllowPrivateIPs = true
		proxy.WithSecurityPolicy(policy)
		proxy.WithClient(&fasthttp.Client{
			NoDefaultUserAgentHeader: true,
			DisablePathNormalizing:   true,
			StreamResponseBody:       true,
			ReadTimeout:              10 * time.Minute,
			WriteTimeout:             time.Minute,
			// Studio (Node) closes idle keep-alive connections after 5s.
			MaxIdleConnDuration: 3 * time.Second,
			MaxConnsPerHost:     1024,
			RetryIfErr:          retryProxyRequest,
		})
	})
}

// retryProxyRequest also retries non-idempotent requests (Studio runs SQL via POST) when a
// pooled connection turned out to be closed before the upstream read anything.
func retryProxyRequest(req *fasthttp.Request, attempts int, err error) (bool, bool) {
	if attempts > 2 {
		return false, false
	}
	if errors.Is(err, fasthttp.ErrConnectionClosed) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return false, true
	}
	return false, req.Header.IsGet() || req.Header.IsHead()
}

type upstreamSet struct {
	found   bool
	host    string
	studio  int
	mail    int
	api     int
	expires time.Time
}

func (u upstreamSet) port(service string) int {
	switch service {
	case "studio":
		return u.studio
	case "mail":
		return u.mail
	case "api":
		return u.api
	}
	return 0
}

// upstreams resolves a project's service ports. Studio loads ~100 assets per page, so lookups
// (including misses) are cached briefly instead of reading the database and config.toml each time.
func (s *Server) upstreams(slug string) (upstreamSet, bool) {
	if slug == "" || len(slug) > 40 {
		return upstreamSet{}, false
	}
	now := time.Now()
	s.upMu.Lock()
	u, ok := s.upCache[slug]
	s.upMu.Unlock()
	if ok && now.Before(u.expires) {
		return u, u.found
	}

	u = upstreamSet{expires: now.Add(upstreamTTL)}
	if p, err := s.projects.Get(slug); err == nil {
		if f, err := s.projects.LoadConfig(p); err == nil {
			pt := f.Settings().Ports
			u = upstreamSet{
				found: true, host: projects.ConnectHost(p),
				studio: pt.Studio, mail: pt.SMTP, api: pt.API, expires: u.expires,
			}
		}
	}
	s.upMu.Lock()
	if len(s.upCache) > 1000 {
		clear(s.upCache)
	}
	s.upCache[slug] = u
	s.upMu.Unlock()
	return u, u.found
}

func (s *Server) forgetUpstreams(slug string) {
	s.upMu.Lock()
	delete(s.upCache, slug)
	s.upMu.Unlock()
}

// ---- request classification ----

func isDocument(c fiber.Ctx) bool {
	if d := c.Get("Sec-Fetch-Dest"); d != "" {
		return d == "document" || d == "iframe"
	}
	return c.Method() == fiber.MethodGet && strings.Contains(c.Get(fiber.HeaderAccept), "text/html")
}

func isWebSocket(c fiber.Ctx) bool {
	return strings.EqualFold(c.Get(fiber.HeaderUpgrade), "websocket") &&
		strings.Contains(strings.ToLower(c.Get(fiber.HeaderConnection)), "upgrade")
}

// sameOrigin rejects cross-site writes and WebSocket handshakes, which would otherwise ride on
// the session cookie (other ports on the same host count as same-site for SameSite=Lax).
func (s *Server) sameOrigin(c fiber.Ctx) bool {
	safe := c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead || c.Method() == fiber.MethodOptions
	if safe && !isWebSocket(c) {
		return true
	}
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		return !isWebSocket(c)
	}
	if s.cfg.DevCORSOrigin != "" && origin == s.cfg.DevCORSOrigin {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == c.Host()
}

// managerPaths are first path segments owned by the manager UI and API.
var managerPaths = map[string]bool{
	"": true, "login": true, "setup": true, "projects": true, "settings": true,
	"assets": true, "favicon.svg": true, "index.html": true, "proxy": true,
}

var managerAPIPaths = map[string]bool{
	"health": true, "version": true, "auth": true, "system": true, "users": true, "audit": true, "jobs": true, "projects": true,
}

func firstSegment(p string) (seg, rest string) {
	seg, rest, _ = strings.Cut(strings.TrimPrefix(p, "/"), "/")
	return seg, rest
}

func isManagerPath(p string) bool {
	seg, rest := firstSegment(p)
	if seg != "api" {
		return managerPaths[seg]
	}
	seg, rest = firstSegment(rest)
	if seg == "projects" {
		// Studio probes HEAD /api/projects/<ref>/api/rest; the manager has no /api/projects/*/api routes.
		parts := strings.SplitN(rest, "/", 3)
		return len(parts) < 2 || parts[1] != "api"
	}
	return managerAPIPaths[seg]
}

// studioRef extracts <ref> from a Studio page path (/project/<ref>/...).
func studioRef(p string) string {
	seg, rest := firstSegment(p)
	if seg != "project" {
		return ""
	}
	ref, _ := firstSegment(rest)
	return ref
}

// studioTarget attributes a root-level Studio request to a managed project.
func (s *Server) studioTarget(c fiber.Ctx) (string, upstreamSet, bool) {
	if slug := studioRef(c.Path()); slug != "" {
		if up, ok := s.upstreams(slug); ok {
			return slug, up, true
		}
	}
	if ref := c.Get(fiber.HeaderReferer); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host == c.Host() {
			if slug := studioRef(u.Path); slug != "" {
				if up, ok := s.upstreams(slug); ok {
					return slug, up, true
				}
			}
		}
	}
	if slug := c.Cookies(studioCookie); slug != "" {
		if up, ok := s.upstreams(slug); ok {
			return slug, up, true
		}
	}
	return "", upstreamSet{}, false
}

// ---- auth ----

// proxySession authenticates a proxied request with the manager session cookie. An expired
// access token is renewed from the refresh cookie without rotating it: Studio fires many
// parallel requests and only one of them could win a rotation. It returns false when the
// request has already been answered (redirect to login, or an error).
func (s *Server) proxySession(c fiber.Ctx) (bool, error) {
	if claims, err := s.auth.ParseAccess(c.Cookies(accessCookie)); err == nil {
		c.Locals(localsClaims, claims)
		return true, nil
	}
	if raw := c.Cookies(refreshCookie); raw != "" {
		if u, err := s.auth.Peek(raw); err == nil {
			if access, err := s.auth.IssueAccess(u); err == nil {
				s.setAccessCookie(c, access)
				return true, nil
			}
		}
	}
	if isDocument(c) {
		return false, c.Redirect().To("/login?next=" + url.QueryEscape(c.OriginalURL()))
	}
	return false, fiber.NewError(fiber.StatusUnauthorized, "not authenticated")
}

func (s *Server) guard(c fiber.Ctx) (bool, error) {
	ok, err := s.proxySession(c)
	if !ok {
		return false, err
	}
	if !s.sameOrigin(c) {
		return false, fiber.NewError(fiber.StatusForbidden, "cross-origin request blocked")
	}
	return true, nil
}

// ---- forwarding ----

func stripManagerCookies(h *fasthttp.RequestHeader) {
	h.DelCookie(accessCookie)
	h.DelCookie(refreshCookie)
	h.DelCookie(studioCookie)
}

// forward sends the current request to host:port with uri (path and query) and leaves the
// upstream response in c.
func (s *Server) forward(c fiber.Ctx, slug, service, host string, port int, uri string) error {
	if port <= 0 {
		return fiber.NewError(fiber.StatusNotFound, service+" has no port configured")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	h := &c.Request().Header
	stripManagerCookies(h)
	if isWebSocket(c) {
		return proxyWebSocket(c, addr, uri)
	}
	h.Set(fiber.HeaderXForwardedHost, c.Host())
	h.Set(fiber.HeaderXForwardedProto, c.Scheme())
	if err := proxy.Do(c, "http://"+addr+uri); err != nil {
		return s.badGateway(c, slug, service, err)
	}
	return nil
}

func (s *Server) badGateway(c fiber.Ctx, slug, service string, err error) error {
	msg := service + " of project " + slug + " is not reachable. Is the project running?"
	if !isDocument(c) {
		return fiber.NewError(fiber.StatusBadGateway, msg)
	}
	c.Response().Reset()
	c.Status(fiber.StatusBadGateway)
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.SendString(`<!doctype html><meta charset="utf-8"><title>Service unavailable</title>` +
		`<body style="font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem">` +
		`<h1 style="font-size:1.25rem">` + html.EscapeString(msg) + `</h1>` +
		`<p style="color:#666">` + html.EscapeString(err.Error()) + `</p>` +
		`<p><a href="/projects/` + url.PathEscape(slug) + `">Open the project in Supabase Manager</a></p>`)
}

// rewriteLocation maps redirects that point at the upstream back onto the public URL space.
func rewriteLocation(c fiber.Ctx, host string, port int, mapPath func(string) string) {
	h := &c.Response().Header
	loc := string(h.Peek(fiber.HeaderLocation))
	if loc == "" {
		return
	}
	for _, origin := range []string{"http://" + host + ":", "http://127.0.0.1:", "http://localhost:"} {
		if rest, ok := strings.CutPrefix(loc, origin+strconv.Itoa(port)); ok {
			loc = rest
			if loc == "" {
				loc = "/"
			}
		}
	}
	if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "//") {
		h.Set(fiber.HeaderLocation, mapPath(loc))
	}
}

// proxyWebSocket tunnels an upgrade request to addr. Fiber's proxy speaks plain HTTP only, so
// the connection is hijacked and bytes are copied in both directions.
func proxyWebSocket(c fiber.Ctx, addr, uri string) error {
	ctx := c.RequestCtx()
	req := fasthttp.AcquireRequest()
	ctx.Request.CopyTo(req)
	req.SetRequestURI(uri)
	req.Header.SetHost(addr)
	req.Header.Del(fiber.HeaderOrigin)

	up, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fasthttp.ReleaseRequest(req)
		return fiber.NewError(fiber.StatusBadGateway, "upstream not reachable")
	}
	ctx.HijackSetNoResponse(true)
	ctx.Hijack(func(client net.Conn) {
		defer fasthttp.ReleaseRequest(req)
		defer func() { _ = up.Close() }()
		_ = client.SetDeadline(time.Time{})
		w := bufio.NewWriter(up)
		if req.Write(w) != nil || w.Flush() != nil {
			return
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(up, client)
			if tc, ok := up.(*net.TCPConn); ok {
				_ = tc.CloseWrite()
			}
			close(done)
		}()
		_, _ = io.Copy(client, up)
		_ = client.Close()
		<-done
	})
	return nil
}

// ---- handlers ----

// serviceProxy handles /proxy/<slug>/<service>/...
func (s *Server) serviceProxy(c fiber.Ctx) error {
	slug, service := c.Params("slug"), c.Params("service")
	up, ok := s.upstreams(slug)
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "project not found")
	}
	if up.port(service) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "unknown service "+strconv.Quote(service))
	}
	if ok, err := s.guard(c); !ok {
		return err
	}

	prefix := proxyPrefix + slug + "/" + service
	rest := strings.TrimPrefix(string(c.Request().RequestURI()), prefix)

	if service == "studio" {
		target := "/project/" + slug
		if p, _, _ := strings.Cut(rest, "?"); strings.Trim(p, "/") != "" {
			target += "/" + strings.TrimPrefix(rest, "/")
		}
		return c.Redirect().Status(fiber.StatusFound).To(target)
	}

	if rest == "" || rest[0] == '?' {
		// Relative asset URLs only resolve under a trailing slash.
		return c.Redirect().Status(fiber.StatusFound).To(prefix + "/" + rest)
	}
	if service == "mail" {
		// Mailpit's HTML is rewritten below, so ask for an uncompressed body.
		c.Request().Header.Del(fiber.HeaderAcceptEncoding)
	}
	if err := s.forward(c, slug, service, up.host, up.port(service), rest); err != nil || isWebSocket(c) {
		return err
	}
	rewriteLocation(c, up.host, up.port(service), func(p string) string { return prefix + p })
	if service == "mail" {
		rewriteMailpitHTML(c, prefix)
	}
	return nil
}

// rewriteMailpitHTML points Mailpit's absolute asset URLs and its web root (used by its UI for
// API and WebSocket URLs) at the proxy prefix.
func rewriteMailpitHTML(c fiber.Ctx, prefix string) {
	resp := c.Response()
	if !bytes.HasPrefix(resp.Header.ContentType(), []byte("text/html")) {
		return
	}
	body := resp.Body()
	body = bytes.ReplaceAll(body, []byte(`data-webroot="/"`), []byte(`data-webroot="`+prefix+`/"`))
	body = bytes.ReplaceAll(body, []byte(`href="/`), []byte(`href="`+prefix+`/`))
	body = bytes.ReplaceAll(body, []byte(`src="/`), []byte(`src="`+prefix+`/`))
	resp.SetBody(body)
}

// studioRefEndpoints return project objects whose "ref" Studio uses to build links; they are
// rewritten from the CLI's fixed "default" to the manager slug.
func isStudioRefEndpoint(p string) bool {
	if p == "/api/platform/profile" || p == "/api/platform/projects" {
		return true
	}
	rest, ok := strings.CutPrefix(p, "/api/platform/projects/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// studioRoute serves Studio requests that arrive at the root of the manager.
func (s *Server) studioRoute(c fiber.Ctx) error {
	if isManagerPath(c.Path()) {
		return c.Next()
	}
	slug, up, ok := s.studioTarget(c)
	if !ok {
		return c.Next()
	}
	if ok, err := s.guard(c); !ok {
		return err
	}

	doc := isDocument(c)
	if ref := studioRef(c.Path()); doc && ref != "" && ref != slug {
		// Studio links to its built-in "default" project; keep the slug in the address bar.
		uri := string(c.Request().RequestURI())
		return c.Redirect().Status(fiber.StatusFound).To("/project/" + slug + strings.TrimPrefix(uri, "/project/"+ref))
	}

	rewriteRefs := c.Method() == fiber.MethodGet && isStudioRefEndpoint(c.Path())
	if rewriteRefs {
		c.Request().Header.Del(fiber.HeaderAcceptEncoding)
	}
	if err := s.forward(c, slug, "studio", up.host, up.studio, string(c.Request().RequestURI())); err != nil || isWebSocket(c) {
		return err
	}

	rewriteLocation(c, up.host, up.studio, func(p string) string {
		if rest, ok := strings.CutPrefix(p, "/project/default"); ok && (rest == "" || rest[0] == '/' || rest[0] == '?') {
			return "/project/" + slug + rest
		}
		return p
	})
	resp := c.Response()
	if rewriteRefs && resp.StatusCode() == fiber.StatusOK {
		resp.SetBody(bytes.ReplaceAll(resp.Body(), []byte(`"ref":"default"`), []byte(`"ref":"`+slug+`"`)))
	}
	if doc && resp.StatusCode() < 400 {
		c.Cookie(&fiber.Cookie{
			Name: studioCookie, Value: slug, Path: "/", HTTPOnly: true,
			Secure: s.cfg.SecureCookies, SameSite: "Lax",
		})
	}
	return nil
}
