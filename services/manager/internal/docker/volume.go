package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"created_at"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
	Scope      string            `json:"scope"`
}

type rawVolume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	CreatedAt  string            `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels"`
	Options    map[string]string `json:"Options"`
	Scope      string            `json:"Scope"`
	UsageData  *struct {
		Size     int64 `json:"Size"`
		RefCount int64 `json:"RefCount"`
	} `json:"UsageData"`
}

func (r rawVolume) volume() Volume {
	return Volume{Name: r.Name, Driver: r.Driver, Mountpoint: r.Mountpoint, CreatedAt: r.CreatedAt, Labels: r.Labels, Options: r.Options, Scope: r.Scope}
}

// VolumeSpec is the body of POST /volumes/create.
type VolumeSpec struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	DriverOpts map[string]string `json:"DriverOpts"`
	Labels     map[string]string `json:"Labels"`
}

func (c *Client) Volumes(ctx context.Context) ([]Volume, error) {
	resp, err := c.do(ctx, http.MethodGet, "/volumes", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		Volumes []rawVolume `json:"Volumes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Volume, 0, len(raw.Volumes))
	for _, r := range raw.Volumes {
		out = append(out, r.volume())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// VolumeSizes returns the disk usage of local volumes by name. It can be slow on large volumes.
func (c *Client) VolumeSizes(ctx context.Context) (map[string]int64, error) {
	resp, err := c.do(ctx, http.MethodGet, "/system/df?type=volume", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		Volumes []rawVolume `json:"Volumes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, v := range raw.Volumes {
		if v.UsageData != nil && v.UsageData.Size >= 0 {
			out[v.Name] = v.UsageData.Size
		}
	}
	return out, nil
}

