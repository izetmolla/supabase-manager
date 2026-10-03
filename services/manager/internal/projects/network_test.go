package projects

import (
	"net/netip"
	"testing"

	"github.com/supabase-manager/manager/internal/models"
)

func TestNormalizeNetwork(t *testing.T) {
	ok := []models.NetworkConfig{
		{},
		{Mode: models.NetworkAuto, Name: "ignored"},
		{Mode: models.NetworkExternal, Name: "my-net"},
		{Mode: models.NetworkManaged},
		{Mode: models.NetworkManaged, BindAddress: "0.0.0.0", Subnet: "10.9.0.7/24", Gateway: "10.9.0.1", IPRange: "10.9.0.128/25", MTU: 1400},
		{Mode: models.NetworkManaged, SubnetV6: "fd00:10::/64", Options: map[string]string{"com.docker.network.bridge.name": "br-sb"}},
	}
	for _, c := range ok {
		if _, err := NormalizeNetwork(c); err != nil {
			t.Errorf("NormalizeNetwork(%+v): %v", c, err)
		}
	}

	bad := []models.NetworkConfig{
		{Mode: "weird"},
		{Mode: models.NetworkExternal},
		{Mode: models.NetworkManaged, Name: "bad name"},
		{Mode: models.NetworkManaged, BindAddress: "::1"},
		{Mode: models.NetworkManaged, BindAddress: "203.0.113.77"},
		{Mode: models.NetworkManaged, BindAddress: "127.0.0.2"},
		{Mode: models.NetworkManaged, BindAddress: "127.0.0.1", Driver: "macvlan"},
		{Mode: models.NetworkManaged, Subnet: "10.9.0.0/30"},
		{Mode: models.NetworkManaged, Subnet: "10.9.0.0/24", Gateway: "10.8.0.1"},
		{Mode: models.NetworkManaged, Gateway: "10.8.0.1"},
		{Mode: models.NetworkManaged, Subnet: "10.9.0.0/24", IPRange: "10.9.0.0/16"},
		{Mode: models.NetworkManaged, MTU: 100},
		{Mode: models.NetworkManaged, SubnetV6: "10.0.0.0/8"},
		{Mode: models.NetworkManaged, Options: map[string]string{optBindIPv4: "0.0.0.0"}},
	}
	for _, c := range bad {
		if _, err := NormalizeNetwork(c); err == nil {
			t.Errorf("NormalizeNetwork(%+v) accepted invalid config", c)
		}
	}

	got, _ := NormalizeNetwork(models.NetworkConfig{Mode: models.NetworkManaged, Subnet: "10.9.0.7/24", SubnetV6: "fd00:10::1/64"})
	if got.Subnet != "10.9.0.0/24" || got.Driver != "bridge" || !got.EnableIPv6 || got.SubnetV6 != "fd00:10::/64" {
		t.Errorf("unexpected normalized config %+v", got)
	}
}

func TestNetworkNameAndHost(t *testing.T) {
	p := &models.Project{Slug: "demo", SupabaseProjectID: "demo-id"}
	if NetworkName(p) != "supabase_network_demo-id" || cliArgs(p) != nil || ConnectHost(p) != "127.0.0.1" {
		t.Errorf("auto mode: %s %v %s", NetworkName(p), cliArgs(p), ConnectHost(p))
	}
	p.Network = models.NetworkConfig{Mode: models.NetworkManaged, BindAddress: "0.0.0.0"}
	if NetworkName(p) != "supabase_manager_demo" || ConnectHost(p) != "127.0.0.1" {
		t.Errorf("managed 0.0.0.0: %s %s", NetworkName(p), ConnectHost(p))
	}
	p.Network = models.NetworkConfig{Mode: models.NetworkExternal, Name: "shared"}
	if args := cliArgs(p); len(args) != 2 || args[1] != "shared" {
		t.Errorf("external args: %v", args)
	}
}

func TestNextFreeSubnet(t *testing.T) {
	pool := netip.MustParsePrefix("10.210.0.0/16")
	used := []netip.Prefix{
		netip.MustParsePrefix("10.210.0.0/24"),
		netip.MustParsePrefix("10.210.1.128/25"),
		netip.MustParsePrefix("172.17.0.0/16"),
	}
	got, ok := nextFreeSubnet(pool, 24, used)
	if !ok || got.String() != "10.210.2.0/24" {
		t.Fatalf("got %v %v, want 10.210.2.0/24", got, ok)
	}
	if _, ok := nextFreeSubnet(netip.MustParsePrefix("10.0.0.0/24"), 24, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}); ok {
		t.Fatal("expected exhausted pool")
	}
	if _, ok := nextFreeSubnet(pool, 8, nil); ok {
		t.Fatal("prefix larger than pool must fail")
	}
}
