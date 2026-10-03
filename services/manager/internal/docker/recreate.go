package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *Client) inspectRaw(ctx context.Context, name string) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ContainerEnv returns the value of an environment variable of a container ("" when unset).
func (c *Client) ContainerEnv(ctx context.Context, name, key string) (string, error) {
	raw, err := c.inspectRaw(ctx, name)
	if err != nil {
		return "", err
	}
	cfg, _ := raw["Config"].(map[string]any)
	envs, _ := cfg["Env"].([]any)
	for _, e := range envs {
		if s, _ := e.(string); strings.HasPrefix(s, key+"=") {
			return strings.TrimPrefix(s, key+"="), nil
		}
	}
	return "", nil
}

// containerSpec turns an inspect result into a create request with the same configuration and
// networks. Values Docker derives from the container ID (hostname, aliases) are left out.
func containerSpec(raw map[string]any) (id, name string, body, hc map[string]any, err error) {
	id, _ = raw["Id"].(string)
	name, _ = raw["Name"].(string)
	name = strings.TrimPrefix(name, "/")
	cfg, _ := raw["Config"].(map[string]any)
	hc, _ = raw["HostConfig"].(map[string]any)
	if id == "" || name == "" || cfg == nil || hc == nil {
		return "", "", nil, nil, fmt.Errorf("container %s: unexpected inspect output", name)
	}
	shortID := id
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}

	body = map[string]any{}
	maps.Copy(body, cfg)
	hostNet := hc["NetworkMode"] == "host"
	if h, _ := body["Hostname"].(string); h == shortID || hostNet {
		delete(body, "Hostname")
	}
	if hostNet {
		delete(body, "Domainname")
		delete(body, "MacAddress")
	}
	body["HostConfig"] = hc

	endpoints := map[string]any{}
	ns, _ := raw["NetworkSettings"].(map[string]any)
	nets, _ := ns["Networks"].(map[string]any)
	for netName, v := range nets {
		ep, _ := v.(map[string]any)
		e := map[string]any{}
		for _, k := range []string{"IPAMConfig", "Links", "DriverOpts"} {
			if ep[k] != nil {
				e[k] = ep[k]
			}
		}
		aliases := []string{}
		as, _ := ep["Aliases"].([]any)
		for _, a := range as {
			if s, _ := a.(string); s != "" && s != shortID {
				aliases = append(aliases, s)
			}
		}
		if len(aliases) > 0 {
			e["Aliases"] = aliases
		}
		endpoints[netName] = e
	}
	body["NetworkingConfig"] = map[string]any{"EndpointsConfig": endpoints}
	return id, name, body, hc, nil
}

// replace swaps container id (called name) for a new one created from body. The original is
// renamed and stopped; it is restored when the new container cannot be created, started or
// fails verify.
func (c *Client) replace(ctx context.Context, id, name string, body map[string]any, verify func(ctx context.Context, newID string) error) error {
	old := name + "_sm_old"
	_ = c.removeContainer(ctx, old)
	if err := c.post(ctx, "/containers/"+id+"/rename?"+url.Values{"name": {old}}.Encode()); err != nil {
		return fmt.Errorf("rename %s: %w", name, err)
	}
	restore := func() {
		_ = c.post(ctx, "/containers/"+id+"/rename?"+url.Values{"name": {name}}.Encode())
		_ = c.post(ctx, "/containers/"+id+"/start")
	}

	resp, err := c.do(ctx, http.MethodPost, "/containers/create?"+url.Values{"name": {name}}.Encode(), body)
	if err != nil {
		restore()
		return fmt.Errorf("re-create %s: %w", name, err)
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()
	if err != nil {
		restore()
		return err
	}

	_ = c.post(ctx, "/containers/"+id+"/stop?t=10") // fails harmlessly when already stopped
	if err := c.post(ctx, "/containers/"+created.ID+"/start"); err != nil {
		_ = c.removeContainer(ctx, created.ID)
		restore()
		return fmt.Errorf("start re-created %s: %w", name, err)
	}
	if verify != nil {
		if err := verify(ctx, created.ID); err != nil {
			_ = c.removeContainer(ctx, created.ID)
			restore()
			return fmt.Errorf("re-created %s: %w", name, err)
		}
	}
	_ = c.removeContainer(ctx, id)
	return nil
}

// EnsureBind makes sure container name has source bound at target. Binds cannot be added to an
// existing container, so the container is re-created with the same configuration, name and
// networks; binds below target are replaced by the new one. changed is false when the bind was
// already there. When the new container fails to start, the original is restored.
func (c *Client) EnsureBind(ctx context.Context, name, source, target string, readOnly bool) (changed bool, err error) {
	raw, err := c.inspectRaw(ctx, name)
	if err != nil {
		return false, err
	}
	id, cname, body, hc, err := containerSpec(raw)
	if err != nil {
		return false, err
	}

	binds := []string{}
	existing, _ := hc["Binds"].([]any)
	for _, b := range existing {
		s, _ := b.(string)
		parts := strings.Split(s, ":")
		if len(parts) >= 2 {
			if parts[1] == target && parts[0] == source {
				return false, nil
			}
			if parts[1] == target || strings.HasPrefix(parts[1], target+"/") {
				continue
			}
		}
		binds = append(binds, s)
	}
	bind := source + ":" + target
	if readOnly {
		bind += ":ro"
	}
	hc["Binds"] = append(binds, bind)

	if err := c.replace(ctx, id, cname, body, nil); err != nil {
		return false, err
	}
	return true, nil
}

// RecreateWithImage re-creates container name from image, keeping its configuration, name and
// networks. The new container must still be running after settle, otherwise the original is
// restored.
func (c *Client) RecreateWithImage(ctx context.Context, name, image string, settle time.Duration) error {
	raw, err := c.inspectRaw(ctx, name)
	if err != nil {
		return err
	}
	id, cname, body, _, err := containerSpec(raw)
	if err != nil {
		return err
	}
	body["Image"] = image
	return c.replace(ctx, id, cname, body, func(ctx context.Context, newID string) error {
		select {
		case <-time.After(settle):
		case <-ctx.Done():
			return ctx.Err()
		}
		st, err := c.inspectRaw(ctx, newID)
		if err != nil {
			return err
		}
		state, _ := st["State"].(map[string]any)
		running, _ := state["Running"].(bool)
		restarting, _ := state["Restarting"].(bool)
		restarts, _ := st["RestartCount"].(float64)
		if !running || restarting || restarts > 0 {
			return fmt.Errorf("exited right after start (exit code %v)", state["ExitCode"])
		}
		if h, _ := state["Health"].(map[string]any); h != nil && h["Status"] == "unhealthy" {
			return fmt.Errorf("unhealthy after start")
		}
		return nil
	})
}

func (c *Client) post(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) removeContainer(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/containers/"+url.PathEscape(id)+"?force=1", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
