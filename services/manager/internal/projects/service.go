package projects

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/supabase-manager/manager/config"
	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/configtoml"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/ports"
	"github.com/supabase-manager/manager/internal/settings"
	"github.com/supabase-manager/manager/internal/supabase"
	"gorm.io/gorm"
)

var (
	slugRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)
	secretKeyRe   = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)
	ErrNotFound   = errors.New("project not found")
	ErrValidation = errors.New("validation error")
)

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }
func (e *ValidationError) Unwrap() error { return ErrValidation }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

const (
	StartTimeout   = 20 * time.Minute
	DefaultTimeout = 10 * time.Minute
)

type Service struct {
	cfg      *config.Config
	db       *gorm.DB
	Runner   *supabase.Runner
	Docker   *docker.Client
	Settings *settings.Store
	alloc    *ports.Allocator
	cipher   *auth.Cipher

	studioMounting sync.Map // project id -> mount of the functions folder in progress
}

func NewService(cfg *config.Config, db *gorm.DB, runner *supabase.Runner, dc *docker.Client, st *settings.Store, cipher *auth.Cipher) *Service {
	return &Service{
		cfg: cfg, db: db, Runner: runner, Docker: dc, Settings: st, cipher: cipher,
		alloc: ports.NewAllocator(cfg.PortRangeStart, cfg.PortBlock, cfg.InspectorStart),
	}
}

// Run executes a short CLI command for a project.
func (s *Service) Run(ctx context.Context, p *models.Project, env []string, args ...string) ([]byte, error) {
	return s.Runner.Run(ctx, p.Path, env, append(args, cliArgs(p)...)...)
}

// Status returns `supabase status` details of a project.
func (s *Service) Status(ctx context.Context, p *models.Project) supabase.Status {
	env, _ := s.Env(p)
	return s.Runner.Status(ctx, p.Path, env, cliArgs(p)...)
}

func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	return out
}

func ValidSlug(s string) bool { return slugRe.MatchString(s) }

// within reports whether path is inside root.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func (s *Service) List() ([]models.Project, error) {
	var ps []models.Project
	err := s.db.Order("created_at").Find(&ps).Error
	return ps, err
}

func (s *Service) Get(slug string) (*models.Project, error) {
	var p models.Project
	if err := s.db.Where("slug = ?", slug).First(&p).Error; err != nil {
		return nil, ErrNotFound
	}
	return &p, nil
}

func ConfigPath(p *models.Project) string {
	return filepath.Join(p.Path, "supabase", "config.toml")
}

func (s *Service) LoadConfig(p *models.Project) (*configtoml.File, error) {
	return configtoml.Load(ConfigPath(p))
}

func (s *Service) usedPorts() (map[int]bool, map[int]bool) {
	var ps []models.Project
	s.db.Select("port_base", "inspector_port").Find(&ps)
	bases, insp := map[int]bool{}, map[int]bool{}
	for _, p := range ps {
		bases[p.PortBase] = true
		insp[p.InspectorPort] = true
	}
	return bases, insp
}

// PreviewPorts returns the ports a new project would get.
func (s *Service) PreviewPorts() (ports.Ports, error) {
	bases, insp := s.usedPorts()
	base, inspector, err := s.alloc.Next(bases, insp)
	if err != nil {
		return ports.Ports{}, err
	}
	return ports.ForBase(base, inspector), nil
}

type CreateInput struct {
	Name     string              `json:"name"`
	Slug     string              `json:"slug"`
	Services configtoml.Services `json:"services"`
	SiteURL  string              `json:"site_url"`
	Start    bool                `json:"start"`
}

