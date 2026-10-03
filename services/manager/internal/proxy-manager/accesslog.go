package proxymanager

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

// AccessEntry is one request from an nginx or Traefik access log.
type AccessEntry struct {
	Time       string  `json:"time"`
	Host       string  `json:"host"`
	HostID     uint    `json:"host_id"`
	Remote     string  `json:"remote"`
	Method     string  `json:"method"`
	URI        string  `json:"uri"`
	Status     int     `json:"status"`
	Bytes      int64   `json:"bytes"`
	DurationMS float64 `json:"duration_ms"`
	Upstream   string  `json:"upstream"`
	UserAgent  string  `json:"user_agent"`
}

type nginxEntry struct {
	Time     string  `json:"time"`
	Host     string  `json:"host"`
	Remote   string  `json:"remote"`
	Method   string  `json:"method"`
	URI      string  `json:"uri"`
	Status   int     `json:"status"`
	Bytes    int64   `json:"bytes"`
	Duration float64 `json:"duration"`
	Upstream string  `json:"upstream"`
	UA       string  `json:"ua"`
}

type traefikEntry struct {
	Time        string `json:"time"`
	StartUTC    string `json:"StartUTC"`
	RequestHost string `json:"RequestHost"`
	ClientHost  string `json:"ClientHost"`
	Method      string `json:"RequestMethod"`
	Path        string `json:"RequestPath"`
	Status      int    `json:"DownstreamStatus"`
	Size        int64  `json:"DownstreamContentSize"`
	Duration    int64  `json:"Duration"`
	ServiceAddr string `json:"ServiceAddr"`
	RouterName  string `json:"RouterName"`
	UA          string `json:"request_User-Agent"`
}

// traefikHostID extracts the host id from router names such as "h12-r3-websecure@http".
func traefikHostID(router string) uint {
	name, _, _ := strings.Cut(router, "@")
	if !strings.HasPrefix(name, "h") {
		return 0
	}
	num, _, _ := strings.Cut(name[1:], "-")
	n, err := strconv.ParseUint(num, 10, 32)
	if err != nil {
		return 0
	}
	return uint(n)
}

func parseAccess(kind, line string, hostID uint) (AccessEntry, bool) {
	if kind == spec.KindTraefik {
		var e traefikEntry
		if json.Unmarshal([]byte(line), &e) != nil || e.Method == "" {
			return AccessEntry{}, false
		}
		t := e.StartUTC
		if t == "" {
			t = e.Time
		}
		return AccessEntry{
			Time: t, Host: e.RequestHost, HostID: traefikHostID(e.RouterName), Remote: e.ClientHost,
			Method: e.Method, URI: e.Path, Status: e.Status, Bytes: e.Size,
			DurationMS: float64(e.Duration) / float64(time.Millisecond), Upstream: e.ServiceAddr, UserAgent: e.UA,
		}, true
	}
	var e nginxEntry
	if json.Unmarshal([]byte(line), &e) != nil || e.Method == "" {
		return AccessEntry{}, false
	}
	return AccessEntry{
		Time: e.Time, Host: e.Host, HostID: hostID, Remote: e.Remote, Method: e.Method, URI: e.URI,
		Status: e.Status, Bytes: e.Bytes, DurationMS: e.Duration * 1000, Upstream: e.Upstream, UserAgent: e.UA,
	}, true
}

// StreamAccessLog sends the last tail requests of an instance (optionally one host), then
// follows the log through the instance's agent until ctx is cancelled.
func (s *Service) StreamAccessLog(ctx context.Context, instanceID, hostID uint, tail int, fn func(AccessEntry) error) error {
	in, err := s.GetInstance(instanceID)
	if err != nil {
		return err
	}
	if tail <= 0 || tail > 2000 {
		tail = 200
	}
	return s.agents.tail(ctx, in.ID, tail, func(source, line string) error {
		// nginx writes one file per host, named after its id.
		var fileHost uint
		if n, err := strconv.ParseUint(strings.TrimSuffix(source, ".log"), 10, 32); err == nil {
			fileHost = uint(n)
		}
		e, ok := parseAccess(in.Kind, strings.TrimSpace(line), fileHost)
		if !ok || (hostID != 0 && e.HostID != hostID) {
			return nil
		}
		return fn(e)
	})
}
