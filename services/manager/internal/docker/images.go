package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LocalTag is a tag of repo present on this host.
type LocalTag struct {
	Tag     string    `json:"tag"`
	Created time.Time `json:"created"`
	Size    int64     `json:"size"`
}

// ImageInfo identifies a local image.
type ImageInfo struct {
	ID string
	// RepoDigests are the registry digests ("repo@sha256:...") the image was pulled as.
	RepoDigests []string
}

// InspectImage returns ErrNotFound when ref is not present on this host.
func (c *Client) InspectImage(ctx context.Context, ref string) (ImageInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil)
	if err != nil {
		return ImageInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID          string   `json:"Id"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{ID: raw.ID, RepoDigests: raw.RepoDigests}, nil
}

// LocalTags lists the tags of repo (e.g. "izetmolla/supabase-manager-proxy-nginx") pulled or
// built on this host.
func (c *Client) LocalTags(ctx context.Context, repo string) ([]LocalTag, error) {
	filters, _ := json.Marshal(map[string][]string{"reference": {repo}})
	resp, err := c.do(ctx, http.MethodGet, "/images/json?"+url.Values{"filters": {string(filters)}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var images []struct {
		RepoTags []string `json:"RepoTags"`
		Created  int64    `json:"Created"`
		Size     int64    `json:"Size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&images); err != nil {
		return nil, err
	}
	var out []LocalTag
	for _, img := range images {
		for _, rt := range img.RepoTags {
			name := strings.TrimPrefix(rt, "docker.io/")
			if tag, ok := strings.CutPrefix(name, repo+":"); ok {
				out = append(out, LocalTag{Tag: tag, Created: time.Unix(img.Created, 0), Size: img.Size})
			}
		}
	}
	return out, nil
}
