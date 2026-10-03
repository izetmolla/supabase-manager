package proxymanager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
)

const (
	UpdateNewRelease = "new_release"
	UpdateRebuilt    = "rebuilt"
	UpdateRecreate   = "recreate"
)

// ImageUpdate tells whether a newer proxy image than the one an instance runs is available.
type ImageUpdate struct {
	InstanceID uint   `json:"instance_id"`
	Current    string `json:"current"`
	Target     string `json:"target,omitempty"`
	// Reason is new_release (a higher X.Y.Z tag), rebuilt (the same tag was pushed again) or
	// recreate (the image is pulled but the container still runs the previous one).
	Reason    string `json:"reason,omitempty"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// splitImage splits repo:tag; a registry port is not mistaken for a tag.
func splitImage(image string) (repo, tag string) {
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[:i], image[i+1:]
	}
	return image, "latest"
}

func digestOf(repoDigests []string, repo string) []string {
	var out []string
	for _, d := range repoDigests {
		name, sum, ok := strings.Cut(d, "@")
		if ok && strings.TrimPrefix(name, "docker.io/") == repo {
			out = append(out, sum)
		}
	}
	return out
}

// CheckUpdates checks every instance against Docker Hub (cached; refresh bypasses the cache).
func (s *Service) CheckUpdates(ctx context.Context, refresh bool) ([]ImageUpdate, error) {
	var list []ProxyInstance
	if err := s.db.Order("id").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]ImageUpdate, 0, len(list))
	for i := range list {
		out = append(out, s.checkUpdate(ctx, &list[i], refresh))
	}
	return out, nil
}

func (s *Service) checkUpdate(ctx context.Context, in *ProxyInstance, refresh bool) ImageUpdate {
	u := ImageUpdate{InstanceID: in.ID, Current: in.Image}
	if strings.Contains(in.Image, "@") {
		u.Error = "the image is pinned by digest"
		return u
	}
	repo, tag := splitImage(strings.TrimPrefix(in.Image, "docker.io/"))
	if !onDockerHub(repo) {
		u.Error = "only Docker Hub images can be checked"
		return u
	}
	tags, err := fetchHubTags(ctx, repo, refresh)
	if err != nil {
		u.Error = err.Error()
		return u
	}

	if cur, ok := releaseParts(tag); ok {
		best, bestTag := cur, ""
		for _, h := range tags {
			if p, ok := releaseParts(h.Name); ok && slices.Compare(p[:], best[:]) > 0 {
				best, bestTag = p, h.Name
			}
		}
		if bestTag != "" {
			u.Available, u.Reason, u.Target = true, UpdateNewRelease, repo+":"+bestTag
			return u
		}
	}

	local, err := s.dc.InspectImage(ctx, in.Image)
	if errors.Is(err, docker.ErrNotFound) {
		return u
	} else if err != nil {
		u.Error = err.Error()
		return u
	}
	var hubDigest string
	for _, h := range tags {
		if h.Name == tag {
			hubDigest = h.Digest
		}
	}
	if have := digestOf(local.RepoDigests, repo); hubDigest != "" && len(have) > 0 && !slices.Contains(have, hubDigest) {
		u.Available, u.Reason, u.Target = true, UpdateRebuilt, in.Image
		return u
	}
	if d, err := s.dc.ContainerDetails(ctx, in.ContainerName()); err == nil && d.ImageID != "" && d.ImageID != local.ID {
		u.Available, u.Reason, u.Target = true, UpdateRecreate, in.Image
	}
	return u
}

// UpdateImage pulls the newer image of an instance and re-creates its container on it. The
// config volume is kept, so the agent comes back with the deployed configuration. A failed
// release upgrade switches back to the previous image.
func (s *Service) UpdateImage(ctx context.Context, id uint, logf func(string)) error {
	s.deployMu.Lock()
	defer s.deployMu.Unlock()
	in, err := s.GetInstance(id)
	if err != nil {
		return err
	}
	logf("Checking Docker Hub for " + in.Image)
	u := s.checkUpdate(ctx, in, true)
	if u.Error != "" {
		return errors.New(u.Error)
	}
	if !u.Available {
		logf("Already up to date")
		return nil
	}
	repo, tag := splitImage(u.Target)
	if u.Reason != UpdateRecreate {
		logf("Pulling " + u.Target)
		if err := s.dc.PullImage(ctx, repo, tag); err != nil {
			return err
		}
	}
	prev := in.Image
	if u.Target != prev {
		if err := s.db.Model(in).Update("image", u.Target).Error; err != nil {
			return err
		}
		in.Image = u.Target
		logf(fmt.Sprintf("Image changed from %s to %s", prev, u.Target))
	}

	st, err := s.dc.ContainerState(ctx, in.ContainerName())
	if err != nil || !st.Exists {
		logf("No container yet; the new image is used on the next deploy")
		return err
	}
	if err := s.recreate(ctx, in, st.Running, logf); err != nil {
		if prev == in.Image {
			return err
		}
		logf("error: " + err.Error())
		logf("Switching back to " + prev)
		in.Image = prev
		s.db.Model(in).Update("image", prev)
		if rerr := s.recreate(ctx, in, st.Running, logf); rerr != nil {
			return fmt.Errorf("%w; restoring %s also failed: %v", err, prev, rerr)
		}
		return fmt.Errorf("the new image failed and %s was restored: %w", prev, err)
	}
	logf(in.Name + " runs " + in.Image)
	return nil
}

func (s *Service) recreate(ctx context.Context, in *ProxyInstance, start bool, logf func(string)) error {
	name := in.ContainerName()
	logf("Re-creating container " + name)
	if err := s.dc.RemoveContainer(ctx, name); err != nil {
		return err
	}
	if !start {
		_, err := s.dc.CreateContainer(ctx, name, s.containerBody(in))
		return err
	}
	if err := s.ensureContainer(ctx, in, logf); err != nil {
		return err
	}
	logf("Waiting for the proxy agent")
	return s.waitRunning(ctx, in)
}

// UpdateImageJob runs UpdateImage as a background job.
func (s *Service) UpdateImageJob(id, userID uint) (*models.Job, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	in, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	return s.runner.StartFunc(0, userID, fmt.Sprintf("update proxy %s", in.Name), deployTimeout, func(ctx context.Context, logf func(string)) error {
		return s.UpdateImage(ctx, id, logf)
	}, nil)
}
