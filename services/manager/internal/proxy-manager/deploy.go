package proxymanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/proxy-manager/spec"
	pb "github.com/supabase-manager/shared/proxyagent/v1"
)

const deployTimeout = 10 * time.Minute

const (
	RevisionKindDeploy      = "deploy"
	RevisionKindRollback    = "rollback"
	RevisionKindCertificate = "certificate"
)

func (s *Service) storeFiles(files map[string]string) (string, error) {
	b, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	enc, err := s.cipher.Encrypt(string(b))
	if err != nil {
		return "", err
	}
	return enc, nil
}

func (s *Service) revisionFiles(revID uint) (map[string]string, error) {
	var rev ConfigRevision
	if err := s.db.First(&rev, revID).Error; err != nil {
		return nil, notFound(err)
	}
	dec, err := s.cipher.Decrypt(rev.FilesEncrypted)
	if err != nil {
		return nil, fmt.Errorf("revision %d cannot be decrypted: %w", rev.Number, err)
	}
	files := map[string]string{}
	return files, json.Unmarshal([]byte(dec), &files)
}

func (s *Service) ListRevisions(instanceID uint) ([]ConfigRevision, error) {
	var list []ConfigRevision
	return list, s.db.Where("instance_id = ?", instanceID).Order("number desc").Limit(100).Find(&list).Error
}

// RevisionFiles returns a revision's files with private keys redacted.
func (s *Service) RevisionFiles(instanceID, revID uint) (map[string]string, error) {
	in, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	var rev ConfigRevision
	if err := s.db.Where("instance_id = ?", instanceID).First(&rev, revID).Error; err != nil {
		return nil, notFound(err)
	}
	files, err := s.revisionFiles(revID)
	if err != nil {
		return nil, err
	}
	for k, v := range files {
		files[k] = s.redact(in, k, v, strings.HasSuffix(k, ".key"))
	}
	return files, nil
}

// ErrorPage returns the configured status and body of an error-page host (served to Traefik).
func (s *Service) ErrorPage(id uint) (int, string, error) {
	h, err := s.GetHost(id)
	if err != nil || h.Kind != spec.HostError {
		return 0, "", ErrNotFound
	}
	code := h.ErrorPage.Code
	if code == 0 {
		code = 404
	}
	return code, h.ErrorPage.Body, nil
}

type bind struct {
	ip    string
	port  int
	udp   bool
	owner string
}

func overlaps(a, b bind) bool {
	if a.port != b.port || a.udp != b.udp {
		return false
	}
	wild := func(ip string) bool { return ip == "" || ip == "0.0.0.0" || ip == "::" }
	return wild(a.ip) || wild(b.ip) || a.ip == b.ip
}

func (s *Service) instanceBinds(in *ProxyInstance, streams []spec.Stream) []bind {
	label := fmt.Sprintf("proxy %q", in.Name)
	var bs []bind
	if in.HTTPPort > 0 {
		bs = append(bs, bind{in.BindIP, in.HTTPPort, false, label + " (HTTP)"})
	}
	if in.HTTPSPort > 0 {
		bs = append(bs, bind{in.BindIP, in.HTTPSPort, false, label + " (HTTPS)"})
	}
	if in.AdminPort > 0 {
		bs = append(bs, bind{"127.0.0.1", in.AdminPort, false, label + " (admin API)"})
	}
	for _, st := range streams {
		bs = append(bs, bind{in.BindIP, st.ListenPort, st.Protocol == "udp", fmt.Sprintf("%s (stream %d/%s)", label, st.ListenPort, st.Protocol)})
	}
	return bs
}

// checkPorts refuses ports used by other proxies, the manager, the TLS-ALPN solver or
// Supabase projects.
func (s *Service) checkPorts(in *ProxyInstance, st *spec.State) error {
	mine := s.instanceBinds(in, st.Streams)
	for i := range mine {
		for j := i + 1; j < len(mine); j++ {
			if overlaps(mine[i], mine[j]) {
				return invalid(fmt.Sprintf("port %d is used twice: %s and %s", mine[i].port, mine[i].owner, mine[j].owner))
			}
		}
	}
	var others []bind
	if p := s.managerPort(); p > 0 {
		host, _, _ := net.SplitHostPort(s.opts.ManagerAddr)
		others = append(others, bind{host, p, false, "the manager"})
	}
	if p := s.alpnPort(); p > 0 {
		host, _, _ := net.SplitHostPort(s.opts.ALPNAddr)
		others = append(others, bind{host, p, false, "the TLS-ALPN-01 solver"})
	}
	if host, port, err := net.SplitHostPort(s.opts.AgentAddr); err == nil {
		if p, _ := strconv.Atoi(port); p > 0 {
			others = append(others, bind{host, p, false, "the proxy agent server"})
		}
	}
	var list []ProxyInstance
	s.db.Where("id <> ? AND enabled = ?", in.ID, true).Find(&list)
	for i := range list {
		ost, _, err := s.buildState(&list[i])
		if err != nil {
			continue
		}
		others = append(others, s.instanceBinds(&list[i], ost.Streams)...)
	}
	if ps, err := s.projects.List(); err == nil {
		for i := range ps {
			f, err := s.projects.LoadConfig(&ps[i])
			if err != nil {
				continue
			}
			for _, p := range f.Settings().Ports.List() {
				if p > 0 {
					others = append(others, bind{"", p, false, fmt.Sprintf("Supabase project %s", ps[i].Slug)})
				}
			}
		}
	}
	for _, m := range mine {
		for _, o := range others {
			if overlaps(m, o) {
				return invalid(fmt.Sprintf("port %d of %s is already used by %s", m.port, m.owner, o.owner))
			}
		}
	}
	return nil
}

