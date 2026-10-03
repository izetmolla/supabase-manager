package proxymanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/registration"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/proxy-manager/acme"
)

const (
	renewBefore  = 30 * 24 * time.Hour
	issueTimeout = 15 * time.Minute
)

var keyTypes = []string{"ec256", "ec384", "rsa2048", "rsa4096"}

func (s *Service) accountData(a *AcmeAccount) *acme.Account {
	acc := &acme.Account{
		Email: a.Email, DirectoryURL: a.DirectoryURL, EABKeyID: a.EABKeyID,
		EABHMAC: s.decrypt(a.EABHMACEncrypted), KeyPEM: s.decrypt(a.KeyEncrypted),
	}
	if a.Registered && a.Registration != "" {
		reg := &registration.Resource{}
		if json.Unmarshal([]byte(a.Registration), reg) == nil && reg.URI != "" {
			acc.Registration = reg
		}
	}
	return acc
}

// storeAccount persists the key and registration lego produced; err is the registration result.
func (s *Service) storeAccount(a *AcmeAccount, acc *acme.Account, err error) {
	updates := map[string]any{}
	if acc.KeyPEM != "" {
		a.KeyEncrypted = s.encrypt(acc.KeyPEM)
		updates["key_encrypted"] = a.KeyEncrypted
	}
	if err != nil {
		a.LastError = err.Error()
	} else {
		a.LastError = ""
	}
	updates["last_error"] = a.LastError
	if acc.Registration != nil {
		b, _ := json.Marshal(acc.Registration)
		a.Registration, a.Registered = string(b), true
		updates["registration"], updates["registered"] = a.Registration, true
	}
	s.db.Model(a).Updates(updates)
}

func (s *Service) ListCertificates() ([]Certificate, error) {
	var list []Certificate
	return list, s.db.Order("id").Find(&list).Error
}

func (s *Service) GetCertificate(id uint) (*Certificate, error) {
	c := &Certificate{}
	if err := s.db.First(c, id).Error; err != nil {
		return nil, notFound(err)
	}
	return c, nil
}

// CertificatePEM returns the public chain of a certificate.
func (s *Service) CertificatePEM(id uint) (string, error) {
	c, err := s.GetCertificate(id)
	if err != nil {
		return "", err
	}
	return c.CertPEM, nil
}

type ACMECertificateInput struct {
	Name          string   `json:"name"`
	Domains       []string `json:"domains"`
	Challenge     string   `json:"challenge"`
	KeyType       string   `json:"key_type"`
	AcmeAccountID uint     `json:"acme_account_id"`
	DNSProviderID uint     `json:"dns_provider_id"`
	AutoRenew     bool     `json:"auto_renew"`
}

func (s *Service) normalizeACME(c *Certificate, in ACMECertificateInput) error {
	domains, err := normalizeDomains(in.Domains)
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return invalid("add at least one domain")
	}
	if in.KeyType == "" {
		in.KeyType = "ec256"
	}
	if !slices.Contains(keyTypes, in.KeyType) {
		return invalid("unknown key type")
	}
	switch in.Challenge {
	case ChallengeHTTP01, ChallengeTLSALPN01:
		in.DNSProviderID = 0
		for _, d := range domains {
			if strings.HasPrefix(d, "*.") {
				return invalid("wildcard domains need the DNS-01 challenge")
			}
		}
	case ChallengeDNS01:
		if err := s.exists(&DNSProvider{}, in.DNSProviderID, "DNS provider"); err != nil {
			return err
		}
	default:
		return invalid("unknown challenge type")
	}
	if in.AcmeAccountID == 0 {
		var def AcmeAccount
		if err := s.db.Where("is_default = ?", true).First(&def).Error; err != nil {
			return invalid("add an ACME account first")
		}
		in.AcmeAccountID = def.ID
	} else if err := s.exists(&AcmeAccount{}, in.AcmeAccountID, "ACME account"); err != nil {
		return err
	}
	c.Name = strings.TrimSpace(in.Name)
	if c.Name == "" {
		c.Name = domains[0]
	}
	c.Domains, c.Source, c.Challenge, c.KeyType = domains, CertSourceACME, in.Challenge, in.KeyType
	c.AcmeAccountID, c.DNSProviderID, c.AutoRenew = in.AcmeAccountID, in.DNSProviderID, in.AutoRenew
	return nil
}

// CreateACMECertificate stores an ACME certificate and starts its issuance job.
func (s *Service) CreateACMECertificate(in ACMECertificateInput, userID uint) (*Certificate, *models.Job, error) {
	c := &Certificate{Status: CertPending}
	if err := s.normalizeACME(c, in); err != nil {
		return nil, nil, err
	}
	if err := s.db.Create(c).Error; err != nil {
		return nil, nil, err
	}
	job, err := s.IssueCertificate(c.ID, userID)
	if err != nil {
		return c, nil, err
	}
	return c, job, nil
}

