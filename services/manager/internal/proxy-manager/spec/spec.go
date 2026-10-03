// Package spec is the resolved, renderer-facing description of what one proxy instance serves.
// It is built from the stored models (drafts) and contains only enabled objects with every
// reference (upstream targets, certificates, access lists) already resolved.
package spec

const (
	KindNginx   = "nginx"
	KindTraefik = "traefik"

	HostProxy    = "proxy"
	HostRedirect = "redirect"
	HostError    = "error"

	PathPrefix = "prefix"
	PathExact  = "exact"
	PathRegex  = "regex"

	AlgoRoundRobin = "round_robin"
	AlgoLeastConn  = "least_conn"
	AlgoIPHash     = "ip_hash"
)

// State is everything one instance needs to render its configuration.
type State struct {
	Instance    Instance             `json:"instance"`
	Hosts       []Host               `json:"hosts"`
	Streams     []Stream             `json:"streams"`
	Upstreams   map[uint]*Upstream   `json:"upstreams"`
	Certs       map[uint]*Cert       `json:"-"`
	AccessLists map[uint]*AccessList `json:"access_lists"`
}

type Instance struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	BindIP    string `json:"bind_ip"`
	HTTPPort  int    `json:"http_port"`
	HTTPSPort int    `json:"https_port"`
	AdminPort int    `json:"admin_port"`
	TLSALPN   bool   `json:"tls_alpn"`
	// ManagerURL is where the proxy forwards ACME HTTP-01 requests and error pages
	// (e.g. http://127.0.0.1:8080).
	ManagerURL string `json:"manager_url"`
	// ALPNSolverAddr is the manager's TLS-ALPN-01 solver (host:port).
	ALPNSolverAddr string `json:"alpn_solver_addr"`
}

type Host struct {
	ID             uint     `json:"id"`
	Kind           string   `json:"kind"`
	Domains        []string `json:"domains"`
	Upstream       uint     `json:"upstream"`
	Routes         []Route  `json:"routes"`
	CertID         uint     `json:"cert_id"`
	ForceHTTPS     bool     `json:"force_https"`
	HSTS           bool     `json:"hsts"`
	HSTSSubdomains bool     `json:"hsts_subdomains"`
	HTTP2          bool     `json:"http2"`
	WebSocket      bool     `json:"websocket"`
	// Seconds; 0 means the proxy default.
	ConnectTimeout int `json:"connect_timeout"`
	ReadTimeout    int `json:"read_timeout"`
	// MaxBodyMB of 0 means the proxy default; -1 means unlimited.
	MaxBodyMB       int       `json:"max_body_mb"`
	AccessList      uint      `json:"access_list"`
	IPRules         []IPRule  `json:"ip_rules"`
	RateLimit       RateLimit `json:"rate_limit"`
	RequestHeaders  []Header  `json:"request_headers"`
	ResponseHeaders []Header  `json:"response_headers"`
	CORS            CORS      `json:"cors"`
	RedirectURL     string    `json:"redirect_url"`
	RedirectCode    int       `json:"redirect_code"`
	PreservePath    bool      `json:"preserve_path"`
	ErrorCode       int       `json:"error_code"`
	ErrorBody       string    `json:"error_body"`
	RawNginx        string    `json:"raw_nginx"`
	RawTraefik      string    `json:"raw_traefik"`
}

type Route struct {
	ID                 uint          `json:"id"`
	PathType           string        `json:"path_type"`
	Path               string        `json:"path"`
	Headers            []HeaderMatch `json:"headers"`
	Upstream           uint          `json:"upstream"`
	StripPrefix        bool          `json:"strip_prefix"`
	RewriteRegex       string        `json:"rewrite_regex"`
	RewriteReplacement string        `json:"rewrite_replacement"`
	// AccessList overrides the host's access list when non-zero.
	AccessList      uint     `json:"access_list"`
	RequestHeaders  []Header `json:"request_headers"`
	ResponseHeaders []Header `json:"response_headers"`
}

type HeaderMatch struct {
	Name string `json:"name"`
	// Value is matched exactly; a leading "~" makes it a regular expression.
	Value string `json:"value"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type IPRule struct {
	Action string `json:"action"` // allow | deny
	CIDR   string `json:"cidr"`
}

type RateLimit struct {
	Enabled bool `json:"enabled"`
	RPS     int  `json:"rps"`
	Burst   int  `json:"burst"`
}

type CORS struct {
	Enabled     bool     `json:"enabled"`
	Origins     []string `json:"origins"`
	Methods     string   `json:"methods"`
	Headers     string   `json:"headers"`
	Credentials bool     `json:"credentials"`
	MaxAge      int      `json:"max_age"`
}

type Upstream struct {
	ID            uint        `json:"id"`
	Name          string      `json:"name"`
	Algorithm     string      `json:"algorithm"`
	Scheme        string      `json:"scheme"`
	TLSSkipVerify bool        `json:"tls_skip_verify"`
	Sticky        bool        `json:"sticky"`
	Targets       []Target    `json:"targets"`
	Health        HealthCheck `json:"health"`
}

type Target struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Weight int    `json:"weight"`
	Backup bool   `json:"backup"`
}

type HealthCheck struct {
	Enabled      bool   `json:"enabled"`
	Path         string `json:"path"`
	Interval     int    `json:"interval"`
	Timeout      int    `json:"timeout"`
	ExpectStatus int    `json:"expect_status"`
}

type Stream struct {
	ID         uint   `json:"id"`
	Protocol   string `json:"protocol"` // tcp | udp
	ListenPort int    `json:"listen_port"`
	Upstream   uint   `json:"upstream"`
}

type Cert struct {
	ID      uint     `json:"id"`
	Domains []string `json:"domains"`
	CertPEM string   `json:"-"`
	KeyPEM  string   `json:"-"`
}

type AccessList struct {
	ID      uint         `json:"id"`
	Users   []AccessUser `json:"users"`
	Allow   []string     `json:"allow"`
	Deny    []string     `json:"deny"`
	Satisfy string       `json:"satisfy"` // any | all
}

type AccessUser struct {
	Username string `json:"username"`
	Hash     string `json:"-"`
}

// Output is a rendered configuration. Files are relative to the instance's config root.
type Output struct {
	Files map[string]string `json:"files"`
	// Secret lists files that contain private keys; they are redacted in previews.
	Secret map[string]bool `json:"secret"`
	// Warnings are features the target proxy cannot express exactly.
	Warnings []string `json:"warnings"`
}

func NewOutput() *Output {
	return &Output{Files: map[string]string{}, Secret: map[string]bool{}}
}

func (o *Output) Warn(msg string) { o.Warnings = append(o.Warnings, msg) }
