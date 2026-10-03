package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/supabase-manager/manager/internal/docker"
	"github.com/supabase-manager/manager/internal/models"
	"github.com/supabase-manager/manager/internal/settings"
)

const (
	labelProject = "com.supabase-manager.project"
	labelHash    = "com.supabase-manager.config-hash"
	optBindIPv4  = "com.docker.network.bridge.host_binding_ipv4"
	optMTU       = "com.docker.network.driver.mtu"

	managedNetworkPrefix = "supabase_manager_"
	maxNetworkOptions    = 32
)

var (
	networkNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
	driverRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:/-]{0,63}$`)
	optionKeyRe   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
)

// DefaultNetworkDefaults applies when no defaults were saved: a dedicated bridge per project
// whose ports are only reachable from this machine.
func DefaultNetworkDefaults() models.NetworkDefaults {
	return models.NetworkDefaults{
		Mode: models.NetworkManaged, Driver: "bridge", BindAddress: "127.0.0.1",
		SubnetPrefix: 24,
	}
}

// NormalizeNetwork validates cfg and returns the form stored in the database.
func NormalizeNetwork(cfg models.NetworkConfig) (models.NetworkConfig, error) {
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	cfg.Name = strings.TrimSpace(cfg.Name)
	switch cfg.Mode {
	case "", models.NetworkAuto:
		return models.NetworkConfig{Mode: models.NetworkAuto}, nil
	case models.NetworkExternal:
		if !networkNameRe.MatchString(cfg.Name) {
			return cfg, invalid("choose the existing Docker network to join")
		}
		return models.NetworkConfig{Mode: models.NetworkExternal, Name: cfg.Name}, nil
	case models.NetworkManaged:
	default:
		return cfg, invalid("unknown network mode %q", cfg.Mode)
	}

	out := models.NetworkConfig{
		Mode:        models.NetworkManaged,
		Name:        cfg.Name,
		BindAddress: strings.TrimSpace(cfg.BindAddress),
		Driver:      strings.TrimSpace(cfg.Driver),
		Subnet:      strings.TrimSpace(cfg.Subnet),
		Gateway:     strings.TrimSpace(cfg.Gateway),
		IPRange:     strings.TrimSpace(cfg.IPRange),
		MTU:         cfg.MTU,
		EnableIPv6:  cfg.EnableIPv6,
		SubnetV6:    strings.TrimSpace(cfg.SubnetV6),
	}
	if out.Name != "" && !networkNameRe.MatchString(out.Name) {
		return cfg, invalid("network names use letters, numbers, '.', '_' and '-' (max 63)")
	}
	if out.Driver == "" {
		out.Driver = "bridge"
	}
	if !driverRe.MatchString(out.Driver) {
		return cfg, invalid("invalid network driver %q", out.Driver)
	}
	switch out.Driver {
	case "macvlan", "ipvlan":
		return cfg, invalid("%s networks do not publish ports, but the Supabase CLI and the manager reach every project through published ports; use bridge", out.Driver)
	case "host", "none", "null":
		return cfg, invalid("the %s driver cannot be used for a project network", out.Driver)
	}

	if out.BindAddress != "" {
		// `supabase start` connects to the database on 127.0.0.1, so ports must be reachable there.
		if out.BindAddress != "127.0.0.1" && out.BindAddress != "0.0.0.0" {
			return cfg, invalid("the bind address must be 127.0.0.1 or 0.0.0.0: the Supabase CLI connects to the database on 127.0.0.1")
		}
		if out.Driver != "bridge" {
			return cfg, invalid("a bind address can only be set for bridge networks")
		}
	}

	var subnet netip.Prefix
	if out.Subnet != "" {
		p, err := netip.ParsePrefix(out.Subnet)
		if err != nil || !p.Addr().Is4() {
			return cfg, invalid("the subnet must be an IPv4 CIDR such as 10.210.0.0/24")
		}
		if p.Bits() > 29 {
			return cfg, invalid("the subnet is too small; use /29 or larger")
		}
		subnet = p.Masked()
		out.Subnet = subnet.String()
	}
	if out.Gateway != "" {
		gw, err := netip.ParseAddr(out.Gateway)
		if err != nil || !gw.Is4() {
			return cfg, invalid("the gateway must be an IPv4 address")
		}
		if !subnet.IsValid() || !subnet.Contains(gw) {
			return cfg, invalid("the gateway must be inside the subnet")
		}
	}
	if out.IPRange != "" {
		r, err := netip.ParsePrefix(out.IPRange)
		if err != nil || !r.Addr().Is4() {
			return cfg, invalid("the IP range must be an IPv4 CIDR")
		}
		if !subnet.IsValid() || !subnet.Contains(r.Addr()) || r.Bits() < subnet.Bits() {
			return cfg, invalid("the IP range must be inside the subnet")
		}
		out.IPRange = r.Masked().String()
	}
	if out.MTU != 0 && (out.MTU < 1280 || out.MTU > 9216) {
		return cfg, invalid("the MTU must be between 1280 and 9216")
	}
	if out.SubnetV6 != "" {
		p, err := netip.ParsePrefix(out.SubnetV6)
		if err != nil || !p.Addr().Is6() || p.Addr().Is4In6() {
			return cfg, invalid("the IPv6 subnet must be an IPv6 CIDR such as fd00:10::/64")
		}
		out.SubnetV6 = p.Masked().String()
		out.EnableIPv6 = true
	}

	if len(cfg.Options) > maxNetworkOptions {
		return cfg, invalid("too many driver options")
	}
	for k, v := range cfg.Options {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if !optionKeyRe.MatchString(k) || len(v) > 255 {
			return cfg, invalid("invalid driver option %q", k)
		}
		if k == optBindIPv4 || k == optMTU {
			return cfg, invalid("set %q with the dedicated field", k)
		}
		if out.Options == nil {
			out.Options = map[string]string{}
		}
		out.Options[k] = v
	}
	return out, nil
}

func networkMode(p *models.Project) string {
	if p.Network.Mode == "" {
		return models.NetworkAuto
	}
	return p.Network.Mode
}

// NetworkName is the Docker network the project's containers join.
func NetworkName(p *models.Project) string {
	switch networkMode(p) {
	case models.NetworkManaged:
		if p.Network.Name != "" {
			return p.Network.Name
		}
		return managedNetworkPrefix + p.Slug
	case models.NetworkExternal:
		return p.Network.Name
	}
	return "supabase_network_" + p.SupabaseProjectID
}

// cliArgs are global Supabase CLI flags every command for the project needs.
func cliArgs(p *models.Project) []string {
	if networkMode(p) == models.NetworkAuto {
		return nil
	}
	return []string{"--network-id", NetworkName(p)}
}

// ConnectHost is the host address the manager uses to reach the project's published ports.
// Every allowed bind address (127.0.0.1, 0.0.0.0, Docker default) is reachable on loopback.
func ConnectHost(p *models.Project) string {
	return "127.0.0.1"
}

func networkSpec(p *models.Project) docker.NetworkSpec {
	cfg := p.Network
	spec := docker.NetworkSpec{Name: NetworkName(p), Driver: cfg.Driver, EnableIPv6: cfg.EnableIPv6, Options: map[string]string{}}
	// Standalone containers (as started by the Supabase CLI) can only join attachable overlays.
	spec.Attachable = cfg.Driver == "overlay"
	spec.IPAM.Driver = "default"
	if cfg.Subnet != "" {
		spec.IPAM.Config = append(spec.IPAM.Config, docker.IPAMConfig{Subnet: cfg.Subnet, Gateway: cfg.Gateway, IPRange: cfg.IPRange})
	}
	if cfg.SubnetV6 != "" {
		spec.IPAM.Config = append(spec.IPAM.Config, docker.IPAMConfig{Subnet: cfg.SubnetV6})
	}
	maps.Copy(spec.Options, cfg.Options)
	if cfg.BindAddress != "" {
		spec.Options[optBindIPv4] = cfg.BindAddress
	}
	if cfg.MTU > 0 {
		spec.Options[optMTU] = fmt.Sprint(cfg.MTU)
	}
	return spec
}

func specHash(spec docker.NetworkSpec) string {
	b, _ := json.Marshal(spec)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// EnsureNetwork makes sure the project's network exists and matches its configuration. It runs
// right before `supabase start`, when none of the project's containers are attached.
func (s *Service) EnsureNetwork(ctx context.Context, p *models.Project, log func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	name := NetworkName(p)
	switch networkMode(p) {
	case models.NetworkAuto:
		return nil
	case models.NetworkExternal:
		n, err := s.Docker.Network(ctx, name)
		if errors.Is(err, docker.ErrNotFound) {
			return fmt.Errorf("docker network %q does not exist; create it or pick another network", name)
		}
		if err != nil {
			return err
		}
		log(fmt.Sprintf("Using existing Docker network %s (%s)", name, n.Driver))
		return nil
	}

	spec := networkSpec(p)
	hash := specHash(spec)
	spec.Labels = map[string]string{labelProject: p.Slug, labelHash: hash}

	n, err := s.Docker.Network(ctx, name)
	switch {
	case errors.Is(err, docker.ErrNotFound):
	case err != nil:
		return err
	case n.Labels[labelProject] != p.Slug:
		return fmt.Errorf("docker network %q already exists and is not managed for this project; rename the network or use 'Existing network' mode", name)
	case n.Labels[labelHash] == hash:
		log("Docker network " + name + " is up to date")
		return nil
	case len(n.Containers) > 0:
		return fmt.Errorf("docker network %q changed but containers are still attached (%s); stop them first", name, strings.Join(n.Containers, ", "))
	default:
		log("Network settings changed, recreating Docker network " + name)
		if err := s.Docker.RemoveNetwork(ctx, name); err != nil {
			return err
		}
	}
	if err := s.Docker.CreateNetwork(ctx, spec); err != nil {
		return fmt.Errorf("create docker network %s: %w", name, err)
	}
	desc := spec.Driver
	if p.Network.Subnet != "" {
		desc += ", " + p.Network.Subnet
	}
	if p.Network.BindAddress != "" {
		desc += ", ports on " + p.Network.BindAddress
	}
	log("Created Docker network " + name + " (" + desc + ")")
	return nil
}

// removeManagedNetwork deletes the project's network if the manager created it.
func (s *Service) removeManagedNetwork(ctx context.Context, p *models.Project) {
	if networkMode(p) != models.NetworkManaged {
		return
	}
	name := NetworkName(p)
	if n, err := s.Docker.Network(ctx, name); err == nil && n.Labels[labelProject] == p.Slug && len(n.Containers) == 0 {
		_ = s.Docker.RemoveNetwork(ctx, name)
	}
}

// SetNetwork validates and stores a project's network settings. They apply on the next start.
func (s *Service) SetNetwork(p *models.Project, cfg models.NetworkConfig) error {
	cfg, err := NormalizeNetwork(cfg)
	if err != nil {
		return err
	}
	next := *p
	next.Network = cfg
	if err := s.checkNetworkConflicts(&next); err != nil {
		return err
	}
	p.Network = cfg
	return s.db.Model(p).Select("network").Updates(p).Error
}

// checkNetworkConflicts rejects networks shared with another project: Supabase containers use
// fixed DNS aliases (db, kong, ...), so two stacks on one network resolve each other's services.
func (s *Service) checkNetworkConflicts(p *models.Project) error {
	if networkMode(p) == models.NetworkAuto {
		return nil
	}
	var others []models.Project
	if err := s.db.Where("id <> ?", p.ID).Find(&others).Error; err != nil {
		return err
	}
	name := NetworkName(p)
	subnet, _ := netip.ParsePrefix(p.Network.Subnet)
	for i := range others {
		o := &others[i]
		if NetworkName(o) == name {
			return invalid("network %q is already used by project %s", name, o.Slug)
		}
		if subnet.IsValid() && networkMode(o) == models.NetworkManaged {
			if other, err := netip.ParsePrefix(o.Network.Subnet); err == nil && other.Overlaps(subnet) {
				return invalid("subnet %s overlaps %s of project %s", subnet, other, o.Slug)
			}
		}
	}
	return nil
}

func (s *Service) NetworkDefaults() models.NetworkDefaults {
	d := DefaultNetworkDefaults()
	if ok, err := s.Settings.Get(settings.KeyNetworkDefaults, &d); err != nil || !ok {
		return DefaultNetworkDefaults()
	}
	return d
}

func (s *Service) SetNetworkDefaults(d models.NetworkDefaults) (models.NetworkDefaults, error) {
	if d.Mode == models.NetworkExternal {
		return d, invalid("projects cannot share one network, so 'Existing network' cannot be a default")
	}
	cfg := d.NetworkConfig
	cfg.Name, cfg.Subnet, cfg.Gateway, cfg.IPRange = "", "", "", ""
	cfg, err := NormalizeNetwork(cfg)
	if err != nil {
		return d, err
	}
	out := models.NetworkDefaults{NetworkConfig: cfg}
	if cfg.Mode == models.NetworkManaged && strings.TrimSpace(d.SubnetPool) != "" {
		pool, err := netip.ParsePrefix(strings.TrimSpace(d.SubnetPool))
		if err != nil || !pool.Addr().Is4() {
			return d, invalid("the subnet pool must be an IPv4 CIDR such as 10.210.0.0/16")
		}
		if d.SubnetPrefix < pool.Bits() || d.SubnetPrefix > 29 {
			return d, invalid("the per-project subnet size must be between /%d and /29", pool.Bits())
		}
		out.SubnetPool = pool.Masked().String()
		out.SubnetPrefix = d.SubnetPrefix
	}
	return out, s.Settings.Put(settings.KeyNetworkDefaults, out)
}

// usedSubnets collects IPv4 ranges a new network must not overlap: existing Docker networks,
// subnets reserved by managed projects and the host's own interface networks.
func (s *Service) usedSubnets(ctx context.Context, excludeProject uint) []netip.Prefix {
	var used []netip.Prefix
	if nets, err := s.Docker.Networks(ctx); err == nil {
		for _, n := range nets {
			for _, c := range n.IPAM {
				if p, err := netip.ParsePrefix(c.Subnet); err == nil {
					used = append(used, p)
				}
			}
		}
	}
	var ps []models.Project
	s.db.Where("id <> ?", excludeProject).Find(&ps)
	for _, p := range ps {
		if q, err := netip.ParsePrefix(p.Network.Subnet); err == nil {
			used = append(used, q)
		}
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Is4() && !p.Addr().IsLoopback() {
				used = append(used, p.Masked())
			}
		}
	}
	return used
}

// FreeSubnet returns the first /bits subnet of pool that overlaps nothing in use, and its gateway.
func (s *Service) FreeSubnet(ctx context.Context, pool string, bits int, excludeProject uint) (string, string, error) {
	pfx, err := netip.ParsePrefix(pool)
	if err != nil || !pfx.Addr().Is4() {
		return "", "", invalid("invalid subnet pool %q", pool)
	}
	sub, ok := nextFreeSubnet(pfx.Masked(), bits, s.usedSubnets(ctx, excludeProject))
	if !ok {
		return "", "", invalid("no free /%d subnet left in %s", bits, pfx.Masked())
	}
	return sub.String(), sub.Addr().Next().String(), nil
}

func nextFreeSubnet(pool netip.Prefix, bits int, used []netip.Prefix) (netip.Prefix, bool) {
	if bits < pool.Bits() || bits > 30 {
		return netip.Prefix{}, false
	}
	sort.Slice(used, func(i, j int) bool { return used[i].Addr().Less(used[j].Addr()) })
	step := uint32(1) << (32 - bits)
	start := pool.Addr().As4()
	base := uint32(start[0])<<24 | uint32(start[1])<<16 | uint32(start[2])<<8 | uint32(start[3])
	count := uint32(1) << (bits - pool.Bits())
	for i := range count {
		a := base + i*step
		cand := netip.PrefixFrom(netip.AddrFrom4([4]byte{byte(a >> 24), byte(a >> 16), byte(a >> 8), byte(a)}), bits)
		free := true
		for _, u := range used {
			if u.Overlaps(cand) {
				free = false
				break
			}
		}
		if free {
			return cand, true
		}
	}
	return netip.Prefix{}, false
}

// networkForNewProject applies the instance defaults to a project being created.
func (s *Service) networkForNewProject(ctx context.Context) (models.NetworkConfig, error) {
	d := s.NetworkDefaults()
	cfg := d.NetworkConfig
	if cfg.Mode == models.NetworkManaged && d.SubnetPool != "" {
		sub, gw, err := s.FreeSubnet(ctx, d.SubnetPool, d.SubnetPrefix, 0)
		if err != nil {
			return cfg, err
		}
		cfg.Subnet, cfg.Gateway = sub, gw
	}
	return NormalizeNetwork(cfg)
}