type CustomCertificateInput struct {
	Name    string `json:"name"`
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

// UploadCertificate stores (id 0) or replaces a custom certificate.
func (s *Service) UploadCertificate(id uint, in CustomCertificateInput, userID uint) (*Certificate, error) {
	c := &Certificate{Source: CertSourceCustom}
	if id != 0 {
		var err error
		if c, err = s.GetCertificate(id); err != nil {
			return nil, err
		}
		if c.Source != CertSourceCustom {
			return nil, invalid("only uploaded certificates can be replaced")
		}
	}
	res, err := acme.Inspect(in.CertPEM, in.KeyPEM)
	if err != nil {
		return nil, invalid(err.Error())
	}
	if res.NotAfter.Before(time.Now()) {
		return nil, invalid("the certificate has expired")
	}
	if strings.TrimSpace(in.Name) != "" {
		c.Name = strings.TrimSpace(in.Name)
	} else if c.Name == "" && len(res.Domains) > 0 {
		c.Name = res.Domains[0]
	}
	clean := s.cleanInstances(c.ID)
	s.applyResult(c, res)
	if err := s.db.Save(c).Error; err != nil {
		return nil, err
	}
	if id != 0 {
		go s.redeploy(clean, userID, nil)
	}
	return c, nil
}

func (s *Service) applyResult(c *Certificate, res *acme.Result) {
	nb, na := res.NotBefore, res.NotAfter
	c.CertPEM, c.KeyEncrypted, c.Issuer = res.CertPEM, s.encrypt(res.KeyPEM), res.Issuer
	c.NotBefore, c.NotAfter, c.Status, c.LastError = &nb, &na, CertValid, ""
	if c.Source == CertSourceCustom {
		c.Domains = res.Domains
	}
}

type CertificateUpdate struct {
	Name          string `json:"name"`
	AutoRenew     bool   `json:"auto_renew"`
	Challenge     string `json:"challenge"`
	DNSProviderID uint   `json:"dns_provider_id"`
	AcmeAccountID uint   `json:"acme_account_id"`
	KeyType       string `json:"key_type"`
}

// UpdateCertificate changes the settings used by the next issuance.
func (s *Service) UpdateCertificate(id uint, in CertificateUpdate) (*Certificate, error) {
	c, err := s.GetCertificate(id)
	if err != nil {
		return nil, err
	}
	if c.Source == CertSourceCustom {
		if n := strings.TrimSpace(in.Name); n != "" {
			c.Name = n
		}
		return c, s.db.Save(c).Error
	}
	if err := s.normalizeACME(c, ACMECertificateInput{
		Name: in.Name, Domains: c.Domains, Challenge: in.Challenge, KeyType: in.KeyType,
		AcmeAccountID: in.AcmeAccountID, DNSProviderID: in.DNSProviderID, AutoRenew: in.AutoRenew,
	}); err != nil {
		return nil, err
	}
	return c, s.db.Save(c).Error
}

func (s *Service) DeleteCertificate(id uint) error {
	var hosts []ProxyHost
	s.db.Find(&hosts)
	for _, h := range hosts {
		if h.TLS.Mode != TLSNone && h.TLS.CertificateID == id {
			return invalid("the certificate is used by host " + hostLabel(&h))
		}
	}
	return s.db.Delete(&Certificate{}, id).Error
}

func hostLabel(h *ProxyHost) string {
	if h.Name != "" {
		return h.Name
	}
	if len(h.Domains) > 0 {
		return h.Domains[0]
	}
	return fmt.Sprintf("#%d", h.ID)
}

// autoCertificate returns a certificate covering domains, creating a pending ACME certificate
// with the default account when none exists.
func (s *Service) autoCertificate(domains []string) (*Certificate, error) {
	var list []Certificate
	s.db.Find(&list)
	for i := range list {
		if acme.Covers(list[i].Domains, domains) && list[i].Status != CertError {
			return &list[i], nil
		}
	}
	in := ACMECertificateInput{Domains: domains, Challenge: ChallengeHTTP01, AutoRenew: true}
	if slices.ContainsFunc(domains, func(d string) bool { return strings.HasPrefix(d, "*.") }) {
		var p DNSProvider
		if err := s.db.Order("id").First(&p).Error; err != nil {
			return nil, invalid("wildcard domains need a DNS provider for the DNS-01 challenge")
		}
		in.Challenge, in.DNSProviderID = ChallengeDNS01, p.ID
	}
	c := &Certificate{Status: CertPending}
	if err := s.normalizeACME(c, in); err != nil {
		return nil, err
	}
	return c, s.db.Create(c).Error
}

// certInstances lists the enabled instances serving a host with the certificate.
func (s *Service) certInstances(certID uint) []uint {
	var hosts []ProxyHost
	s.db.Where("enabled = ?", true).Find(&hosts)
	var ids []uint
	for _, h := range hosts {
		if h.TLS.Mode == TLSNone || h.TLS.CertificateID != certID {
			continue
		}
		for _, id := range h.InstanceIDs {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// cleanInstances lists the deployed instances using the certificate that have no pending
// changes; they are redeployed automatically when the certificate changes.
func (s *Service) cleanInstances(certID uint) []uint {
	if certID == 0 {
		return nil
	}
	var out []uint
	for _, id := range s.certInstances(certID) {
		in, err := s.GetInstance(id)
		if err == nil && in.Enabled && in.DeployedRevisionID != 0 && !s.pending(in) {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) redeploy(ids []uint, userID uint, logf func(string)) {
	if logf == nil {
		logf = func(string) {}
	}
	for _, id := range ids {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		in, err := s.GetInstance(id)
		if err == nil && s.pending(in) {
			logf(fmt.Sprintf("Deploying proxy %q", in.Name))
			_, err = s.Deploy(ctx, id, userID, RevisionKindCertificate, "certificate update", logf)
			if err != nil {
				logf(fmt.Sprintf("Deploy of proxy %q failed: %v", in.Name, err))
				log.Printf("proxy %d: deploy after certificate update: %v", id, err)
			}
		}
		cancel()
	}
}

// IssueCertificate starts a job that obtains (or renews) an ACME certificate and then deploys
// the instances using it that had no other pending changes.
func (s *Service) IssueCertificate(id, userID uint) (*models.Job, error) {
	return s.issueJob(id, userID, nil)
}

func (s *Service) issueJob(id, userID uint, onFinish func(*models.Job)) (*models.Job, error) {
	c, err := s.GetCertificate(id)
	if err != nil {
		return nil, err
	}
	if c.Source != CertSourceACME {
		return nil, invalid("uploaded certificates cannot be issued")
	}
	title := "issue certificate " + strings.Join(c.Domains, ", ")
	job, err := s.runner.StartFunc(0, userID, title, issueTimeout, func(ctx context.Context, logf func(string)) error {
		return s.issue(ctx, id, userID, logf)
	}, onFinish)
	if err != nil {
		return nil, err
	}
	s.db.Model(c).Update("last_job_id", job.ID)
	return job, nil
}

func (s *Service) issue(ctx context.Context, id, userID uint, logf func(string)) error {
	c, err := s.GetCertificate(id)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		s.db.Model(c).Updates(map[string]any{"status": CertError, "last_error": err.Error()})
		return err
	}
	var a AcmeAccount
	if err := s.db.First(&a, c.AcmeAccountID).Error; err != nil {
		return fail(errors.New("the ACME account no longer exists"))
	}
	req := acme.Request{Account: s.accountData(&a), Domains: c.Domains, Challenge: c.Challenge, KeyType: c.KeyType, Log: logf}
	if c.Challenge == ChallengeDNS01 {
		var p DNSProvider
		if err := s.db.First(&p, c.DNSProviderID).Error; err != nil {
			return fail(errors.New("the DNS provider no longer exists"))
		}
		req.DNSCode, req.DNSCreds = p.Code, s.dnsCreds(&p)
	}
	if c.Challenge == ChallengeTLSALPN01 && !s.anyALPNInstance() {
		logf("Warning: no enabled nginx instance has TLS-ALPN forwarding on port 443; the challenge will likely fail")
	}
	hadAccount := req.Account.Registration != nil
	res, err := s.ACME.Obtain(req)
	if !hadAccount {
		s.storeAccount(&a, req.Account, nil)
	}
	if err != nil {
		return fail(err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	clean := s.cleanInstances(c.ID)
	s.applyResult(c, res)
	if err := s.db.Save(c).Error; err != nil {
		return err
	}
	logf("Certificate stored")
	if len(clean) > 0 {
		s.redeploy(clean, userID, logf)
	}
	if others := len(s.certInstances(c.ID)) - len(clean); others > 0 {
		logf(fmt.Sprintf("%d instance(s) have other pending changes; deploy them to use the new certificate", others))
	}
	return nil
}

func (s *Service) anyALPNInstance() bool {
	var n int64
	s.db.Model(&ProxyInstance{}).Where("enabled = ? AND tls_alpn = ? AND kind = ?", true, true, "nginx").Count(&n)
	return n > 0
}

func (s *Service) renewLoop(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.renewDue(ctx)
			timer.Reset(12 * time.Hour)
		}
	}
}

// renewDue runs issuance jobs, one at a time, for auto-renewing certificates that expire within
// 30 days or were never issued. Failed certificates are retried after 12 hours.
func (s *Service) renewDue(ctx context.Context) {
	var list []Certificate
	s.db.Where("source = ? AND auto_renew = ?", CertSourceACME, true).Find(&list)
	for _, c := range list {
		due := c.NotAfter == nil || time.Until(*c.NotAfter) < renewBefore
		if c.Status == CertError && c.UpdatedAt.After(time.Now().Add(-12*time.Hour)) {
			due = false
		}
		if !due {
			continue
		}
		done := make(chan struct{})
		if _, err := s.issueJob(c.ID, 0, func(*models.Job) { close(done) }); err != nil {
			log.Printf("proxy manager: renew certificate %d: %v", c.ID, err)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-done:
		}
	}
}
