package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/supabase-manager/manager/config"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/db"
	"github.com/supabase-manager/manager/internal/docker"
	httpapi "github.com/supabase-manager/manager/internal/http"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/projects"
	proxymanager "github.com/supabase-manager/manager/internal/proxy-manager"
	"github.com/supabase-manager/manager/internal/selfupdate"
	"github.com/supabase-manager/manager/internal/settings"
	"github.com/supabase-manager/manager/internal/supabase"
	"github.com/supabase-manager/manager/web"
	"github.com/supabase-manager/version"
)

func main() {
	if runCLI(os.Args[1:]) {
		return
	}
	cfg := config.Load()

	gdb, err := db.Open(cfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	// Jobs cannot survive a restart; mark leftovers as failed.
	now := time.Now()
	gdb.Model(&models.Job{}).Where("status = ?", models.JobRunning).
		Updates(map[string]any{"status": models.JobFailed, "exit_code": -1, "finished_at": now})

	cipher, err := auth.NewCipher(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("cipher: %v", err)
	}
	runner := supabase.NewRunner(cfg.SupabaseBin, gdb)
	dc := docker.New(cfg.DockerSocket)
	st := settings.New(gdb)
	cli := supabase.NewCLIManager(runner, cfg.SupabaseBin, cfg.ToolsDir, st, settings.KeyCLI)
	ps := projects.NewService(cfg, gdb, runner, dc, st, cipher)
	up := selfupdate.New(dc, cfg.UpdateRepository, version.Version, version.CommitSHA, cfg.DockerSocket, cfg.SelfContainer)
	managerURL := cfg.ProxyManagerURL
	if managerURL == "" {
		managerURL = proxymanager.ManagerURLFromAddr(cfg.Addr)
	}
	var bootstrap *proxymanager.Bootstrap
	if cfg.ProxyManagerEnable {
		bootstrap = &proxymanager.Bootstrap{Kind: cfg.ProxyManagerKind, HTTPPort: cfg.ProxyHTTPPort, HTTPSPort: cfg.ProxyHTTPSPort}
	}
	pm := proxymanager.New(gdb, dc, cipher, ps, runner, st, proxymanager.Options{
		ManagerURL: managerURL, ALPNAddr: cfg.ACMETLSALPNAddr, ManagerAddr: cfg.Addr,
		AgentAddr: cfg.ProxyAgentAddr, ImageRepository: cfg.ProxyImageRepository,
		ImageTag:  proxymanager.ImageTag(cfg.ProxyImageTag, version.Version),
		Bootstrap: bootstrap,
	})
	srv := httpapi.New(cfg, gdb, auth.NewService(gdb, cfg.JWTSecret), ps, cli, up, pm)

	bg, stopBG := context.WithCancel(context.Background())
	defer stopBG()
	go cli.Run(bg, cfg.CLIAutoInstall)
	go pm.Run(bg)

	ui, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatalf("embedded ui: %v", err)
	}
	app := srv.App(ui)

	go func() {
		log.Printf("Supabase Manager %s (%s) listening on http://%s (db=%s, projects=%s)",
			version.Version, version.CommitSHA, cfg.Addr, cfg.DBDriver, cfg.ProjectsRoot)
		if err := app.Listen(cfg.Addr); err != nil {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = app.ShutdownWithContext(ctx)
}
