// Package selfupdate checks Docker Hub for newer manager images and replaces the running
// container with one on the new image.
//
// A container cannot re-create itself (stopping it ends the process), so the swap is done by a
// short-lived helper container started from the new image, which runs `supabase-manager
// self-update-apply` against the Docker socket.
package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supabase-manager/manager/internal/docker"
)

const (
	hubAPI        = "https://hub.docker.com/v2/repositories/"
	cacheTTL      = time.Hour
	helperName    = "supabase-manager-updater"
	helperSocket  = "/var/run/docker.sock"
	helperCommand = "self-update-apply"
	maxTagPages   = 5
)

var (
	releaseRe     = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
	containerIDRe = regexp.MustCompile(`/containers/([0-9a-f]{64})/`)
)

type State struct {
	Running bool   `json:"running"`
	Version string `json:"version"`
	Phase   string `json:"phase"`
	Error   string `json:"error,omitempty"`
}

type Info struct {
	Current         string     `json:"current"`
	Commit          string     `json:"commit"`
	Release         bool       `json:"release"`
	Repository      string     `json:"repository"`
	Latest          string     `json:"latest"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	CheckError      string     `json:"check_error,omitempty"`
	UpdateAvailable bool       `json:"update_available"`
	Supported       bool       `json:"supported"`
	Unsupported     string     `json:"unsupported,omitempty"`
	Container       string     `json:"container,omitempty"`
	Update          State      `json:"update"`
}

type Updater struct {
	dc       *docker.Client
	repo     string
	current  string
	commit   string
	socket   string
	selfName string
	http     *http.Client

	mu        sync.Mutex
	latest    string
	checkedAt time.Time
	checkErr  string
	state     State
}

// New creates an Updater for repo (namespace/name on Docker Hub). selfName overrides the
// container lookup; socket is the Docker socket path inside this container.
func New(dc *docker.Client, repo, current, commit, socket, selfName string) *Updater {
	return &Updater{
		dc: dc, repo: repo, current: current, commit: commit, socket: socket, selfName: selfName,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

// self finds the container this process runs in. Docker bind-mounts /etc/hostname and friends
// from /var/lib/docker/containers/<id>/, which also works with --network host.
func (u *Updater) self(ctx context.Context) (docker.ContainerDetails, error) {
	ref := u.selfName
	if ref == "" {
		b, err := os.ReadFile("/proc/self/mountinfo")
		if err == nil {
			if m := containerIDRe.FindSubmatch(b); m != nil {
				ref = string(m[1])
			}
		}
	}
	if ref == "" {
		return docker.ContainerDetails{}, errors.New("not running in a Docker container")
	}
	d, err := u.dc.ContainerDetails(ctx, ref)
	if err != nil {
		return d, fmt.Errorf("cannot inspect own container: %w", err)
	}
	return d, nil
}

func (u *Updater) supported(ctx context.Context) (docker.ContainerDetails, error) {
	d, err := u.self(ctx)
	if err != nil {
		return d, err
	}
	if d.Labels["io.kubernetes.pod.name"] != "" {
		return d, errors.New("managed by Kubernetes: roll out the new image with `make docker-deploy`")
	}
	if d.Mounts[u.socket] == "" {
		return d, fmt.Errorf("the Docker socket %s is not mounted from the host", u.socket)
	}
	return d, nil
}

func (u *Updater) Info(ctx context.Context) Info {
	info := Info{Current: u.current, Commit: u.commit, Release: releaseRe.MatchString(u.current), Repository: u.repo}
	if d, err := u.supported(ctx); err != nil {
		info.Unsupported = err.Error()
	} else {
		info.Supported, info.Container = true, d.Name
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	info.Latest, info.CheckError, info.Update = u.latest, u.checkErr, u.state
	if !u.checkedAt.IsZero() {
		t := u.checkedAt
		info.CheckedAt = &t
	}
	info.UpdateAvailable = info.Latest != "" && (!info.Release || compare(info.Current, info.Latest) < 0)
	return info
}

// CheckLatest asks Docker Hub for the highest release tag (X.Y.Z) of the repository.
func (u *Updater) CheckLatest(ctx context.Context, force bool) (string, error) {
	u.mu.Lock()
	if !force && !u.checkedAt.IsZero() && time.Since(u.checkedAt) < cacheTTL {
		v, e := u.latest, u.checkErr
		u.mu.Unlock()
		if e != "" {
			return v, errors.New(e)
		}
		return v, nil
	}
	u.mu.Unlock()

	v, err := u.fetchLatest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checkedAt = time.Now()
	if err != nil {
		u.checkErr = err.Error()
		return "", err
	}
	u.latest, u.checkErr = v, ""
	return v, nil
}

func (u *Updater) fetchLatest(ctx context.Context) (string, error) {
	next := hubAPI + u.repo + "/tags?" + url.Values{"page_size": {"100"}}.Encode()
	best := ""
	for page := 0; next != "" && page < maxTagPages; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return "", err
		}
		resp, err := u.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("cannot reach Docker Hub: %w", err)
		}
		var body struct {
			Next    string `json:"next"`
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("docker hub tags for %s: %s", u.repo, resp.Status)
		}
		if err != nil {
			return "", fmt.Errorf("docker hub tags for %s: %w", u.repo, err)
		}
		for _, r := range body.Results {
			if releaseRe.MatchString(r.Name) && (best == "" || compare(r.Name, best) > 0) {
				best = r.Name
			}
		}
		next = body.Next
	}
	if best == "" {
		return "", fmt.Errorf("no release tags (X.Y.Z) found for %s", u.repo)
	}
	return best, nil
}

func (u *Updater) setPhase(phase string) {
	u.mu.Lock()
	u.state.Phase = phase
	u.mu.Unlock()
}

// Apply pulls version (latest when empty) and starts the helper that swaps the container. It
// returns once the update is under way; the manager is restarted shortly after.
func (u *Updater) Apply(ctx context.Context, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		v, err := u.CheckLatest(ctx, true)
		if err != nil {
			return err
		}
		version = v
	}
	if !releaseRe.MatchString(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	self, err := u.supported(ctx)
	if err != nil {
		return err
	}

	u.mu.Lock()
	if u.state.Running {
		u.mu.Unlock()
		return errors.New("an update is already running")
	}
	u.state = State{Running: true, Version: version, Phase: "pulling"}
	u.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := u.apply(ctx, self, version)
		u.mu.Lock()
		defer u.mu.Unlock()
		if err != nil {
			log.Printf("self-update to %s failed: %v", version, err)
			u.state.Running, u.state.Error = false, err.Error()
		}
	}()
	return nil
}

func (u *Updater) apply(ctx context.Context, self docker.ContainerDetails, version string) error {
	if err := u.dc.PullImage(ctx, u.repo, version); err != nil {
		return err
	}
	image := u.repo + ":" + version

	u.setPhase("restarting")
	log.Printf("self-update: replacing container %s with %s", self.Name, image)
	_, err := u.dc.RunContainer(ctx, helperName, map[string]any{
		"Image":  image,
		"User":   "0:0",
		"Cmd":    []string{helperCommand, "--container", self.ID, "--image", image},
		"Env":    []string{"DOCKER_SOCKET=" + helperSocket},
		"Labels": map[string]string{"io.supabase-manager.role": "updater"},
		"HostConfig": map[string]any{
			"AutoRemove":  true,
			"NetworkMode": "none",
			"Binds":       []string{self.Mounts[u.socket] + ":" + helperSocket},
		},
	})
	return err
}

// ApplyInHelper runs inside the helper container: it waits for the manager to answer the
// request that started the update, then re-creates container on image.
func ApplyInHelper(ctx context.Context, dc *docker.Client, container, image string) error {
	time.Sleep(2 * time.Second)
	return dc.RecreateWithImage(ctx, container, image, 15*time.Second)
}

// compare compares X.Y.Z versions; a leading "v" is ignored.
func compare(a, b string) int {
	pa, pb := releaseRe.FindStringSubmatch(a), releaseRe.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
