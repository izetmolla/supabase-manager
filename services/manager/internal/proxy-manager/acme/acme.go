// Package acme issues certificates with go-acme/lego using HTTP-01, DNS-01 or TLS-ALPN-01.
//
// HTTP-01 tokens are kept in memory and answered by the manager (proxies forward
// /.well-known/acme-challenge/ to it). TLS-ALPN-01 is answered by a short-lived listener on
// ALPNAddr, which nginx instances forward "acme-tls/1" connections to.
package acme

import (
	"bytes"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	stdlog "log"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/tlsalpn01"
	"github.com/go-acme/lego/v4/lego"
	legolog "github.com/go-acme/lego/v4/log"
	"github.com/go-acme/lego/v4/providers/dns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
)

// Directory presets offered in the UI.
var Directories = []Directory{
	{ID: "letsencrypt", Name: "Let's Encrypt", URL: lego.LEDirectoryProduction},
	{ID: "letsencrypt-staging", Name: "Let's Encrypt (staging)", URL: lego.LEDirectoryStaging},
	{ID: "zerossl", Name: "ZeroSSL (needs EAB credentials)", URL: "https://acme.zerossl.com/v2/DV90"},
	{ID: "buypass", Name: "Buypass Go SSL", URL: "https://api.buypass.com/acme/directory"},
	{ID: "google", Name: "Google Trust Services (needs EAB credentials)", URL: "https://dv.acme-v02.api.pki.goog/directory"},
}

type Directory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// HTTPStore answers HTTP-01 challenges; it implements challenge.Provider.
type HTTPStore struct {
	mu     sync.Mutex
	tokens map[string]string
}

func (s *HTTPStore) Present(_, token, keyAuth string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]string{}
	}
	s.tokens[token] = keyAuth
	return nil
}

func (s *HTTPStore) CleanUp(_, token, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
	return nil
}

// KeyAuth returns the key authorization for a pending HTTP-01 token.
func (s *HTTPStore) KeyAuth(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.tokens[token]
	return v, ok
}

// Account is an ACME account; KeyPEM and Registration are filled by Register.
type Account struct {
	Email        string
	DirectoryURL string
	EABKeyID     string
	EABHMAC      string
	KeyPEM       string
	Registration *registration.Resource
}

type user struct {
	email string
	reg   *registration.Resource
	key   crypto.PrivateKey
}

func (u *user) GetEmail() string                        { return u.email }
func (u *user) GetRegistration() *registration.Resource { return u.reg }
func (u *user) GetPrivateKey() crypto.PrivateKey        { return u.key }

// Service issues certificates. Issuance is serialized: lego's logger is global and the
// TLS-ALPN-01 solver owns one port.
type Service struct {
	HTTP     *HTTPStore
	ALPNAddr string
	mu       sync.Mutex
}

func New(alpnAddr string) *Service {
	return &Service{HTTP: &HTTPStore{}, ALPNAddr: alpnAddr}
}

func (a *Account) user() (*user, error) {
	if a.KeyPEM == "" {
		key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
		if err != nil {
			return nil, err
		}
		a.KeyPEM = string(certcrypto.PEMEncode(key))
	}
	key, err := certcrypto.ParsePEMPrivateKey([]byte(a.KeyPEM))
	if err != nil {
		return nil, fmt.Errorf("account key: %w", err)
	}
	return &user{email: a.Email, reg: a.Registration, key: key}, nil
}

func (a *Account) client(keyType certcrypto.KeyType) (*lego.Client, *user, error) {
	u, err := a.user()
	if err != nil {
		return nil, nil, err
	}
	cfg := lego.NewConfig(u)
	cfg.CADirURL = a.DirectoryURL
	cfg.UserAgent = "supabase-manager"
	if keyType != "" {
		cfg.Certificate.KeyType = keyType
	}
	cl, err := lego.NewClient(cfg)
	return cl, u, err
}

func (a *Account) register(cl *lego.Client) error {
	var reg *registration.Resource
	var err error
	if a.EABKeyID != "" {
		reg, err = cl.Registration.RegisterWithExternalAccountBinding(registration.RegisterEABOptions{
			TermsOfServiceAgreed: true, Kid: a.EABKeyID, HmacEncoded: a.EABHMAC,
		})
	} else {
		reg, err = cl.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	}
	if err != nil {
		return err
	}
	a.Registration = reg
	return nil
}

