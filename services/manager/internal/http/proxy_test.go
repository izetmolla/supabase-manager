package httpapi

import (
	"testing"

	"github.com/valyala/fasthttp"
)

func TestIsManagerPath(t *testing.T) {
	cases := map[string]bool{
		"/":                              true,
		"/login":                         true,
		"/projects":                      true,
		"/projects/zzz/settings/ports":   true,
		"/assets/index-abc.js":           true,
		"/proxy/zzz/mail/":               true,
		"/api/health":                    true,
		"/api/auth/me":                   true,
		"/api/projects":                  true,
		"/api/projects/zzz/config/raw":   true,
		"/api/jobs/4/stream":             true,
		"/project/zzz/editor":            false,
		"/_next/static/chunks/a.js":      false,
		"/favicon/favicon.ico":           false,
		"/api/platform/profile":          false,
		"/api/v1/projects/zzz/api-keys":  false,
		"/api/projects/zzz/api/rest":     false,
		"/api/get-utc-time":              false,
		"/org/default-org-slug/settings": false,
	}
	for p, want := range cases {
		if got := isManagerPath(p); got != want {
			t.Errorf("isManagerPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestStudioRef(t *testing.T) {
	cases := map[string]string{
		"/project/zzz":            "zzz",
		"/project/zzz/sql/new":    "zzz",
		"/project/default/editor": "default",
		"/projects/zzz":           "",
		"/_next/static/a.js":      "",
		"/":                       "",
	}
	for p, want := range cases {
		if got := studioRef(p); got != want {
			t.Errorf("studioRef(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestIsStudioRefEndpoint(t *testing.T) {
	cases := map[string]bool{
		"/api/platform/profile":            true,
		"/api/platform/projects":           true,
		"/api/platform/projects/zzz":       true,
		"/api/platform/projects/zzz/rest":  false,
		"/api/platform/pg-meta/zzz/query":  false,
		"/api/platform/projects/":          false,
		"/api/platform/organizations/x/xx": false,
	}
	for p, want := range cases {
		if got := isStudioRefEndpoint(p); got != want {
			t.Errorf("isStudioRefEndpoint(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestStripManagerCookies(t *testing.T) {
	var h fasthttp.RequestHeader
	h.Set("Cookie", "sm_session=a; theme=dark; sm_renew=b; sm_studio=zzz")
	stripManagerCookies(&h)
	if got := string(h.Peek("Cookie")); got != "theme=dark" {
		t.Fatalf("cookies after strip = %q, want %q", got, "theme=dark")
	}
}
