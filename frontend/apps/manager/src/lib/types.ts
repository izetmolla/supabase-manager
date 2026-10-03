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

export interface SMTPSettings {
  enabled: boolean
  host: string
  port: number
  user: string
  /** Only sent when changing it; never returned. */
  pass?: string
  has_pass: boolean
  admin_email: string
  sender_name: string
  emails_per_hour: number
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

export type ProjectDomainService = 'api' | 'studio' | 'mail' | 'site'

export interface ProjectDomain {
  service?: ProjectDomainService
  host_id: number
  host_name: string
  domain: string
  url: string
}

export interface ProjectDomains {
  enabled: boolean
  available?: ProjectDomain[]
  sites?: ProjectDomain[]
  selected?: Partial<Record<ProjectDomainService, string>>
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

// ---- Proxy manager ----

export type ProxyKind = 'nginx' | 'traefik'

export interface ContainerState {
  exists: boolean
  id: string
  image: string
  status: string
  running: boolean
  restarting: boolean
  restart_count: number
  exit_code: number
  error: string
  started_at: string | null
}

export interface ProxyInstance {
  id: number
  name: string
  kind: ProxyKind
  image: string
  bind_ip: string
  http_port: number
  https_port: number
  admin_port: number
  tls_alpn: boolean
  enabled: boolean
  notes: string
  deployed_revision_id: number
  deployed_checksum: string
  deployed_at: string | null
  created_at: string
}

export interface ProxyAgentInfo {
  connected: boolean
  connected_at?: string
  remote_addr?: string
  agent_version?: string
  proxy_version?: string
  proxy_running: boolean
  proxy_started_at?: string
  restarts: number
  config_checksum?: string
  last_error?: string
}

export interface ProxyInstanceView extends ProxyInstance {
  container: ContainerState | null
  agent: ProxyAgentInfo
  in_sync: boolean
  pending: boolean
  host_count: number
  stream_count: number
}

export interface KV {
  name: string
  value: string
}

export interface IPRule {
  action: 'allow' | 'deny'
  cidr: string
}

export interface CORSOptions {
  enabled: boolean
  origins: string[]
  methods: string
  headers: string
  credentials: boolean
  max_age: number
}

export type PathType = 'prefix' | 'exact' | 'regex'

export interface ProxyRoute {
  id: number
  path_type: PathType
  path: string
  headers: KV[]
  upstream_id: number
  strip_prefix: boolean
  rewrite_regex: string
  rewrite_replacement: string
  access_list_id: number
  request_headers: KV[]
  response_headers: KV[]
}

export type HostKind = 'proxy' | 'redirect' | 'error'
export type TLSMode = 'none' | 'certificate' | 'auto'

export interface ProxyHost {
  id: number
  name: string
  kind: HostKind
  domains: string[]
  upstream_id: number
  routes: ProxyRoute[]
  instance_ids: number[]
  tls: {
    mode: TLSMode
    certificate_id: number
    force_https: boolean
    hsts: boolean
    hsts_subdomains: boolean
    http2: boolean
  }
  options: { websocket: boolean; connect_timeout: number; read_timeout: number; max_body_mb: number }
  security: {
    access_list_id: number
    ip_rules: IPRule[]
    rate_limit: { enabled: boolean; rps: number; burst: number }
  }
  headers: { request: KV[]; response: KV[]; cors: CORSOptions }
  redirect: { url: string; code: number; preserve_path: boolean }
  error_page: { code: number; body: string }
  raw_nginx: string
  raw_traefik: string
  enabled: boolean
  notes: string
  created_at?: string
  updated_at?: string
}

export interface UpstreamTarget {
  kind: 'static' | 'project'
  address: string
  port: number
  project: string
  service: string
  weight: number
  backup: boolean
}

export type LBAlgorithm = 'round_robin' | 'least_conn' | 'ip_hash'

export interface HealthCheck {
  enabled: boolean
  path: string
  interval: number
  timeout: number
  expect_status: number
}

export interface ProxyUpstream {
  id: number
  name: string
  algorithm: LBAlgorithm
  scheme: 'http' | 'https'
  tls_skip_verify: boolean
  sticky: boolean
  targets: UpstreamTarget[]
  health: HealthCheck
  notes: string
}

export interface ProxyStream {
  id: number
  name: string
  protocol: 'tcp' | 'udp'
  listen_port: number
  upstream_id: number
  instance_ids: number[]
  enabled: boolean
}

export interface AccessList {
  id: number
  name: string
  users: { username: string }[]
  allow: string[]
  deny: string[]
  satisfy: 'any' | 'all'
}

export type ChallengeType = 'http-01' | 'dns-01' | 'tls-alpn-01'

export interface Certificate {
  id: number
  name: string
  domains: string[]
  source: 'acme' | 'custom'
  challenge: ChallengeType | ''
  key_type: string
  acme_account_id: number
  dns_provider_id: number
  issuer: string
  not_before: string | null
  not_after: string | null
  status: 'pending' | 'valid' | 'error'
  last_error: string
  auto_renew: boolean
  last_job_id: number
  created_at: string
  updated_at: string
}

export interface AcmeAccount {
  id: number
  name: string
  email: string
  directory_url: string
  eab_key_id: string
  registered: boolean
  is_default: boolean
  last_error: string
  created_at: string
}

export interface DNSProvider {
  id: number
  name: string
  code: string
  keys: string[]
  created_at: string
}

export interface CatalogField {
  key: string
  description: string
}

export interface DNSCatalogEntry {
  code: string
  name: string
  url: string
  credentials: CatalogField[]
  additional: CatalogField[]
}

export interface ProxyMeta {
  directories: { id: string; name: string; url: string }[]
  dns_providers: DNSCatalogEntry[]
  nginx_image: string
  traefik_image: string
  manager_url: string
  alpn_addr: string
  challenge_path: string
}

export interface ProxyImageTag {
  tag: string
  image: string
  release: boolean
  local: boolean
  remote: boolean
  updated?: string
  size?: number
}

export interface ProxyImageTags {
  repository: string
  default: string
  tags: ProxyImageTag[]
  hub_error?: string
}

export interface ProxyImageUpdate {
  instance_id: number
  current: string
  target?: string
  reason?: 'new_release' | 'rebuilt' | 'recreate'
  available: boolean
  error?: string
}

export interface ProjectService {
  project: string
  project_name: string
  service: string
  label: string
  host: string
  port: number
  status: string
}

export interface UnhealthyTarget {
  upstream_id: number
  upstream: string
  address: string
  error: string
}

export interface ProxyStatus {
  pending_instances: number[]
  instances: number
  running: number
  hosts: number
  streams: number
  upstreams: number
  certificates: number
  expiring: Certificate[]
  unhealthy_targets: UnhealthyTarget[]
}

export interface TargetHealth {
  address: string
  state: 'up' | 'down'
  error?: string
  latency_ms: number
  checked_at: string
}

export interface PreviewFile {
  path: string
  content: string
  deployed: string
  status: 'added' | 'removed' | 'changed' | 'same'
  secret: boolean
}

export interface ProxyPreview {
  instance_id: number
  kind: ProxyKind
  checksum: string
  pending: boolean
  files: PreviewFile[]
  warnings: string[] | null
}

export interface ConfigRevision {
  id: number
  instance_id: number
  number: number
  kind: 'deploy' | 'rollback' | 'certificate'
  checksum: string
  status: 'applied' | 'failed' | 'deploying' | 'rolled_back'
  error: string
  warnings: string[] | null
  note: string
  created_by: number
  created_at: string
}

export interface AccessEntry {
  time: string
  host: string
  host_id: number
  remote: string
  method: string
  uri: string
  status: number
  bytes: number
  duration_ms: number
  upstream: string
  user_agent: string
}
