package supabase

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	releasesURL      = "https://github.com/supabase/cli/releases"
	latestCacheTTL   = time.Hour
	autoUpdatePeriod = 6 * time.Hour
)

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?`)

// SettingsStore persists the CLI preferences.
type SettingsStore interface {
	Get(key string, out any) (bool, error)
	Put(key string, v any) error
}

// CLISettings is stored under settings.KeyCLI.
type CLISettings struct {
	// Bin is the binary chosen by the manager after an install; it wins over SUPABASE_BIN.
	Bin        string `json:"bin"`
	AutoUpdate bool   `json:"auto_update"`
}

type InstallState struct {
	Running    bool       `json:"running"`
	Version    string     `json:"version"`
	Phase      string     `json:"phase"`
	Downloaded int64      `json:"downloaded"`
	Total      int64      `json:"total"`
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type CLIInfo struct {
	Installed       bool         `json:"installed"`
	Path            string       `json:"path"`
	Version         string       `json:"version"`
	Source          string       `json:"source"`
	VersionError    string       `json:"version_error,omitempty"`
	Latest          string       `json:"latest"`
	CheckedAt       *time.Time   `json:"checked_at,omitempty"`
	CheckError      string       `json:"check_error,omitempty"`
	UpdateAvailable bool         `json:"update_available"`
	AutoUpdate      bool         `json:"auto_update"`
	ManagedDir      string       `json:"managed_dir"`
	Platform        string       `json:"platform"`
	Install         InstallState `json:"install"`
}

// CLIManager finds, installs and updates the Supabase CLI used by the Runner. Installs go to
// ToolsDir as the official release binaries; a system or Homebrew installation is never touched.
type CLIManager struct {
	runner     *Runner
	configured string
	toolsDir   string
	store      SettingsStore
	key        string
	http       *http.Client

	mu         sync.Mutex
	settings   CLISettings
	latest     string
	checkedAt  time.Time
	checkErr   string
	version    string
	versionOf  string
	install    InstallState
	installing atomic.Bool
}

func NewCLIManager(runner *Runner, configured, toolsDir string, store SettingsStore, key string) *CLIManager {
	m := &CLIManager{
		runner: runner, configured: configured, toolsDir: toolsDir, store: store, key: key,
		http: &http.Client{Timeout: 15 * time.Minute},
	}
	if _, err := store.Get(key, &m.settings); err != nil {
		log.Printf("cli settings: %v", err)
	}
	if bin := m.resolve(); bin != "" {
		runner.SetBin(bin)
	}
	return m
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}

func (m *CLIManager) managedBin() string { return filepath.Join(m.toolsDir, "supabase") }

// resolve picks the binary: the one installed by the manager, then SUPABASE_BIN / PATH, then a
// managed copy left from an earlier install. It returns "" when no CLI is available.
func (m *CLIManager) resolve() string {
	m.mu.Lock()
	saved := m.settings.Bin
	m.mu.Unlock()
	if saved != "" && executable(saved) {
		return saved
	}
	if p, err := exec.LookPath(m.configured); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	if executable(m.managedBin()) {
		return m.managedBin()
	}
	return ""
}

func (m *CLIManager) source(path string) string {
	switch {
	case path == "":
		return ""
	case strings.HasPrefix(path, m.toolsDir+string(filepath.Separator)):
		return "managed"
	case strings.Contains(path, "/Cellar/") || strings.Contains(path, "linuxbrew") || strings.Contains(path, "/homebrew/"):
		return "homebrew"
	}
	return "system"
}

func binaryVersion(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CI=1")
	out, err := cmd.Output()
	if v := versionRe.FindString(string(out)); v != "" {
		return v, nil
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s", firstLine(strings.TrimSpace(string(ee.Stderr))))
		}
		return "", err
	}
	return "", fmt.Errorf("unexpected --version output: %q", firstLine(string(out)))
}

func platformAsset() (string, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return "", fmt.Errorf("automatic install is not supported on %s", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return "", fmt.Errorf("automatic install is not supported on %s", runtime.GOARCH)
	}
	return runtime.GOOS + "_" + runtime.GOARCH, nil
}

// compareVersions compares dotted numeric versions; pre-release suffixes are ignored.
func compareVersions(a, b string) int {
	pa, pb := strings.SplitN(strings.SplitN(a, "-", 2)[0], ".", 3), strings.SplitN(strings.SplitN(b, "-", 2)[0], ".", 3)
	for i := range 3 {
		var x, y int
		if i < len(pa) {
			_, _ = fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			_, _ = fmt.Sscan(pb[i], &y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Info reports the active CLI, cached latest release and install progress.
func (m *CLIManager) Info(ctx context.Context) CLIInfo {
	bin := m.runner.Bin()
	if !executable(bin) {
		if p, err := exec.LookPath(bin); err == nil {
			bin = p
		} else {
			bin = ""
		}
	}
	info := CLIInfo{Path: bin, Source: m.source(bin), ManagedDir: m.toolsDir}
	info.Platform, _ = platformAsset()

	if bin != "" {
		m.mu.Lock()
		cached := m.versionOf == bin && m.version != ""
		v := m.version
		m.mu.Unlock()
		if !cached {
			var err error
			v, err = binaryVersion(ctx, bin)
			if err != nil {
				info.VersionError = err.Error()
			} else {
				m.mu.Lock()
				m.version, m.versionOf = v, bin
				m.mu.Unlock()
			}
		}
		info.Version = v
		info.Installed = v != "" || info.VersionError != ""
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	info.Latest, info.CheckError, info.AutoUpdate, info.Install = m.latest, m.checkErr, m.settings.AutoUpdate, m.install
	if !m.checkedAt.IsZero() {
		t := m.checkedAt
		info.CheckedAt = &t
	}
	info.UpdateAvailable = info.Latest != "" && info.Version != "" && compareVersions(info.Version, info.Latest) < 0
	return info
}

// CheckLatest asks GitHub for the latest release. The /releases/latest redirect is used
// instead of the REST API, which is rate limited for anonymous clients.
func (m *CLIManager) CheckLatest(ctx context.Context, force bool) (string, error) {
	m.mu.Lock()
	if !force && m.latest != "" && time.Since(m.checkedAt) < latestCacheTTL {
		v := m.latest
		m.mu.Unlock()
		return v, nil
	}
	m.mu.Unlock()

	v, err := m.fetchLatest(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkedAt = time.Now()
	if err != nil {
		m.checkErr = err.Error()
		return "", err
	}
	m.latest, m.checkErr = v, ""
	return v, nil
}

func (m *CLIManager) fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach GitHub: %w", err)
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	_, tag, ok := strings.Cut(loc, "/releases/tag/")
	if !ok {
		return "", fmt.Errorf("unexpected response from GitHub (%s)", resp.Status)
	}
	v := versionRe.FindString(tag)
	if v == "" {
		return "", fmt.Errorf("unexpected release tag %q", tag)
	}
	return v, nil
}

func (m *CLIManager) setPhase(phase string) {
	m.mu.Lock()
	m.install.Phase = phase
	m.mu.Unlock()
}

// Install downloads a release (latest when version is empty) in the background.
func (m *CLIManager) Install(version string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version != "" && !versionRe.MatchString(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	if _, err := platformAsset(); err != nil {
		return err
	}
	if !m.installing.CompareAndSwap(false, true) {
		return errors.New("an installation is already running")
	}
	m.mu.Lock()
	m.install = InstallState{Running: true, Version: version, Phase: "starting"}
	m.mu.Unlock()

	go func() {
		defer m.installing.Store(false)
		err := m.doInstall(context.Background(), version)
		now := time.Now()
		m.mu.Lock()
		m.install.Running = false
		m.install.FinishedAt = &now
		if err != nil {
			m.install.Phase = "failed"
			m.install.Error = err.Error()
			log.Printf("supabase cli install failed: %v", err)
		} else {
			m.install.Phase = "done"
			log.Printf("supabase cli %s installed to %s", m.install.Version, m.managedBin())
		}
		m.mu.Unlock()
	}()
	return nil
}

func (m *CLIManager) doInstall(ctx context.Context, version string) error {
	if version == "" {
		m.setPhase("checking latest version")
		v, err := m.CheckLatest(ctx, true)
		if err != nil {
			return err
		}
		version = v
		m.mu.Lock()
		m.install.Version = v
		m.mu.Unlock()
	}
	plat, _ := platformAsset()
	asset := fmt.Sprintf("supabase_%s_%s.tar.gz", version, plat)
	base := fmt.Sprintf("%s/download/v%s/", releasesURL, version)

	m.setPhase("fetching checksums")
	sums, err := m.fetchText(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	want := ""
	for line := range strings.SplitSeq(sums, "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return fmt.Errorf("release v%s has no %s", version, asset)
	}

	if err := os.MkdirAll(m.toolsDir, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(m.toolsDir, ".install-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	m.setPhase("downloading")
	archive := filepath.Join(stage, asset)
	got, err := m.download(ctx, base+asset, archive)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}

	m.setPhase("extracting")
	files, err := extractTarGz(archive, stage)
	if err != nil {
		return err
	}
	if _, ok := files["supabase"]; !ok {
		return fmt.Errorf("%s does not contain a supabase binary", asset)
	}

	m.setPhase("verifying")
	if v, err := binaryVersion(ctx, filepath.Join(stage, "supabase")); err != nil {
		return fmt.Errorf("downloaded binary does not run: %w", err)
	} else if compareVersions(v, version) != 0 {
		return fmt.Errorf("downloaded binary reports version %s, expected %s", v, version)
	}

	m.setPhase("activating")
	// Helper binaries first, so the new supabase never runs next to an old helper.
	for name := range files {
		if name != "supabase" {
			if err := os.Rename(filepath.Join(stage, name), filepath.Join(m.toolsDir, name)); err != nil {
				return err
			}
		}
	}
	if err := os.Rename(filepath.Join(stage, "supabase"), m.managedBin()); err != nil {
		return err
	}

	m.mu.Lock()
	m.settings.Bin = m.managedBin()
	m.version, m.versionOf = version, m.managedBin()
	settings := m.settings
	m.mu.Unlock()
	m.runner.SetBin(m.managedBin())
	return m.store.Put(m.key, settings)
}

func (m *CLIManager) fetchText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

type progressWriter struct {
	m *CLIManager
}

func (w progressWriter) Write(p []byte) (int, error) {
	w.m.mu.Lock()
	w.m.install.Downloaded += int64(len(p))
	w.m.mu.Unlock()
	return len(p), nil
}

// download saves url to path and returns the hex SHA-256 of the content.
func (m *CLIManager) download(ctx context.Context, url, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	m.mu.Lock()
	m.install.Total = resp.ContentLength
	m.install.Downloaded = 0
	m.mu.Unlock()

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h, progressWriter{m}), resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), f.Close()
}

// extractTarGz writes the regular top-level files of an archive into dir.
func extractTarGz(archive, dir string) (map[string]bool, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	files := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, err
		}
		name := filepath.Base(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || name != hdr.Name || strings.HasPrefix(name, ".") {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 1<<30)); err != nil {
			_ = out.Close()
			return nil, err
		}
		if err := out.Close(); err != nil {
			return nil, err
		}
		files[name] = true
	}
}

// UseConfigured drops the manager-installed binary as the preferred one and goes back to
// SUPABASE_BIN / PATH (or the managed copy when nothing else is found).
func (m *CLIManager) UseConfigured() error {
	m.mu.Lock()
	m.settings.Bin = ""
	s := m.settings
	m.mu.Unlock()
	if err := m.store.Put(m.key, s); err != nil {
		return err
	}
	if bin := m.resolve(); bin != "" {
		m.runner.SetBin(bin)
		return nil
	}
	return errors.New("no Supabase CLI found on PATH")
}

func (m *CLIManager) SetAutoUpdate(on bool) error {
	m.mu.Lock()
	m.settings.AutoUpdate = on
	s := m.settings
	m.mu.Unlock()
	return m.store.Put(m.key, s)
}

// Run installs the CLI when it is missing (if autoInstall) and keeps it updated when the
// auto-update setting is on. It blocks until ctx is cancelled.
func (m *CLIManager) Run(ctx context.Context, autoInstall bool) {
	if autoInstall && m.resolve() == "" {
		log.Printf("supabase cli not found (SUPABASE_BIN=%s), installing the latest release to %s", m.configured, m.toolsDir)
		if err := m.Install(""); err != nil {
			log.Printf("supabase cli install: %v", err)
		}
	}
	next := time.NewTimer(time.Minute)
	defer next.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-next.C:
			m.autoUpdate(ctx)
			next.Reset(autoUpdatePeriod)
		}
	}
}

func (m *CLIManager) autoUpdate(ctx context.Context) {
	if _, err := m.CheckLatest(ctx, true); err != nil {
		return
	}
	info := m.Info(ctx)
	if !info.AutoUpdate || !info.UpdateAvailable || m.runner.Busy() {
		return
	}
	log.Printf("supabase cli auto-update %s -> %s", info.Version, info.Latest)
	_ = m.Install(info.Latest)
}
