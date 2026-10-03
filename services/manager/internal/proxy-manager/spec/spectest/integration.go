package spectest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

// Runnable returns Full with targets that resolve inside a container and a real certificate,
// so the rendered configuration passes the proxy's own validation.
func Runnable(t *testing.T, kind string) *spec.State {
	t.Helper()
	st := Full(kind)
	for _, u := range st.Upstreams {
		for i := range u.Targets {
			u.Targets[i].Host = "127.0.0.1"
		}
	}
	certPEM, keyPEM := SelfSigned(t, st.Certs[1].Domains)
	st.Certs[1].CertPEM, st.Certs[1].KeyPEM = certPEM, keyPEM
	return st
}

// SelfSigned returns a PEM certificate and key for the domains.
func SelfSigned(t *testing.T, domains []string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domains[0]},
		DNSNames:     domains,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// WriteFiles writes rendered files into a new temporary directory readable by containers.
func WriteFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Docker runs the docker CLI and skips the test when Docker is unavailable.
func Docker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
