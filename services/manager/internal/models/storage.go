package models

const (
	// StorageDocker keeps project data in volumes managed by Docker (its data root).
	StorageDocker = "docker"
	// StorageHost binds the volumes to folders on the host.
	StorageHost = "host"
	// StorageNFS mounts the volumes from an NFS export.
	StorageNFS = "nfs"
	// StorageDriver creates the volumes with any volume driver and options.
	StorageDriver = "driver"
)

// StorageConfig decides where the persistent volumes of a project (database, storage files) live.
//
// For host mode, Path is a base folder: a project's data goes to <Path>/<slug>/<volume>, or to
// <project folder>/volumes/<volume> when Path is empty. For NFS, data goes to
// <Export>/<slug>/<volume> on Server. Driver options may contain {project} and {volume}.
type StorageConfig struct {
	Mode          string            `json:"mode"`
	Path          string            `json:"path,omitempty"`
	Server        string            `json:"server,omitempty"`
	Export        string            `json:"export,omitempty"`
	MountOptions  string            `json:"mount_options,omitempty"`
	Driver        string            `json:"driver,omitempty"`
	DriverOptions map[string]string `json:"driver_options,omitempty"`
}