func (s *Service) Create(ctx context.Context, in CreateInput, userID uint) (*models.Project, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 255 {
		return nil, invalid("project name is required")
	}
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	if !ValidSlug(in.Slug) {
		return nil, invalid("slug must be 3-40 characters of lowercase letters, numbers and dashes")
	}
	var n int64
	s.db.Model(&models.Project{}).Where("slug = ? OR supabase_project_id = ?", in.Slug, in.Slug).Count(&n)
	if n > 0 {
		return nil, invalid("a project with slug %q already exists", in.Slug)
	}

	if err := os.MkdirAll(s.cfg.ProjectsRoot, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(s.cfg.ProjectsRoot, in.Slug)
	if !within(s.cfg.ProjectsRoot, path) {
		return nil, invalid("invalid project path")
	}
	if _, err := os.Stat(path); err == nil {
		return nil, invalid("directory %s already exists", path)
	}

	bases, insp := s.usedPorts()
	base, inspector, err := s.alloc.Next(bases, insp)
	if err != nil {
		return nil, err
	}
	network, err := s.networkForNewProject(ctx)
	if err != nil {
		return nil, err
	}

	if err := os.Mkdir(path, 0o755); err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(path) }

	initCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if _, err := s.Runner.Run(initCtx, path, nil, "init"); err != nil {
		cleanup()
		return nil, fmt.Errorf("supabase init failed: %w", err)
	}

	cfgFile, err := configtoml.Load(filepath.Join(path, "supabase", "config.toml"))
	if err != nil {
		cleanup()
		return nil, err
	}
	p := ports.ForBase(base, inspector)
	steps := []func() error{
		func() error { return cfgFile.SetProjectID(in.Slug) },
		func() error { return cfgFile.SetPorts(p) },
		func() error { return cfgFile.SetServices(in.Services) },
	}
	if in.SiteURL != "" {
		steps = append(steps, func() error { return cfgFile.Set("auth", "site_url", in.SiteURL) })
	}
	for _, step := range steps {
		if err := step(); err != nil {
			cleanup()
			return nil, err
		}
	}
	if err := cfgFile.Save(); err != nil {
		cleanup()
		return nil, err
	}
	_ = os.Remove(cfgFile.Path + ".bak")

	proj := &models.Project{
		Slug: in.Slug, Name: in.Name, Path: path, SupabaseProjectID: in.Slug,
		PortBase: base, InspectorPort: inspector, Status: models.StatusStopped, CreatedByID: userID,
		Network: network, Storage: s.storageForNewProject(),
	}
	if err := s.db.Create(proj).Error; err != nil {
		cleanup()
		return nil, err
	}
	return proj, nil
}

type ImportInput struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Service) Import(ctx context.Context, in ImportInput, userID uint) (*models.Project, error) {
	path, err := filepath.Abs(strings.TrimSpace(in.Path))
	if err != nil || in.Path == "" {
		return nil, invalid("a valid project directory is required")
	}
	if filepath.Base(path) == "supabase" {
		if _, err := os.Stat(filepath.Join(path, "config.toml")); err == nil {
			path = filepath.Dir(path)
		}
	}
	cfgFile, err := configtoml.Load(filepath.Join(path, "supabase", "config.toml"))
	if err != nil {
		return nil, invalid("no readable supabase/config.toml in %s", path)
	}
	settings := cfgFile.Settings()
	projectID := settings.ProjectID
	if projectID == "" {
		projectID = filepath.Base(path)
	}
	if in.Slug == "" {
		in.Slug = Slugify(projectID)
	}
	if !ValidSlug(in.Slug) {
		return nil, invalid("slug must be 3-40 characters of lowercase letters, numbers and dashes")
	}
	if in.Name == "" {
		in.Name = projectID
	}

	var n int64
	s.db.Model(&models.Project{}).Where("slug = ? OR path = ? OR supabase_project_id = ?", in.Slug, path, projectID).Count(&n)
	if n > 0 {
		return nil, invalid("this project (or its slug/project_id) is already registered")
	}

	base := settings.Ports.API - ports.OffsetAPI
	bases, insp := s.usedPorts()
	if bases[base] {
		return nil, invalid("port block %d is already used by another managed project; change its ports first", base)
	}
	if insp[settings.Ports.Inspector] {
		return nil, invalid("inspector port %d is already used by another managed project", settings.Ports.Inspector)
	}

	proj := &models.Project{
		Slug: in.Slug, Name: in.Name, Path: path, SupabaseProjectID: projectID,
		PortBase: base, InspectorPort: settings.Ports.Inspector, Status: models.StatusUnknown,
		Imported: true, CreatedByID: userID, Network: models.NetworkConfig{Mode: models.NetworkAuto},
		Storage: models.StorageConfig{Mode: models.StorageDocker},
	}
	if err := s.db.Create(proj).Error; err != nil {
		return nil, err
	}
	s.RefreshStatus(ctx, proj)
	return proj, nil
}

// UpdatePorts rewrites the project's ports in config.toml and the database.
func (s *Service) UpdatePorts(p *models.Project, np ports.Ports) error {
	base := np.API - ports.OffsetAPI
	var n int64
	s.db.Model(&models.Project{}).Where("id <> ? AND (port_base = ? OR inspector_port = ?)", p.ID, base, np.Inspector).Count(&n)
	if n > 0 {
		return invalid("these ports overlap with another managed project")
	}
	f, err := s.LoadConfig(p)
	if err != nil {
		return err
	}
	if err := f.SetPorts(np); err != nil {
		return invalid("%s", err.Error())
	}
	if err := f.Save(); err != nil {
		return err
	}
	p.PortBase = base
	p.InspectorPort = np.Inspector
	return s.db.Model(p).Updates(map[string]any{"port_base": base, "inspector_port": np.Inspector}).Error
}

