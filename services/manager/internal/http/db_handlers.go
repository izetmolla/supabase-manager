package httpapi

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/projects"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ---- migrations & database ----

func (s *Server) dbURL(c fiber.Ctx, p *models.Project) string {
	if s.projects.RefreshStatus(c.Context(), p) != models.StatusRunning {
		return ""
	}
	f, err := s.projects.LoadConfig(p)
	if err != nil {
		return ""
	}
	return "postgresql://postgres:postgres@" + projects.ConnectHost(p) + ":" + itoa(f.Settings().Ports.DB) + "/postgres"
}

func (s *Server) listMigrations(c fiber.Ctx) error {
	p := project(c)
	out, err := s.projects.Migrations(c.Context(), p, s.dbURL(c, p))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) newMigration(c fiber.Ctx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	file, err := s.projects.NewMigration(c.Context(), project(c), in.Name)
	if err != nil {
		return mapErr(err)
	}
	s.audit(c, "migration.new", project(c).Slug, fiber.Map{"file": file})
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"file": file})
}

func (s *Server) getMigration(c fiber.Ctx) error {
	content, err := s.projects.ReadMigration(project(c), c.Params("file"))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"file": c.Params("file"), "content": content})
}

func (s *Server) putMigration(c fiber.Ctx) error {
	var in struct {
		Content string `json:"content"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.WriteMigration(project(c), c.Params("file"), in.Content); err != nil {
		return mapErr(err)
	}
	s.audit(c, "migration.edit", project(c).Slug, fiber.Map{"file": c.Params("file")})
	return c.JSON(fiber.Map{"file": c.Params("file"), "content": in.Content})
}

func (s *Server) dbUp(c fiber.Ctx) error {
	return s.runJob(c, "db.up", "migration", "up", "--local")
}

func (s *Server) dbReset(c fiber.Ctx) error {
	p := project(c)
	var in struct {
		Confirm string `json:"confirm"`
		NoSeed  bool   `json:"no_seed"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.Confirm != p.Slug {
		return fiber.NewError(fiber.StatusBadRequest, "type the project slug to confirm")
	}
	args, err := s.projects.DBResetArgs(c.Context(), p, in.NoSeed)
	if err != nil {
		return mapErr(err)
	}
	return s.runJob(c, "db.reset", args...)
}

func (s *Server) dbDiff(c fiber.Ctx) error {
	var in struct {
		File   string `json:"file"`
		Schema string `json:"schema"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	args := []string{"db", "diff", "--local"}
	if in.File != "" {
		if !projects.ValidName(in.File) {
			return fiber.NewError(fiber.StatusBadRequest, "migration names use lowercase letters, numbers, - and _")
		}
		args = append(args, "-f", in.File)
	}
	if in.Schema != "" {
		if !projects.ValidName(in.Schema) {
			return fiber.NewError(fiber.StatusBadRequest, "invalid schema name")
		}
		args = append(args, "--schema", in.Schema)
	}
	return s.runJob(c, "db.diff", args...)
}

func (s *Server) genTypes(c fiber.Ctx) error {
	out, err := s.projects.GenTypes(c.Context(), project(c), c.Query("lang", "typescript"))
	if err != nil {
		if mapped := mapErr(err); mapped != err {
			return mapped
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"content": out})
}

// ---- functions ----

func (s *Server) listFunctions(c fiber.Ctx) error {
	p := project(c)
	out, err := s.projects.Functions(p)
	if err != nil {
		return err
	}
	if p.Status == models.StatusRunning {
		s.projects.MountStudioFunctionsAsync(p)
	}
	return c.JSON(out)
}

func (s *Server) newFunction(c fiber.Ctx) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.NewFunction(c.Context(), project(c), in.Name); err != nil {
		if mapped := mapErr(err); mapped != err {
			return mapped
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	s.audit(c, "function.new", project(c).Slug, fiber.Map{"name": in.Name})
	return s.listFunctions(c)
}

// ---- containers ----

func (s *Server) listContainers(c fiber.Ctx) error {
	cts, err := s.projects.Docker.ListProject(c.Context(), project(c).SupabaseProjectID)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	return c.JSON(cts)
}

func (s *Server) findContainer(c fiber.Ctx) (*docker.Container, error) {
	cts, err := s.projects.Docker.ListProject(c.Context(), project(c).SupabaseProjectID)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	name := c.Params("name")
	for i := range cts {
		if cts[i].Service == name || cts[i].Name == name {
			return &cts[i], nil
		}
	}
	return nil, fiber.NewError(fiber.StatusNotFound, "container not found")
}

func (s *Server) projectStats(c fiber.Ctx) error {
	cts, err := s.projects.Docker.ListProject(c.Context(), project(c).SupabaseProjectID)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, err.Error())
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	type row struct {
		docker.Stats
		Service string `json:"service"`
	}
	out := make([]row, 0, len(cts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ct := range cts {
		if ct.State != "running" {
			continue
		}
		wg.Add(1)
		go func(ct docker.Container) {
			defer wg.Done()
			st, err := s.projects.Docker.Stats(ctx, ct.ID)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, row{Stats: st, Service: ct.Service})
			mu.Unlock()
		}(ct)
	}
	wg.Wait()
	return c.JSON(out)
}

func (s *Server) restartContainer(c fiber.Ctx) error {
	ct, err := s.findContainer(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), time.Minute)
	defer cancel()
	if err := s.projects.Docker.Restart(ctx, ct.ID); err != nil {
		return err
	}
	s.audit(c, "container.restart", project(c).Slug, fiber.Map{"container": ct.Name})
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) containerLogs(c fiber.Ctx) error {
	ct, err := s.findContainer(c)
	if err != nil {
		return err
	}
	tail := fiber.Query[int](c, "tail", 300)
	if tail < 1 || tail > 5000 {
		tail = 300
	}
	follow := c.Query("follow", "true") == "true"
	id := ct.ID
	dc := s.projects.Docker

	sseHeaders(c)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		lines := make(chan [2]string, 512)
		go func() {
			defer close(lines)
			_ = dc.Logs(ctx, id, tail, follow, func(stream, line string) error {
				select {
				case lines <- [2]string{stream, line}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					_ = writeEvent(w, "end", "")
					return
				}
				if writeEvent(w, "", fiber.Map{"stream": l[0], "line": l[1]}) != nil {
					return
				}
			case <-ping.C:
				if writePing(w) != nil {
					return
				}
			}
		}
	})
}

// ---- jobs ----

func (s *Server) getJob(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	var job models.Job
	if err := s.db.First(&job, id).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "job not found")
	}
	return c.JSON(job)
}

func (s *Server) streamJob(c fiber.Ctx) error {
	id := fiber.Params[uint](c, "id")
	var job models.Job
	if err := s.db.First(&job, id).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "job not found")
	}
	history, lines, done, cancel, live := s.projects.Runner.Subscribe(job.ID)

	sseHeaders(c)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		if !live {
			for _, l := range splitLines(job.Output) {
				if writeEvent(w, "", l) != nil {
					return
				}
			}
			_ = writeEvent(w, "done", fiber.Map{"status": job.Status, "exit_code": job.ExitCode})
			return
		}
		for _, l := range history {
			if writeEvent(w, "", l) != nil {
				return
			}
		}
		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case l := <-lines:
				if writeEvent(w, "", l) != nil {
					return
				}
			case status := <-done:
				for drained := false; !drained; {
					select {
					case l := <-lines:
						if writeEvent(w, "", l) != nil {
							return
						}
					default:
						drained = true
					}
				}
				var final models.Job
				s.db.Select("status", "exit_code").First(&final, job.ID)
				if final.Status == "" {
					final.Status = status
				}
				_ = writeEvent(w, "done", fiber.Map{"status": final.Status, "exit_code": final.ExitCode})
				return
			case <-ping.C:
				if writePing(w) != nil {
					return
				}
			}
		}
	})
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
