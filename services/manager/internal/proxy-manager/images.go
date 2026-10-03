package proxymanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
)

const (
	hubTagPages  = 3
	imageTagsTTL = 10 * time.Minute
)

var hubTagsAPI = "https://hub.docker.com/v2/repositories/"

// ProxyImageTag is one tag of a proxy image, from Docker Hub and/or this host.
type ProxyImageTag struct {
	Tag     string     `json:"tag"`
	Image   string     `json:"image"`
	Release bool       `json:"release"`
	Local   bool       `json:"local"`
	Remote  bool       `json:"remote"`
	Updated *time.Time `json:"updated,omitempty"`
	Size    int64      `json:"size,omitempty"`
}

// ImageTags lists the available proxy images of one kind.
type ImageTags struct {
	Repository string     `json:"repository"`
	Default    string     `json:"default"`
	Tags       []ProxyImageTag `json:"tags"`
	// HubError explains why Docker Hub could not be listed; local tags are still returned.
	HubError string `json:"hub_error,omitempty"`
}

type hubTag struct {
	Name        string    `json:"name"`
	LastUpdated time.Time `json:"last_updated"`
	FullSize    int64     `json:"full_size"`
}

type hubPage struct {
	Next    string   `json:"next"`
	Results []hubTag `json:"results"`
}

type hubCacheEntry struct {
	at   time.Time
	tags []hubTag
	err  error
}

var (
	hubCacheMu sync.Mutex
	hubCache   = map[string]hubCacheEntry{}
	hubClient  = &http.Client{Timeout: 15 * time.Second}
)

func (s *Service) imageRepository(kind string) string {
	repo := s.opts.ImageRepository
	if repo == "" {
		repo = "izetmolla/supabase-manager-proxy"
	}
	return repo + "-" + kind
}

// onDockerHub reports whether repo has no registry host, i.e. lives on Docker Hub.
func onDockerHub(repo string) bool {
	first, _, ok := strings.Cut(repo, "/")
	return !ok || !(strings.ContainsAny(first, ".:") || first == "localhost")
}

func fetchHubTags(ctx context.Context, repo string, refresh bool) ([]hubTag, error) {
	hubCacheMu.Lock()
	e, ok := hubCache[repo]
	hubCacheMu.Unlock()
	if ok && !refresh && time.Since(e.at) < imageTagsTTL {
		return e.tags, e.err
	}
	path := repo
	if !strings.Contains(path, "/") {
		path = "library/" + path
	}
	var tags []hubTag
	var err error
	next := hubTagsAPI + path + "/tags?" + url.Values{"page_size": {"100"}, "ordering": {"last_updated"}}.Encode()
	for page := 0; next != "" && page < hubTagPages; page++ {
		var body hubPage
		if err = getHubPage(ctx, next, &body); err != nil {
			break
		}
		tags = append(tags, body.Results...)
		next = body.Next
	}
	if err != nil {
		tags = nil
	}
	hubCacheMu.Lock()
	hubCache[repo] = hubCacheEntry{at: time.Now(), tags: tags, err: err}
	hubCacheMu.Unlock()
	return tags, err
}

func getHubPage(ctx context.Context, u string, out *hubPage) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := hubClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Docker Hub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("the repository is not on Docker Hub yet")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("docker Hub answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker Hub: %w", err)
	}
	return nil
}

// releaseParts parses X.Y.Z (with an optional leading v).
func releaseParts(tag string) ([3]int, bool) {
	var p [3]int
	if !reReleaseVersion.MatchString(tag) {
		return p, false
	}
	for i, s := range strings.SplitN(strings.TrimPrefix(tag, "v"), ".", 3) {
		p[i], _ = strconv.Atoi(s)
	}
	return p, true
}

// ListImageTags merges the Docker Hub tags of the kind's proxy image with the ones present on
// this host: releases newest first, then the other tags by date.
func (s *Service) ListImageTags(ctx context.Context, kind string, refresh bool) (*ImageTags, error) {
	if kind != spec.KindNginx && kind != spec.KindTraefik {
		return nil, invalid("kind must be nginx or traefik")
	}
	repo := s.imageRepository(kind)
	out := &ImageTags{Repository: repo, Default: s.DefaultImage(kind)}
	byTag := map[string]*ProxyImageTag{}
	get := func(tag string) *ProxyImageTag {
		if t := byTag[tag]; t != nil {
			return t
		}
		_, rel := releaseParts(tag)
		t := &ProxyImageTag{Tag: tag, Image: repo + ":" + tag, Release: rel}
		byTag[tag] = t
		return t
	}

	if onDockerHub(repo) {
		tags, err := fetchHubTags(ctx, repo, refresh)
		if err != nil {
			out.HubError = err.Error()
		}
		for _, h := range tags {
			t := get(h.Name)
			t.Remote, t.Size = true, h.FullSize
			if !h.LastUpdated.IsZero() {
				u := h.LastUpdated
				t.Updated = &u
			}
		}
	} else {
		out.HubError = "the image repository is not on Docker Hub; only local images are listed"
	}
	if s.dc != nil {
		if local, err := s.dc.LocalTags(ctx, repo); err == nil {
			for _, l := range local {
				t := get(l.Tag)
				t.Local = true
				if t.Updated == nil {
					c := l.Created
					t.Updated = &c
				}
				if t.Size == 0 {
					t.Size = l.Size
				}
			}
		}
	}

	for _, t := range byTag {
		out.Tags = append(out.Tags, *t)
	}
	slices.SortFunc(out.Tags, func(a, b ProxyImageTag) int {
		pa, ra := releaseParts(a.Tag)
		pb, rb := releaseParts(b.Tag)
		switch {
		case ra && rb:
			return slices.Compare(pb[:], pa[:])
		case ra != rb:
			if ra {
				return -1
			}
			return 1
		case a.Tag == "latest" || b.Tag == "latest":
			if a.Tag == "latest" {
				return -1
			}
			return 1
		}
		var ta, tb time.Time
		if a.Updated != nil {
			ta = *a.Updated
		}
		if b.Updated != nil {
			tb = *b.Updated
		}
		return tb.Compare(ta)
	})
	return out, nil
}
