// Package proxy drives nginx or Traefik inside the proxy container: it keeps the proxy process
// running and applies new configurations without losing the last working one.
package proxy

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/supabase-manager/proxy-agent/internal/supervisor"
)

// Proxy is one supported proxy server.
type Proxy interface {
	Kind() string
	Version() string
	// Checksum identifies the live configuration; empty when there is none.
	Checksum() string
	// Boot starts the proxy with the live configuration, if there is one.
	Boot() error
	// Apply validates files, switches to them and reloads the proxy. On failure the previous
	// configuration stays (or is put back) and the error explains why.
	Apply(ctx context.Context, files map[string]File, checksum string, adminPort int) (string, error)
	Restart() error
	State() supervisor.State
	Changed() <-chan struct{}
	// AccessLogs returns the access log files to follow (glob patterns allowed).
	AccessLogs() []string
	Shutdown()
}

// Options are the container paths of a proxy.
type Options struct {
	ConfigRoot string
	LogDir     string
}

// New returns the proxy for kind.
func New(kind string, opts Options) (Proxy, error) {
	switch kind {
	case "nginx":
		return newNginx(opts), nil
	case "traefik":
		return newTraefik(opts), nil
	}
	return nil, fmt.Errorf("unknown proxy kind %q (want nginx or traefik)", kind)
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
