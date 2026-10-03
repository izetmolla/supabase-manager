package configtoml

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/supabase-manager/manager/internal/ports"
)

var Providers = []string{
	"apple", "azure", "bitbucket", "discord", "facebook", "github", "gitlab", "google",
	"keycloak", "linkedin_oidc", "notion", "slack", "spotify", "twitch", "twitter", "workos", "x", "zoom",
}

func IsProvider(name string) bool {
	return slices.Contains(Providers, name)
}

// ProviderSecretEnv is the env var a provider secret is read from, as written by `supabase init`.
func ProviderSecretEnv(provider string) string {
	return "SUPABASE_AUTH_EXTERNAL_" + strings.ToUpper(provider) + "_SECRET"
}

var envRef = regexp.MustCompile(`^env\(([A-Za-z_][A-Za-z0-9_]*)\)$`)

// EnvRef returns the variable name when value has the form env(NAME).
func EnvRef(value string) (string, bool) {
	m := envRef.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return "", false
	}
	return m[1], true
}

type Services struct {
	Studio      bool `json:"studio"`
	Analytics   bool `json:"analytics"`
	Inbucket    bool `json:"inbucket"`
	EdgeRuntime bool `json:"edge_runtime"`
	Storage     bool `json:"storage"`
	Realtime    bool `json:"realtime"`
	Pooler      bool `json:"pooler"`
}

type AuthSettings struct {
	SiteURL                string   `json:"site_url"`
	AdditionalRedirectURLs []string `json:"additional_redirect_urls"`
	JWTExpiry              int      `json:"jwt_expiry"`
	EnableSignup           bool     `json:"enable_signup"`
	EnableAnonymousSignIns bool     `json:"enable_anonymous_sign_ins"`
	EnableManualLinking    bool     `json:"enable_manual_linking"`
	MinimumPasswordLength  int      `json:"minimum_password_length"`
	EmailEnableSignup      bool     `json:"email_enable_signup"`
	EmailConfirmations     bool     `json:"email_enable_confirmations"`
	EmailDoubleConfirm     bool     `json:"email_double_confirm_changes"`
}

type Provider struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	ClientID       string `json:"client_id"`
	Secret         string `json:"secret,omitempty"`
	SecretEnv      string `json:"secret_env"`
	SecretIsEnvRef bool   `json:"secret_is_env_ref"`
	HasSecret      bool   `json:"has_secret"`
	RedirectURI    string `json:"redirect_uri"`
	URL            string `json:"url"`
	SkipNonceCheck bool   `json:"skip_nonce_check"`
	EmailOptional  bool   `json:"email_optional"`
}

type Settings struct {
	ProjectID string       `json:"project_id"`
	Ports     ports.Ports  `json:"ports"`
	Services  Services     `json:"services"`
	Auth      AuthSettings `json:"auth"`
}

func (f *File) inbucketSection() string {
	if f.HasSection("inbucket") {
		return "inbucket"
	}
	return "local_smtp"
}

func (f *File) Settings() Settings {
	return Settings{
		ProjectID: f.String("", "project_id"),
		Ports: ports.Ports{
			API:       f.Int(54321, "api", "port"),
			DB:        f.Int(54322, "db", "port"),
			Shadow:    f.Int(54320, "db", "shadow_port"),
			Pooler:    f.Int(54329, "db", "pooler", "port"),
			Studio:    f.Int(54323, "studio", "port"),
			SMTP:      f.Int(54324, f.inbucketSection(), "port"),
			Analytics: f.Int(54327, "analytics", "port"),
			Inspector: f.Int(8083, "edge_runtime", "inspector_port"),
		},
		Services: Services{
			Studio:      f.Bool(true, "studio", "enabled"),
			Analytics:   f.Bool(true, "analytics", "enabled"),
			Inbucket:    f.Bool(true, f.inbucketSection(), "enabled"),
			EdgeRuntime: f.Bool(true, "edge_runtime", "enabled"),
			Storage:     f.Bool(true, "storage", "enabled"),
			Realtime:    f.Bool(true, "realtime", "enabled"),
			Pooler:      f.Bool(false, "db", "pooler", "enabled"),
		},
		Auth: AuthSettings{
			SiteURL:                f.String("", "auth", "site_url"),
			AdditionalRedirectURLs: f.Strings("auth", "additional_redirect_urls"),
			JWTExpiry:              f.Int(3600, "auth", "jwt_expiry"),
			EnableSignup:           f.Bool(true, "auth", "enable_signup"),
			EnableAnonymousSignIns: f.Bool(false, "auth", "enable_anonymous_sign_ins"),
			EnableManualLinking:    f.Bool(false, "auth", "enable_manual_linking"),
			MinimumPasswordLength:  f.Int(6, "auth", "minimum_password_length"),
			EmailEnableSignup:      f.Bool(true, "auth", "email", "enable_signup"),
			EmailConfirmations:     f.Bool(false, "auth", "email", "enable_confirmations"),
			EmailDoubleConfirm:     f.Bool(true, "auth", "email", "double_confirm_changes"),
		},
	}
}

