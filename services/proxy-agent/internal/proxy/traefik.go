package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/supabase-manager/proxy-agent/internal/supervisor"
)

var (
	// traefikReady bounds the wait for Traefik's API after a (re)start.
	traefikReady = 30 * time.Second
	// traefikWatch is how long the file provider needs to notice changed dynamic files.
	traefikWatch = 3 * time.Second
)

// traefik reads its static configuration (traefik.yml) at start and watches the dynamic
// directory, so only static changes need a restart. It cannot validate a configuration up
// front; instead the agent compares the errors its API reports before and after a change.
type traefik struct {
	root, logDir string
	proc         *supervisor.Process
	mu           sync.Mutex
	client       *http.Client
}

func newTraefik(o Options) *traefik {
	t := &traefik{root: o.ConfigRoot, logDir: o.LogDir, client: &http.Client{Timeout: 5 * time.Second}}
	t.proc = supervisor.New(syscall.SIGTERM, "traefik", "--configFile="+t.static())
	return t
}

func (t *traefik) static() string { return filepath.Join(t.root, "traefik.yml") }

func (t *traefik) Kind() string             { return "traefik" }
func (t *traefik) State() supervisor.State  { return t.proc.State() }
func (t *traefik) Changed() <-chan struct{} { return t.proc.Changed() }
func (t *traefik) Checksum() string         { return readChecksum(t.root) }
func (t *traefik) AccessLogs() []string     { return []string{filepath.Join(t.logDir, "access.log")} }
func (t *traefik) Shutdown()                { t.proc.Stop(15 * time.Second) }
func (t *traefik) Restart() error           { return t.proc.Restart(15 * time.Second) }

func (t *traefik) Version() string {
	out, _ := commandOutput(context.Background(), "traefik", "version")
	for l := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "Version" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (t *traefik) Boot() error {
	if err := os.MkdirAll(t.logDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(t.static()); err != nil {
		return nil
	}
	return t.proc.Start()
}

func (t *traefik) Apply(ctx context.Context, files map[string]File, checksum string, adminPort int) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if adminPort <= 0 {
		return "", errors.New("the Traefik admin port is not set")
	}
	if _, ok := files["traefik.yml"]; !ok {
		return "", errors.New("traefik.yml is missing from the configuration")
	}
	if err := os.MkdirAll(t.logDir, 0o755); err != nil {
		return "", err
	}
	prev, err := readTree(t.root)
	if err != nil {
		return "", err
	}
	running := t.proc.State().Running
	staticChanged := !running || !bytes.Equal(prev["traefik.yml"].Content, files["traefik.yml"].Content)
	var before []string
	if running && !staticChanged {
		before, _ = t.errors(ctx, adminPort)
	}

	if err := replaceTree(t.root, withChecksum(files, checksum)); err != nil {
		t.restore(prev, staticChanged)
		return "", fmt.Errorf("write configuration: %w", err)
	}
	var log []string
	if staticChanged {
		log = append(log, "Entrypoints or providers changed, restarting Traefik")
		if err := t.proc.Restart(15 * time.Second); err != nil {
			t.restore(prev, true)
			return strings.Join(log, "\n"), err
		}
	}
	if err := t.waitAPI(ctx, adminPort); err != nil {
		t.restore(prev, staticChanged)
		return strings.Join(log, "\n"), err
	}
	select {
	case <-ctx.Done():
		t.restore(prev, staticChanged)
		return strings.Join(log, "\n"), ctx.Err()
	case <-time.After(traefikWatch):
	}
	after, err := t.errors(ctx, adminPort)
	if err != nil {
		t.restore(prev, staticChanged)
		return strings.Join(log, "\n"), err
	}
	var fresh []string
	for _, e := range after {
		if !slices.Contains(before, e) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) > 0 {
		t.restore(prev, staticChanged)
		return strings.Join(log, "\n"), errors.New("Traefik reported errors, the previous configuration was restored:\n" + strings.Join(fresh, "\n"))
	}
	return strings.Join(log, "\n"), nil
}

func (t *traefik) restore(prev map[string]File, restart bool) {
	if err := replaceTree(t.root, prev); err != nil {
		return
	}
	if _, ok := prev["traefik.yml"]; !ok {
		t.proc.Stop(15 * time.Second)
		return
	}
	if restart {
		_ = t.proc.Restart(15 * time.Second)
	}
}

func (t *traefik) waitAPI(ctx context.Context, port int) error {
	deadline := time.Now().Add(traefikReady)
	for {
		if _, err := t.rawdata(ctx, port); err == nil {
			return nil
		}
		if st := t.proc.State(); !st.Running && st.LastError != "" {
			return fmt.Errorf("traefik is not running: %s", st.LastError)
		}
		if time.Now().After(deadline) {
			return errors.New("the Traefik API did not answer in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (t *traefik) rawdata(ctx context.Context, port int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/rawdata", port), nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("traefik API: %s", resp.Status)
	}
	var b bytes.Buffer
	_, err = b.ReadFrom(resp.Body)
	return b.Bytes(), err
}

// errors lists the router, service and middleware errors of the file provider.
func (t *traefik) errors(ctx context.Context, port int) ([]string, error) {
	body, err := t.rawdata(ctx, port)
	if err != nil {
		return nil, err
	}
	return parseRawdataErrors(body)
}

func parseRawdataErrors(body []byte) ([]string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var errs []string
	for section, msg := range raw {
		var items map[string]struct {
			Error []string `json:"error"`
		}
		if json.Unmarshal(msg, &items) != nil {
			continue
		}
		for name, it := range items {
			if !strings.HasSuffix(name, "@file") {
				continue
			}
			for _, e := range it.Error {
				errs = append(errs, fmt.Sprintf("%s %s: %s", strings.TrimSuffix(section, "s"), name, e))
			}
		}
	}
	slices.Sort(errs)
	return errs, nil
}
