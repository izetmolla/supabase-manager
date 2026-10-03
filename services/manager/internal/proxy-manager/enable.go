package proxymanager

import (
	"context"
	"errors"
	"log"
	"time"

	pb "github.com/supabase-manager/shared/proxyagent/v1"
)

// ErrDisabled is returned by API calls while the Proxy Manager is turned off.
var ErrDisabled = errors.New("the Proxy Manager is disabled; enable it in System settings")

const settingsKey = "proxy_manager"

// Settings is the instance-wide Proxy Manager switch.
type Settings struct {
	Enabled bool `json:"enabled"`
}

// Enabled reports whether the Proxy Manager is turned on. It is off until an admin enables it.
func (s *Service) Enabled() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.enabled
}

// Settings returns the stored switch.
func (s *Service) Settings() Settings { return Settings{Enabled: s.Enabled()} }

// SetEnabled turns the Proxy Manager on or off. Turning it off stops every proxy container and
// the background work (agent connections, health checks, certificate renewal) but keeps the
// configuration; turning it on starts the instances that were deployed before.
func (s *Service) SetEnabled(ctx context.Context, on bool) (Settings, error) {
	s.stateMu.Lock()
	if s.enabled == on {
		s.stateMu.Unlock()
		return Settings{Enabled: on}, nil
	}
	if err := s.settings.Put(settingsKey, Settings{Enabled: on}); err != nil {
		s.stateMu.Unlock()
		return Settings{}, err
	}
	s.enabled = on
	if on {
		s.startBackground()
	} else {
		s.stopBackground()
	}
	s.stateMu.Unlock()

	if on {
		s.startContainers(ctx)
	} else {
		s.stopContainers(ctx)
	}
	return Settings{Enabled: on}, nil
}

// Run loads the switch and runs the background work while the Proxy Manager is enabled. It
// returns when ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	var st Settings
	stored, err := s.settings.Get(settingsKey, &st)
	if err != nil {
		log.Printf("proxy manager: read settings: %v", err)
	}
	s.migrateInstances()
	var first uint
	if !stored && err == nil && s.opts.Bootstrap != nil {
		if first, err = s.bootstrap(); err != nil {
			log.Printf("proxy manager: bootstrap: %v", err)
		} else {
			st.Enabled = true
		}
	}
	s.stateMu.Lock()
	s.baseCtx = ctx
	s.enabled = st.Enabled
	if s.enabled {
		s.startBackground()
	}
	s.stateMu.Unlock()
	if first != 0 {
		go s.deployBootstrap(ctx, first)
	}
	<-ctx.Done()
	s.stateMu.Lock()
	s.stopBackground()
	s.stateMu.Unlock()
}

// bootstrap turns the Proxy Manager on and creates the install-time instance when there is
// none yet. It returns the instance to deploy (0 when instances already exist).
func (s *Service) bootstrap() (uint, error) {
	b := s.opts.Bootstrap
	if err := s.settings.Put(settingsKey, Settings{Enabled: true}); err != nil {
		return 0, err
	}
	log.Printf("proxy manager: enabled at install time (%s, HTTP %d, HTTPS %d)", b.Kind, b.HTTPPort, b.HTTPSPort)
	var n int64
	s.db.Model(&ProxyInstance{}).Count(&n)
	if n > 0 {
		return 0, nil
	}
	in := &ProxyInstance{
		Name: "default", Kind: b.Kind, HTTPPort: b.HTTPPort, HTTPSPort: b.HTTPSPort,
		TLSALPN: b.Kind == "nginx" && b.HTTPSPort > 0, Enabled: true,
	}
	if err := s.CreateInstance(in); err != nil {
		return 0, err
	}
	return in.ID, nil
}

func (s *Service) deployBootstrap(ctx context.Context, id uint) {
	c, cancel := context.WithTimeout(ctx, deployTimeout)
	defer cancel()
	if _, err := s.Deploy(c, id, 0, RevisionKindDeploy, "created at install time", func(string) {}); err != nil {
		log.Printf("proxy manager: deploy of the install-time instance failed (deploy it from Proxy Manager > Instances): %v", err)
		return
	}
	log.Printf("proxy manager: install-time instance %d is running", id)
}

// startBackground must be called with stateMu held.
func (s *Service) startBackground() {
	if s.baseCtx == nil || s.stopBg != nil {
		return
	}
	ctx, cancel := context.WithCancel(s.baseCtx)
	s.stopBg = cancel
	go s.health.run(ctx)
	go s.renewLoop(ctx)
	go s.agents.serve(ctx)
}

// stopBackground must be called with stateMu held.
func (s *Service) stopBackground() {
	if s.stopBg != nil {
		s.stopBg()
		s.stopBg = nil
	}
	s.health.reset()
}

func (s *Service) stopContainers(ctx context.Context) {
	var list []ProxyInstance
	s.db.Find(&list)
	for i := range list {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		st, err := s.dc.ContainerState(c, list[i].ContainerName())
		if err == nil && st.Running {
			err = s.dc.StopContainer(c, list[i].ContainerName())
		}
		if err != nil {
			log.Printf("proxy manager: stop %s: %v", list[i].ContainerName(), err)
		}
		cancel()
	}
}

func (s *Service) startContainers(ctx context.Context) {
	var list []ProxyInstance
	s.db.Where("enabled = ? AND deployed_revision_id <> 0", true).Find(&list)
	for i := range list {
		c, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if err := s.ensureContainer(c, &list[i], func(string) {}); err != nil {
			log.Printf("proxy manager: start %s: %v", list[i].ContainerName(), err)
		}
		cancel()
	}
}

// migrateInstances moves instances off the plain nginx and Traefik images (which have no agent)
// to the proxy images, and gives every instance an agent token.
func (s *Service) migrateInstances() {
	var list []ProxyInstance
	s.db.Find(&list)
	for i := range list {
		in := &list[i]
		updates := map[string]any{}
		if legacyImage(in.Image) {
			updates["image"] = s.DefaultImage(in.Kind)
		}
		if s.decrypt(in.TokenEncrypted) == "" {
			updates["token_encrypted"] = s.encrypt(randomToken())
		}
		if len(updates) > 0 {
			if err := s.db.Model(in).Updates(updates).Error; err != nil {
				log.Printf("proxy manager: migrate instance %d: %v", in.ID, err)
			}
		}
	}
}

// resync pushes the deployed revision to an agent whose configuration differs from it, e.g. after
// its container was recreated with an empty config volume. A deploy in progress takes priority.
func (s *Service) resync(ctx context.Context, id uint) {
	if !s.deployMu.TryLock() {
		return
	}
	defer s.deployMu.Unlock()
	in, err := s.GetInstance(id)
	if err != nil || in.DeployedRevisionID == 0 {
		return
	}
	files, err := s.revisionFiles(in.DeployedRevisionID)
	if err != nil {
		log.Printf("proxy manager: resync instance %d: %v", id, err)
		return
	}
	apply := &pb.Apply{Files: agentFiles(files), Checksum: in.DeployedChecksum, AdminPort: uint32(in.AdminPort)}
	c, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := s.agents.call(c, id, &pb.ManagerMessage{Msg: &pb.ManagerMessage_Apply{Apply: apply}})
	switch {
	case err != nil:
		log.Printf("proxy manager: resync instance %d: %v", id, err)
	case !res.Ok:
		log.Printf("proxy manager: resync instance %d: %s", id, res.Error)
	default:
		log.Printf("proxy manager: instance %d resynced to its deployed configuration", id)
	}
}
