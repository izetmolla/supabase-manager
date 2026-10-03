package models

import "time"

const (
	// NetworkAuto lets the Supabase CLI create its own network (supabase_network_<project_id>).
	NetworkAuto = "auto"
	// NetworkManaged is a network the manager creates and keeps in sync with NetworkConfig.
	NetworkManaged = "managed"
	// NetworkExternal joins an existing Docker network that the manager never modifies.
	NetworkExternal = "external"
)

// NetworkConfig selects the Docker network of a project's containers and, for managed networks,
// how that network is created. BindAddress is the host IP that published ports bind to; it maps
// to the bridge option com.docker.network.bridge.host_binding_ipv4.
type NetworkConfig struct {
	Mode        string            `json:"mode"`
	Name        string            `json:"name"`
	BindAddress string            `json:"bind_address"`
	Driver      string            `json:"driver"`
	Subnet      string            `json:"subnet"`
	Gateway     string            `json:"gateway"`
	IPRange     string            `json:"ip_range"`
	MTU         int               `json:"mtu"`
	EnableIPv6  bool              `json:"enable_ipv6"`
	SubnetV6    string            `json:"subnet_v6"`
	Options     map[string]string `json:"options"`
}

// NetworkDefaults is applied to new projects. With SubnetPool set, each new managed network gets
// the first free /SubnetPrefix subnet of the pool.
type NetworkDefaults struct {
	NetworkConfig
	SubnetPool   string `json:"subnet_pool"`
	SubnetPrefix int    `json:"subnet_prefix"`
}

// Setting is a small key/value store for instance-wide settings, stored as JSON.
type Setting struct {
	Key       string `gorm:"primaryKey;size:64"`
	Value     string `gorm:"type:text;not null"`
	UpdatedAt time.Time
}
