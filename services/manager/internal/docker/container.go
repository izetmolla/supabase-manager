package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CreateContainer creates (but does not start) a container called name.
func (c *Client) CreateContainer(ctx context.Context, name string, body map[string]any) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/containers/create?"+url.Values{"name": {name}}.Encode(), body)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, name string) error {
	return c.post(ctx, "/containers/"+url.PathEscape(name)+"/start")
}

func (c *Client) StopContainer(ctx context.Context, name string) error {
	return c.post(ctx, "/containers/"+url.PathEscape(name)+"/stop?t=10")
}

// RemoveContainer force-removes a container; a missing container is not an error.
func (c *Client) RemoveContainer(ctx context.Context, name string) error {
	if err := c.removeContainer(ctx, name); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

// RemoveVolumeIfExists removes a named volume; a missing volume is not an error.
func (c *Client) RemoveVolumeIfExists(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name)+"?force=1", nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// ContainerState is the runtime state of a container.
type ContainerState struct {
	Exists       bool       `json:"exists"`
	ID           string     `json:"id"`
	Image        string     `json:"image"`
	Status       string     `json:"status"`
	Running      bool       `json:"running"`
	Restarting   bool       `json:"restarting"`
	RestartCount int        `json:"restart_count"`
	ExitCode     int        `json:"exit_code"`
	Error        string     `json:"error"`
	StartedAt    *time.Time `json:"started_at"`
}

func (c *Client) ContainerState(ctx context.Context, name string) (ContainerState, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ContainerState{}, nil
		}
		return ContainerState{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID           string `json:"Id"`
		RestartCount int    `json:"RestartCount"`
		Config       struct {
			Image string `json:"Image"`
		} `json:"Config"`
		State struct {
			Status     string `json:"Status"`
			Running    bool   `json:"Running"`
			Restarting bool   `json:"Restarting"`
			ExitCode   int    `json:"ExitCode"`
			Error      string `json:"Error"`
			StartedAt  string `json:"StartedAt"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ContainerState{}, err
	}
	st := ContainerState{
		Exists: true, ID: raw.ID, Image: raw.Config.Image, Status: raw.State.Status,
		Running: raw.State.Running, Restarting: raw.State.Restarting, RestartCount: raw.RestartCount,
		ExitCode: raw.State.ExitCode, Error: strings.TrimSpace(raw.State.Error),
	}
	if t, err := time.Parse(time.RFC3339Nano, raw.State.StartedAt); err == nil && t.Year() > 1 {
		st.StartedAt = &t
	}
	return st, nil
}
