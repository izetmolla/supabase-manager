package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const ProjectLabel = "com.supabase.cli.project"

// Client talks to the Docker Engine HTTP API over its unix socket.
type Client struct {
	http *http.Client
}

func New(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{http: &http.Client{Transport: tr}}
}

type Port struct {
	IP          string `json:"ip"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port"`
	Type        string `json:"type"`
}

type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Health  string `json:"health"`
	Ports   []Port `json:"ports"`
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := "http://docker" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}
	if resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.get(ctx, "/_ping", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

var healthRe = regexp.MustCompile(`\((healthy|unhealthy|health: starting)\)`)

func (c *Client) ListProject(ctx context.Context, projectID string) ([]Container, error) {
	filters, _ := json.Marshal(map[string][]string{"label": {ProjectLabel + "=" + projectID}})
	resp, err := c.get(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {string(filters)}})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var raw []struct {
		ID     string   `json:"Id"`
		Names  []string `json:"Names"`
		Image  string   `json:"Image"`
		State  string   `json:"State"`
		Status string   `json:"Status"`
		Ports  []struct {
			IP          string `json:"IP"`
			PrivatePort int    `json:"PrivatePort"`
			PublicPort  int    `json:"PublicPort"`
			Type        string `json:"Type"`
		} `json:"Ports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		name := ""
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		ct := Container{
			ID: r.ID, Name: name, Image: r.Image, State: r.State, Status: r.Status,
			Service: serviceName(name, projectID),
		}
		if m := healthRe.FindStringSubmatch(r.Status); m != nil {
			ct.Health = strings.TrimPrefix(m[1], "health: ")
		}
		seen := map[string]bool{}
		for _, p := range r.Ports {
			key := fmt.Sprintf("%d/%d/%s", p.PrivatePort, p.PublicPort, p.Type)
			if seen[key] {
				continue
			}
			seen[key] = true
			ct.Ports = append(ct.Ports, Port{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
		}
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
}

// serviceName turns supabase_db_myproj into db.
func serviceName(name, projectID string) string {
	s := strings.TrimPrefix(name, "supabase_")
	return strings.TrimSuffix(s, "_"+projectID)
}

type Stats struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
}

func (c *Client) Stats(ctx context.Context, id string) (Stats, error) {
	resp, err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/stats", url.Values{"stream": {"false"}})
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		Name     string `json:"name"`
		CPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs  int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Stats{}, err
	}
	st := Stats{ID: id, Name: strings.TrimPrefix(raw.Name, "/"), MemLimit: raw.MemoryStats.Limit}
	st.MemUsage = raw.MemoryStats.Usage
	if cache, ok := raw.MemoryStats.Stats["inactive_file"]; ok && cache < st.MemUsage {
		st.MemUsage -= cache
	}
	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		cpus := raw.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		st.CPUPercent = cpuDelta / sysDelta * float64(cpus) * 100
	}
	return st, nil
}

// Logs streams log lines from a container until ctx is cancelled (when follow is
// true) or the log ends. Each line is passed to fn; returning an error stops it.
func (c *Client) Logs(ctx context.Context, id string, tail int, follow bool, fn func(stream, line string) error) error {
	q := url.Values{
		"stdout":     {"1"},
		"stderr":     {"1"},
		"timestamps": {"1"},
		"tail":       {strconv.Itoa(tail)},
	}
	if follow {
		q.Set("follow", "1")
	}
	resp, err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/logs", q)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	br := bufio.NewReaderSize(resp.Body, 64*1024)
	head, err := br.Peek(8)
	if err != nil || head[0] > 2 || head[1] != 0 || head[2] != 0 || head[3] != 0 {
		return scanLines(br, "stdout", fn)
	}

	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			return nil
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		streamName := "stdout"
		if hdr[0] == 2 {
			streamName = "stderr"
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(br, frame); err != nil {
			return nil
		}
		for line := range strings.SplitSeq(strings.TrimRight(string(frame), "\n"), "\n") {
			if err := fn(streamName, line); err != nil {
				return err
			}
		}
	}
}

func scanLines(r io.Reader, streamName string, fn func(stream, line string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if err := fn(streamName, sc.Text()); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) Restart(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker/containers/"+url.PathEscape(id)+"/restart?t=10", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
