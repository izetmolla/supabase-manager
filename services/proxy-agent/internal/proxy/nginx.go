package proxy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/supabase-manager/proxy-agent/internal/supervisor"
)

// reloadSettle is how long the agent watches nginx after a start or reload before declaring
// the new configuration live.
var reloadSettle = 2 * time.Second

// nginx keeps three configuration directories under the root: live (in use), next (being
// validated) and prev (the last working one, restored when a reload fails). nginx resolves
// relative include, certificate and password-file paths against the directory of the main
// config file, so a directory is valid wherever it is validated.
type nginx struct {
	root, logDir string
	proc         *supervisor.Process
	mu           sync.Mutex
}

func newNginx(o Options) *nginx {
	n := &nginx{root: o.ConfigRoot, logDir: o.LogDir}
	n.proc = supervisor.New(syscall.SIGQUIT, "nginx", "-c", n.conf("live"), "-g", "daemon off;")
	return n
}

func (n *nginx) dir(name string) string  { return filepath.Join(n.root, name) }
func (n *nginx) conf(name string) string { return filepath.Join(n.root, name, "nginx.conf") }

func (n *nginx) Kind() string             { return "nginx" }
func (n *nginx) State() supervisor.State  { return n.proc.State() }
func (n *nginx) Changed() <-chan struct{} { return n.proc.Changed() }
func (n *nginx) Checksum() string         { return readChecksum(n.dir("live")) }
func (n *nginx) AccessLogs() []string     { return []string{filepath.Join(n.logDir, "*.log")} }
func (n *nginx) Shutdown()                { n.proc.Stop(15 * time.Second) }
func (n *nginx) Restart() error           { return n.proc.Restart(15 * time.Second) }
func (n *nginx) exists(name string) bool  { _, err := os.Stat(name); return err == nil }
func (n *nginx) validate(ctx context.Context, dir string) (string, error) {
	return commandOutput(ctx, "nginx", "-t", "-c", n.conf(dir))
}

func (n *nginx) Version() string {
	out, _ := commandOutput(context.Background(), "nginx", "-v")
	return strings.TrimPrefix(strings.TrimSpace(out), "nginx version: ")
}

func (n *nginx) Boot() error {
	if err := os.MkdirAll(n.logDir, 0o755); err != nil {
		return err
	}
	if !n.exists(n.conf("live")) {
		return nil
	}
	return n.proc.Start()
}

func (n *nginx) Apply(ctx context.Context, files map[string]File, checksum string, _ int) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := os.MkdirAll(n.logDir, 0o755); err != nil {
		return "", err
	}
	next := n.dir("next")
	if err := os.RemoveAll(next); err != nil {
		return "", err
	}
	if err := writeTree(next, withChecksum(files, checksum)); err != nil {
		return "", fmt.Errorf("write configuration: %w", err)
	}
	out, err := n.validate(ctx, "next")
	if err != nil {
		_ = os.RemoveAll(next)
		return out, fmt.Errorf("nginx rejected the configuration:\n%s", out)
	}

	if err := os.RemoveAll(n.dir("prev")); err != nil {
		return out, err
	}
	hadLive := n.exists(n.dir("live"))
	if hadLive {
		if err := os.Rename(n.dir("live"), n.dir("prev")); err != nil {
			return out, err
		}
	}
	if err := os.Rename(next, n.dir("live")); err != nil {
		n.restore(hadLive)
		return out, err
	}

	since := time.Now()
	if n.proc.State().Running {
		n.proc.Signal(syscall.SIGHUP)
	} else if err := n.proc.Start(); err != nil {
		n.restore(hadLive)
		return out, err
	}
	time.Sleep(reloadSettle)
	problems := n.proc.OutputSince(since, "[emerg]", "[alert]")
	if st := n.proc.State(); !st.Running {
		problems = append(problems, st.LastError)
	}
	if len(problems) > 0 {
		n.restore(hadLive)
		return out, errors.New("nginx could not load the configuration, the previous one was restored:\n" + strings.Join(problems, "\n"))
	}
	return out, nil
}

// restore puts prev back as live and reloads; without a previous configuration nginx stops.
func (n *nginx) restore(hadLive bool) {
	_ = os.RemoveAll(n.dir("live"))
	if !hadLive {
		n.proc.Stop(15 * time.Second)
		return
	}
	if err := os.Rename(n.dir("prev"), n.dir("live")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	if n.proc.State().Running {
		n.proc.Signal(syscall.SIGHUP)
	} else {
		_ = n.proc.Start()
	}
}
