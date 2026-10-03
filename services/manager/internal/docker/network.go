package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("not found")

type IPAMConfig struct {
	Subnet  string `json:"Subnet,omitempty"`
	Gateway string `json:"Gateway,omitempty"`
	IPRange string `json:"IPRange,omitempty"`
}

// NetworkSpec is the body of POST /networks/create.
type NetworkSpec struct {
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	EnableIPv6 bool   `json:"EnableIPv6"`
	Attachable bool   `json:"Attachable,omitempty"`
	IPAM       struct {
		Driver string       `json:"Driver"`
		Config []IPAMConfig `json:"Config"`
	} `json:"IPAM"`
	Options map[string]string `json:"Options"`
	Labels  map[string]string `json:"Labels"`
}

type Network struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Internal   bool              `json:"internal"`
	EnableIPv6 bool              `json:"enable_ipv6"`
	IPAM       []IPAMConfig      `json:"ipam"`
	Options    map[string]string `json:"options"`
	Labels     map[string]string `json:"labels"`
	Containers []string          `json:"containers"`
}

type rawNetwork struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Scope      string `json:"Scope"`
	Internal   bool   `json:"Internal"`
	EnableIPv6 bool   `json:"EnableIPv6"`
	IPAM       struct {
		Config []IPAMConfig `json:"Config"`
	} `json:"IPAM"`
	Options    map[string]string `json:"Options"`
	Labels     map[string]string `json:"Labels"`
	Containers map[string]struct {
		Name string `json:"Name"`
	} `json:"Containers"`
}

func (r rawNetwork) network() Network {
	n := Network{
		ID: r.ID, Name: r.Name, Driver: r.Driver, Scope: r.Scope, Internal: r.Internal,
		EnableIPv6: r.EnableIPv6, IPAM: r.IPAM.Config, Options: r.Options, Labels: r.Labels,
		Containers: []string{},
	}
	for _, ct := range r.Containers {
		n.Containers = append(n.Containers, ct.Name)
	}
	sort.Strings(n.Containers)
	return n
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		var e struct {
			Message string `json:"message"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(b, &e) == nil && e.Message != "" {
			return nil, fmt.Errorf("docker: %s", e.Message)
		}
		return nil, fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func (c *Client) Networks(ctx context.Context) ([]Network, error) {
	resp, err := c.do(ctx, http.MethodGet, "/networks", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw []rawNetwork
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.network())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Network inspects a network by name or ID. It returns ErrNotFound when it does not exist.
func (c *Client) Network(ctx context.Context, name string) (Network, error) {
	resp, err := c.do(ctx, http.MethodGet, "/networks/"+url.PathEscape(name), nil)
	if err != nil {
		return Network{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw rawNetwork
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Network{}, err
	}
	n := raw.network()
	if n.Name != name && n.ID != name {
		// The API also matches ID prefixes; only exact names count here.
		return Network{}, ErrNotFound
	}
	return n, nil
}

func (c *Client) CreateNetwork(ctx context.Context, spec NetworkSpec) error {
	resp, err := c.do(ctx, http.MethodPost, "/networks/create", spec)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
