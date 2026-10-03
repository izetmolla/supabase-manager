package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ArchiveFile is one file written by PutArchive.
type ArchiveFile struct {
	Content []byte
	Mode    int64
}

// PutArchive writes files (paths relative to dir) into a container, creating parent
// directories. dir must already exist in the container; this works on stopped containers and
// on paths backed by volumes.
func (c *Client) PutArchive(ctx context.Context, container, dir string, files map[string]ArchiveFile) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Now()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	dirs := map[string]bool{}
	for _, name := range names {
		parts := strings.Split(name, "/")
		for i := 1; i < len(parts); i++ {
			d := strings.Join(parts[:i], "/") + "/"
			if dirs[d] {
				continue
			}
			dirs[d] = true
			if err := tw.WriteHeader(&tar.Header{Name: d, Typeflag: tar.TypeDir, Mode: 0o755, ModTime: now}); err != nil {
				return err
			}
		}
		f := files[name]
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(f.Content)), ModTime: now}); err != nil {
			return err
		}
		if _, err := tw.Write(f.Content); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}

	u := "http://docker/containers/" + url.PathEscape(container) + "/archive?" + url.Values{"path": {dir}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-tar")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}
