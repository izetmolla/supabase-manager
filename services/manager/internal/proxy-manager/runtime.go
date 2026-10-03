package proxymanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/render/nginx"
	"github.com/supabase-manager/manager/internal/proxy-manager/render/traefik"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	pb "github.com/supabase-manager/shared/proxyagent/v1"
)

const (
	nginxRoot           = "/etc/nginx/sm"
	labelProxy          = "com.supabase-manager.proxy"
	labelKind           = "com.supabase-manager.proxy.kind"
	labelSpec           = "com.supabase-manager.proxy.spec"
	startSettle         = 3 * time.Second
	agentConnectTimeout = 45 * time.Second
)

// containerEnv is what the agent needs to reach and authenticate to the manager.
func (s *Service) containerEnv(in *ProxyInstance) []string {
	env := []string{
		"SM_INSTANCE_ID=" + strconv.FormatUint(uint64(in.ID), 10),
		"SM_TOKEN=" + s.decrypt(in.TokenEncrypted),
		"SM_MANAGER_ADDR=" + s.agentDialAddr(),
	}
	if s.agentTLS() {
		env = append(env, "SM_MANAGER_TLS=true")
		if _, fp, err := s.agentCertificate(); err == nil {
			env = append(env, "SM_MANAGER_CERT_SHA256="+fp)
		}
	}
	return env
}

func (s *Service) containerBody(in *ProxyInstance) map[string]any {
	env := s.containerEnv(in)
	sum := sha256.Sum256([]byte(in.Image + "\n" + strings.Join(env, "\n")))
	labels := map[string]string{
		labelProxy: strconv.FormatUint(uint64(in.ID), 10),
		labelKind:  in.Kind,
		labelSpec:  hex.EncodeToString(sum[:8]),
	}
	var mounts []map[string]any
	if in.Kind == spec.KindTraefik {
		mounts = []map[string]any{
			{"Type": "volume", "Source": in.ConfigVolume(), "Target": traefik.ConfigDir},
			{"Type": "volume", "Source": in.LogVolume(), "Target": "/var/log/traefik"},
		}
	} else {
		mounts = []map[string]any{
			{"Type": "volume", "Source": in.ConfigVolume(), "Target": nginxRoot},
			{"Type": "volume", "Source": in.LogVolume(), "Target": nginx.AccessLogDir},
		}
	}
	return map[string]any{
		"Image":  in.Image,
		"Env":    env,
		"Labels": labels,
		"HostConfig": map[string]any{
			"NetworkMode":   "host",
			"RestartPolicy": map[string]any{"Name": "unless-stopped"},
			"Mounts":        mounts,
			"LogConfig":     map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "10m", "max-file": "3"}},
		},
	}
}

// ensureContainer makes the instance container exist with the current image and agent
// settings, and run. A container with an outdated image or environment is recreated; the
// config volume is kept, so the agent boots with the last applied configuration.
func (s *Service) ensureContainer(ctx context.Context, in *ProxyInstance, logf func(string)) error {
	name := in.ContainerName()
	body := s.containerBody(in)
	want := body["Labels"].(map[string]string)[labelSpec]

	st, err := s.dc.ContainerState(ctx, name)
	if err != nil {
		return err
	}
	if st.Exists {
		d, err := s.dc.ContainerDetails(ctx, name)
		if err != nil {
			return err
		}
		if d.Labels[labelSpec] == want {
			if st.Running {
				return nil
			}
			logf("Starting container " + name)
			return s.dc.StartContainer(ctx, name)
		}
		logf("Recreating container " + name + " (image or agent settings changed)")
		if err := s.dc.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}
	logf("Pulling " + in.Image)
	if err := s.dc.EnsureImage(ctx, in.Image); err != nil {
		return err
	}
	logf("Creating container " + name)
	if _, err := s.dc.CreateContainer(ctx, name, body); err != nil {
		return err
	}
	logf("Starting container " + name)
	return s.dc.StartContainer(ctx, name)
}

// waitRunning fails when the container stops or restarts shortly after a start, reporting the
// tail of its log, then waits for the agent to connect.
func (s *Service) waitRunning(ctx context.Context, in *ProxyInstance) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(startSettle):
	}
	st, err := s.dc.ContainerState(ctx, in.ContainerName())
	if err != nil {
		return err
	}
	if !st.Running || st.Restarting {
		return fmt.Errorf("the proxy container is not running (%s): %s", st.Status, s.logTail(ctx, in, 15))
	}
	if err := s.agents.waitConnected(ctx, in.ID, agentConnectTimeout); err != nil {
		return fmt.Errorf("the proxy agent did not connect to the manager at %s: %w\n%s", s.agentDialAddr(), err, s.logTail(ctx, in, 15))
	}
	return nil
}

func (s *Service) logTail(ctx context.Context, in *ProxyInstance, n int) string {
	var lines []string
	_ = s.dc.Logs(ctx, in.ContainerName(), n, false, func(_, line string) error {
		if _, l, ok := strings.Cut(line, " "); ok {
			line = l
		}
		lines = append(lines, line)
		return nil
	})
	if len(lines) == 0 {
		return "no log output"
	}
	return strings.Join(lines, "\n")
}

func (s *Service) Start(ctx context.Context, id uint) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	in, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if in.DeployedRevisionID == 0 {
		return invalid("the instance has not been deployed yet; deploy it to create its container")
	}
	if err := s.ensureContainer(ctx, in, func(string) {}); err != nil {
		return err
	}
	return s.waitRunning(ctx, in)
}

func (s *Service) Stop(ctx context.Context, id uint) error {
	in, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	return s.dc.StopContainer(ctx, in.ContainerName())
}

// Restart restarts the proxy process inside the container through its agent, or the whole
// container when the agent is not connected.
func (s *Service) Restart(ctx context.Context, id uint) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	in, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	if s.agents.info(id).Connected {
		res, err := s.agents.call(ctx, id, &pb.ManagerMessage{Msg: &pb.ManagerMessage_Restart{Restart: &pb.Restart{}}})
		if err != nil {
			return err
		}
		if !res.Ok {
			return fmt.Errorf("restart failed: %s", res.Error)
		}
		return nil
	}
	if err := s.dc.Restart(ctx, in.ContainerName()); err != nil {
		return err
	}
	return s.waitRunning(ctx, in)
}

// ContainerName returns the container of an instance (for logs).
func (s *Service) ContainerName(id uint) (string, error) {
	in, err := s.GetInstance(id)
	if err != nil {
		return "", err
	}
	return in.ContainerName(), nil
}

func (s *Service) removeRuntime(ctx context.Context, in *ProxyInstance) error {
	if err := s.dc.RemoveContainer(ctx, in.ContainerName()); err != nil {
		return err
	}
	if err := s.dc.RemoveVolumeIfExists(ctx, in.ConfigVolume()); err != nil {
		return err
	}
	return s.dc.RemoveVolumeIfExists(ctx, in.LogVolume())
}

// legacyImage reports whether an instance still uses a plain nginx or Traefik image, which
// has no agent and cannot be managed any more.
func legacyImage(image string) bool {
	repo, _, _ := strings.Cut(image, ":")
	repo = strings.TrimPrefix(strings.TrimPrefix(repo, "docker.io/"), "library/")
	return slices.Contains([]string{"nginx", "traefik"}, repo)
}
