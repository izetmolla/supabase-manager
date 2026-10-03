//go:build integration

package proxymanager

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/proxy-manager/render/nginx"
	"github.com/supabase-manager/manager/internal/proxy-manager/render/traefik"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec/spectest"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// get returns the status of GET url, retrying while the proxy comes up.
func get(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
			last = resp.Status
		} else {
			last = err.Error()
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("GET %s: %s, want %d", url, last, want)
}

// TestAgentEndToEnd runs the proxy images (make proxy-images) against a real agent server and
// deploys, breaks, restarts and recreates them through the manager.
// Run with: go test -tags integration -run AgentEndToEnd ./internal/proxy-manager/
func TestAgentEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	tag := os.Getenv("SM_PROXY_IMAGE_TAG")
	if tag == "" {
		tag = "latest"
	}
	for _, kind := range []string{spec.KindNginx, spec.KindTraefik} {
		t.Run(kind, func(t *testing.T) {
			image := "izetmolla/supabase-manager-proxy-" + kind + ":" + tag
			if out, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
				t.Skipf("%s is not built (make proxy-images): %s", image, strings.TrimSpace(string(out)))
			}
			agentE2E(t, kind, image)
		})
	}
}

func agentE2E(t *testing.T, kind, image string) {
	s := newTestService(t)
	s.dc = docker.New("/var/run/docker.sock")
	s.opts.AgentAddr = fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	srvCtx, stopSrv := context.WithCancel(ctx)
	defer stopSrv()
	go s.agents.serve(srvCtx)

	in := &ProxyInstance{
		Name: "e2e-" + kind, Kind: kind, Image: image, BindIP: "127.0.0.1",
		HTTPPort: freePort(t), Enabled: true, TokenEncrypted: s.encrypt(randomToken()),
	}
	in.ID = uint(900000 + rand.IntN(99999))
	if kind == spec.KindTraefik {
		in.AdminPort = freePort(t)
	}
	if err := s.db.Create(in).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		if t.Failed() {
			t.Logf("container log:\n%s", s.logTail(c, in, 40))
		}
		_ = s.removeRuntime(c, in)
	})

	render := func(mutate func(*spec.State)) (map[string]string, string) {
		st := spectest.Minimal(kind)
		st.Instance.ID, st.Instance.Name = in.ID, in.Name
		st.Instance.BindIP, st.Instance.HTTPPort, st.Instance.AdminPort = in.BindIP, in.HTTPPort, in.AdminPort
		if mutate != nil {
			mutate(st)
		}
		var out *spec.Output
		if kind == spec.KindTraefik {
			out = traefik.Render(st)
		} else {
			out = nginx.Render(st)
		}
		return out.Files, fmt.Sprintf("sum-%d", rand.Int())
	}
	logf := func(m string) { t.Log(m) }
	base := fmt.Sprintf("http://127.0.0.1:%d", in.HTTPPort)

	// First deploy: the container is created, the agent connects and loads the configuration.
	files, sum := render(nil)
	if err := s.applyViaAgent(ctx, in, files, sum, logf); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	s.db.Model(in).Updates(map[string]any{"deployed_checksum": sum})
	in.DeployedChecksum = sum
	get(t, base+"/nothing-here", http.StatusNotFound)
	info := s.agents.info(in.ID)
	if !info.Connected || info.ConfigChecksum != sum || info.AgentVersion == "" || info.ProxyVersion == "" {
		t.Fatalf("agent info after deploy: %+v", info)
	}

	// A broken configuration is rejected and the proxy keeps serving the previous one.
	broken := map[string]string{}
	for k, v := range files {
		broken[k] = v
	}
	if kind == spec.KindTraefik {
		broken[traefik.RoutingFile] = `{"http":{"routers":{"broken":{"rule":"Host(` + "`x.test`" + `)","service":"missing"}}}}`
	} else {
		broken["nginx.conf"] = files["nginx.conf"] + "\nthis is not nginx;\n"
	}
	err := s.applyViaAgent(ctx, in, broken, "broken-sum", logf)
	if err == nil {
		t.Fatal("a broken configuration was accepted")
	}
	t.Logf("broken configuration rejected as expected: %v", err)
	get(t, base+"/still-here", http.StatusNotFound)
	if got := s.agents.info(in.ID).ConfigChecksum; got != sum {
		t.Fatalf("checksum after a failed apply = %q, want %q", got, sum)
	}

	// Restart goes through the agent.
	if err := s.Restart(ctx, in.ID); err != nil {
		t.Fatalf("restart: %v", err)
	}
	get(t, base+"/after-restart", http.StatusNotFound)

	// Requests reach the access log stream.
	if kind == spec.KindNginx {
		files, sum = render(func(st *spec.State) {
			st.Hosts = append(st.Hosts, spec.Host{ID: 77, Kind: spec.HostRedirect, Domains: []string{"e2e.test"}, RedirectURL: "https://example.com", RedirectCode: 302})
		})
		if err := s.applyViaAgent(ctx, in, files, sum, logf); err != nil {
			t.Fatalf("apply with a host: %v", err)
		}
		in.DeployedChecksum = sum
		s.db.Model(in).Updates(map[string]any{"deployed_checksum": sum})
		req, _ := http.NewRequest(http.MethodGet, base+"/logged", nil)
		req.Host = "e2e.test"
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("redirect host returned %s", resp.Status)
		}
		tctx, tdone := context.WithTimeout(ctx, 10*time.Second)
		var seen *AccessEntry
		_ = s.StreamAccessLog(tctx, in.ID, 77, 50, func(e AccessEntry) error {
			if e.URI == "/logged" {
				seen = &e
				tdone()
			}
			return nil
		})
		tdone()
		if seen == nil || seen.Status != http.StatusFound || seen.HostID != 77 {
			t.Fatalf("access log entry = %+v", seen)
		}
	}

	// A recreated container (new agent settings) boots from the kept config volume.
	if err := s.dc.RemoveContainer(ctx, in.ContainerName()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for s.agents.info(in.ID).Connected && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if err := s.ensureContainer(ctx, in, logf); err != nil {
		t.Fatal(err)
	}
	if err := s.agents.waitConnected(ctx, in.ID, agentConnectTimeout); err != nil {
		t.Fatal(err)
	}
	get(t, base+"/after-recreate", http.StatusNotFound)
	if got := s.agents.info(in.ID).ConfigChecksum; got != in.DeployedChecksum {
		t.Fatalf("checksum after recreate = %q, want %q", got, in.DeployedChecksum)
	}
}
