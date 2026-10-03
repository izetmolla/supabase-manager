package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	proxymanager "github.com/supabase-manager/manager/internal/proxy-manager"
	"github.com/supabase-manager/manager/internal/proxy-manager/acme"
	"github.com/supabase-manager/manager/internal/supabase"
)

// pmError maps proxy manager errors to HTTP errors.
func pmError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, proxymanager.ErrDisabled):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	case errors.Is(err, proxymanager.ErrValidation):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, proxymanager.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "not found")
	case errors.Is(err, supabase.ErrBusy):
		return fiber.NewError(fiber.StatusConflict, "another proxy manager job is running, try again when it finishes")
	}
	return err
}

func (s *Server) registerProxyManagerPublic(app *fiber.App) {
	// Proxies forward ACME HTTP-01 requests and Traefik error pages to the manager.
	enabled := func(c fiber.Ctx) error {
		if !s.pm.Enabled() {
			return c.SendStatus(fiber.StatusNotFound)
		}
		return c.Next()
	}
	app.Get("/.well-known/acme-challenge/:token", enabled, func(c fiber.Ctx) error {
		ka, ok := s.pm.ACME.HTTP.KeyAuth(c.Params("token"))
		if !ok {
			return c.SendStatus(fiber.StatusNotFound)
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlain)
		return c.SendString(ka)
	})
	app.Get("/.well-known/sm-proxy/error/:id", enabled, func(c fiber.Ctx) error {
		code, body, err := s.pm.ErrorPage(fiber.Params[uint](c, "id"))
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.Status(code).SendString(body)
	})
}

func (s *Server) registerProxyManager(r fiber.Router) {
	g := r.Group("/proxy-manager", s.requireAdmin, func(c fiber.Ctx) error {
		if !s.pm.Enabled() {
			return pmError(proxymanager.ErrDisabled)
		}
		return c.Next()
	})
	g.Get("/meta", s.pmMeta)
	g.Get("/images", s.pmImages)
	g.Get("/updates", s.pmUpdates)
	g.Get("/status", s.pmStatus)
	g.Get("/health", func(c fiber.Ctx) error { return c.JSON(s.pm.Health()) })
	g.Post("/deploy", s.pmDeployAll)

	g.Get("/instances", s.pmListInstances)
	g.Post("/instances", s.pmCreateInstance)
	g.Get("/instances/:id", s.pmGetInstance)
	g.Put("/instances/:id", s.pmUpdateInstance)
	g.Delete("/instances/:id", s.pmDeleteInstance)
	g.Post("/instances/:id/start", s.pmInstanceAction)
	g.Post("/instances/:id/stop", s.pmInstanceAction)
	g.Post("/instances/:id/restart", s.pmInstanceAction)
	g.Post("/instances/:id/deploy", s.pmDeploy)
	g.Post("/instances/:id/update", s.pmUpdateImage)
	g.Get("/instances/:id/preview", s.pmPreview)
	g.Get("/instances/:id/revisions", s.pmRevisions)
	g.Get("/instances/:id/revisions/:rev", s.pmRevisionFiles)
	g.Post("/instances/:id/revisions/:rev/rollback", s.pmRollback)
	g.Get("/instances/:id/logs", s.pmContainerLogs)
	g.Get("/instances/:id/access-log", s.pmAccessLog)

	g.Get("/hosts", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListHosts())) })
	g.Get("/hosts/:id", func(c fiber.Ctx) error {
		h, err := s.pm.GetHost(fiber.Params[uint](c, "id"))
		if err != nil {
			return pmError(err)
		}
		return c.JSON(h)
	})
	g.Post("/hosts", s.pmSaveHost)
	g.Put("/hosts/:id", s.pmSaveHost)
	g.Delete("/hosts/:id", s.pmDelete("proxy.host.delete", s.pm.DeleteHost))

	g.Get("/upstreams", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListUpstreams())) })
	g.Post("/upstreams", s.pmSaveUpstream)
	g.Put("/upstreams/:id", s.pmSaveUpstream)
	g.Delete("/upstreams/:id", s.pmDelete("proxy.upstream.delete", s.pm.DeleteUpstream))
	g.Get("/project-services", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ProjectServices())) })

	g.Get("/streams", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListStreams())) })
	g.Post("/streams", s.pmSaveStream)
	g.Put("/streams/:id", s.pmSaveStream)
	g.Delete("/streams/:id", s.pmDelete("proxy.stream.delete", s.pm.DeleteStream))

	g.Get("/access-lists", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListAccessLists())) })
	g.Post("/access-lists", s.pmSaveAccessList)
	g.Put("/access-lists/:id", s.pmSaveAccessList)
	g.Delete("/access-lists/:id", s.pmDelete("proxy.access_list.delete", s.pm.DeleteAccessList))

	g.Get("/certificates", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListCertificates())) })
	g.Post("/certificates/acme", s.pmCreateACMECert)
	g.Post("/certificates/upload", s.pmUploadCert)
	g.Put("/certificates/:id/upload", s.pmUploadCert)
	g.Put("/certificates/:id", s.pmUpdateCert)
	g.Post("/certificates/:id/issue", s.pmIssueCert)
	g.Get("/certificates/:id/pem", s.pmCertPEM)
	g.Delete("/certificates/:id", s.pmDelete("proxy.certificate.delete", s.pm.DeleteCertificate))

	g.Get("/dns-providers", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListDNSProviders())) })
	g.Post("/dns-providers", s.pmSaveDNSProvider)
	g.Put("/dns-providers/:id", s.pmSaveDNSProvider)
	g.Delete("/dns-providers/:id", s.pmDelete("proxy.dns_provider.delete", s.pm.DeleteDNSProvider))

	g.Get("/acme-accounts", func(c fiber.Ctx) error { return listJSON(c, list(s.pm.ListAcmeAccounts())) })
	g.Post("/acme-accounts", s.pmSaveAcmeAccount)
	g.Put("/acme-accounts/:id", s.pmSaveAcmeAccount)
	g.Post("/acme-accounts/:id/register", s.pmRegisterAcmeAccount)
	g.Delete("/acme-accounts/:id", s.pmDelete("proxy.acme_account.delete", s.pm.DeleteAcmeAccount))
}

