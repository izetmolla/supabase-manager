package config

import (
	"crypto/sha256"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/supabase-manager/shared/envfile"
)

type Config struct {
	Addr           string
	DBDriver       string
	DBDSN          string
	JWTSecret      []byte
	EncryptionKey  []byte
	ProjectsRoot   string
	SupabaseBin    string
	ToolsDir       string
	CLIAutoInstall bool
	DockerSocket   string
	PortRangeStart int
	PortBlock      int
	InspectorStart int
	SecureCookies  bool
	DevCORSOrigin  string
	// UpdateRepository is the Docker Hub repository checked for new manager images.
	UpdateRepository string
	// SelfContainer overrides detection of the container the manager runs in.
	SelfContainer string
}

func Load() *Config {
	envfile.Load(".env")
	if p := os.Getenv("SECRETS_FILE"); p != "" {
		envfile.Load(p)
		keys, err := envfile.EnsureSecrets(p, "JWT_SECRET", "ENCRYPTION_KEY")
		if err != nil {
			log.Fatal(err)
		}
		if len(keys) > 0 {
			log.Printf("generated %s (stored in %s)", strings.Join(keys, " and "), p)
		}
	}
	home, _ := os.UserHomeDir()
	c := &Config{
		Addr:           env("ADDR", "127.0.0.1:8080"),
		DBDriver:       env("DB_DRIVER", "sqlite"),
		DBDSN:          env("DB_DSN", "data/manager.db"),
		ProjectsRoot:   env("PROJECTS_ROOT", filepath.Join(home, "supabase-projects")),
		SupabaseBin:    env("SUPABASE_BIN", "supabase"),
		ToolsDir:       env("TOOLS_DIR", "data/bin"),
		CLIAutoInstall: env("SUPABASE_AUTO_INSTALL", "true") == "true",
		DockerSocket:   env("DOCKER_SOCKET", "/var/run/docker.sock"),
		PortRangeStart: envInt("PORT_RANGE_START", 54300),
		PortBlock:      envInt("PORT_BLOCK", 100),
		InspectorStart: envInt("INSPECTOR_PORT_START", 8083),
		SecureCookies:  env("SECURE_COOKIES", "false") == "true",
		DevCORSOrigin:  env("DEV_CORS_ORIGIN", ""),

		UpdateRepository: env("UPDATE_REPOSITORY", "izetmolla/supabase-manager"),
		SelfContainer:    env("SELF_CONTAINER", ""),
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Println("WARNING: JWT_SECRET not set, using an insecure development default")
		jwtSecret = "dev-insecure-jwt-secret-change-me"
	}
	c.JWTSecret = []byte(jwtSecret)

	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		log.Println("WARNING: ENCRYPTION_KEY not set, deriving one from JWT_SECRET")
		encKey = jwtSecret
	}
	sum := sha256.Sum256([]byte(encKey))
	c.EncryptionKey = sum[:]

	if abs, err := filepath.Abs(c.ProjectsRoot); err == nil {
		c.ProjectsRoot = abs
	}
	if abs, err := filepath.Abs(c.ToolsDir); err == nil {
		c.ToolsDir = abs
	}
	return c
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
