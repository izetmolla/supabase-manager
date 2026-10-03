//go:build integration

package nginx

import (
	"strings"
	"testing"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec/spectest"
)

const image = "nginx:stable-alpine"

// TestNginxAccepts runs `nginx -t` on rendered configurations in the official image.
// Run with: go test -tags integration ./internal/proxy-manager/render/...
func TestNginxAccepts(t *testing.T) {
	for name, st := range map[string]*spec.State{
		"full":    spectest.Runnable(t, spec.KindNginx),
		"alpn":    func() *spec.State { s := spectest.Runnable(t, spec.KindNginx); s.Instance.TLSALPN = true; return s }(),
		"minimal": spectest.Minimal(spec.KindNginx),
	} {
		t.Run(name, func(t *testing.T) {
			// nginx -t binds the listen addresses, which only exist on the real host.
			st.Instance.BindIP = ""
			dir := spectest.WriteFiles(t, Render(st).Files)
			out, err := spectest.Docker(t, "run", "--rm", "--network", "none", "-v", dir+":/etc/nginx/sm:ro", image,
				"sh", "-c", "mkdir -p "+AccessLogDir+" && nginx -t -c /etc/nginx/sm/nginx.conf")
			if err != nil {
				t.Fatalf("nginx -t failed: %v\n%s", err, out)
			}
			if strings.Contains(out, "[warn]") {
				t.Errorf("nginx -t warned:\n%s", out)
			}
		})
	}
}
