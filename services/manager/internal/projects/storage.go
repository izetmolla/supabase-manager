package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/settings"
)

const (
	labelStorage = "com.supabase-manager.storage"
	labelHelper  = "com.supabase-manager.helper"
	labelCompose = "com.docker.compose.project"

	// helperImage runs mkdir/cp for storage preparation and data moves.
	helperImage = "alpine:3"

	storageTimeout = 2 * time.Hour
)

var (
	serverRe    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.:-]{0,252}$`)
	mountOptsRe = regexp.MustCompile(`^[a-zA-Z0-9_=.,:/-]{0,255}$`)
)

// StorageVolume is one persistent volume the Supabase CLI creates for a project.
type StorageVolume struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Mount string `json:"mount"`
}

// StorageVolumes are the volumes holding data worth keeping; the Edge Runtime cache is left to Docker.
func StorageVolumes(p *models.Project) []StorageVolume {
	return []StorageVolume{
		{Key: "db", Name: "supabase_db_" + p.SupabaseProjectID, Label: "Database (Postgres data)", Mount: "/var/lib/postgresql/data"},
		{Key: "storage", Name: "supabase_storage_" + p.SupabaseProjectID, Label: "Storage (uploaded files)", Mount: "/mnt"},
	}
}

func storageMode(cfg models.StorageConfig) string {
	if cfg.Mode == "" {
		return models.StorageDocker
	}
	return cfg.Mode
}

// CustomStorage reports whether the project's volumes live outside Docker's own storage.
func CustomStorage(p *models.Project) bool { return storageMode(p.Storage) != models.StorageDocker }

func cleanAbs(p, what string) (string, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		return "", invalid("the %s must be an absolute path such as /srv/supabase-data", what)
	}
	p = filepath.Clean(p)
	if p == "/" {
		return "", invalid("the %s cannot be the root folder", what)
	}
	for _, sys := range []string{"/proc", "/sys", "/dev", "/run", "/etc", "/boot", "/bin", "/sbin", "/lib", "/usr"} {
		if p == sys || strings.HasPrefix(p, sys+"/") {
			return "", invalid("the %s cannot be inside %s", what, sys)
		}
	}
	return p, nil
}

// NormalizeStorage validates cfg and returns the form stored in the database.
func NormalizeStorage(cfg models.StorageConfig) (models.StorageConfig, error) {
	switch strings.TrimSpace(cfg.Mode) {
	case "", models.StorageDocker:
		return models.StorageConfig{Mode: models.StorageDocker}, nil

	case models.StorageHost:
		out := models.StorageConfig{Mode: models.StorageHost}
		if strings.TrimSpace(cfg.Path) != "" {
			p, err := cleanAbs(cfg.Path, "host folder")
			if err != nil {
				return cfg, err
			}
			out.Path = p
		}
		return out, nil

	case models.StorageNFS:
		out := models.StorageConfig{Mode: models.StorageNFS, Server: strings.TrimSpace(cfg.Server), MountOptions: strings.TrimSpace(cfg.MountOptions)}
		if !serverRe.MatchString(out.Server) {
			return cfg, invalid("enter the NFS server as a host name or IP address")
		}
		export, err := cleanAbs(cfg.Export, "NFS export")
		if err != nil {
			return cfg, err
		}
		out.Export = export
		if !mountOptsRe.MatchString(out.MountOptions) || strings.Contains(out.MountOptions, "addr=") {
			return cfg, invalid("mount options are comma separated, e.g. nfsvers=4.1,rw (addr is set from the server)")
		}
		return out, nil

	case models.StorageDriver:
		out := models.StorageConfig{Mode: models.StorageDriver, Driver: strings.TrimSpace(cfg.Driver)}
		if !driverRe.MatchString(out.Driver) {
			return cfg, invalid("choose a volume driver")
		}
		if len(cfg.DriverOptions) > maxNetworkOptions {
			return cfg, invalid("too many driver options")
		}
		for k, v := range cfg.DriverOptions {
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if k == "" {
				continue
			}
			if !optionKeyRe.MatchString(k) || len(v) > 1024 {
				return cfg, invalid("invalid driver option %q", k)
			}
			if out.DriverOptions == nil {
				out.DriverOptions = map[string]string{}
			}
			out.DriverOptions[k] = v
		}
		if out.Driver == "local" && len(out.DriverOptions) == 0 {
			return cfg, invalid("the local driver without options is the 'Docker volumes' mode")
		}
		if dev, ok := out.DriverOptions["device"]; ok && !strings.Contains(dev, "{volume}") {
			return cfg, invalid("the device option must contain {volume}, otherwise the database and storage share one location")
		}
		return out, nil
	}
	return cfg, invalid("unknown storage mode %q", cfg.Mode)
}

// volumeTarget is where one volume of a project lives under its storage settings.
type volumeTarget struct {
	Driver   string
	Opts     map[string]string
	Location string
	// prep lets a helper container reach the parent of the data folder so it can create it.
	prep     *docker.HelperMount
	prepPath string
}

func (t volumeTarget) hash() string {
	b, _ := json.Marshal(struct {
		D string
		O map[string]string
	}{t.Driver, t.Opts})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// HostRoot is the folder that holds a project's volumes in host mode.
func HostRoot(p *models.Project, cfg models.StorageConfig) string {
	if cfg.Path == "" {
		return filepath.Join(p.Path, "volumes")
	}
	return filepath.Join(cfg.Path, p.Slug)
}

func nfsMountOpts(cfg models.StorageConfig) string {
	opts := cfg.MountOptions
	if opts == "" {
		opts = "rw,nfsvers=4"
	}
	return "addr=" + cfg.Server + "," + opts
}

func targetFor(p *models.Project, cfg models.StorageConfig, v StorageVolume) volumeTarget {
	switch storageMode(cfg) {
	case models.StorageHost:
		root := HostRoot(p, cfg)
		dir := filepath.Join(root, v.Key)
		return volumeTarget{
			Driver: "local", Opts: map[string]string{"type": "none", "o": "bind", "device": dir}, Location: dir,
			prep: &docker.HelperMount{Target: "/base", Bind: root}, prepPath: v.Key,
		}
	case models.StorageNFS:
		o := nfsMountOpts(cfg)
		dev := path.Join(cfg.Export, p.Slug, v.Key)
		return volumeTarget{
			Driver: "local", Opts: map[string]string{"type": "nfs", "o": o, "device": ":" + dev}, Location: cfg.Server + ":" + dev,
			prep:     &docker.HelperMount{Target: "/base", Driver: "local", DriverOpts: map[string]string{"type": "nfs", "o": o, "device": ":" + cfg.Export}},
			prepPath: p.Slug + "/" + v.Key,
		}
	case models.StorageDriver:
		r := strings.NewReplacer("{project}", p.Slug, "{volume}", v.Key)
		opts := map[string]string{}
		for k, val := range cfg.DriverOptions {
			opts[k] = r.Replace(val)
		}
		loc := cfg.Driver + " driver"
		if dev := opts["device"]; dev != "" {
			loc += " (" + dev + ")"
		}
		return volumeTarget{Driver: cfg.Driver, Opts: opts, Location: loc}
	}
	return volumeTarget{Driver: "local", Location: "Docker volume " + v.Name}
}

func storageLabels(p *models.Project, mode, hash string) map[string]string {
	return map[string]string{
		docker.ProjectLabel: p.SupabaseProjectID,
		labelCompose:        p.SupabaseProjectID,
		labelProject:        p.Slug,
		labelStorage:        mode,
		labelHash:           hash,
	}
}

// DescribeVolume tells where an existing volume keeps its data.
func DescribeVolume(v docker.Volume) (mode, location string) {
	o := v.Options
	switch {
	case v.Driver != "local":
		mode = models.StorageDriver
	case o["type"] == "nfs" || o["type"] == "nfs4":
		mode = models.StorageNFS
	case strings.Contains(o["o"], "bind"):
		mode = models.StorageHost
	case len(o) > 0:
		mode = models.StorageDriver
	default:
		mode = models.StorageDocker
	}
	switch {
	case o["device"] != "" && mode == models.StorageNFS:
		addr := ""
		for part := range strings.SplitSeq(o["o"], ",") {
			if a, ok := strings.CutPrefix(part, "addr="); ok {
				addr = a
			}
		}
		location = addr + o["device"]
	case o["device"] != "":
		location = o["device"]
	default:
		location = v.Mountpoint
	}
	return mode, location
}

func (s *Service) helperLabels(p *models.Project) map[string]string {
	return map[string]string{labelHelper: "true", labelProject: p.Slug}
}

// prepareTarget creates the data folder of t and reports whether it is empty. known is false
// when the target cannot be inspected (third-party drivers).
func (s *Service) prepareTarget(ctx context.Context, p *models.Project, t volumeTarget) (empty, known bool, err error) {
	if t.prep == nil {
		return false, false, nil
	}
	script := `set -e; d="/base/$1"; mkdir -p "$d"; if [ -z "$(ls -A "$d")" ]; then echo EMPTY; else echo DATA; fi`
	out, err := s.Docker.RunHelper(ctx, helperImage, []string{"sh", "-c", script, "sh", t.prepPath}, []docker.HelperMount{*t.prep}, s.helperLabels(p))
	if err != nil {
		return false, false, fmt.Errorf("prepare %s: %w", t.Location, err)
	}
	return strings.Contains(out, "EMPTY"), true, nil
}

func (s *Service) copyData(ctx context.Context, p *models.Project, from, to docker.HelperMount) error {
	from.Target, to.Target = "/from", "/to"
	_, err := s.Docker.RunHelper(ctx, helperImage, []string{"sh", "-c", "cp -a /from/. /to/"}, []docker.HelperMount{from, to}, s.helperLabels(p))
	return err
}

func (s *Service) createVolume(ctx context.Context, p *models.Project, name, mode string, t volumeTarget) error {
	spec := docker.VolumeSpec{Name: name, Driver: t.Driver, DriverOpts: t.Opts, Labels: storageLabels(p, mode, t.hash())}
	if err := s.Docker.CreateVolume(ctx, spec); err != nil {
		return fmt.Errorf("create volume %s: %w", name, err)
	}
	return nil
}

// EnsureStorage makes the project's volumes match its storage settings before `supabase start`:
// it creates missing volumes at the configured location and moves existing data when the location
// changed. freshDB is true when a new, empty database volume was created, in which case the CLI
// skips migrations and seed data on its first start.
func (s *Service) EnsureStorage(ctx context.Context, p *models.Project, log func(string)) (freshDB bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, storageTimeout)
	defer cancel()
	mode := storageMode(p.Storage)
	for _, v := range StorageVolumes(p) {
		t := targetFor(p, p.Storage, v)
		cur, err := s.Docker.Volume(ctx, v.Name)
		exists := err == nil
		if err != nil && !errors.Is(err, docker.ErrNotFound) {
			return false, err
		}
		if exists && cur.Labels[labelProject] != "" && cur.Labels[labelProject] != p.Slug {
			return false, fmt.Errorf("volume %s belongs to project %s", v.Name, cur.Labels[labelProject])
		}

		if mode == models.StorageDocker {
			if !exists || cur.Labels[labelStorage] == "" || cur.Labels[labelStorage] == models.StorageDocker {
				continue
			}
			if err := s.moveToDocker(ctx, p, v, cur, log); err != nil {
				return false, err
			}
			continue
		}

		hash := t.hash()
		if exists && cur.Labels[labelHash] == hash && cur.Labels[labelStorage] == mode {
			log(fmt.Sprintf("Storage %s: %s", v.Key, t.Location))
			continue
		}
		empty, known, err := s.prepareTarget(ctx, p, t)
		if err != nil {
			return false, err
		}
		if !exists {
			if known && !empty {
				log(fmt.Sprintf("Storage %s: using the existing data in %s", v.Key, t.Location))
			}
			if err := s.createVolume(ctx, p, v.Name, mode, t); err != nil {
				return false, err
			}
			log(fmt.Sprintf("Created volume %s at %s", v.Name, t.Location))
			if v.Key == "db" && known && empty {
				freshDB = true
			}
			continue
		}

		_, curLoc := DescribeVolume(cur)
		if t.prep == nil {
			return false, invalid("volume %s already holds data in %s; moving data to a %s volume is not supported. Move it yourself or delete the volume first", v.Name, curLoc, t.Driver)
		}
		if known && !empty {
			return false, invalid("%s already contains data; empty it or choose another location before moving %s there", t.Location, v.Name)
		}
		if err := s.moveTo(ctx, p, v, cur, mode, t, log); err != nil {
			return false, err
		}
	}
	return freshDB, nil
}

// moveTo copies an existing volume to target t and re-creates the volume there under its name.
func (s *Service) moveTo(ctx context.Context, p *models.Project, v StorageVolume, cur docker.Volume, mode string, t volumeTarget, log func(string)) error {
	_, curLoc := DescribeVolume(cur)
	log(fmt.Sprintf("Moving %s from %s to %s ...", v.Name, curLoc, t.Location))
	tmp := v.Name + "_sm_move"
	_ = s.Docker.RemoveVolume(ctx, tmp)
	if err := s.Docker.CreateVolume(ctx, docker.VolumeSpec{Name: tmp, Driver: t.Driver, DriverOpts: t.Opts, Labels: s.helperLabels(p)}); err != nil {
		return fmt.Errorf("create temporary volume: %w", err)
	}
	start := time.Now()
	err := s.copyData(ctx, p, docker.HelperMount{Volume: cur.Name}, docker.HelperMount{Volume: tmp})
	_ = s.Docker.RemoveVolume(ctx, tmp)
	if err != nil {
		return fmt.Errorf("copy %s to %s failed (the original volume is unchanged): %w", v.Name, t.Location, err)
	}
	if err := s.Docker.RemoveVolume(ctx, cur.Name); err != nil {
		return fmt.Errorf("data was copied to %s but the old volume %s could not be removed: %w", t.Location, cur.Name, err)
	}
	if err := s.createVolume(ctx, p, v.Name, mode, t); err != nil {
		return err
	}
	log(fmt.Sprintf("Moved %s to %s in %s", v.Name, t.Location, time.Since(start).Round(time.Second)))
	if curMode, _ := DescribeVolume(cur); curMode != models.StorageDocker {
		log("The previous copy is still in " + curLoc + "; delete it when you no longer need it.")
	}
	return nil
}

// moveToDocker copies a volume from a custom location back into a Docker-managed volume.
func (s *Service) moveToDocker(ctx context.Context, p *models.Project, v StorageVolume, cur docker.Volume, log func(string)) error {
	_, curLoc := DescribeVolume(cur)
	if cur.Driver != "local" {
		return invalid("volume %s uses the %s driver; moving it back into Docker storage is not supported", v.Name, cur.Driver)
	}
	log(fmt.Sprintf("Moving %s from %s into a Docker-managed volume ...", v.Name, curLoc))
	if err := s.Docker.RemoveVolume(ctx, cur.Name); err != nil {
		return fmt.Errorf("remove volume %s: %w", cur.Name, err)
	}
	t := volumeTarget{Driver: "local"}
	restore := func() {
		_ = s.Docker.RemoveVolume(ctx, v.Name)
		_ = s.Docker.CreateVolume(ctx, docker.VolumeSpec{Name: cur.Name, Driver: cur.Driver, DriverOpts: cur.Options, Labels: cur.Labels})
	}
	if err := s.createVolume(ctx, p, v.Name, models.StorageDocker, t); err != nil {
		restore()
		return err
	}
	src := docker.HelperMount{Driver: cur.Driver, DriverOpts: cur.Options}
	if err := s.copyData(ctx, p, src, docker.HelperMount{Volume: v.Name}); err != nil {
		restore()
		return fmt.Errorf("copy %s failed (the original location is unchanged): %w", v.Name, err)
	}
	log(fmt.Sprintf("Moved %s into Docker storage. The previous copy is still in %s; delete it when you no longer need it.", v.Name, curLoc))
	return nil
}

// removeProjectVolumes deletes the project's volume definitions. Data in custom locations stays.
func (s *Service) removeProjectVolumes(ctx context.Context, p *models.Project) {
	vols, err := s.Docker.Volumes(ctx)
	if err != nil {
		return
	}
	for _, v := range vols {
		if v.Labels[docker.ProjectLabel] == p.SupabaseProjectID {
			_ = s.Docker.RemoveVolume(ctx, v.Name)
		}
	}
}

// DBResetURL is a connection string that lets `supabase db reset --db-url` reset the database in
// place. A local reset would delete and re-create the volume, losing its custom location.
func (s *Service) DBResetURL(ctx context.Context, p *models.Project) (string, error) {
	ip, err := s.Docker.ContainerIP(ctx, "supabase_db_"+p.SupabaseProjectID, NetworkName(p))
	if err != nil {
		if errors.Is(err, docker.ErrNotFound) {
			return "", invalid("the database is not running; start the project first")
		}
		return "", err
	}
	return "postgresql://postgres:postgres@" + ip + ":5432/postgres?sslmode=disable", nil
}

// DBResetArgs returns the CLI arguments that reset the project's local database.
func (s *Service) DBResetArgs(ctx context.Context, p *models.Project, noSeed bool) ([]string, error) {
	args := []string{"db", "reset", "--local"}
	if CustomStorage(p) {
		url, err := s.DBResetURL(ctx, p)
		if err != nil {
			return nil, err
		}
		args = []string{"db", "reset", "--db-url", url}
	}
	if noSeed {
		args = append(args, "--no-seed")
	}
	return args, nil
}

// ---- settings ----

func (s *Service) StorageDefaults() models.StorageConfig {
	d := models.StorageConfig{Mode: models.StorageDocker}
	if ok, err := s.Settings.Get(settings.KeyStorageDefaults, &d); err != nil || !ok {
		return models.StorageConfig{Mode: models.StorageDocker}
	}
	return d
}

func (s *Service) SetStorageDefaults(cfg models.StorageConfig) (models.StorageConfig, error) {
	cfg, err := NormalizeStorage(cfg)
	if err != nil {
		return cfg, err
	}
	if cfg.Mode == models.StorageDriver {
		for _, v := range cfg.DriverOptions {
			if strings.Contains(v, "{volume}") && !strings.Contains(v, "{project}") {
				return cfg, invalid("options that contain {volume} must also contain {project}, otherwise projects share their data")
			}
		}
	}
	return cfg, s.Settings.Put(settings.KeyStorageDefaults, cfg)
}

// SetStorage validates and stores a project's storage settings. Volumes are created or moved the
// next time the project starts.
func (s *Service) SetStorage(p *models.Project, cfg models.StorageConfig) error {
	cfg, err := NormalizeStorage(cfg)
	if err != nil {
		return err
	}
	next := *p
	next.Storage = cfg
	if err := s.checkStorageConflicts(&next); err != nil {
		return err
	}
	p.Storage = cfg
	return s.db.Model(p).Select("storage").Updates(p).Error
}

// checkStorageConflicts rejects locations already used by another project.
func (s *Service) checkStorageConflicts(p *models.Project) error {
	if !CustomStorage(p) {
		return nil
	}
	mine := map[string]string{}
	for _, v := range StorageVolumes(p) {
		if t := targetFor(p, p.Storage, v); t.Opts["device"] != "" {
			mine[t.Driver+"|"+t.Opts["device"]] = v.Key
		}
	}
	var others []models.Project
	if err := s.db.Where("id <> ?", p.ID).Find(&others).Error; err != nil {
		return err
	}
	for i := range others {
		o := &others[i]
		if !CustomStorage(o) {
			continue
		}
		for _, v := range StorageVolumes(o) {
			t := targetFor(o, o.Storage, v)
			if _, clash := mine[t.Driver+"|"+t.Opts["device"]]; clash && t.Opts["device"] != "" {
				return invalid("%s is already used by project %s", t.Location, o.Slug)
			}
		}
	}
	return nil
}

// ---- views ----

type VolumeView struct {
	StorageVolume
	Target       string         `json:"target"`
	Exists       bool           `json:"exists"`
	CurrentMode  string         `json:"current_mode,omitempty"`
	Location     string         `json:"location,omitempty"`
	InSync       bool           `json:"in_sync"`
	Live         *docker.Volume `json:"live,omitempty"`
	CustomDriver bool           `json:"custom_driver"`
}

// StorageView describes the project's volumes: where they are now and where they will be.
func (s *Service) StorageView(ctx context.Context, p *models.Project) ([]VolumeView, error) {
	mode := storageMode(p.Storage)
	out := []VolumeView{}
	for _, v := range StorageVolumes(p) {
		t := targetFor(p, p.Storage, v)
		vv := VolumeView{StorageVolume: v, Target: t.Location, CustomDriver: t.prep == nil && mode != models.StorageDocker}
		cur, err := s.Docker.Volume(ctx, v.Name)
		switch {
		case err == nil:
			vv.Exists, vv.Live = true, &cur
			vv.CurrentMode, vv.Location = DescribeVolume(cur)
			if mode == models.StorageDocker {
				vv.InSync = cur.Labels[labelStorage] == "" || cur.Labels[labelStorage] == models.StorageDocker
			} else {
				vv.InSync = cur.Labels[labelHash] == t.hash() && cur.Labels[labelStorage] == mode
			}
		case errors.Is(err, docker.ErrNotFound):
			vv.InSync = true
		default:
			return nil, err
		}
		out = append(out, vv)
	}
	return out, nil
}

// storageForNewProject applies the instance defaults to a project being created.
func (s *Service) storageForNewProject() models.StorageConfig {
	cfg, err := NormalizeStorage(s.StorageDefaults())
	if err != nil {
		return models.StorageConfig{Mode: models.StorageDocker}
	}
	return cfg
}