// Deploy renders the drafts of an instance and applies them. On failure the previous
// configuration stays active and the attempt is recorded as a failed revision.
func (s *Service) Deploy(ctx context.Context, id, userID uint, kind, note string, logf func(string)) (*ConfigRevision, error) {
	s.deployMu.Lock()
	defer s.deployMu.Unlock()
	if logf == nil {
		logf = func(string) {}
	}
	in, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	if !in.Enabled {
		return nil, invalid("the instance is disabled; enable it before deploying")
	}
	st, _, err := s.buildState(in)
	if err != nil {
		return nil, err
	}
	if err := s.checkPorts(in, st); err != nil {
		return nil, err
	}
	out, err := s.Render(in)
	if err != nil {
		return nil, err
	}
	return s.applyFiles(ctx, in, out.Files, out.Warnings, userID, kind, note, logf)
}

func (s *Service) applyFiles(ctx context.Context, in *ProxyInstance, files map[string]string, warnings []string, userID uint, kind, note string, logf func(string)) (*ConfigRevision, error) {
	enc, err := s.storeFiles(files)
	if err != nil {
		return nil, err
	}
	var last ConfigRevision
	s.db.Where("instance_id = ?", in.ID).Order("number desc").Limit(1).Find(&last)
	rev := &ConfigRevision{
		InstanceID: in.ID, Number: last.Number + 1, Kind: kind, FilesEncrypted: enc,
		Checksum: checksum(files), Status: "deploying", Warnings: warnings, Note: note, CreatedBy: userID,
	}
	if err := s.db.Create(rev).Error; err != nil {
		return nil, err
	}

	if err := s.applyViaAgent(ctx, in, files, rev.Checksum, logf); err != nil {
		rev.Status, rev.Error = RevisionFailed, err.Error()
		s.db.Model(rev).Updates(map[string]any{"status": rev.Status, "error": rev.Error})
		return rev, err
	}
	now := time.Now()
	rev.Status = RevisionApplied
	s.db.Model(rev).Update("status", rev.Status)
	in.DeployedRevisionID, in.DeployedChecksum, in.DeployedAt = rev.ID, rev.Checksum, &now
	s.db.Model(in).Updates(map[string]any{"deployed_revision_id": rev.ID, "deployed_checksum": rev.Checksum, "deployed_at": now})
	logf(fmt.Sprintf("Revision %d is live", rev.Number))
	return rev, nil
}

func agentFiles(files map[string]string) map[string]*pb.File {
	out := make(map[string]*pb.File, len(files))
	for name, content := range files {
		mode := uint32(0o644)
		if strings.HasSuffix(name, ".key") {
			mode = 0o600
		}
		out[name] = &pb.File{Content: []byte(content), Mode: mode}
	}
	return out
}

// applyViaAgent makes sure the container runs, then hands the files to its agent, which
// validates them, switches to them and reloads the proxy (restoring the previous configuration
// itself when that fails).
func (s *Service) applyViaAgent(ctx context.Context, in *ProxyInstance, files map[string]string, sum string, logf func(string)) error {
	if err := s.ensureContainer(ctx, in, logf); err != nil {
		return err
	}
	logf("Waiting for the proxy agent")
	if err := s.agents.waitConnected(ctx, in.ID, agentConnectTimeout); err != nil {
		return fmt.Errorf("the proxy agent did not connect to the manager at %s: %w\n%s", s.agentDialAddr(), err, s.logTail(ctx, in, 15))
	}
	apply := &pb.Apply{Files: agentFiles(files), Checksum: sum, AdminPort: uint32(in.AdminPort)}
	logf(fmt.Sprintf("Sending %d files to the agent for validation and reload", len(files)))
	res, err := s.agents.call(ctx, in.ID, &pb.ManagerMessage{Msg: &pb.ManagerMessage_Apply{Apply: apply}})
	if err != nil {
		return err
	}
	if out := strings.TrimSpace(res.Output); out != "" {
		logf(out)
	}
	if !res.Ok {
		return errors.New(res.Error)
	}
	return nil
}

