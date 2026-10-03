package httpapi

import (
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/supabase-manager/manager/config"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/projects"
	proxymanager "github.com/supabase-manager/manager/internal/proxy-manager"
	"github.com/supabase-manager/manager/internal/selfupdate"
	"github.com/supabase-manager/manager/internal/supabase"
	"github.com/supabase-manager/version"
	"gorm.io/gorm"
)

type Server struct {
	cfg      *config.Config
	db       *gorm.DB
	auth     *auth.Service
	projects *projects.Service
	cli      *supabase.CLIManager
	updater  *selfupdate.Updater
	pm       *proxymanager.Service
	setupMu  sync.Mutex

	upMu    sync.Mutex
	upCache map[string]upstreamSet
}

func New(cfg *config.Config, db *gorm.DB, authSvc *auth.Service, ps *projects.Service, cli *supabase.CLIManager, up *selfupdate.Updater, pm *proxymanager.Service) *Server {
	return &Server{cfg: cfg, db: db, auth: authSvc, projects: ps, cli: cli, updater: up, pm: pm, upCache: map[string]upstreamSet{}}
}

func (s *Server) App(ui fs.FS) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "Supabase Manager",
		ErrorHandler: errorHandler,
		BodyLimit:    4 * 1024 * 1024,
		ReadTimeout:  30 * time.Second,
		IdleTimeout:  2 * time.Minute,
	})
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{Next: func(c fiber.Ctx) bool {
		return !strings.HasPrefix(c.Path(), "/api") || !isManagerPath(c.Path())
	}}))
	if s.cfg.DevCORSOrigin != "" {
		app.Use(cors.New(cors.Config{AllowOrigins: []string{s.cfg.DevCORSOrigin}, AllowCredentials: true}))
	}

	configureProxy()
	s.registerProxyManagerPublic(app)
	app.All("/proxy/:slug/:service", s.serviceProxy)
	app.All("/proxy/:slug/:service/*", s.serviceProxy)
	app.Use(s.studioRoute)

	api := app.Group("/api")
	api.Get("/health", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"ok": true}) })
	api.Get("/version", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"version": version.Version, "commit": version.CommitSHA})
	})

	a := api.Group("/auth")
	a.Get("/setup", s.setupStatus)
	a.Post("/setup", s.setup)
	a.Post("/login", s.login)
	a.Post("/refresh", s.refresh)
	a.Post("/logout", s.logout)
	a.Get("/me", s.requireAuth, s.me)

	r := api.Group("", s.requireAuth)
	r.Get("/system", s.systemInfo)
	sys := r.Group("/system")
	sys.Get("/cli", s.cliInfo)
	sys.Post("/cli/check", s.requireAdmin, s.cliCheck)
	sys.Post("/cli/install", s.requireAdmin, s.cliInstall)
	sys.Post("/cli/use-system", s.requireAdmin, s.cliUseSystem)
	sys.Put("/cli/settings", s.requireAdmin, s.cliSettings)
	sys.Get("/update", s.requireAdmin, s.updateInfo)
	sys.Post("/update/check", s.requireAdmin, s.updateCheck)
	sys.Post("/update/apply", s.requireAdmin, s.updateApply)
	sys.Get("/network-defaults", s.getNetworkDefaults)
	sys.Put("/network-defaults", s.requireAdmin, s.putNetworkDefaults)
	sys.Get("/docker-networks", s.listDockerNetworks)
	sys.Post("/free-subnet", s.freeSubnet)
	sys.Get("/docker-info", s.dockerInfo)
	sys.Get("/storage-defaults", s.getStorageDefaults)
	sys.Put("/storage-defaults", s.requireAdmin, s.putStorageDefaults)
	sys.Get("/mounts", s.requireAdmin, s.mounts)
	sys.Get("/smtp", s.requireAdmin, s.getSMTPDefaults)
	sys.Put("/smtp", s.requireAdmin, s.putSMTPDefaults)
	sys.Post("/smtp/test", s.requireAdmin, s.testSMTPDefaults)
	sys.Get("/proxy-manager", s.getProxyManagerSettings)
	sys.Put("/proxy-manager", s.requireAdmin, s.putProxyManagerSettings)

	admin := r.Group("/users", s.requireAdmin)
	admin.Get("/", s.listUsers)
	admin.Post("/", s.createUser)
	admin.Patch("/:id", s.updateUser)
	admin.Delete("/:id", s.deleteUser)
	r.Get("/audit", s.requireAdmin, s.listAudit)

	r.Get("/projects", s.listProjects)
	r.Get("/projects/ports-preview", s.previewPorts)
	r.Post("/projects", s.requireAdmin, s.createProject)
	r.Post("/projects/import", s.requireAdmin, s.importProject)

	p := r.Group("/projects/:slug", s.loadProject)
	p.Get("/", s.getProject)
	p.Patch("/", s.updateProject)
	p.Delete("/", s.requireAdmin, s.deleteProject)

	p.Post("/start", s.startProject)
	p.Post("/stop", s.stopProject)
	p.Post("/restart", s.restartProject)
	p.Get("/status", s.projectStatus)
	p.Get("/jobs", s.listJobs)

	p.Get("/config", s.getConfig)
	p.Put("/config/ports", s.updatePorts)
	p.Put("/config/services", s.updateServices)
	p.Put("/config/auth", s.updateAuth)
	p.Get("/network", s.getProjectNetwork)
	p.Put("/network", s.requireAdmin, s.putProjectNetwork)
	p.Get("/domains", s.getProjectDomains)
	p.Put("/domains", s.requireAdmin, s.putProjectDomains)
	p.Get("/storage", s.getProjectStorage)
	p.Put("/storage", s.requireAdmin, s.putProjectStorage)
	p.Get("/config/raw", s.getRawConfig)
	p.Put("/config/raw", s.requireAdmin, s.putRawConfig)
	p.Get("/smtp", s.getProjectSMTP)
	p.Put("/smtp", s.requireAdmin, s.putProjectSMTP)
	p.Post("/smtp/test", s.requireAdmin, s.testProjectSMTP)
	p.Get("/auth/providers", s.listProviders)
	p.Get("/auth/providers/:provider", s.getProvider)
	p.Put("/auth/providers/:provider", s.putProvider)

	p.Get("/secrets", s.listSecrets)
	p.Put("/secrets", s.putSecret)
	p.Delete("/secrets/:key", s.deleteSecret)

	p.Get("/migrations", s.listMigrations)
	p.Post("/migrations", s.newMigration)
	p.Get("/migrations/:file", s.getMigration)
	p.Put("/migrations/:file", s.putMigration)
	p.Post("/db/up", s.dbUp)
	p.Post("/db/reset", s.requireAdmin, s.dbReset)
	p.Post("/db/diff", s.dbDiff)
	p.Get("/types", s.genTypes)

	p.Get("/functions", s.listFunctions)
	p.Post("/functions", s.newFunction)

	p.Get("/containers", s.listContainers)
	p.Get("/stats", s.projectStats)
	p.Post("/containers/:name/restart", s.restartContainer)
	p.Get("/containers/:name/logs", s.containerLogs)

	s.registerProxyManager(r)

	r.Get("/jobs/:id", s.getJob)
	r.Get("/jobs/:id/stream", s.streamJob)

	api.Use(func(c fiber.Ctx) error {
		return fiber.NewError(fiber.StatusNotFound, "not found")
	})

	if ui != nil {
		app.Use("/", static.New("", static.Config{FS: ui, Compress: true}))
		index, _ := fs.ReadFile(ui, "index.html")
		app.Use(func(c fiber.Ctx) error {
			if c.Method() != fiber.MethodGet {
				return fiber.ErrNotFound
			}
			c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
			c.Set(fiber.HeaderCacheControl, "no-cache")
			return c.Send(index)
		})
	}
	return app
}