func (s *Service) Delete(ctx context.Context, p *models.Project, removeFiles bool) error {
	if s.Runner.RunningJob(p.ID) != 0 {
		return supabase.ErrBusy
	}
	env, _ := s.Env(p)
	if cts, err := s.Docker.ListProject(ctx, p.SupabaseProjectID); err == nil && len(cts) > 0 {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if _, err := s.Runner.Run(stopCtx, p.Path, env, "stop", "--no-backup"); err != nil {
			return fmt.Errorf("could not stop project: %w", err)
		}
	}
	s.removeManagedNetwork(ctx, p)
	s.removeProjectVolumes(ctx, p)
	if removeFiles {
		if p.Imported || !within(s.cfg.ProjectsRoot, p.Path) {
			return invalid("files of imported projects are never deleted; uncheck 'delete files'")
		}
		if err := os.RemoveAll(p.Path); err != nil {
			return err
		}
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", p.ID).Delete(&models.ProjectSecret{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", p.ID).Delete(&models.Job{}).Error; err != nil {
			return err
		}
		return tx.Delete(p).Error
	})
}

// RefreshStatus derives the project status from its containers.
func (s *Service) RefreshStatus(ctx context.Context, p *models.Project) string {
	if s.Runner.RunningJob(p.ID) != 0 && (p.Status == models.StatusStarting || p.Status == models.StatusStopping) {
		return p.Status
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status := models.StatusStopped
	cts, err := s.Docker.ListProject(ctx, p.SupabaseProjectID)
	switch {
	case err != nil:
		status = models.StatusUnknown
	case len(cts) > 0:
		running := 0
		for _, c := range cts {
			if c.State == "running" {
				running++
			}
		}
		switch {
		case running == len(cts):
			status = models.StatusRunning
		case running > 0:
			status = models.StatusError
		}
	}
	if status != p.Status {
		p.Status = status
		s.db.Model(p).Update("status", status)
	}
	return status
}

func (s *Service) setStatus(p *models.Project, status string) {
	p.Status = status
	s.db.Model(&models.Project{}).Where("id = ?", p.ID).Update("status", status)
}

// StartJob runs a long CLI command for a project in the background.
func (s *Service) StartJob(p *models.Project, userID uint, args ...string) (*models.Job, error) {
	env, err := s.Env(p)
	if err != nil {
		return nil, err
	}
	timeout := DefaultTimeout
	var transitional string
	if len(args) > 0 {
		switch args[0] {
		case "start":
			timeout, transitional = StartTimeout, models.StatusStarting
		case "stop":
			transitional = models.StatusStopping
		}
	}
	var hooks supabase.Hooks
	switch {
	case len(args) > 0 && args[0] == "start":
		hooks = s.startHooks(p)
		args = append(args, cliArgs(p)...)
	case len(args) > 0 && args[0] == "stop":
		// stop finds containers by label; without --network-id it also removes a network the
		// CLI created earlier, which matters after switching away from automatic mode.
	default:
		args = append(args, cliArgs(p)...)
	}
	proj := *p
	job, err := s.Runner.StartHooks(p.ID, userID, p.Path, env, timeout, hooks, func(*models.Job) {
		s.RefreshStatus(context.Background(), &proj)
	}, args...)
	if err != nil {
		return nil, err
	}
	if transitional != "" {
		s.setStatus(p, transitional)
		proj.Status = transitional
	}
	return job, nil
}

// Restart chains stop and start in a single background job.
func (s *Service) Restart(p *models.Project, userID uint) (*models.Job, error) {
	env, err := s.Env(p)
	if err != nil {
		return nil, err
	}
	proj := *p
	job, err := s.Runner.Start(p.ID, userID, p.Path, env, DefaultTimeout, func(job *models.Job) {
		if job.Status != models.JobSucceeded {
			s.RefreshStatus(context.Background(), &proj)
			return
		}
		next, err := s.Runner.StartHooks(proj.ID, userID, proj.Path, env, StartTimeout, s.startHooks(&proj), func(*models.Job) {
			s.RefreshStatus(context.Background(), &proj)
		}, append([]string{"start"}, cliArgs(&proj)...)...)
		if err == nil && next != nil {
			s.setStatus(&proj, models.StatusStarting)
		}
	}, "stop")
	if err != nil {
		return nil, err
	}
	s.setStatus(p, models.StatusStopping)
	proj.Status = models.StatusStopping
	return job, nil
}

// startHooks prepares network and storage before `supabase start`. A new database in custom
// storage is started by the CLI as if restored from a backup, which skips migrations and seed
// data, so they are applied afterwards with an in-place reset of the (empty) database.
func (s *Service) startHooks(p *models.Project) supabase.Hooks {
	proj := *p
	fresh := false
	return supabase.Hooks{
		Prepare: func(log func(string)) error {
			if err := os.MkdirAll(functionsDir(&proj), 0o755); err != nil {
				return err
			}
			if err := s.EnsureNetwork(context.Background(), &proj, log); err != nil {
				return err
			}
			f, err := s.EnsureStorage(context.Background(), &proj, log)
			fresh = f
			return err
		},
		After: func(ctx context.Context, run func(args ...string) error, log func(string)) error {
			s.mountStudioFunctions(ctx, &proj, log)
			if !fresh {
				return nil
			}
			url, err := s.DBResetURL(ctx, &proj)
			if err != nil {
				return err
			}
			log("New database in custom storage: applying migrations and seed data")
			return run(append([]string{"db", "reset", "--db-url", url}, cliArgs(&proj)...)...)
		},
	}
}

func functionsDir(p *models.Project) string { return filepath.Join(p.Path, "supabase", "functions") }

// mountStudioFunctions binds the whole functions folder into Studio. The CLI mounts only the
// functions that exist at start, so Studio cannot list functions when there are none and does
// not see functions created while the project runs.
func (s *Service) mountStudioFunctions(ctx context.Context, p *models.Project, log func(string)) {
	name := "supabase_studio_" + p.SupabaseProjectID
	target, err := s.Docker.ContainerEnv(ctx, name, "EDGE_FUNCTIONS_MANAGEMENT_FOLDER")
	if errors.Is(err, docker.ErrNotFound) || (err == nil && target == "") {
		return
	}
	if err == nil {
		var changed bool
		changed, err = s.Docker.EnsureBind(ctx, name, functionsDir(p), target, true)
		if err == nil {
			if changed {
				log("Mounted " + functionsDir(p) + " into Studio for its Edge Functions page")
				stdlog.Printf("project %s: mounted %s into %s", p.Slug, functionsDir(p), name)
			}
			return
		}
	}
	log("warning: could not mount the functions folder into Studio: " + err.Error())
	stdlog.Printf("project %s: mount functions folder into %s: %v", p.Slug, name, err)
}

// MountStudioFunctionsAsync runs mountStudioFunctions in the background, once at a time per project.
func (s *Service) MountStudioFunctionsAsync(p *models.Project) {
	if _, busy := s.studioMounting.LoadOrStore(p.ID, true); busy {
		return
	}
	proj := *p
	go func() {
		defer s.studioMounting.Delete(proj.ID)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.mountStudioFunctions(ctx, &proj, func(string) {})
	}()
}

// Env returns the decrypted project secrets as KEY=VALUE pairs.
func (s *Service) Env(p *models.Project) ([]string, error) {
	var secrets []models.ProjectSecret
	if err := s.db.Where("project_id = ?", p.ID).Find(&secrets).Error; err != nil {
		return nil, err
	}
	env := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		v, err := s.cipher.Decrypt(sec.ValueEncrypted)
		if err != nil {
			return nil, fmt.Errorf("cannot decrypt secret %s (was ENCRYPTION_KEY changed?)", sec.Key)
		}
		env = append(env, sec.Key+"="+v)
	}
	return env, nil
}

