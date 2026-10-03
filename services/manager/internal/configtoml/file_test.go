package configtoml

import (
	"strings"
	"testing"

	"github.com/supabase-manager/manager/internal/ports"
)

const sample = `# top comment
project_id = "old"

[api]
enabled = true
# Port to use for the API URL.
port = 54321

[auth]
site_url = "http://127.0.0.1:3000"
additional_redirect_urls = [
  "https://127.0.0.1:3000",
]

[auth.external.apple]
enabled = false
client_id = ""
secret = "env(SUPABASE_AUTH_EXTERNAL_APPLE_SECRET)"
`

func load(t *testing.T) *File {
	t.Helper()
	f := &File{}
	if err := f.SetText(sample); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSetPreservesComments(t *testing.T) {
	f := load(t)
	if err := f.SetProjectID("new-id"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set("api", "port", 55321); err != nil {
		t.Fatal(err)
	}
	if got := f.String("", "project_id"); got != "new-id" {
		t.Fatalf("project_id = %q", got)
	}
	if got := f.Int(0, "api", "port"); got != 55321 {
		t.Fatalf("api.port = %d", got)
	}
	for _, want := range []string{"# top comment", "# Port to use for the API URL."} {
		if !strings.Contains(f.Text(), want) {
			t.Fatalf("lost comment %q", want)
		}
	}
}

func TestSetMultilineArray(t *testing.T) {
	f := load(t)
	if err := f.Set("auth", "additional_redirect_urls", []string{"http://a", "http://b"}); err != nil {
		t.Fatal(err)
	}
	got := f.Strings("auth", "additional_redirect_urls")
	if len(got) != 2 || got[1] != "http://b" {
		t.Fatalf("urls = %v", got)
	}
	if f.String("", "auth", "site_url") != "http://127.0.0.1:3000" {
		t.Fatal("site_url was clobbered")
	}
}

func TestNewSectionAndProvider(t *testing.T) {
	f := load(t)
	if err := f.SetProvider(Provider{Name: "github", Enabled: true, ClientID: "abc"}); err != nil {
		t.Fatal(err)
	}
	p := f.Provider("github")
	if !p.Enabled || p.ClientID != "abc" || !p.SecretIsEnvRef || p.SecretEnv != "SUPABASE_AUTH_EXTERNAL_GITHUB_SECRET" {
		t.Fatalf("provider = %+v", p)
	}
	apple := f.Provider("apple")
	if apple.Enabled || apple.SecretEnv != "SUPABASE_AUTH_EXTERNAL_APPLE_SECRET" {
		t.Fatalf("apple = %+v", apple)
	}
}

func TestRealConfigRoundTrip(t *testing.T) {
	f, err := Load("testdata/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	before := f.Settings()
	if before.Ports.API != 54321 || before.Ports.Inspector != 8083 {
		t.Fatalf("unexpected defaults: %+v", before.Ports)
	}
	p := ports.ForBase(55300, 8084)
	if err := f.SetPorts(p); err != nil {
		t.Fatal(err)
	}
	if err := f.SetServices(Services{Studio: true, Analytics: false, Inbucket: true, EdgeRuntime: true, Storage: true, Realtime: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetProvider(Provider{Name: "apple", Enabled: true, ClientID: "com.example.web"}); err != nil {
		t.Fatal(err)
	}
	after := f.Settings()
	if after.Ports != p || after.Services.Analytics || !after.Services.Studio {
		t.Fatalf("after = %+v", after)
	}
	if a := f.Provider("apple"); !a.Enabled || a.ClientID != "com.example.web" {
		t.Fatalf("apple = %+v", a)
	}
	if !strings.Contains(f.Text(), "# DO NOT commit your OAuth provider secret to git.") {
		t.Fatal("comments were lost")
	}
}

func TestSetPorts(t *testing.T) {
	f := load(t)
	p := ports.ForBase(55300, 8084)
	if err := f.SetPorts(p); err != nil {
		t.Fatal(err)
	}
	if got := f.Settings().Ports; got != p {
		t.Fatalf("ports = %+v, want %+v", got, p)
	}
}
