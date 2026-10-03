package httpapi

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/configtoml"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/ports"
	"github.com/supabase-manager/manager/internal/projects"
	"github.com/supabase-manager/manager/internal/supabase"
)

const localsProject = "project"

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var ve *projects.ValidationError
	switch {
	case errors.As(err, &ve):
		return fiber.NewError(fiber.StatusBadRequest, ve.Msg)
	case errors.Is(err, projects.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	case errors.Is(err, supabase.ErrBusy):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return err
}

func (s *Server) loadProject(c fiber.Ctx) error {
	p, err := s.projects.Get(c.Params("slug"))
	if err != nil {
		return mapErr(err)
	}
	c.Locals(localsProject, p)
	return c.Next()
}

func project(c fiber.Ctx) *models.Project {
	return c.Locals(localsProject).(*models.Project)
}

type projectView struct {
	models.Project
	Ports      any  `json:"ports"`
	RunningJob uint `json:"running_job"`
}

func (s *Server) view(c fiber.Ctx, p *models.Project) projectView {
	v := projectView{Project: *p, RunningJob: s.projects.Runner.RunningJob(p.ID)}
	if f, err := s.projects.LoadConfig(p); err == nil {
		v.Ports = f.Settings().Ports
	}
	return v
}

func (s *Server) listProjects(c fiber.Ctx) error {
	ps, err := s.projects.List()
	if err != nil {
		return err
	}
	out := make([]projectView, 0, len(ps))
	for i := range ps {
		s.projects.RefreshStatus(c.Context(), &ps[i])
		out = append(out, s.view(c, &ps[i]))
	}
	return c.JSON(out)
}

func (s *Server) previewPorts(c fiber.Ctx) error {
	p, err := s.projects.PreviewPorts()
	if err != nil {
		return err
	}
	return c.JSON(p)
}

func (s *Server) createProject(c fiber.Ctx) error {
	var in projects.CreateInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := s.projects.Create(c.Context(), in, currentClaims(c).UserID)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "project.create", p.Slug, fiber.Map{"port_base": p.PortBase})
	resp := fiber.Map{"project": s.view(c, p)}
	if in.Start {
		if job, err := s.projects.StartJob(p, currentClaims(c).UserID, "start"); err == nil {
			resp["job"] = job
		}
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
}

func (s *Server) importProject(c fiber.Ctx) error {
	var in projects.ImportInput
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := s.projects.Import(c.Context(), in, currentClaims(c).UserID)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "project.import", p.Slug, fiber.Map{"path": p.Path})
	return c.Status(fiber.StatusCreated).JSON(s.view(c, p))
}

func (s *Server) getProject(c fiber.Ctx) error {
	p := project(c)
	s.projects.RefreshStatus(c.Context(), p)
	return c.JSON(s.view(c, p))
}

func (s *Server) updateProject(c fiber.Ctx) error {
	p := project(c)
	var in struct {
		Name string `json:"name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.Name == "" || len(in.Name) > 255 {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	p.Name = in.Name
	if err := s.db.Model(p).Update("name", in.Name).Error; err != nil {
		return err
	}
	return c.JSON(s.view(c, p))
}

func (s *Server) deleteProject(c fiber.Ctx) error {
	p := project(c)
	var in struct {
		Confirm     string `json:"confirm"`
		DeleteFiles bool   `json:"delete_files"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.Confirm != p.Slug {
		return fiber.NewError(fiber.StatusBadRequest, "type the project slug to confirm")
	}
	if err := s.projects.Delete(c.Context(), p, in.DeleteFiles); err != nil {
		return mapErr(err)
	}
	s.audit(c, "project.delete", p.Slug, fiber.Map{"delete_files": in.DeleteFiles})
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) runJob(c fiber.Ctx, action string, args ...string) error {
	p := project(c)
	job, err := s.projects.StartJob(p, currentClaims(c).UserID, args...)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, action, p.Slug, fiber.Map{"job_id": job.ID})
	return c.Status(fiber.StatusAccepted).JSON(job)
}

func (s *Server) startProject(c fiber.Ctx) error {
	return s.runJob(c, "project.start", "start")
}