type listResult struct {
	v   any
	err error
}

// list wraps a list result, using [] instead of null for empty lists.
func list[T any](l []T, err error) listResult {
	if l == nil {
		l = []T{}
	}
	return listResult{l, err}
}

func listJSON(c fiber.Ctx, r listResult) error {
	if r.err != nil {
		return pmError(r.err)
	}
	return c.JSON(r.v)
}

func (s *Server) pmDelete(action string, fn func(uint) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		id := fiber.Params[uint](c, "id")
		if err := fn(id); err != nil {
			return pmError(err)
		}
		s.audit(c, action, fmt.Sprint(id), nil)
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (s *Server) pmMeta(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"directories":    acme.Directories,
		"dns_providers":  acme.Catalog(),
		"nginx_image":    s.pm.DefaultImage("nginx"),
		"traefik_image":  s.pm.DefaultImage("traefik"),
		"manager_url":    s.pm.Options().ManagerURL,
		"alpn_addr":      s.pm.Options().ALPNAddr,
		"challenge_path": "/.well-known/acme-challenge/",
	})
}

// pmImages lists the proxy image tags (Docker Hub and local) for ?kind=nginx|traefik;
// ?refresh=1 bypasses the cache.
func (s *Server) pmImages(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	out, err := s.pm.ListImageTags(ctx, c.Query("kind"), c.Query("refresh") == "1")
	if err != nil {
		return pmError(err)
	}
	return c.JSON(out)
}

// pmUpdates reports newer proxy images per instance; ?refresh=1 bypasses the Docker Hub cache.
func (s *Server) pmUpdates(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	return listJSON(c, list(s.pm.CheckUpdates(ctx, c.Query("refresh") == "1")))
}