// Register creates the account key when missing and registers the account with the CA.
func (s *Service) Register(a *Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cl, _, err := a.client("")
	if err != nil {
		return err
	}
	return a.register(cl)
}

// Request describes one issuance.
type Request struct {
	Account   *Account
	Domains   []string
	Challenge string // http-01 | dns-01 | tls-alpn-01
	KeyType   string // ec256 | ec384 | rsa2048 | rsa4096
	DNSCode   string
	DNSCreds  map[string]string
	Log       func(string)
}

// Result is an issued certificate.
type Result struct {
	CertPEM   string
	KeyPEM    string
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
	Domains   []string
}

func keyType(s string) certcrypto.KeyType {
	switch s {
	case "ec384":
		return certcrypto.EC384
	case "rsa2048":
		return certcrypto.RSA2048
	case "rsa4096":
		return certcrypto.RSA4096
	}
	return certcrypto.EC256
}

type lineWriter struct{ fn func(string) }

func (w lineWriter) Write(p []byte) (int, error) {
	for l := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if l != "" {
			w.fn(l)
		}
	}
	return len(p), nil
}

// Obtain issues a certificate. The account is registered first when needed (the caller should
// persist req.Account afterwards).
func (s *Service) Obtain(req Request) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logf := req.Log
	if logf == nil {
		logf = func(string) {}
	}
	prev := legolog.Logger
	legolog.Logger = stdlog.New(lineWriter{logf}, "", 0)
	defer func() { legolog.Logger = prev }()

	cl, _, err := req.Account.client(keyType(req.KeyType))
	if err != nil {
		return nil, err
	}
	if req.Account.Registration == nil {
		logf("Registering ACME account " + req.Account.Email)
		if err := req.Account.register(cl); err != nil {
			return nil, fmt.Errorf("register account: %w", err)
		}
		// The client keeps the key id from construction; build a new one with the registration.
		if cl, _, err = req.Account.client(keyType(req.KeyType)); err != nil {
			return nil, err
		}
	}

	switch req.Challenge {
	case "dns-01":
		p, err := DNSProvider(req.DNSCode, req.DNSCreds)
		if err != nil {
			return nil, fmt.Errorf("dns provider %s: %w", req.DNSCode, err)
		}
		if err := cl.Challenge.SetDNS01Provider(p); err != nil {
			return nil, err
		}
		logf("Using DNS-01 via " + req.DNSCode)
	case "tls-alpn-01":
		host, port, err := net.SplitHostPort(s.ALPNAddr)
		if err != nil {
			return nil, fmt.Errorf("tls-alpn solver address %q: %w", s.ALPNAddr, err)
		}
		if err := cl.Challenge.SetTLSALPN01Provider(tlsalpn01.NewProviderServer(host, port)); err != nil {
			return nil, err
		}
		logf("Using TLS-ALPN-01 (solver on " + s.ALPNAddr + ", forwarded by nginx on port 443)")
	default:
		for _, d := range req.Domains {
			if strings.HasPrefix(d, "*.") {
				return nil, errors.New("wildcard domains need the DNS-01 challenge")
			}
		}
		if err := cl.Challenge.SetHTTP01Provider(s.HTTP); err != nil {
			return nil, err
		}
		logf("Using HTTP-01 (proxies forward /.well-known/acme-challenge/ to the manager)")
	}

	logf("Requesting a certificate for " + strings.Join(req.Domains, ", "))
	res, err := cl.Certificate.Obtain(certificate.ObtainRequest{Domains: req.Domains, Bundle: true})
	if err != nil {
		return nil, err
	}
	out, err := Inspect(string(res.Certificate), string(res.PrivateKey))
	if err != nil {
		return nil, err
	}
	logf(fmt.Sprintf("Issued by %s, valid until %s", out.Issuer, out.NotAfter.Format(time.RFC1123)))
	return out, nil
}