func (s *Server) stopProject(c fiber.Ctx) error {
	return s.runJob(c, "project.stop", "stop")
}

func (s *Server) restartProject(c fiber.Ctx) error {
	p := project(c)
	job, err := s.projects.Restart(p, currentClaims(c).UserID)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "project.restart", p.Slug, fiber.Map{"job_id": job.ID})
	return c.Status(fiber.StatusAccepted).JSON(job)
}

func (s *Server) projectStatus(c fiber.Ctx) error {
	p := project(c)
	status := s.projects.RefreshStatus(c.Context(), p)
	resp := fiber.Map{"status": status, "running_job": s.projects.Runner.RunningJob(p.ID)}
	if status == models.StatusRunning || status == models.StatusError {
		resp["details"] = s.projects.Status(c.Context(), p)
	}
	return c.JSON(resp)
}

func (s *Server) listJobs(c fiber.Ctx) error {
	var jobs []models.Job
	err := s.db.Select("id", "project_id", "command", "status", "exit_code", "started_by", "created_at", "finished_at").
		Where("project_id = ?", project(c).ID).Order("id desc").Limit(50).Find(&jobs).Error
	if err != nil {
		return err
	}
	return c.JSON(jobs)
}

// ---- config ----

func (s *Server) loadConfig(c fiber.Ctx) (*configtoml.File, error) {
	f, err := s.projects.LoadConfig(project(c))
	if err != nil {
		return nil, fiber.NewError(fiber.StatusInternalServerError, "cannot read config.toml: "+err.Error())
	}
	return f, nil
}

func (s *Server) getConfig(c fiber.Ctx) error {
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	return c.JSON(f.Settings())
}

