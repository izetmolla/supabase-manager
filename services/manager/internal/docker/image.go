package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ContainerDetails is the part of an inspect result the self-update needs.
type ContainerDetails struct {
	ID     string
	Name   string
	Image  string
	Labels map[string]string
	// Mounts maps container paths to host sources.
	Mounts map[string]string
}

func (c *Client) ContainerDetails(ctx context.Context, name string) (ContainerDetails, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return ContainerDetails{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID     string `json:"Id"`
		Name   string `json:"Name"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ContainerDetails{}, err
	}
	d := ContainerDetails{
		ID: raw.ID, Name: strings.TrimPrefix(raw.Name, "/"), Image: raw.Config.Image,
		Labels: raw.Config.Labels, Mounts: map[string]string{},
	}
	for _, m := range raw.Mounts {
		d.Mounts[m.Destination] = m.Source
	}
	return d, nil
}

// PullImage pulls repo:tag. Docker reports pull failures inside the 200 response stream.
func (c *Client) PullImage(ctx context.Context, repo, tag string) error {
	resp, err := c.do(ctx, http.MethodPost, "/images/create?"+url.Values{"fromImage": {repo}, "tag": {tag}}.Encode(), nil)
	if err != nil {
		return fmt.Errorf("pull %s:%s: %w", repo, tag, err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var msg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Error != "" {
			return fmt.Errorf("pull %s:%s: %s", repo, tag, msg.Error)
		}
	}
	return sc.Err()
}

// RunContainer creates and starts a container called name, replacing a leftover with that name.
func (c *Client) RunContainer(ctx context.Context, name string, body map[string]any) (string, error) {
	if err := c.removeContainer(ctx, name); err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/create?"+url.Values{"name": {name}}.Encode(), body)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	var created struct {
		ID string `json:"Id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&created)
	_ = resp.Body.Close()
	if err != nil {
		return "", err
	}
	if err := c.post(ctx, "/containers/"+created.ID+"/start"); err != nil {
		_ = c.removeContainer(ctx, created.ID)
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	return created.ID, nil
}
