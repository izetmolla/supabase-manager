// Command sm-proxy-agent runs inside a Supabase Manager proxy container. It starts nginx or
// Traefik, connects to the manager over gRPC and applies the configuration the manager sends.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/supabase-manager/proxy-agent/internal/agent"
	"github.com/supabase-manager/proxy-agent/internal/logs"
	"github.com/supabase-manager/proxy-agent/internal/proxy"
	"github.com/supabase-manager/version"
)

const (
	trimEvery = 10 * time.Minute
	trimAbove = 50 << 20
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("sm-proxy-agent: ")
	if len(os.Args) > 1 && os.Args[1] == "version" {
		_, _ = os.Stdout.WriteString(version.Version + "\n")
		return
	}

	kind := env("SM_PROXY_KIND", "nginx")
	defRoot, defLogs := "/etc/nginx/sm", "/var/log/nginx/sm"
	if kind == "traefik" {
		defRoot, defLogs = "/etc/traefik/sm", "/var/log/traefik"
	}
	p, err := proxy.New(kind, proxy.Options{ConfigRoot: env("SM_CONFIG_ROOT", defRoot), LogDir: env("SM_LOG_DIR", defLogs)})
	if err != nil {
		log.Fatal(err)
	}
	id, err := strconv.ParseUint(os.Getenv("SM_INSTANCE_ID"), 10, 64)
	if err != nil || id == 0 {
		log.Fatal("SM_INSTANCE_ID must be set to the proxy instance id")
	}
	addr := env("SM_MANAGER_ADDR", "127.0.0.1:7070")
	token := os.Getenv("SM_TOKEN")
	if token == "" {
		log.Fatal("SM_TOKEN must be set")
	}
	useTLS := !isLoopback(addr)
	if v := os.Getenv("SM_MANAGER_TLS"); v != "" {
		useTLS, _ = strconv.ParseBool(v)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	log.Printf("%s %s, proxy %s %s, instance %d", version.Version, version.CommitSHA, kind, p.Version(), id)
	if err := p.Boot(); err != nil {
		log.Printf("starting %s with the stored configuration: %v", kind, err)
	}
	go func() {
		t := time.NewTicker(trimEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				logs.Trim(p.AccessLogs(), trimAbove)
			}
		}
	}()

	agent.New(agent.Config{
		ManagerAddr: addr,
		InstanceID:  id,
		Token:       token,
		TLS:         useTLS,
		CertSHA256:  os.Getenv("SM_MANAGER_CERT_SHA256"),
		Version:     version.Version,
	}, p).Run(ctx)

	log.Printf("shutting down %s", kind)
	p.Shutdown()
}