func (s *Server) updatePorts(c fiber.Ctx) error {
	p := project(c)
	var in struct {
		API       int `json:"api"`
		DB        int `json:"db"`
		Shadow    int `json:"shadow"`
		Pooler    int `json:"pooler"`
		Studio    int `json:"studio"`
		SMTP      int `json:"smtp"`
		Analytics int `json:"analytics"`
		Inspector int `json:"inspector"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	np := f.Settings().Ports
	np.API, np.DB, np.Shadow, np.Pooler = in.API, in.DB, in.Shadow, in.Pooler
	np.Studio, np.SMTP, np.Analytics, np.Inspector = in.Studio, in.SMTP, in.Analytics, in.Inspector
	if err := s.projects.UpdatePorts(p, np); err != nil {
		return mapErr(err)
	}
	s.audit(c, "config.ports", p.Slug, np)
	return s.getConfig(c)
}

func (s *Server) saveConfig(c fiber.Ctx, action string, apply func(f *configtoml.File) error) error {
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	if err := apply(f); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := f.Save(); err != nil {
		return err
	}
	s.audit(c, action, project(c).Slug, nil)
	return c.JSON(f.Settings())
}

func (s *Server) updateServices(c fiber.Ctx) error {
	var in configtoml.Services
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	return s.saveConfig(c, "config.services", func(f *configtoml.File) error { return f.SetServices(in) })
}

func (s *Server) updateAuth(c fiber.Ctx) error {
	var in configtoml.AuthSettings
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	return s.saveConfig(c, "config.auth", func(f *configtoml.File) error { return f.SetAuth(in) })
}

func (s *Server) getRawConfig(c fiber.Ctx) error {
	b, err := os.ReadFile(projects.ConfigPath(project(c)))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"content": string(b)})
}

func (s *Server) putRawConfig(c fiber.Ctx) error {
	p := project(c)
	var in struct {
		Content string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	f := &configtoml.File{Path: projects.ConfigPath(p)}
	if err := f.SetText(in.Content); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if id := f.Settings().ProjectID; id != "" && id != p.SupabaseProjectID {
		return fiber.NewError(fiber.StatusBadRequest, "project_id cannot be changed here (managed value: "+p.SupabaseProjectID+")")
	}
	np := f.Settings().Ports
	base := np.API - ports.OffsetAPI
	var n int64
	s.db.Model(&models.Project{}).Where("id <> ? AND (port_base = ? OR inspector_port = ?)", p.ID, base, np.Inspector).Count(&n)
	if n > 0 {
		return fiber.NewError(fiber.StatusBadRequest, "ports overlap with another managed project")
	}
	if err := f.Save(); err != nil {
		return err
	}
	s.db.Model(p).Updates(map[string]any{"port_base": base, "inspector_port": np.Inspector})
	s.audit(c, "config.raw", p.Slug, nil)
	return c.JSON(fiber.Map{"content": f.Text()})
}

// ---- auth providers ----

type providerView struct {
	configtoml.Provider
	SecretStored bool   `json:"secret_stored"`
	CallbackURL  string `json:"callback_url"`
}

func (s *Server) providerView(c fiber.Ctx, f *configtoml.File, name string) providerView {
	pr := f.Provider(name)
	v := providerView{Provider: pr, SecretStored: s.projects.HasSecret(project(c), pr.SecretEnv)}
	v.CallbackURL = "http://127.0.0.1:" + itoa(f.Settings().Ports.API) + "/auth/v1/callback"
	return v
}

func (s *Server) listProviders(c fiber.Ctx) error {
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	out := make([]providerView, 0, len(configtoml.Providers))
	for _, name := range configtoml.Providers {
		out = append(out, s.providerView(c, f, name))
	}
	return c.JSON(out)
}

func (s *Server) getProvider(c fiber.Ctx) error {
	name := c.Params("provider")
	if !configtoml.IsProvider(name) {
		return fiber.NewError(fiber.StatusNotFound, "unknown provider")
	}
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	return c.JSON(s.providerView(c, f, name))
}

func (s *Server) putProvider(c fiber.Ctx) error {
	p := project(c)
	name := c.Params("provider")
	if !configtoml.IsProvider(name) {
		return fiber.NewError(fiber.StatusNotFound, "unknown provider")
	}
	var in configtoml.Provider
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	in.Name = name
	f, err := s.loadConfig(c)
	if err != nil {
		return err
	}
	in.SecretEnv = f.Provider(name).SecretEnv
	if in.Secret != "" {
		if err := s.projects.SetSecret(p, in.SecretEnv, in.Secret); err != nil {
			return mapErr(err)
		}
	}
	if in.Enabled && in.ClientID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "client ID is required to enable a provider")
	}
	if err := f.SetProvider(in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := f.Save(); err != nil {
		return err
	}
	s.audit(c, "auth.provider", p.Slug, fiber.Map{"provider": name, "enabled": in.Enabled})
	return c.JSON(s.providerView(c, f, name))
}

// ---- secrets ----

func (s *Server) listSecrets(c fiber.Ctx) error {
	out, err := s.projects.ListSecrets(project(c))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) putSecret(c fiber.Ctx) error {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.SetSecret(project(c), in.Key, in.Value); err != nil {
		return mapErr(err)
	}
	s.audit(c, "secret.set", project(c).Slug, fiber.Map{"key": in.Key})
	return s.listSecrets(c)
}

func (s *Server) deleteSecret(c fiber.Ctx) error {
	key := c.Params("key")
	if err := s.projects.DeleteSecret(project(c), key); err != nil {
		return err
	}
	s.audit(c, "secret.delete", project(c).Slug, fiber.Map{"key": key})
	return c.SendStatus(fiber.StatusNoContent)
}

// ---- system ----

func (s *Server) systemInfo(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	info := fiber.Map{
		"projects_root": s.cfg.ProjectsRoot,
		"db_driver":     s.cfg.DBDriver,
		"go_version":    runtime.Version(),
		"docker":        s.projects.Docker.Ping(ctx) == nil,
	}
	ci := s.cli.Info(ctx)
	info["supabase_cli"] = ci.Version
	info["supabase_cli_update"] = ci.UpdateAvailable
	if ci.Version == "" {
		info["supabase_cli_error"] = ci.VersionError
		if !ci.Installed {
			info["supabase_cli_error"] = "Supabase CLI not found"
		}
	}
	return c.JSON(info)
}
