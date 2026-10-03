export type Role = 'admin' | 'member'

export interface User {
  id: number
  email: string
  name: string
  role: Role
  created_at: string
}

export interface Ports {
  api: number
  db: number
  shadow: number
  pooler: number
  studio: number
  smtp: number
  analytics: number
  inspector: number
}

export type ProjectStatus = 'running' | 'stopped' | 'starting' | 'stopping' | 'error' | 'unknown'

export interface Project {
  id: number
  slug: string
  name: string
  path: string
  supabase_project_id: string
  port_base: number
  inspector_port: number
  status: ProjectStatus
  imported: boolean
  created_at: string
  ports: Ports | null
  running_job: number
  network: NetworkConfig
  storage: StorageConfig
}

export type StorageMode = 'docker' | 'host' | 'nfs' | 'driver'

export interface StorageConfig {
  mode: StorageMode | ''
  path?: string
  server?: string
  export?: string
  mount_options?: string
  driver?: string
  driver_options?: Record<string, string> | null
}

export interface DockerVolume {
  name: string
  driver: string
  mountpoint: string
  created_at: string
  labels: Record<string, string> | null
  options: Record<string, string> | null
  scope: string
}

export interface ProjectVolume {
  key: string
  name: string
  label: string
  mount: string
  target: string
  exists: boolean
  current_mode?: StorageMode
  location?: string
  in_sync: boolean
  live?: DockerVolume
  custom_driver: boolean
}

export interface ProjectStorage {
  config: StorageConfig
  host_root: string
  docker_root?: string
  volumes?: ProjectVolume[]
  live_error?: string
}

export interface DockerInfo {
  server_version: string
  docker_root_dir: string
  storage_driver: string
  swarm_active: boolean
  network_drivers: string[] | null
  volume_drivers: string[] | null
}

export interface MountView {
  type: string
  name?: string
  source: string
  destination: string
  driver?: string
  rw: boolean
  storage_mode?: StorageMode
  location: string
  managed: boolean
}

export interface ContainerMounts {
  id: string
  name: string
  image: string
  state: string
  status: string
  project?: string
  mounts: MountView[]
  custom_storage: boolean
}

export interface VolumeOverview extends DockerVolume {
  storage_mode: StorageMode
  location: string
  managed: boolean
  project?: string
  used_by: string[]
  size?: number
}

export interface MountsResponse {
  containers: ContainerMounts[]
  volumes: VolumeOverview[]
  docker_root?: string
}

export type NetworkMode = 'auto' | 'managed' | 'external'

export interface NetworkConfig {
  mode: NetworkMode | ''
  name?: string
  bind_address?: string
  driver?: string
  subnet?: string
  gateway?: string
  ip_range?: string
  mtu?: number
  enable_ipv6?: boolean
  subnet_v6?: string
  options?: Record<string, string> | null
}

export interface NetworkDefaults extends NetworkConfig {
  subnet_pool?: string
  subnet_prefix?: number
}

export interface DockerNetwork {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  enable_ipv6: boolean
  ipam: { Subnet?: string; Gateway?: string; IPRange?: string }[] | null
  options: Record<string, string> | null
  labels: Record<string, string> | null
  containers: string[] | null
  used_by?: string
}

export interface ProjectNetwork {
  config: NetworkConfig
  name: string
  connect_host: string
  live?: DockerNetwork
  live_error?: string
}

export interface CLIInstallState {
  running: boolean
  version: string
  phase: string
  downloaded: number
  total: number
  error?: string
  finished_at?: string
}

export interface ManagerUpdateInfo {
  current: string
  commit: string
  /** current is a release (X.Y.Z) rather than a development build */
  release: boolean
  repository: string
  latest: string
  checked_at?: string
  check_error?: string
  update_available: boolean
  /** the running container can replace itself */
  supported: boolean
  unsupported?: string
  container?: string
  update: { running: boolean; version: string; phase: string; error?: string }
}

export interface CLIInfo {
  installed: boolean
  path: string
  version: string
  source: '' | 'managed' | 'homebrew' | 'system'
  version_error?: string
  latest: string
  checked_at?: string
  check_error?: string
  update_available: boolean
  auto_update: boolean
  managed_dir: string
  platform: string
  install: CLIInstallState
}

export interface Services {
  studio: boolean
  analytics: boolean
  inbucket: boolean
  edge_runtime: boolean
  storage: boolean
  realtime: boolean
  pooler: boolean
}

export interface AuthSettings {
  site_url: string
  additional_redirect_urls: string[]
  jwt_expiry: number
  enable_signup: boolean
  enable_anonymous_sign_ins: boolean
  enable_manual_linking: boolean
  minimum_password_length: number
  email_enable_signup: boolean
  email_enable_confirmations: boolean
  email_double_confirm_changes: boolean
}

export interface Settings {
  project_id: string
  ports: Ports
  services: Services
  auth: AuthSettings
}

export interface StatusDetails {
  running: boolean
  api_url: string
  rest_url: string
  graphql_url: string
  functions_url: string
  studio_url: string
  inbucket_url: string
  storage_s3_url: string
  mcp_url: string
  db_url: string
  anon_key: string
  service_role_key: string
  publishable_key: string
  secret_key: string
  jwt_secret: string
  s3_access_key_id: string
  s3_secret_key: string
  s3_region: string
  error?: string
}

export interface ProjectStatusResponse {
  status: ProjectStatus
  running_job: number
  details?: StatusDetails
}

export interface Job {
  id: number
  project_id: number
  command: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  exit_code: number
  output?: string
  started_by: number
  created_at: string
  finished_at: string | null
}

export interface Provider {
  name: string
  enabled: boolean
  client_id: string
  secret?: string
  secret_env: string
  secret_is_env_ref: boolean
  has_secret: boolean
  redirect_uri: string
  url: string
  skip_nonce_check: boolean
  email_optional: boolean
  secret_stored: boolean
  callback_url: string
}

export interface Secret {
  key: string
  preview: string
  updated_at: string
}

export interface Migration {
  version: string
  name: string
  file: string
  size: number
  applied: boolean | null
}

export interface EdgeFunction {
  name: string
  shared: boolean
  files: string[]
  modified: string
}

export interface Container {
  id: string
  name: string
  service: string
  image: string
  state: string
  status: string
  health: string
  ports: { ip: string; private_port: number; public_port: number; type: string }[] | null
}

export interface ContainerStats {
  id: string
  name: string
  service: string
  cpu_percent: number
  mem_usage: number
  mem_limit: number
}

export interface SystemInfo {
  projects_root: string
  db_driver: string
  go_version: string
  docker: boolean
  supabase_cli: string
  supabase_cli_update?: boolean
  supabase_cli_error?: string
}

export interface AuditLog {
  id: number
  actor_id: number
  action: string
  target: string
  metadata: string
  created_at: string
}
