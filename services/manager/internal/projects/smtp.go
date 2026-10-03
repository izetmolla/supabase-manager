package projects

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/configtoml"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/settings"
)

// storedSMTP is the default SMTP server with its password encrypted.
type storedSMTP struct {
	configtoml.SMTP
	PassEncrypted string `json:"pass_encrypted,omitempty"`
}

// SMTPDefaults returns the default SMTP server for new projects, without the password.
func (s *Service) SMTPDefaults() configtoml.SMTP {
	st := s.storedSMTP()
	out := st.SMTP
	out.Pass, out.HasPass = "", st.PassEncrypted != ""
	return out
}

func (s *Service) storedSMTP() storedSMTP {
	st := storedSMTP{SMTP: configtoml.SMTP{Port: 587, EmailsPerHour: 100}}
	_, _ = s.Settings.Get(settings.KeySMTPDefaults, &st)
	return st
}

// SetSMTPDefaults stores the default SMTP server; an empty password keeps the stored one.
func (s *Service) SetSMTPDefaults(in configtoml.SMTP) (configtoml.SMTP, error) {
	if in.EmailsPerHour == 0 {
		in.EmailsPerHour = 100
	}
	if err := in.Validate(); err != nil {
		return in, invalid("%s", err.Error())
	}
	st := s.storedSMTP()
	if in.Pass != "" {
		enc, err := s.cipher.Encrypt(in.Pass)
		if err != nil {
			return in, err
		}
		st.PassEncrypted = enc
	}
	in.Pass, in.HasPass = "", false
	st.SMTP = in
	if err := s.Settings.Put(settings.KeySMTPDefaults, st); err != nil {
		return in, err
	}
	return s.SMTPDefaults(), nil
}

func (s *Service) smtpDefaultsPass() string {
	st := s.storedSMTP()
	if st.PassEncrypted == "" {
		return ""
	}
	v, _ := s.cipher.Decrypt(st.PassEncrypted)
	return v
}

// ProjectSMTP returns a project's SMTP server; HasPass tells whether its password is stored.
func (s *Service) ProjectSMTP(p *models.Project) (configtoml.SMTP, error) {
	f, err := s.LoadConfig(p)
	if err != nil {
		return configtoml.SMTP{}, err
	}
	out := f.SMTP()
	out.HasPass = out.HasPass || s.HasSecret(p, configtoml.SMTPPassEnv)
	return out, nil
}

// SetProjectSMTP writes a project's SMTP server; an empty password keeps the stored one.
// useDefaults copies the default server, password included.
func (s *Service) SetProjectSMTP(p *models.Project, in configtoml.SMTP, useDefaults bool) (configtoml.SMTP, error) {
	if useDefaults {
		in = s.storedSMTP().SMTP
		in.Pass = s.smtpDefaultsPass()
		if !in.Enabled || in.Host == "" {
			return in, invalid("no default SMTP server is configured; set one in System settings")
		}
	}
	if in.Enabled && in.Pass == "" && in.User != "" && !s.HasSecret(p, configtoml.SMTPPassEnv) {
		return in, invalid("the SMTP password is required")
	}
	f, err := s.LoadConfig(p)
	if err != nil {
		return in, err
	}
	if err := f.SetSMTP(in); err != nil {
		return in, invalid("%s", err.Error())
	}
	if in.Pass != "" {
		if err := s.SetSecret(p, configtoml.SMTPPassEnv, in.Pass); err != nil {
			return in, err
		}
	}
	if err := f.Save(); err != nil {
		return in, err
	}
	return s.ProjectSMTP(p)
}

// applySMTPDefaults gives a new project the default SMTP server when one is enabled.
func (s *Service) applySMTPDefaults(p *models.Project) error {
	if d := s.storedSMTP(); !d.Enabled || d.Host == "" {
		return nil
	}
	_, err := s.SetProjectSMTP(p, configtoml.SMTP{}, true)
	return err
}

// SMTPPassFor returns the password to test a server with: in.Pass, else the stored one of the
// project (or of the defaults when p is nil).
func (s *Service) SMTPPassFor(p *models.Project, in configtoml.SMTP) string {
	if in.Pass != "" {
		return in.Pass
	}
	if p == nil {
		return s.smtpDefaultsPass()
	}
	env, _ := s.Env(p)
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, configtoml.SMTPPassEnv+"="); ok {
			return v
		}
	}
	return ""
}

// SendTestEmail sends a short message through the server: implicit TLS on port 465, STARTTLS
// when the server offers it otherwise.
func SendTestEmail(ctx context.Context, cfg configtoml.SMTP, pass, to string) error {
	if !strings.Contains(to, "@") {
		return invalid("a recipient email address is required")
	}
	cfg.Enabled = true
	if cfg.EmailsPerHour == 0 {
		cfg.EmailsPerHour = 1
	}
	if err := cfg.Validate(); err != nil {
		return invalid("%s", err.Error())
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	if cfg.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("cannot connect to %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP handshake with %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()
	if ok, _ := c.Extension("STARTTLS"); ok && cfg.Port != 465 {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.User, pass, cfg.Host)); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}
	if err := c.Mail(cfg.AdminEmail); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("recipient rejected: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	from := cfg.AdminEmail
	if cfg.SenderName != "" {
		from = fmt.Sprintf("%q <%s>", cfg.SenderName, cfg.AdminEmail)
	}
	msg := "From: " + from + "\r\nTo: " + to + "\r\nSubject: Supabase Manager SMTP test\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"This is a test message from Supabase Manager. Your SMTP settings work.\r\n"
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