// Rollback re-deploys a stored revision as a new revision.
func (s *Service) Rollback(ctx context.Context, instanceID, revID, userID uint, logf func(string)) (*ConfigRevision, error) {
	s.deployMu.Lock()
	defer s.deployMu.Unlock()
	if logf == nil {
		logf = func(string) {}
	}
	in, err := s.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	var rev ConfigRevision
	if err := s.db.Where("instance_id = ?", instanceID).First(&rev, revID).Error; err != nil {
		return nil, notFound(err)
	}
	files, err := s.revisionFiles(revID)
	if err != nil {
		return nil, err
	}
	return s.applyFiles(ctx, in, files, rev.Warnings, userID, RevisionKindRollback, fmt.Sprintf("rollback to revision %d", rev.Number), logf)
}

// DeployJob runs Deploy as a background job (shown in the job panel).
func (s *Service) DeployJob(id, userID uint, note string) (*models.Job, error) {
	in, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	return s.runner.StartFunc(0, userID, fmt.Sprintf("deploy proxy %s", in.Name), deployTimeout, func(ctx context.Context, logf func(string)) error {
		_, err := s.Deploy(ctx, id, userID, RevisionKindDeploy, note, logf)
		return err
	}, nil)
}

// RollbackJob runs Rollback as a background job.
func (s *Service) RollbackJob(id, revID, userID uint) (*models.Job, error) {
	in, err := s.GetInstance(id)
	if err != nil {
		return nil, err
	}
	return s.runner.StartFunc(0, userID, fmt.Sprintf("roll back proxy %s", in.Name), deployTimeout, func(ctx context.Context, logf func(string)) error {
		_, err := s.Rollback(ctx, id, revID, userID, logf)
		return err
	}, nil)
}

// DeployAllJob deploys every instance with pending changes in one job.
func (s *Service) DeployAllJob(userID uint) (*models.Job, error) {
	return s.runner.StartFunc(0, userID, "deploy all proxies", 4*deployTimeout, func(ctx context.Context, logf func(string)) error {
		var list []ProxyInstance
		s.db.Where("enabled = ?", true).Order("id").Find(&list)
		var failed []string
		n := 0
		for i := range list {
			if !s.pending(&list[i]) {
				continue
			}
			n++
			logf(fmt.Sprintf("== %s (%s)", list[i].Name, list[i].Kind))
			if _, err := s.Deploy(ctx, list[i].ID, userID, RevisionKindDeploy, "", logf); err != nil {
				logf("error: " + err.Error())
				failed = append(failed, list[i].Name)
			}
		}
		if n == 0 {
			logf("Nothing to deploy")
		}
		if len(failed) > 0 {
			return fmt.Errorf("deploy failed for %s", strings.Join(failed, ", "))
		}
		return nil
	}, nil)
}

// Status summarizes the proxy manager for the overview and deploy bar.
type Status struct {
	PendingInstances []uint            `json:"pending_instances"`
	Instances        int               `json:"instances"`
	Running          int               `json:"running"`
	Hosts            int64             `json:"hosts"`
	Streams          int64             `json:"streams"`
	Upstreams        int64             `json:"upstreams"`
	Certificates     int64             `json:"certificates"`
	Expiring         []Certificate     `json:"expiring"`
	UnhealthyTargets []UnhealthyTarget `json:"unhealthy_targets"`
}

type UnhealthyTarget struct {
	UpstreamID uint   `json:"upstream_id"`
	Upstream   string `json:"upstream"`
	Address    string `json:"address"`
	Error      string `json:"error"`
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	st := &Status{PendingInstances: []uint{}, Expiring: []Certificate{}, UnhealthyTargets: []UnhealthyTarget{}}
	var list []ProxyInstance
	if err := s.db.Find(&list).Error; err != nil {
		return nil, err
	}
	st.Instances = len(list)
	for i := range list {
		if s.pending(&list[i]) {
			st.PendingInstances = append(st.PendingInstances, list[i].ID)
		}
		if c, err := s.dc.ContainerState(ctx, list[i].ContainerName()); err == nil && c.Running {
			st.Running++
		}
	}
	s.db.Model(&ProxyHost{}).Count(&st.Hosts)
	s.db.Model(&ProxyStream{}).Count(&st.Streams)
	s.db.Model(&ProxyUpstream{}).Count(&st.Upstreams)
	s.db.Model(&Certificate{}).Count(&st.Certificates)
	s.db.Where("not_after IS NOT NULL AND not_after < ?", time.Now().Add(30*24*time.Hour)).Order("not_after").Find(&st.Expiring)
	var ups []ProxyUpstream
	s.db.Find(&ups)
	names := map[uint]string{}
	for _, u := range ups {
		names[u.ID] = u.Name
	}
	for uid, targets := range s.health.snapshot() {
		for _, t := range targets {
			if t.State == "down" {
				st.UnhealthyTargets = append(st.UnhealthyTargets, UnhealthyTarget{UpstreamID: uid, Upstream: names[uid], Address: t.Address, Error: t.Error})
			}
		}
	}
	return st, nil
}
