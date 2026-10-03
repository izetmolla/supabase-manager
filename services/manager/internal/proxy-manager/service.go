package proxymanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"regexp"
	"strconv"
	"sync"

	"github.com/supabase-manager/manager/internal/auth"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/projects"
	"github.com/supabase-manager/manager/internal/proxy-manager/acme"
	"github.com/supabase-manager/manager/internal/settings"
	"github.com/supabase-manager/manager/internal/supabase"
	"gorm.io/gorm"
)

// Options configures how proxies reach the manager.
type Options struct {
	// ManagerURL is the manager's address as seen from the proxies (host network), used for
	// ACME HTTP-01, error pages and Traefik's HTTP provider.
	ManagerURL string
	// ALPNAddr is where the TLS-ALPN-01 solver listens during a challenge.
	ALPNAddr string
	// ManagerAddr is the manager's listen address, reserved in port-conflict checks.
	ManagerAddr string
	// AgentAddr is the listen address of the gRPC server proxy agents connect to.
	AgentAddr string
	// ImageRepository and ImageTag name the default proxy images:
	// <ImageRepository>-<kind>:<ImageTag>.
	ImageRepository string
	ImageTag        string
	// Bootstrap, when set, enables the Proxy Manager on first start (before an admin ever
	// toggled it) and creates and deploys a first instance.
	Bootstrap *Bootstrap
}

// Bootstrap is the first proxy instance chosen at install time.
type Bootstrap struct {
	Kind      string
	HTTPPort  int
	HTTPSPort int
}

var reReleaseVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)

// ImageTag picks the proxy image tag: the override, else the manager's release version, else
// latest (development builds).
func ImageTag(override, managerVersion string) string {
	if override != "" {
		return override
	}
	if reReleaseVersion.MatchString(managerVersion) {
		return managerVersion
	}
	return "latest"
}

// DefaultImage is the image new instances of kind use.
func (s *Service) DefaultImage(kind string) string {
	repo, tag := s.opts.ImageRepository, s.opts.ImageTag
	if repo == "" {
		repo = "izetmolla/supabase-manager-proxy"
	}
	if tag == "" {
		tag = "latest"
	}
	return repo + "-" + kind + ":" + tag
}

// ErrValidation marks errors caused by invalid input.
var ErrValidation = errors.New("invalid input")

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
func (e *validationError) Unwrap() error { return ErrValidation }

func invalid(msg string) error { return &validationError{msg} }

// Service is the proxy manager.
type Service struct {
	db       *gorm.DB
	dc       *docker.Client
	cipher   *auth.Cipher
	projects *projects.Service
	runner   *supabase.Runner
	opts     Options
	settings *settings.Store
	ACME     *acme.Service
	health   *healthMonitor
	agents   *agentHub

	deployMu sync.Mutex

	stateMu sync.Mutex
	enabled bool
	baseCtx context.Context
	stopBg  context.CancelFunc
}

func New(db *gorm.DB, dc *docker.Client, cipher *auth.Cipher, ps *projects.Service, runner *supabase.Runner, st *settings.Store, opts Options) *Service {
	if opts.AgentAddr == "" {
		opts.AgentAddr = "127.0.0.1:7070"
	}
	s := &Service{db: db, dc: dc, cipher: cipher, projects: ps, runner: runner, settings: st, opts: opts, ACME: acme.New(opts.ALPNAddr)}
	s.health = newHealthMonitor(s)
	s.agents = newAgentHub(s)
	return s
}

func (s *Service) Options() Options { return s.opts }

// ManagerURLFromAddr derives the URL proxies use to reach a manager listening on addr.
func ManagerURLFromAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// saveRow creates or updates a row; updates keep created_at, which clients do not send back.
func (s *Service) saveRow(v any, isNew bool) error {
	if isNew {
		return s.db.Create(v).Error
	}
	return s.db.Omit("created_at").Save(v).Error
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) encrypt(v string) string {
	if v == "" {
		return ""
	}
	enc, err := s.cipher.Encrypt(v)
	if err != nil {
		log.Printf("proxy manager: encrypt: %v", err)
		return ""
	}
	return enc
}

func (s *Service) decrypt(v string) string {
	if v == "" {
		return ""
	}
	dec, err := s.cipher.Decrypt(v)
	if err != nil {
		return ""
	}
	return dec
}

func (s *Service) managerPort() int {
	_, p, err := net.SplitHostPort(s.opts.ManagerAddr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

func (s *Service) alpnPort() int {
	_, p, err := net.SplitHostPort(s.opts.ALPNAddr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}