func (f *File) SetProjectID(id string) error { return f.Set("", "project_id", id) }

func (f *File) SetPorts(p ports.Ports) error {
	sets := []struct {
		section, key string
		val          int
	}{
		{"api", "port", p.API},
		{"db", "port", p.DB},
		{"db", "shadow_port", p.Shadow},
		{"db.pooler", "port", p.Pooler},
		{"studio", "port", p.Studio},
		{f.inbucketSection(), "port", p.SMTP},
		{"analytics", "port", p.Analytics},
		{"edge_runtime", "inspector_port", p.Inspector},
	}
	for _, s := range sets {
		if s.val < 1024 || s.val > 65535 {
			return fmt.Errorf("port %d for %s.%s is out of range", s.val, s.section, s.key)
		}
		if err := f.Set(s.section, s.key, s.val); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) SetServices(s Services) error {
	sets := []struct {
		section string
		val     bool
	}{
		{"studio", s.Studio},
		{"analytics", s.Analytics},
		{f.inbucketSection(), s.Inbucket},
		{"edge_runtime", s.EdgeRuntime},
		{"storage", s.Storage},
		{"realtime", s.Realtime},
		{"db.pooler", s.Pooler},
	}
	for _, x := range sets {
		if err := f.Set(x.section, "enabled", x.val); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) SetAuth(a AuthSettings) error {
	if a.AdditionalRedirectURLs == nil {
		a.AdditionalRedirectURLs = []string{}
	}
	if a.JWTExpiry < 1 || a.JWTExpiry > 604800 {
		return fmt.Errorf("jwt_expiry must be between 1 and 604800 seconds")
	}
	if a.MinimumPasswordLength < 6 {
		return fmt.Errorf("minimum_password_length must be at least 6")
	}
	sets := []struct {
		section, key string
		val          any
	}{
		{"auth", "site_url", a.SiteURL},
		{"auth", "additional_redirect_urls", a.AdditionalRedirectURLs},
		{"auth", "jwt_expiry", a.JWTExpiry},
		{"auth", "enable_signup", a.EnableSignup},
		{"auth", "enable_anonymous_sign_ins", a.EnableAnonymousSignIns},
		{"auth", "enable_manual_linking", a.EnableManualLinking},
		{"auth", "minimum_password_length", a.MinimumPasswordLength},
		{"auth.email", "enable_signup", a.EmailEnableSignup},
		{"auth.email", "enable_confirmations", a.EmailConfirmations},
		{"auth.email", "double_confirm_changes", a.EmailDoubleConfirm},
	}
	for _, s := range sets {
		if err := f.Set(s.section, s.key, s.val); err != nil {
			return err
		}
	}
	return nil
}

// SMTPPassEnv is the env var config.toml references for the SMTP password.
const SMTPPassEnv = "SMTP_PASS"

// SMTP is the server Auth sends its emails through (confirmations, invitations, magic links,
// password recovery, email changes). Disabled, emails are captured by Mailpit instead.
type SMTP struct {
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Pass       string `json:"pass,omitempty"`
	HasPass    bool   `json:"has_pass"`
	AdminEmail string `json:"admin_email"`
	SenderName string `json:"sender_name"`
	// EmailsPerHour is auth.rate_limit.email_sent; the CLI's default of 2 is too low for real use.
	EmailsPerHour int `json:"emails_per_hour"`
}

func (s SMTP) Validate() error {
	if !s.Enabled {
		return nil
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("the SMTP host is required")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("the SMTP port must be between 1 and 65535")
	}
	if !strings.Contains(s.AdminEmail, "@") {
		return fmt.Errorf("the sender email address is required")
	}
	if s.EmailsPerHour < 1 {
		return fmt.Errorf("emails per hour must be at least 1")
	}
	return nil
}

func (f *File) SMTP() SMTP {
	s := SMTP{
		Enabled:       f.Bool(false, "auth", "email", "smtp", "enabled"),
		Host:          f.String("", "auth", "email", "smtp", "host"),
		Port:          f.Int(587, "auth", "email", "smtp", "port"),
		User:          f.String("", "auth", "email", "smtp", "user"),
		AdminEmail:    f.String("", "auth", "email", "smtp", "admin_email"),
		SenderName:    f.String("", "auth", "email", "smtp", "sender_name"),
		EmailsPerHour: f.Int(2, "auth", "rate_limit", "email_sent"),
	}
	pass := f.String("", "auth", "email", "smtp", "pass")
	_, isRef := EnvRef(pass)
	s.HasPass = pass != "" && !isRef
	return s
}

// SetSMTP writes the SMTP server. Like provider secrets, the password is never written to the
// file: it references SMTP_PASS, kept in the encrypted secret store.
func (f *File) SetSMTP(s SMTP) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.EmailsPerHour < 1 {
		s.EmailsPerHour = 2
	}
	sets := []struct {
		section, key string
		val          any
	}{
		{"auth.email.smtp", "enabled", s.Enabled},
		{"auth.email.smtp", "host", strings.TrimSpace(s.Host)},
		{"auth.email.smtp", "port", s.Port},
		{"auth.email.smtp", "user", strings.TrimSpace(s.User)},
		{"auth.email.smtp", "pass", "env(" + SMTPPassEnv + ")"},
		{"auth.email.smtp", "admin_email", strings.TrimSpace(s.AdminEmail)},
		{"auth.email.smtp", "sender_name", strings.TrimSpace(s.SenderName)},
		{"auth.rate_limit", "email_sent", s.EmailsPerHour},
	}
	for _, st := range sets {
		if err := f.Set(st.section, st.key, st.val); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) Provider(name string) Provider {
	p := Provider{
		Name:           name,
		Enabled:        f.Bool(false, "auth", "external", name, "enabled"),
		ClientID:       f.String("", "auth", "external", name, "client_id"),
		RedirectURI:    f.String("", "auth", "external", name, "redirect_uri"),
		URL:            f.String("", "auth", "external", name, "url"),
		SkipNonceCheck: f.Bool(false, "auth", "external", name, "skip_nonce_check"),
		EmailOptional:  f.Bool(false, "auth", "external", name, "email_optional"),
		SecretEnv:      ProviderSecretEnv(name),
	}
	secret := f.String("", "auth", "external", name, "secret")
	if env, ok := EnvRef(secret); ok {
		p.SecretEnv = env
		p.SecretIsEnvRef = true
	} else {
		p.HasSecret = secret != ""
	}
	return p
}

// SetProvider writes a provider. The secret itself is never written to the file:
// it always references an env var so the value can live in the encrypted secret store.
func (f *File) SetProvider(p Provider) error {
	section := "auth.external." + p.Name
	envName := p.SecretEnv
	if envName == "" {
		envName = ProviderSecretEnv(p.Name)
	}
	sets := []struct {
		key string
		val any
	}{
		{"enabled", p.Enabled},
		{"client_id", p.ClientID},
		{"secret", "env(" + envName + ")"},
		{"redirect_uri", p.RedirectURI},
		{"url", p.URL},
		{"skip_nonce_check", p.SkipNonceCheck},
		{"email_optional", p.EmailOptional},
	}
	for _, s := range sets {
		if err := f.Set(section, s.key, s.val); err != nil {
			return err
		}
	}
	return nil
}