// Volume inspects a volume. It returns ErrNotFound when it does not exist.
func (c *Client) Volume(ctx context.Context, name string) (Volume, error) {
	resp, err := c.do(ctx, http.MethodGet, "/volumes/"+url.PathEscape(name), nil)
	if err != nil {
		return Volume{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw rawVolume
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Volume{}, err
	}
	return raw.volume(), nil
}

func (c *Client) CreateVolume(ctx context.Context, spec VolumeSpec) error {
	resp, err := c.do(ctx, http.MethodPost, "/volumes/create", spec)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// RemoveVolume deletes a volume definition. For the local driver with bind, NFS or CIFS options
// the data at the device stays where it is; for plain local volumes the data is deleted.
func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ---- daemon info ----

type Info struct {
	ServerVersion  string   `json:"server_version"`
	DockerRootDir  string   `json:"docker_root_dir"`
	StorageDriver  string   `json:"storage_driver"`
	SwarmActive    bool     `json:"swarm_active"`
	NetworkDrivers []string `json:"network_drivers"`
	VolumeDrivers  []string `json:"volume_drivers"`
}

func (c *Client) Info(ctx context.Context) (Info, error) {
	resp, err := c.do(ctx, http.MethodGet, "/info", nil)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ServerVersion string `json:"ServerVersion"`
		DockerRootDir string `json:"DockerRootDir"`
		Driver        string `json:"Driver"`
		Plugins       struct {
			Volume  []string `json:"Volume"`
			Network []string `json:"Network"`
		} `json:"Plugins"`
		Swarm struct {
			LocalNodeState string `json:"LocalNodeState"`
		} `json:"Swarm"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Info{}, err
	}
	return Info{
		ServerVersion: raw.ServerVersion, DockerRootDir: raw.DockerRootDir, StorageDriver: raw.Driver,
		SwarmActive:    raw.Swarm.LocalNodeState == "active",
		NetworkDrivers: raw.Plugins.Network, VolumeDrivers: raw.Plugins.Volume,
	}, nil
}

// ---- containers and their mounts ----

type Mount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Driver      string `json:"driver,omitempty"`
	RW          bool   `json:"rw"`
}

type ContainerMounts struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Labels  map[string]string `json:"labels"`
	Mounts  []Mount           `json:"mounts"`
	Project string            `json:"project_id,omitempty"`
}

// ContainersWithMounts lists all containers (running or not) with their mounts.
func (c *Client) ContainersWithMounts(ctx context.Context) ([]ContainerMounts, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw []struct {
		ID     string            `json:"Id"`
		Names  []string          `json:"Names"`
		Image  string            `json:"Image"`
		State  string            `json:"State"`
		Status string            `json:"Status"`
		Labels map[string]string `json:"Labels"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
			Driver      string `json:"Driver"`
			RW          bool   `json:"RW"`
		} `json:"Mounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]ContainerMounts, 0, len(raw))
	for _, r := range raw {
		ct := ContainerMounts{ID: r.ID, Image: r.Image, State: r.State, Status: r.Status, Labels: r.Labels, Mounts: []Mount{}, Project: r.Labels[ProjectLabel]}
		if len(r.Names) > 0 {
			ct.Name = strings.TrimPrefix(r.Names[0], "/")
		}
		for _, m := range r.Mounts {
			ct.Mounts = append(ct.Mounts, Mount{Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, Driver: m.Driver, RW: m.RW})
		}
		sort.Slice(ct.Mounts, func(i, j int) bool { return ct.Mounts[i].Destination < ct.Mounts[j].Destination })
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ContainerIP returns the container's address on network, or on any network when it is not
// attached to that one.
func (c *Client) ContainerIP(ctx context.Context, name, network string) (string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
		NetworkSettings struct {
			Networks map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", err
	}
	if !raw.State.Running {
		return "", fmt.Errorf("container %s is not running", name)
	}
	if n, ok := raw.NetworkSettings.Networks[network]; ok && n.IPAddress != "" {
		return n.IPAddress, nil
	}
	names := make([]string, 0, len(raw.NetworkSettings.Networks))
	for k := range raw.NetworkSettings.Networks {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if ip := raw.NetworkSettings.Networks[k].IPAddress; ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("container %s has no IP address", name)
}

// ---- helper containers ----

// HelperMount is a mount of a helper container: a named volume (Volume), a host folder (Bind,
// created if missing) or an anonymous volume created with a driver and options.
type HelperMount struct {
	Target     string
	Volume     string
	Bind       string
	Driver     string
	DriverOpts map[string]string
}

func (m HelperMount) api() map[string]any {
	switch {
	case m.Bind != "":
		return map[string]any{"Type": "bind", "Source": m.Bind, "Target": m.Target, "BindOptions": map[string]any{"CreateMountpoint": true}}
	case m.Volume != "":
		return map[string]any{"Type": "volume", "Source": m.Volume, "Target": m.Target}
	default:
		return map[string]any{"Type": "volume", "Target": m.Target, "VolumeOptions": map[string]any{
			"DriverConfig": map[string]any{"Name": m.Driver, "Options": m.DriverOpts},
		}}
	}
}

// EnsureImage pulls image when it is not present locally.
func (c *Client) EnsureImage(ctx context.Context, image string) error {
	if resp, err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(image)+"/json", nil); err == nil {
		_ = resp.Body.Close()
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	ref, tag, ok := strings.Cut(image, ":")
	if !ok || strings.Contains(tag, "/") {
		ref, tag = image, "latest"
	}
	resp, err := c.do(ctx, http.MethodPost, "/images/create?"+url.Values{"fromImage": {ref}, "tag": {tag}}.Encode(), nil)
	if err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var msg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Error != "" {
			return fmt.Errorf("pull %s: %s", image, msg.Error)
		}
	}
	return sc.Err()
}

// RunHelper runs cmd in a short-lived, network-less container and returns its output. It fails
// when the command exits with a non-zero status. Anonymous volumes are removed afterwards.
func (c *Client) RunHelper(ctx context.Context, image string, cmd []string, mounts []HelperMount, labels map[string]string) (string, error) {
	if err := c.EnsureImage(ctx, image); err != nil {
		return "", err
	}
	ms := make([]map[string]any, 0, len(mounts))
	for _, m := range mounts {
		ms = append(ms, m.api())
	}
	body := map[string]any{
		"Image":      image,
		"Cmd":        cmd,
		"Labels":     labels,
		"HostConfig": map[string]any{"Mounts": ms, "NetworkMode": "none"},
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/create", body)
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()
	if err != nil {
		return "", err
	}
	defer func() {
		rm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if resp, err := c.do(rm, http.MethodDelete, "/containers/"+created.ID+"?force=1&v=1", nil); err == nil {
			_ = resp.Body.Close()
		}
	}()

	if resp, err = c.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil); err != nil {
		return "", err
	}
	_ = resp.Body.Close()
	if resp, err = c.do(ctx, http.MethodPost, "/containers/"+created.ID+"/wait", nil); err != nil {
		return "", err
	}
	var waited struct {
		StatusCode int `json:"StatusCode"`
	}
	err = json.NewDecoder(resp.Body).Decode(&waited)
	_ = resp.Body.Close()
	if err != nil {
		return "", err
	}

	var out strings.Builder
	_ = c.Logs(ctx, created.ID, 200, false, func(_, line string) error {
		if _, l, ok := strings.Cut(line, " "); ok {
			line = l // drop the timestamp
		}
		out.WriteString(line + "\n")
		return nil
	})
	text := strings.TrimSpace(out.String())
	if waited.StatusCode != 0 {
		if text == "" {
			text = fmt.Sprintf("exit status %d", waited.StatusCode)
		}
		return text, errors.New(text)
	}
	return text, nil
}
