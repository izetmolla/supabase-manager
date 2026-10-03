package projects

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/supabase-manager/manager/internal/models"
)

var (
	migrationFileRe = regexp.MustCompile(`^(\d+)_([A-Za-z0-9_\-]+)\.sql$`)
	nameRe          = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-]{0,62}$`)
)

func ValidName(s string) bool { return nameRe.MatchString(s) }

type Migration struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	Applied *bool  `json:"applied"`
}

func migrationsDir(p *models.Project) string {
	return filepath.Join(p.Path, "supabase", "migrations")
}

// Migrations lists local migration files. When dbURL is set, Applied reports
// whether each version is recorded in supabase_migrations.schema_migrations.
func (s *Service) Migrations(ctx context.Context, p *models.Project, dbURL string) ([]Migration, error) {
	entries, err := os.ReadDir(migrationsDir(p))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	applied := map[string]bool{}
	haveApplied := false
	if dbURL != "" {
		if v, err := appliedVersions(ctx, dbURL); err == nil {
			applied, haveApplied = v, true
		}
	}
	out := []Migration{}
	for _, e := range entries {
		m := migrationFileRe.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		info, _ := e.Info()
		mig := Migration{Version: m[1], Name: m[2], File: e.Name()}
		if info != nil {
			mig.Size = info.Size()
		}
		if haveApplied {
			a := applied[m[1]]
			mig.Applied = &a
		}
		out = append(out, mig)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func appliedVersions(ctx context.Context, dbURL string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close(ctx) }()
	rows, err := conn.Query(ctx, "select version from supabase_migrations.schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (s *Service) ReadMigration(p *models.Project, file string) (string, error) {
	if !migrationFileRe.MatchString(file) {
		return "", invalid("invalid migration file name")
	}
	b, err := os.ReadFile(filepath.Join(migrationsDir(p), file))
	if err != nil {
		return "", ErrNotFound
	}
	return string(b), nil
}

func (s *Service) WriteMigration(p *models.Project, file, content string) error {
	if !migrationFileRe.MatchString(file) {
		return invalid("invalid migration file name")
	}
	path := filepath.Join(migrationsDir(p), file)
	if _, err := os.Stat(path); err != nil {
		return ErrNotFound
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (s *Service) NewMigration(ctx context.Context, p *models.Project, name string) (string, error) {
	if !ValidName(name) {
		return "", invalid("migration names use lowercase letters, numbers, - and _")
	}
	before := map[string]bool{}
	if entries, err := os.ReadDir(migrationsDir(p)); err == nil {
		for _, e := range entries {
			before[e.Name()] = true
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := s.Runner.Run(ctx, p.Path, nil, "migration", "new", name); err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(migrationsDir(p))
	for _, e := range entries {
		if !before[e.Name()] && strings.HasSuffix(e.Name(), "_"+name+".sql") {
			return e.Name(), nil
		}
	}
	return "", nil
}

type Function struct {
	Name     string    `json:"name"`
	Shared   bool      `json:"shared"`
	Files    []string  `json:"files"`
	Modified time.Time `json:"modified"`
}

func (s *Service) Functions(p *models.Project) ([]Function, error) {
	dir := filepath.Join(p.Path, "supabase", "functions")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Function{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Function{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fn := Function{Name: e.Name(), Shared: strings.HasPrefix(e.Name(), "_")}
		if info, err := e.Info(); err == nil {
			fn.Modified = info.ModTime()
		}
		if files, err := os.ReadDir(filepath.Join(dir, e.Name())); err == nil {
			for _, f := range files {
				fn.Files = append(fn.Files, f.Name())
			}
		}
		out = append(out, fn)
	}
	return out, nil
}

func (s *Service) NewFunction(ctx context.Context, p *models.Project, name string) error {
	if !ValidName(name) {
		return invalid("function names use lowercase letters, numbers, - and _")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if _, err := s.Runner.Run(ctx, p.Path, nil, "functions", "new", name); err != nil {
		return err
	}
	s.mountStudioFunctions(ctx, p, func(string) {})
	return nil
}

func (s *Service) GenTypes(ctx context.Context, p *models.Project, lang string) (string, error) {
	switch lang {
	case "", "typescript":
		lang = "typescript"
	case "go", "python", "swift", "dart":
	default:
		return "", invalid("unsupported language %q", lang)
	}
	env, err := s.Env(p)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := s.Run(ctx, p, env, "gen", "types", "--lang", lang, "--local")
	return string(out), err
}
