// Package proxymanager stores reverse-proxy configuration (hosts, upstreams, streams, access
// lists, certificates) and deploys it to nginx and Traefik containers managed by the manager.
package proxymanager

import (
	"fmt"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

const (
	CertSourceACME   = "acme"
	CertSourceCustom = "custom"

	ChallengeHTTP01    = "http-01"
	ChallengeDNS01     = "dns-01"
	ChallengeTLSALPN01 = "tls-alpn-01"

	CertPending = "pending"
	CertValid   = "valid"
	CertError   = "error"

	TLSNone        = "none"
	TLSCertificate = "certificate"
	TLSAuto        = "auto"

	RevisionApplied    = "applied"
	RevisionFailed     = "failed"
	RevisionRolledBack = "rolled_back"
)

// Models lists the tables of the proxy manager for AutoMigrate.
func Models() []any {
	return []any{
		&ProxyInstance{}, &ProxyHost{}, &ProxyUpstream{}, &ProxyStream{}, &AccessList{},
		&Certificate{}, &AcmeAccount{}, &DNSProvider{}, &ConfigRevision{},
	}
}

// ProxyInstance is an nginx or Traefik container run by the manager on the host network.
type ProxyInstance struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:100;not null" json:"name"`
	Kind      string `gorm:"size:20;not null" json:"kind"`
	Image     string `gorm:"size:255;not null" json:"image"`
	BindIP    string `gorm:"size:64" json:"bind_ip"`
	HTTPPort  int    `json:"http_port"`
	HTTPSPort int    `json:"https_port"`
	// AdminPort is Traefik's API/ping entrypoint on 127.0.0.1.
	AdminPort int    `json:"admin_port"`
	TLSALPN   bool   `json:"tls_alpn"`
	Enabled   bool   `gorm:"not null" json:"enabled"`
	Notes     string `gorm:"type:text" json:"notes"`

	TokenEncrypted     string     `gorm:"type:text" json:"-"`
	DeployedRevisionID uint       `json:"deployed_revision_id"`
	DeployedChecksum   string     `gorm:"size:64" json:"deployed_checksum"`
	DeployedAt         *time.Time `json:"deployed_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (i *ProxyInstance) ContainerName() string { return fmt.Sprintf("sm-proxy-%d", i.ID) }

// ConfigVolume holds the deployed configuration and certificates.
func (i *ProxyInstance) ConfigVolume() string { return fmt.Sprintf("sm-proxy-%d-config", i.ID) }

// LogVolume holds the access logs.
func (i *ProxyInstance) LogVolume() string { return fmt.Sprintf("sm-proxy-%d-logs", i.ID) }

type TLSOptions struct {
	Mode           string `json:"mode"`
	CertificateID  uint   `json:"certificate_id"`
	ForceHTTPS     bool   `json:"force_https"`
	HSTS           bool   `json:"hsts"`
	HSTSSubdomains bool   `json:"hsts_subdomains"`
	HTTP2          bool   `json:"http2"`
}

type HostOptions struct {
	WebSocket      bool `json:"websocket"`
	ConnectTimeout int  `json:"connect_timeout"`
	ReadTimeout    int  `json:"read_timeout"`
	MaxBodyMB      int  `json:"max_body_mb"`
}

type SecurityOptions struct {
	AccessListID uint           `json:"access_list_id"`
	IPRules      []spec.IPRule  `json:"ip_rules"`
	RateLimit    spec.RateLimit `json:"rate_limit"`
}

type HeaderOptions struct {
	Request  []spec.Header `json:"request"`
	Response []spec.Header `json:"response"`
	CORS     spec.CORS     `json:"cors"`
}

type RedirectOptions struct {
	URL          string `json:"url"`
	Code         int    `json:"code"`
	PreservePath bool   `json:"preserve_path"`
}

type ErrorPageOptions struct {
	Code int    `json:"code"`
	Body string `json:"body"`
}

// Route is a rule inside a host. Routes are stored inline on the host, in priority order.
type Route struct {
	ID                 uint               `json:"id"`
	PathType           string             `json:"path_type"`
	Path               string             `json:"path"`
	Headers            []spec.HeaderMatch `json:"headers"`
	UpstreamID         uint               `json:"upstream_id"`
	StripPrefix        bool               `json:"strip_prefix"`
	RewriteRegex       string             `json:"rewrite_regex"`
	RewriteReplacement string             `json:"rewrite_replacement"`
	AccessListID       uint               `json:"access_list_id"`
	RequestHeaders     []spec.Header      `json:"request_headers"`
	ResponseHeaders    []spec.Header      `json:"response_headers"`
}

// ProxyHost is a set of domains served by one or more instances.
type ProxyHost struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	Name        string           `gorm:"size:255" json:"name"`
	Kind        string           `gorm:"size:20;not null" json:"kind"`
	Domains     []string         `gorm:"serializer:json;type:text" json:"domains"`
	UpstreamID  uint             `json:"upstream_id"`
	Routes      []Route          `gorm:"serializer:json;type:text" json:"routes"`
	InstanceIDs []uint           `gorm:"serializer:json;type:text" json:"instance_ids"`
	TLS         TLSOptions       `gorm:"serializer:json;type:text" json:"tls"`
	Options     HostOptions      `gorm:"serializer:json;type:text" json:"options"`
	Security    SecurityOptions  `gorm:"serializer:json;type:text" json:"security"`
	Headers     HeaderOptions    `gorm:"serializer:json;type:text" json:"headers"`
	Redirect    RedirectOptions  `gorm:"serializer:json;type:text" json:"redirect"`
	ErrorPage   ErrorPageOptions `gorm:"serializer:json;type:text" json:"error_page"`
	RawNginx    string           `gorm:"type:text" json:"raw_nginx"`
	RawTraefik  string           `gorm:"type:text" json:"raw_traefik"`
	Enabled     bool             `gorm:"not null" json:"enabled"`
	Notes       string           `gorm:"type:text" json:"notes"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// Target is one backend of an upstream: a fixed address or a project service whose port is
// read from the project's config.toml when the configuration is rendered.
type Target struct {
	Kind    string `json:"kind"` // static | project
	Address string `json:"address"`
	Port    int    `json:"port"`
	Project string `json:"project"`
	Service string `json:"service"`
	Weight  int    `json:"weight"`
	Backup  bool   `json:"backup"`
}

type ProxyUpstream struct {
	ID            uint             `gorm:"primaryKey" json:"id"`
	Name          string           `gorm:"size:100;not null" json:"name"`
	Algorithm     string           `gorm:"size:20;not null" json:"algorithm"`
	Scheme        string           `gorm:"size:10;not null" json:"scheme"`
	TLSSkipVerify bool             `json:"tls_skip_verify"`
	Sticky        bool             `json:"sticky"`
	Targets       []Target         `gorm:"serializer:json;type:text" json:"targets"`
	Health        spec.HealthCheck `gorm:"serializer:json;type:text" json:"health"`
	Notes         string           `gorm:"type:text" json:"notes"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type ProxyStream struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:100" json:"name"`
	Protocol    string    `gorm:"size:5;not null" json:"protocol"`
	ListenPort  int       `gorm:"not null" json:"listen_port"`
	UpstreamID  uint      `json:"upstream_id"`
	InstanceIDs []uint    `gorm:"serializer:json;type:text" json:"instance_ids"`
	Enabled     bool      `gorm:"not null" json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AccessUser stores an $apr1$ hash, the one format both nginx (musl) and Traefik verify.
type AccessUser struct {
	Username string `json:"username"`
	Hash     string `json:"hash,omitempty"`
}

type AccessList struct {
	ID        uint         `gorm:"primaryKey" json:"id"`
	Name      string       `gorm:"size:100;not null" json:"name"`
	Users     []AccessUser `gorm:"serializer:json;type:text" json:"users"`
	Allow     []string     `gorm:"serializer:json;type:text" json:"allow"`
	Deny      []string     `gorm:"serializer:json;type:text" json:"deny"`
	Satisfy   string       `gorm:"size:5;not null" json:"satisfy"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Certificate struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Name          string     `gorm:"size:255" json:"name"`
	Domains       []string   `gorm:"serializer:json;type:text" json:"domains"`
	Source        string     `gorm:"size:10;not null" json:"source"`
	Challenge     string     `gorm:"size:20" json:"challenge"`
	KeyType       string     `gorm:"size:10" json:"key_type"`
	AcmeAccountID uint       `json:"acme_account_id"`
	DNSProviderID uint       `json:"dns_provider_id"`
	CertPEM       string     `gorm:"type:text" json:"-"`
	KeyEncrypted  string     `gorm:"type:text" json:"-"`
	Issuer        string     `gorm:"size:255" json:"issuer"`
	NotBefore     *time.Time `json:"not_before"`
	NotAfter      *time.Time `json:"not_after"`
	Status        string     `gorm:"size:10;not null" json:"status"`
	LastError     string     `gorm:"type:text" json:"last_error"`
	AutoRenew     bool       `json:"auto_renew"`
	LastJobID     uint       `json:"last_job_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type AcmeAccount struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Name             string    `gorm:"size:100;not null" json:"name"`
	Email            string    `gorm:"size:255;not null" json:"email"`
	DirectoryURL     string    `gorm:"size:500;not null" json:"directory_url"`
	EABKeyID         string    `gorm:"size:255" json:"eab_key_id"`
	EABHMACEncrypted string    `gorm:"type:text" json:"-"`
	KeyEncrypted     string    `gorm:"type:text" json:"-"`
	Registration     string    `gorm:"type:text" json:"-"`
	Registered       bool      `json:"registered"`
	IsDefault        bool      `json:"is_default"`
	LastError        string    `gorm:"type:text" json:"last_error"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type DNSProvider struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	Name                 string    `gorm:"size:100;not null" json:"name"`
	Code                 string    `gorm:"size:50;not null" json:"code"`
	CredentialsEncrypted string    `gorm:"type:text" json:"-"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ConfigRevision is one deployed (or attempted) configuration of an instance. The rendered
// files contain private keys, so they are stored encrypted.
type ConfigRevision struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	InstanceID     uint      `gorm:"index;not null" json:"instance_id"`
	Number         int       `gorm:"not null" json:"number"`
	Kind           string    `gorm:"size:20;not null" json:"kind"`
	FilesEncrypted string    `gorm:"type:text" json:"-"`
	Checksum       string    `gorm:"size:64" json:"checksum"`
	Status         string    `gorm:"size:20;not null" json:"status"`
	Error          string    `gorm:"type:text" json:"error"`
	Warnings       []string  `gorm:"serializer:json;type:text" json:"warnings"`
	Note           string    `gorm:"size:255" json:"note"`
	CreatedBy      uint      `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}