// Inspect validates a certificate chain and key pair and reads its domains and validity.
func Inspect(certPEM, keyPEM string) (*Result, error) {
	certPEM = strings.TrimSpace(certPEM) + "\n"
	keyPEM = strings.TrimSpace(keyPEM) + "\n"
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("certificate and key do not match or are not valid PEM: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	domains := slices.Clone(leaf.DNSNames)
	if len(domains) == 0 && leaf.Subject.CommonName != "" {
		domains = []string{leaf.Subject.CommonName}
	}
	issuer := leaf.Issuer.CommonName
	if issuer == "" && len(leaf.Issuer.Organization) > 0 {
		issuer = leaf.Issuer.Organization[0]
	}
	return &Result{
		CertPEM: certPEM, KeyPEM: keyPEM, Issuer: issuer,
		NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, Domains: domains,
	}, nil
}

// Covers reports whether a certificate for certDomains is valid for every domain in want.
func Covers(certDomains, want []string) bool {
	for _, w := range want {
		ok := false
		for _, c := range certDomains {
			if strings.EqualFold(c, w) {
				ok = true
				break
			}
			if rest, found := strings.CutPrefix(c, "*."); found {
				if i := strings.IndexByte(w, '.'); i > 0 && !strings.HasPrefix(w, "*.") && strings.EqualFold(w[i+1:], rest) {
					ok = true
					break
				}
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

var envMu sync.Mutex

func pick(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

// DNSProvider builds a lego DNS provider. Cloudflare is configured directly; every other
// provider reads its settings from the environment, which is set only while it is created.
func DNSProvider(code string, creds map[string]string) (challenge.Provider, error) {
	if code == "cloudflare" {
		cfg := cloudflare.NewDefaultConfig()
		cfg.AuthEmail = pick(creds, "CF_API_EMAIL", "CLOUDFLARE_EMAIL")
		cfg.AuthKey = pick(creds, "CF_API_KEY", "CLOUDFLARE_API_KEY")
		cfg.AuthToken = pick(creds, "CF_DNS_API_TOKEN", "CLOUDFLARE_DNS_API_TOKEN")
		cfg.ZoneToken = pick(creds, "CF_ZONE_API_TOKEN", "CLOUDFLARE_ZONE_API_TOKEN")
		if cfg.ZoneToken == "" {
			cfg.ZoneToken = cfg.AuthToken
		}
		return cloudflare.NewDNSProviderConfig(cfg)
	}
	if _, ok := catalogByCode()[code]; !ok {
		return nil, fmt.Errorf("unknown DNS provider %q", code)
	}
	envMu.Lock()
	defer envMu.Unlock()
	type saved struct {
		val string
		ok  bool
	}
	old := map[string]saved{}
	for k, v := range creds {
		ov, ok := os.LookupEnv(k)
		old[k] = saved{ov, ok}
		_ = os.Setenv(k, v)
	}
	defer func() {
		for k, s := range old {
			if s.ok {
				_ = os.Setenv(k, s.val)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}()
	return dns.NewDNSChallengeProviderByName(code)
}

//go:embed dns_catalog.json
var catalogJSON []byte

// CatalogField is one setting of a DNS provider (an environment variable name for lego).
type CatalogField struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// CatalogEntry describes a lego DNS provider.
type CatalogEntry struct {
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Credentials []CatalogField `json:"credentials"`
	Additional  []CatalogField `json:"additional"`
}

var (
	catalogOnce sync.Once
	catalog     []CatalogEntry
	catalogIdx  map[string]CatalogEntry
)

// Catalog lists the DNS providers, Cloudflare first.
func Catalog() []CatalogEntry {
	catalogOnce.Do(func() {
		_ = json.NewDecoder(bytes.NewReader(catalogJSON)).Decode(&catalog)
		slices.SortStableFunc(catalog, func(a, b CatalogEntry) int {
			switch {
			case a.Code == "cloudflare":
				return -1
			case b.Code == "cloudflare":
				return 1
			}
			return 0
		})
		catalogIdx = make(map[string]CatalogEntry, len(catalog))
		for _, c := range catalog {
			catalogIdx[c.Code] = c
		}
	})
	return catalog
}

func catalogByCode() map[string]CatalogEntry {
	Catalog()
	return catalogIdx
}

// AllowedKey reports whether key is a known setting of the provider, so stored credentials
// cannot set arbitrary environment variables.
func AllowedKey(code, key string) bool {
	e, ok := catalogByCode()[code]
	if !ok {
		return false
	}
	for _, f := range append(slices.Clone(e.Credentials), e.Additional...) {
		if f.Key == key {
			return true
		}
	}
	return false
}
