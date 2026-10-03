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
