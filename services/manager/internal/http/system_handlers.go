package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/projects"
)

// ---- Supabase CLI ----

func (s *Server) cliInfo(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	_, _ = s.cli.CheckLatest(ctx, false)
	return c.JSON(s.cli.Info(ctx))
}

func (s *Server) cliCheck(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	_, _ = s.cli.CheckLatest(ctx, true)
	return c.JSON(s.cli.Info(ctx))
}

func (s *Server) cliInstall(c fiber.Ctx) error {
	var in struct {
		Version string `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.cli.Install(in.Version); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	s.audit(c, "cli.install", "supabase", fiber.Map{"version": in.Version})
	return c.Status(fiber.StatusAccepted).JSON(s.cli.Info(c.Context()))
}

func (s *Server) cliUseSystem(c fiber.Ctx) error {
	if err := s.cli.UseConfigured(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	s.audit(c, "cli.use_system", "supabase", nil)
	return c.JSON(s.cli.Info(c.Context()))
}

func (s *Server) cliSettings(c fiber.Ctx) error {
	var in struct {
		AutoUpdate bool `json:"auto_update"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.cli.SetAutoUpdate(in.AutoUpdate); err != nil {
		return err
	}
	s.audit(c, "cli.settings", "supabase", in)
	return c.JSON(s.cli.Info(c.Context()))
}

// ---- manager self-update ----

func (s *Server) updateInfo(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	_, _ = s.updater.CheckLatest(ctx, false)
	return c.JSON(s.updater.Info(ctx))
}

func (s *Server) updateCheck(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	_, _ = s.updater.CheckLatest(ctx, true)
	return c.JSON(s.updater.Info(ctx))
}

func (s *Server) updateApply(c fiber.Ctx) error {
	var in struct {
		Version string `json:"version"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	if err := s.updater.Apply(ctx, in.Version); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	s.audit(c, "manager.update", "manager", fiber.Map{"version": in.Version})
	return c.Status(fiber.StatusAccepted).JSON(s.updater.Info(ctx))
}

// ---- networks ----

func (s *Server) getNetworkDefaults(c fiber.Ctx) error {
	return c.JSON(s.projects.NetworkDefaults())
}

func (s *Server) putNetworkDefaults(c fiber.Ctx) error {
	var in models.NetworkDefaults
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	out, err := s.projects.SetNetworkDefaults(in)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "network.defaults", "system", out)
	return c.JSON(out)
}

type dockerNetworkView struct {
	docker.Network
	UsedBy string `json:"used_by,omitempty"`
}

func (s *Server) listDockerNetworks(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	nets, err := s.projects.Docker.Networks(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	ps, err := s.projects.List()
	if err != nil {
		return err
	}
	used := map[string]string{}
	for i := range ps {
		used[projects.NetworkName(&ps[i])] = ps[i].Slug
	}
	out := make([]dockerNetworkView, 0, len(nets))
	for _, n := range nets {
		out = append(out, dockerNetworkView{Network: n, UsedBy: used[n.Name]})
	}
	return c.JSON(out)
}

func (s *Server) freeSubnet(c fiber.Ctx) error {
	var in struct {
		Pool    string `json:"pool"`
		Prefix  int    `json:"prefix"`
		Project string `json:"project"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.Pool == "" {
		d := s.projects.NetworkDefaults()
		in.Pool, in.Prefix = d.SubnetPool, d.SubnetPrefix
		if in.Pool == "" {
			in.Pool = "10.210.0.0/16"
		}
	}
	if in.Prefix == 0 {
		in.Prefix = 24
	}
	var exclude uint
	if in.Project != "" {
		if p, err := s.projects.Get(in.Project); err == nil {
			exclude = p.ID
		}
	}
	sub, gw, err := s.projects.FreeSubnet(c.Context(), in.Pool, in.Prefix, exclude)
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"subnet": sub, "gateway": gw})
}

func (s *Server) getProjectNetwork(c fiber.Ctx) error {
	p := project(c)
	name := projects.NetworkName(p)
	resp := fiber.Map{
		"config":       p.Network,
		"name":         name,
		"connect_host": projects.ConnectHost(p),
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	n, err := s.projects.Docker.Network(ctx, name)
	switch {
	case err == nil:
		resp["live"] = n
	case !errors.Is(err, docker.ErrNotFound):
		resp["live_error"] = err.Error()
	}
	return c.JSON(resp)
}

func (s *Server) putProjectNetwork(c fiber.Ctx) error {
	p := project(c)
	var in models.NetworkConfig
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.SetNetwork(p, in); err != nil {
		return mapErr(err)
	}
	s.forgetUpstreams(p.Slug)
	s.audit(c, "network.update", p.Slug, p.Network)
	return s.getProjectNetwork(c)
}
