//go:build integration

package traefik

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec/spectest"
)

const image = "traefik:v3"

// TestTraefikAccepts loads rendered configurations into the official image and fails on any
// router, service or middleware error reported by /api/rawdata.
// Run with: go test -tags integration ./internal/proxy-manager/render/...
func TestTraefikAccepts(t *testing.T) {
	for name, st := range map[string]*spec.State{
		"full":    spectest.Runnable(t, spec.KindTraefik),
		"minimal": spectest.Minimal(spec.KindTraefik),
	} {
		t.Run(name, func(t *testing.T) {
			st.Instance.BindIP = ""
			dir := spectest.WriteFiles(t, Render(st).Files)

			cname := fmt.Sprintf("sm-traefik-test-%d", time.Now().UnixNano())
			if out, err := spectest.Docker(t, "run", "-d", "--name", cname, "--network", "none", "-v", dir+":"+ConfigDir+":ro",
				image, "--configFile="+ConfigDir+"/traefik.yml"); err != nil {
				t.Fatalf("starting traefik: %v\n%s", err, out)
			}
			defer func() { _, _ = spectest.Docker(t, "rm", "-f", cname) }()

			url := fmt.Sprintf("http://127.0.0.1:%d/api/rawdata", st.Instance.AdminPort)
			var raw string
			deadline := time.Now().Add(20 * time.Second)
			for {
				out, err := spectest.Docker(t, "exec", cname, "wget", "-qO-", url)
				if err == nil && strings.Contains(out, "routers") {
					raw = out
					break
				}
				if time.Now().After(deadline) {
					logs, _ := spectest.Docker(t, "logs", cname)
					t.Fatalf("traefik did not serve rawdata: %v\n%s\n%s", err, out, logs)
				}
				time.Sleep(time.Second)
			}
			var data map[string]map[string]struct {
				Err []string `json:"error"`
			}
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				t.Fatalf("decoding rawdata: %v", err)
			}
			for section, items := range data {
				for n, it := range items {
					for _, e := range it.Err {
						t.Errorf("%s %s: %s", section, n, e)
					}
				}
			}
		})
	}
}