type SecretView struct {
	Key       string    `json:"key"`
	Preview   string    `json:"preview"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Service) ListSecrets(p *models.Project) ([]SecretView, error) {
	var secrets []models.ProjectSecret
	if err := s.db.Where("project_id = ?", p.ID).Order("key").Find(&secrets).Error; err != nil {
		return nil, err
	}
	out := make([]SecretView, 0, len(secrets))
	for _, sec := range secrets {
		preview := "********"
		if v, err := s.cipher.Decrypt(sec.ValueEncrypted); err == nil && len(v) > 12 {
			preview = v[:4] + "..." + v[len(v)-4:]
		}
		out = append(out, SecretView{Key: sec.Key, Preview: preview, UpdatedAt: sec.UpdatedAt})
	}
	return out, nil
}

func (s *Service) HasSecret(p *models.Project, key string) bool {
	var n int64
	s.db.Model(&models.ProjectSecret{}).Where("project_id = ? AND key = ?", p.ID, key).Count(&n)
	return n > 0
}

func (s *Service) SetSecret(p *models.Project, key, value string) error {
	if !secretKeyRe.MatchString(key) {
		return invalid("secret names must be UPPER_SNAKE_CASE")
	}
	if value == "" {
		return invalid("secret value cannot be empty")
	}
	enc, err := s.cipher.Encrypt(value)
	if err != nil {
		return err
	}
	var sec models.ProjectSecret
	err = s.db.Where("project_id = ? AND key = ?", p.ID, key).First(&sec).Error
	if err == nil {
		return s.db.Model(&sec).Update("value_encrypted", enc).Error
	}
	return s.db.Create(&models.ProjectSecret{ProjectID: p.ID, Key: key, ValueEncrypted: enc}).Error
}

func (s *Service) DeleteSecret(p *models.Project, key string) error {
	return s.db.Where("project_id = ? AND key = ?", p.ID, key).Delete(&models.ProjectSecret{}).Error
}