func (s *Server) pmUpdateImage(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	job, err := s.pm.UpdateImageJob(id, currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.instance.update_image", fmt.Sprint(id), nil)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID})
}

// getProjectDomains lists the proxy host domains that can replace a project's local URLs.
func (s *Server) getProjectDomains(c fiber.Ctx) error {
	if !s.pm.Enabled() {
		return c.JSON(fiber.Map{"enabled": false})
	}
	d, err := s.pm.GetProjectDomains(project(c).Slug)
	if err != nil {
		return pmError(err)
	}
	return c.JSON(fiber.Map{"enabled": true, "available": d.Available, "sites": d.Sites, "selected": d.Selected})
}

func (s *Server) putProjectDomains(c fiber.Ctx) error {
	if !s.pm.Enabled() {
		return pmError(proxymanager.ErrDisabled)
	}
	var in struct {
		Selected map[string]string `json:"selected"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p := project(c)
	changed, err := s.pm.SetProjectDomains(p, in.Selected)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "project.domains", p.Slug, fiber.Map{"selected": in.Selected})
	return c.JSON(fiber.Map{"config_changed": changed})
}

func (s *Server) pmStatus(c fiber.Ctx) error {
	st, err := s.pm.Status(c.Context())
	if err != nil {
		return pmError(err)
	}
	return c.JSON(st)
}

func (s *Server) pmDeployAll(c fiber.Ctx) error {
	job, err := s.pm.DeployAllJob(currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.deploy_all", "", nil)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID})
}

func (s *Server) pmListInstances(c fiber.Ctx) error {
	return listJSON(c, list(s.pm.ListInstances(c.Context())))
}

func (s *Server) pmGetInstance(c fiber.Ctx) error {
	list, err := s.pm.ListInstances(c.Context())
	if err != nil {
		return pmError(err)
	}
	id := fiber.Params[uint](c, "id")
	for _, v := range list {
		if v.ID == id {
			return c.JSON(v)
		}
	}
	return fiber.NewError(fiber.StatusNotFound, "not found")
}

func (s *Server) pmCreateInstance(c fiber.Ctx) error {
	var in proxymanager.ProxyInstance
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.pm.CreateInstance(&in); err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.instance.create", fmt.Sprint(in.ID), fiber.Map{"name": in.Name, "kind": in.Kind})
	return c.Status(fiber.StatusCreated).JSON(in)
}

func (s *Server) pmUpdateInstance(c fiber.Ctx) error {
	var in proxymanager.ProxyInstance
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	id := fiber.Params[uint](c, "id")
	out, err := s.pm.UpdateInstance(id, &in)
	if err != nil {
		return pmError(err)
	}
	if !out.Enabled {
		_ = s.pm.Stop(c.Context(), id)
	}
	s.audit(c, "proxy.instance.update", fmt.Sprint(id), fiber.Map{"name": out.Name})
	return c.JSON(out)
}

func (s *Server) pmDeleteInstance(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	if err := s.pm.DeleteInstance(c.Context(), id); err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.instance.delete", fmt.Sprint(id), nil)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) pmInstanceAction(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	path := c.Path()
	action := path[strings.LastIndex(path, "/")+1:]
	ctx, cancel := context.WithTimeout(c.Context(), time.Minute)
	defer cancel()
	var err error
	switch action {
	case "start":
		err = s.pm.Start(ctx, id)
	case "stop":
		err = s.pm.Stop(ctx, id)
	default:
		err = s.pm.Restart(ctx, id)
	}
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.instance."+action, fmt.Sprint(id), nil)
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) pmDeploy(c fiber.Ctx) error {
	var body struct {
		Note string `json:"note"`
	}
	_ = c.Bind().JSON(&body)
	id := fiber.Params[uint](c, "id")
	job, err := s.pm.DeployJob(id, currentClaims(c).UserID, strings.TrimSpace(body.Note))
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.deploy", fmt.Sprint(id), fiber.Map{"note": body.Note})
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID})
}

func (s *Server) pmPreview(c fiber.Ctx) error {
	p, err := s.pm.Preview(fiber.Params[uint](c, "id"))
	if err != nil {
		return pmError(err)
	}
	return c.JSON(p)
}

func (s *Server) pmRevisions(c fiber.Ctx) error {
	return listJSON(c, list(s.pm.ListRevisions(fiber.Params[uint](c, "id"))))
}

func (s *Server) pmRevisionFiles(c fiber.Ctx) error {
	files, err := s.pm.RevisionFiles(fiber.Params[uint](c, "id"), fiber.Params[uint](c, "rev"))
	if err != nil {
		return pmError(err)
	}
	return c.JSON(files)
}

func (s *Server) pmRollback(c fiber.Ctx) error {
	id, rev := fiber.Params[uint](c, "id"), fiber.Params[uint](c, "rev")
	job, err := s.pm.RollbackJob(id, rev, currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.rollback", fmt.Sprint(id), fiber.Map{"revision_id": rev})
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID})
}

func (s *Server) pmContainerLogs(c fiber.Ctx) error {
	name, err := s.pm.ContainerName(fiber.Params[uint](c, "id"))
	if err != nil {
		return pmError(err)
	}
	tail := fiber.Query[int](c, "tail", 300)
	if tail < 1 || tail > 5000 {
		tail = 300
	}
	dc := s.projects.Docker
	sseHeaders(c)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		streamLines(w, func(ctx context.Context, emit func(any) error) error {
			return dc.Logs(ctx, name, tail, true, func(stream, line string) error {
				return emit(fiber.Map{"stream": stream, "line": line})
			})
		})
	})
}

func (s *Server) pmAccessLog(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	if _, err := s.pm.GetInstance(id); err != nil {
		return pmError(err)
	}
	hostID := fiber.Query[uint](c, "host", 0)
	tail := fiber.Query[int](c, "tail", 200)
	sseHeaders(c)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		streamLines(w, func(ctx context.Context, emit func(any) error) error {
			return s.pm.StreamAccessLog(ctx, id, hostID, tail, func(e proxymanager.AccessEntry) error {
				return emit(e)
			})
		})
	})
}

// streamLines runs src in the background and writes its items as SSE events with keep-alive
// pings until the source ends or the client leaves.
func streamLines(w *bufio.Writer, src func(ctx context.Context, emit func(any) error) error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := make(chan any, 512)
	errc := make(chan error, 1)
	go func() {
		defer close(items)
		errc <- src(ctx, func(v any) error {
			select {
			case items <- v:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case v, ok := <-items:
			if !ok {
				if err := <-errc; err != nil && !errors.Is(err, context.Canceled) {
					_ = writeEvent(w, "error", err.Error())
				}
				_ = writeEvent(w, "end", "")
				return
			}
			if writeEvent(w, "", v) != nil {
				return
			}
		case <-ping.C:
			if writePing(w) != nil {
				return
			}
		}
	}
}

func (s *Server) pmSaveHost(c fiber.Ctx) error {
	var h proxymanager.ProxyHost
	if err := bindJSON(c, &h); err != nil {
		return err
	}
	h.ID = fiber.Params[uint](c, "id", 0)
	res, err := s.pm.SaveHost(&h, currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.host.save", fmt.Sprint(h.ID), fiber.Map{"domains": h.Domains})
	return c.JSON(res)
}

func (s *Server) pmSaveUpstream(c fiber.Ctx) error {
	var u proxymanager.ProxyUpstream
	if err := bindJSON(c, &u); err != nil {
		return err
	}
	u.ID = fiber.Params[uint](c, "id", 0)
	if err := s.pm.SaveUpstream(&u); err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.upstream.save", fmt.Sprint(u.ID), fiber.Map{"name": u.Name})
	return c.JSON(u)
}

func (s *Server) pmSaveStream(c fiber.Ctx) error {
	var st proxymanager.ProxyStream
	if err := bindJSON(c, &st); err != nil {
		return err
	}
	st.ID = fiber.Params[uint](c, "id", 0)
	if err := s.pm.SaveStream(&st); err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.stream.save", fmt.Sprint(st.ID), fiber.Map{"port": st.ListenPort, "protocol": st.Protocol})
	return c.JSON(st)
}

func (s *Server) pmSaveAccessList(c fiber.Ctx) error {
	var in proxymanager.AccessListInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	al, err := s.pm.SaveAccessList(fiber.Params[uint](c, "id", 0), in)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.access_list.save", fmt.Sprint(al.ID), fiber.Map{"name": al.Name})
	return c.JSON(al)
}

func (s *Server) pmCreateACMECert(c fiber.Ctx) error {
	var in proxymanager.ACMECertificateInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cert, job, err := s.pm.CreateACMECertificate(in, currentClaims(c).UserID)
	if err != nil && cert == nil {
		return pmError(err)
	}
	s.audit(c, "proxy.certificate.create", fmt.Sprint(cert.ID), fiber.Map{"domains": cert.Domains, "challenge": cert.Challenge})
	out := fiber.Map{"certificate": cert}
	if job != nil {
		out["job_id"] = job.ID
	} else if err != nil {
		out["warning"] = pmError(err).Error()
	}
	return c.Status(fiber.StatusCreated).JSON(out)
}

func (s *Server) pmUploadCert(c fiber.Ctx) error {
	var in proxymanager.CustomCertificateInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cert, err := s.pm.UploadCertificate(fiber.Params[uint](c, "id", 0), in, currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.certificate.upload", fmt.Sprint(cert.ID), fiber.Map{"domains": cert.Domains})
	return c.JSON(cert)
}

func (s *Server) pmUpdateCert(c fiber.Ctx) error {
	var in proxymanager.CertificateUpdate
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cert, err := s.pm.UpdateCertificate(fiber.Params[uint](c, "id"), in)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.certificate.update", fmt.Sprint(cert.ID), nil)
	return c.JSON(cert)
}

func (s *Server) pmIssueCert(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	job, err := s.pm.IssueCertificate(id, currentClaims(c).UserID)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.certificate.issue", fmt.Sprint(id), nil)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"job_id": job.ID})
}

func (s *Server) pmCertPEM(c fiber.Ctx) error {
	pem, err := s.pm.CertificatePEM(fiber.Params[uint](c, "id"))
	if err != nil {
		return pmError(err)
	}
	c.Set(fiber.HeaderContentType, "application/x-pem-file")
	return c.SendString(pem)
}

func (s *Server) pmSaveDNSProvider(c fiber.Ctx) error {
	var in proxymanager.DNSProviderInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := s.pm.SaveDNSProvider(fiber.Params[uint](c, "id", 0), in)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.dns_provider.save", fmt.Sprint(p.ID), fiber.Map{"code": p.Code})
	return c.JSON(p)
}

func (s *Server) pmSaveAcmeAccount(c fiber.Ctx) error {
	var in proxymanager.AcmeAccountInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	a, err := s.pm.SaveAcmeAccount(fiber.Params[uint](c, "id", 0), in)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.acme_account.save", fmt.Sprint(a.ID), fiber.Map{"email": a.Email, "directory": a.DirectoryURL})
	return c.JSON(a)
}

func (s *Server) pmRegisterAcmeAccount(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	a, err := s.pm.RegisterAcmeAccount(id)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy.acme_account.register", fmt.Sprint(id), nil)
	return c.JSON(a)
}

func (s *Server) getProxyManagerSettings(c fiber.Ctx) error {
	return c.JSON(s.pm.Settings())
}

func (s *Server) putProxyManagerSettings(c fiber.Ctx) error {
	var in proxymanager.Settings
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Minute)
	defer cancel()
	out, err := s.pm.SetEnabled(ctx, in.Enabled)
	if err != nil {
		return pmError(err)
	}
	s.audit(c, "proxy_manager.settings", "system", out)
	return c.JSON(out)
}
