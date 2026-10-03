package httpapi

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/projects"
)

// dockerInfo lists what the daemon supports, for the network and storage driver pickers.
func (s *Server) dockerInfo(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	info, err := s.projects.Docker.Info(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	return c.JSON(info)
}

func (s *Server) getStorageDefaults(c fiber.Ctx) error {
	return c.JSON(s.projects.StorageDefaults())
}

func (s *Server) putStorageDefaults(c fiber.Ctx) error {
	var in models.StorageConfig
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	out, err := s.projects.SetStorageDefaults(in)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "storage.defaults", "system", out)
	return c.JSON(out)
}

func (s *Server) getProjectStorage(c fiber.Ctx) error {
	p := project(c)
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	resp := fiber.Map{"config": p.Storage, "host_root": projects.HostRoot(p, p.Storage)}
	vols, err := s.projects.StorageView(ctx, p)
	if err != nil {
		resp["live_error"] = err.Error()
	} else {
		resp["volumes"] = vols
	}
	if info, err := s.projects.Docker.Info(ctx); err == nil {
		resp["docker_root"] = info.DockerRootDir
	}
	return c.JSON(resp)
}

func (s *Server) putProjectStorage(c fiber.Ctx) error {
	p := project(c)
	var in models.StorageConfig
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.SetStorage(p, in); err != nil {
		return mapErr(err)
	}
	s.audit(c, "storage.update", p.Slug, p.Storage)
	return s.getProjectStorage(c)
}

type mountView struct {
	docker.Mount
	StorageMode string `json:"storage_mode,omitempty"`
	Location    string `json:"location"`
	Managed     bool   `json:"managed"`
}

type containerMountsView struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Image         string      `json:"image"`
	State         string      `json:"state"`
	Status        string      `json:"status"`
	Project       string      `json:"project,omitempty"`
	Mounts        []mountView `json:"mounts"`
	CustomStorage bool        `json:"custom_storage"`
}

type volumeOverview struct {
	docker.Volume
	StorageMode string   `json:"storage_mode"`
	Location    string   `json:"location"`
	Managed     bool     `json:"managed"`
	Project     string   `json:"project,omitempty"`
	UsedBy      []string `json:"used_by"`
	Size        *int64   `json:"size,omitempty"`
}

// mounts lists every container with its mounts and every volume, marking volumes whose location
// the manager controls (custom persistent storage).
func (s *Server) mounts(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Minute)
	defer cancel()
	dc := s.projects.Docker
	cts, err := dc.ContainersWithMounts(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	vols, err := dc.Volumes(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	var sizes map[string]int64
	if c.Query("sizes") == "1" {
		sizes, _ = dc.VolumeSizes(ctx)
	}
	ps, err := s.projects.List()
	if err != nil {
		return err
	}
	slugs := map[string]string{}
	for _, p := range ps {
		slugs[p.SupabaseProjectID] = p.Slug
	}

	byName := map[string]*volumeOverview{}
	outVols := make([]*volumeOverview, 0, len(vols))
	for _, v := range vols {
		mode, loc := projects.DescribeVolume(v)
		vo := &volumeOverview{Volume: v, StorageMode: mode, Location: loc, UsedBy: []string{}}
		vo.Managed = v.Labels["com.supabase-manager.storage"] != ""
		vo.Project = slugs[v.Labels[docker.ProjectLabel]]
		if sz, ok := sizes[v.Name]; ok {
			vo.Size = &sz
		}
		byName[v.Name] = vo
		outVols = append(outVols, vo)
	}

	outCts := make([]containerMountsView, 0, len(cts))
	for _, ct := range cts {
		cv := containerMountsView{ID: ct.ID, Name: ct.Name, Image: ct.Image, State: ct.State, Status: ct.Status, Project: slugs[ct.Project], Mounts: []mountView{}}
		if ct.Project != "" && cv.Project == "" {
			cv.Project = ct.Project
		}
		for _, m := range ct.Mounts {
			mv := mountView{Mount: m, Location: m.Source}
			switch m.Type {
			case "volume":
				if vo := byName[m.Name]; vo != nil {
					mv.StorageMode, mv.Location, mv.Managed = vo.StorageMode, vo.Location, vo.Managed
					vo.UsedBy = append(vo.UsedBy, ct.Name)
					if vo.Managed && vo.StorageMode != models.StorageDocker {
						cv.CustomStorage = true
					}
				}
			case "bind":
				mv.StorageMode = models.StorageHost
			}
			cv.Mounts = append(cv.Mounts, mv)
		}
		outCts = append(outCts, cv)
	}

	resp := fiber.Map{"containers": outCts, "volumes": outVols}
	if info, err := dc.Info(ctx); err == nil {
		resp["docker_root"] = info.DockerRootDir
	}
	return c.JSON(resp)
}
