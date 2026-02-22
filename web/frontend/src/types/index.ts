// API Response Types
export interface APIResponse<T = any> {
  data?: T;
  error?: string;
  message?: string;
  success: boolean;
}

// User and Authentication Types
export interface User {
  id: string;
  username: string;
  displayName?: string;
  email?: string;
  roles: string[];
  status: 'active' | 'inactive' | 'suspended';
  createdAt: number | string;
  lastLogin?: string;
  tenantId?: string;
  metadata?: Record<string, string>;
  lockedUntil?: number;
  failedLoginAttempts?: number;
  lastFailedLogin?: number;
  twoFactorEnabled?: boolean;
  themePreference?: string;
  languagePreference?: string;
  authProvider?: string;
  externalId?: string;
}

export interface AccessKey {
  id: string;
  accessKey: string;
  secretKey?: string; // Only shown when creating
  userId: string;
  status: 'active' | 'inactive';
  permissions: string[];
  createdAt: string | number;
  lastUsed?: string | number;
}

export interface AuthToken {
  token: string;
  refreshToken?: string;
  expiresAt: number;
  user: User;
}

export interface LoginRequest {
  username: string;
  password: string;
  rememberMe?: boolean;
}

export interface LoginResponse {
  success: boolean;
  token?: string;
  refreshToken?: string;
  user?: User;
  error?: string;
  requires_2fa?: boolean;
  user_id?: string;
  message?: string;
  default_password?: boolean;
  sso_hint?: boolean;
}

export interface CreateUserRequest {
  username: string;
  email?: string;
  password: string;
  roles?: string[];
  status?: 'active' | 'inactive' | 'suspended';
  tenantId?: string;
  authProvider?: string;
  externalId?: string;
}

export interface EditUserForm {
  displayName?: string;
  email?: string;
  roles: string[];
  status?: 'active' | 'inactive' | 'suspended';
  tenantId?: string;
}

// Tenant Types
export interface Tenant {
  id: string;
  name: string;
  displayName: string;
  description?: string;
  status: 'active' | 'inactive' | 'deleted';
  maxAccessKeys: number;
  currentAccessKeys: number;
  maxStorageBytes: number;
  currentStorageBytes: number;
  maxBuckets: number;
  currentBuckets: number;
  metadata?: Record<string, string>;
  createdAt: number;
  updatedAt: number;
}

export interface CreateTenantRequest {
  name: string;
  displayName: string;
  description?: string;
  maxAccessKeys?: number;
  maxStorageBytes?: number;
  maxBuckets?: number;
  metadata?: Record<string, string>;
}

export interface UpdateTenantRequest {
  displayName?: string;
  description?: string;
  status?: 'active' | 'inactive';
  maxAccessKeys?: number;
  maxStorageBytes?: number;
  maxBuckets?: number;
  currentStorageBytes?: number;
  currentBuckets?: number;
  metadata?: Record<string, string>;
}

// Bucket Permission Types
export interface BucketPermission {
  id: string;
  bucketName: string;
  userId?: string;
  tenantId?: string;
  permissionLevel: 'read' | 'write' | 'admin';
  grantedBy: string;
  grantedAt: number;
  expiresAt?: number;
}

export interface GrantPermissionRequest {
  userId?: string;
  tenantId?: string;
  permissionLevel: 'read' | 'write' | 'admin';
  grantedBy: string;
  expiresAt?: number;
}


// Bucket Types
export interface Bucket {
  name: string;
  tenant_id?: string; // Backend uses snake_case
  tenantId?: string; // Alias for compatibility
  creation_date: string; // Backend uses snake_case
  creationDate?: string; // Alias for compatibility
  region?: string;
  owner_id?: string; // Backend uses snake_case
  ownerId?: string; // Alias for compatibility
  owner_type?: string; // Backend uses snake_case
  ownerType?: string; // Alias for compatibility
  is_public?: boolean; // Backend uses snake_case
  isPublic?: boolean; // Alias for compatibility
  versioning?: VersioningConfig;
  cors?: CORSConfig;
  lifecycle?: LifecycleConfig;
  objectLock?: ObjectLockConfig;
  policy?: BucketPolicy;
  encryption?: EncryptionConfig;
  metadata?: Record<string, string>;
  tags?: Record<string, string>;
  object_count?: number; // Backend uses snake_case
  objectCount?: number; // Alias for compatibility
  size?: number; // Backend uses 'size'
  totalSize?: number; // Alias for compatibility
  // Cluster-specific fields (only populated in multi-node cluster mode)
  node_id?: string; // Backend uses snake_case
  nodeId?: string; // Alias for compatibility
  node_name?: string; // Backend uses snake_case
  nodeName?: string; // Alias for compatibility
  node_status?: string; // Backend uses snake_case
  nodeStatus?: string; // Alias for compatibility
}

export interface VersioningConfig {
  Status: 'Enabled' | 'Suspended';  // Backend uses capital S
  mfaDelete?: 'Enabled' | 'Disabled';
}

export interface CORSConfig {
  corsRules: CORSRule[];
}

export interface CORSRule {
  id?: string;
  allowedOrigins: string[];
  allowedMethods: string[];
  allowedHeaders?: string[];
  exposedHeaders?: string[];
  maxAgeSeconds?: number;
}

export interface LifecycleConfig {
  Rules: LifecycleRule[];
}

export interface LifecycleRule {
  ID: string;
  Status: 'Enabled' | 'Disabled';
  Filter?: {
    Prefix?: string;
  };
  Expiration?: {
    Days?: number;
    Date?: string;
    ExpiredObjectDeleteMarker?: boolean;
  };
  NoncurrentVersionExpiration?: {
    NoncurrentDays: number;
  };
  Transition?: LifecycleTransition[];
  AbortIncompleteMultipartUpload?: {
    DaysAfterInitiation: number;
  };
}

export interface LifecycleTransition {
  days: number;
  storageClass: string;
}

export interface NotificationConfiguration {
  bucketName: string;
  tenantId?: string;
  rules: NotificationRule[];
  updatedAt: string;
  updatedBy: string;
}

export interface NotificationRule {
  id: string;
  enabled: boolean;
  webhookUrl: string;
  events: string[];
  filterPrefix?: string;
  filterSuffix?: string;
  customHeaders?: Record<string, string>;
}

export interface ObjectLockConfig {
  objectLockEnabled: boolean;
  rule?: ObjectLockRule;
}

export interface ObjectLockRule {
  defaultRetention?: {
    mode: 'GOVERNANCE' | 'COMPLIANCE';
    days?: number;
    years?: number;
  };
}

export interface BucketPolicy {
  version: string;
  statement: PolicyStatement[];
}

export interface PolicyStatement {
  sid?: string;
  effect: 'Allow' | 'Deny';
  principal?: string | string[] | Record<string, string>;
  action: string | string[];
  resource: string | string[];
  condition?: Record<string, any>;
}

export interface EncryptionConfig {
  algorithm: string;
  keySource: 'server' | 'customer' | 'kms';
  masterKey?: string;
}

// ACL Types
export interface ACL {
  owner: ACLOwner;
  grants: ACLGrant[];
  cannedACL?: string;
}

export interface ACLOwner {
  id: string;
  displayName: string;
}

export interface ACLGrant {
  grantee: ACLGrantee;
  permission: ACLPermission;
}

export interface ACLGrantee {
  type: 'CanonicalUser' | 'AmazonCustomerByEmail' | 'Group';
  id?: string;
  displayName?: string;
  emailAddress?: string;
  uri?: string;
}

export type ACLPermission = 'READ' | 'WRITE' | 'READ_ACP' | 'WRITE_ACP' | 'FULL_CONTROL';

export type CannedACL =
  | 'private'
  | 'public-read'
  | 'public-read-write'
  | 'authenticated-read'
  | 'bucket-owner-read'
  | 'bucket-owner-full-control'
  | 'log-delivery-write';

export const CANNED_ACL_DESCRIPTIONS: Record<CannedACL, string> = {
  'private': 'Owner gets FULL_CONTROL. No one else has access.',
  'public-read': 'Owner gets FULL_CONTROL. Anyone can READ.',
  'public-read-write': 'Owner gets FULL_CONTROL. Anyone can READ and WRITE.',
  'authenticated-read': 'Owner gets FULL_CONTROL. Any authenticated AWS user can READ.',
  'bucket-owner-read': 'Object owner gets FULL_CONTROL. Bucket owner gets READ.',
  'bucket-owner-full-control': 'Both object owner and bucket owner get FULL_CONTROL.',
  'log-delivery-write': 'LogDelivery group gets WRITE and READ_ACP permissions.',
};

// Object Types
export interface S3Object {
  key: string;
  lastModified: string;
  etag: string;
  size: number;
  storageClass?: string;
  owner?: {
    id: string;
    displayName: string;
  };
  metadata?: Record<string, string>;
  tags?: Record<string, string>;
  retention?: ObjectRetention;
  legalHold?: ObjectLegalHold;
  versioning?: ObjectVersion[];
  contentType?: string;
  contentEncoding?: string;
  cacheControl?: string;
  expires?: string;
  isDeleteMarker?: boolean;
}

export interface ObjectRetention {
  mode: 'GOVERNANCE' | 'COMPLIANCE';
  retainUntilDate: string;
}

export interface ObjectLegalHold {
  status: 'ON' | 'OFF';
}

export interface ObjectVersion {
  versionId: string;
  isLatest: boolean;
  lastModified: string;
  etag: string;
  size: number;
  storageClass?: string;
  isDeleteMarker?: boolean;
  key?: string;
}

export interface ListObjectVersionsResponse {
  versions: ObjectVersion[];
  deleteMarkers: ObjectVersion[];
  name: string;
  prefix?: string;
  keyMarker?: string;
  versionIdMarker?: string;
  nextKeyMarker?: string;
  nextVersionIdMarker?: string;
  maxKeys: number;
  isTruncated: boolean;
}

export interface GeneratePresignedURLRequest {
  bucket: string;
  key: string;
  tenantId?: string;
  expiresIn: number;  // seconds
  method?: string;    // HTTP method (GET, PUT, etc.)
}

export interface GeneratePresignedURLResponse {
  url: string;
  method: string;
  expiresIn: number;
  expiresAt: string;
}

export interface MultipartUpload {
  uploadId: string;
  key: string;
  initiated: string;
  storageClass?: string;
  owner?: {
    id: string;
    displayName: string;
  };
  parts?: UploadPart[];
}

export interface UploadPart {
  partNumber: number;
  lastModified: string;
  etag: string;
  size: number;
}

// List Operations
export interface ListBucketsResponse {
  buckets: Bucket[];
  owner?: {
    id: string;
    displayName: string;
  };
}

export interface ListObjectsResponse {
  objects: S3Object[];
  commonPrefixes?: string[];
  isTruncated: boolean;
  nextContinuationToken?: string;
  keyCount: number;
  maxKeys: number;
  prefix?: string;
  delimiter?: string;
  encodingType?: string;
}

export interface ListObjectsRequest {
  bucket: string;
  tenantId?: string;
  prefix?: string;
  delimiter?: string;
  maxKeys?: number;
  continuationToken?: string;
  fetchOwner?: boolean;
  startAfter?: string;
}

export interface ObjectSearchFilter {
  contentTypes?: string[];
  minSize?: number;
  maxSize?: number;
  modifiedAfter?: string; // RFC3339
  modifiedBefore?: string; // RFC3339
  tags?: Record<string, string>;
}

export interface SearchObjectsRequest {
  bucket: string;
  tenantId?: string;
  prefix?: string;
  delimiter?: string;
  maxKeys?: number;
  marker?: string;
  filter?: ObjectSearchFilter;
}

// Upload Types
export interface UploadRequest {
  bucket: string;
  tenantId?: string;
  key: string;
  file: File;
  contentType?: string;
  metadata?: Record<string, string>;
  tags?: Record<string, string>;
  storageClass?: string;
  serverSideEncryption?: string;
  onProgress?: (progress: UploadProgress) => void;
}

export interface UploadProgress {
  loaded: number;
  total: number;
  percentage: number;
  speed: number; // bytes per second
  timeRemaining: number; // seconds
}

export interface DownloadRequest {
  bucket: string;
  tenantId?: string;
  key: string;
  versionId?: string;
  range?: string;
  onProgress?: (progress: DownloadProgress) => void;
}

export interface DownloadProgress {
  loaded: number;
  total: number;
  percentage: number;
  speed: number;
}

// Metrics and Statistics
export interface StorageMetrics {
  totalBuckets: number;
  totalObjects: number;
  totalSize: number;
  bucketMetrics: Record<string, BucketMetric>;
  storageOperations: Record<string, number>;
  averageObjectSize: number;
  objectSizeDistribution: Record<string, number>;
  timestamp: number;
}

export interface BucketMetric {
  name: string;
  objectCount: number;
  totalSize: number;
  averageSize: number;
  lastModified: number;
}

export interface SystemMetrics {
  cpuUsagePercent: number;
  cpuCores?: number; // Physical CPU cores
  cpuLogicalCores?: number; // Logical CPU cores (with hyperthreading)
  cpuFrequencyMhz?: number; // CPU frequency in MHz
  cpuModelName?: string; // CPU model name
  memoryUsagePercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes: number;
  diskUsagePercent: number;
  diskUsedBytes: number;
  diskTotalBytes: number;
  networkBytesIn: number;
  networkBytesOut: number;
  uptime?: number; // Server uptime in seconds
  goroutines?: number; // Active goroutines
  heapAllocBytes?: number; // Bytes allocated in heap
  gcRuns?: number; // Number of GC runs
  timestamp: number;
}

export interface S3Metrics {
  totalRequests: number;
  totalErrors: number;
  avgLatency: number;
  requestsPerSec: number;
  timestamp: number;
}

// Performance Metrics Types (from PerformanceCollector)
export interface PerformanceLatencyStats {
  operation: string;
  count: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  mean_ms: number;
  min_ms: number;
  max_ms: number;
  success_rate: number;
  error_count: number;
}

export interface LatenciesResponse {
  timestamp: string;
  latencies: Record<string, PerformanceLatencyStats>;
}

export interface ThroughputStats {
  requests_per_second: number;
  bytes_per_second: number;
  objects_per_second: number;
  timestamp: string;
}

export interface ThroughputResponse {
  current: ThroughputStats;
}

// UI State Types
export interface NotificationState {
  id: string;
  type: 'info' | 'success' | 'warning' | 'error';
  title: string;
  message?: string;
  duration?: number;
  dismissible?: boolean;
  actions?: NotificationAction[];
}

export interface NotificationAction {
  label: string;
  action: () => void;
  style?: 'primary' | 'secondary' | 'danger';
}

export interface ModalState {
  isOpen: boolean;
  title?: string;
  size?: 'sm' | 'md' | 'lg' | 'xl';
  closeOnOverlay?: boolean;
  closeOnEscape?: boolean;
}

export interface TableState {
  page: number;
  pageSize: number;
  sortBy?: string;
  sortOrder?: 'asc' | 'desc';
  filters?: Record<string, any>;
  selection?: string[];
}

// Form Types
export interface CreateBucketForm {
  name: string;
  region?: string;
  versioning?: boolean;
  objectLock?: boolean;
  encryption?: {
    enabled: boolean;
    algorithm?: string;
    keySource?: 'server' | 'customer';
    masterKey?: string;
  };
}

export interface EditBucketForm {
  versioning?: VersioningConfig;
  cors?: CORSConfig;
  lifecycle?: LifecycleConfig;
  policy?: string; // JSON string
  encryption?: EncryptionConfig;
}

export interface BucketWithReplication {
  name: string;
  tenant_id?: string;
  primary_node: string;
  replica_count: number;
  has_replication: boolean;
  replication_rules: number;
  object_count: number;
  total_size: number;
}

export interface CreateUserForm {
  username: string;
  email?: string;
  password: string;
  confirmPassword: string;
  roles: string[];
  generateAccessKey?: boolean;
}

export interface CreateAccessKeyForm {
  userId: string;
  permissions: string[];
  description?: string;
}

// Error Types
export interface APIError {
  code: string;
  message: string;
  details?: any;
  requestId?: string;
  timestamp?: string;
}

export interface ValidationError {
  field: string;
  message: string;
  code?: string;
}

// Configuration Types
export interface AppConfig {
  apiBaseUrl: string;
  s3ApiUrl: string;
  enableMetrics: boolean;
  enableObjectLock: boolean;
  enableVersioning: boolean;
  maxFileSize: number;
  allowedFileTypes: string[];
  theme: 'light' | 'dark' | 'auto';
  language: string;
  timezone: string;
}

// Hook Return Types
export interface UseAPIResult<T> {
  data: T | null;
  loading: boolean;
  error: APIError | null;
  refetch: () => Promise<void>;
}

export interface UseMutationResult<T, P> {
  mutate: (params: P) => Promise<T>;
  loading: boolean;
  error: APIError | null;
  reset: () => void;
}

// Utility Types
export type SortOrder = 'asc' | 'desc';
export type LoadingState = 'idle' | 'loading' | 'succeeded' | 'failed';
export type ActionType = 'create' | 'read' | 'update' | 'delete';
export type PermissionLevel = 'none' | 'read' | 'write' | 'admin';

// Server Configuration Types
export interface ServerConfig {
  version: string;
  commit: string;
  buildDate: string;
  server: {
    s3ApiPort: string;
    consoleApiPort: string;
    dataDir: string;
    publicApiUrl: string;
    publicConsoleUrl: string;
    enableTls: boolean;
    logLevel: string;
  };
  storage: {
    backend: string;
    root: string;
    enableCompression: boolean;
    compressionType: string;
    compressionLevel: number;
    enableEncryption: boolean;
    enableObjectLock: boolean;
  };
  auth: {
    enableAuth: boolean;
  };
  metrics: {
    enable: boolean;
    path: string;
    interval: number;
  };
  features: {
    multiTenancy: boolean;
    objectLock: boolean;
    versioning: boolean;
    encryption: boolean;
    compression: boolean;
    multipart: boolean;
    presignedUrls: boolean;
    cors: boolean;
    lifecycle: boolean;
    tagging: boolean;
  };
}

// Two-Factor Authentication types
export interface TwoFactorSetupResponse {
  secret: string;
  qr_code: string; // Base64 encoded QR code image
  url: string; // otpauth:// URL
}

export interface TwoFactorEnableRequest {
  code: string;
  secret: string;
}

export interface TwoFactorEnableResponse {
  success: boolean;
  backup_codes: string[];
  message: string;
}

export interface TwoFactorVerifyRequest {
  user_id: string;
  code: string;
}

export interface TwoFactorVerifyResponse {
  success: boolean;
  token?: string;
  user?: User;
  error?: string;
}

export interface TwoFactorStatusResponse {
  enabled: boolean;
  setup_at: number;
}

export interface TwoFactorDisableRequest {
  user_id?: string; // Optional - for admin to disable other user's 2FA
}

// Audit Log Types
export interface AuditLog {
  id: number;
  timestamp: number;
  tenant_id?: string;
  tenantId?: string; // Alias
  user_id: string;
  userId?: string; // Alias
  username: string;
  event_type: string;
  eventType?: string; // Alias
  resource_type?: string;
  resourceType?: string; // Alias
  resource_id?: string;
  resourceId?: string; // Alias
  resource_name?: string;
  resourceName?: string; // Alias
  action: string;
  status: 'success' | 'failed';
  ip_address?: string;
  ipAddress?: string; // Alias
  user_agent?: string;
  userAgent?: string; // Alias
  details?: Record<string, any>;
  created_at: number;
  createdAt?: number; // Alias
}

export interface AuditLogFilters {
  tenant_id?: string;
  tenantId?: string;
  user_id?: string;
  userId?: string;
  event_type?: string;
  eventType?: string;
  resource_type?: string;
  resourceType?: string;
  action?: string;
  status?: 'success' | 'failed' | '';
  start_date?: number;
  startDate?: number;
  end_date?: number;
  endDate?: number;
  page?: number;
  page_size?: number;
  pageSize?: number;
}

export interface AuditLogsResponse {
  logs: AuditLog[];
  total: number;
  page: number;
  page_size: number;
  pageSize?: number; // Alias
}

// Settings Types
export type SettingType = 'string' | 'int' | 'bool' | 'json';
export type SettingCategory = 'security' | 'audit' | 'storage' | 'metrics' | 'logging' | 'system';

export interface Setting {
  key: string;
  value: string;
  type: SettingType;
  category: SettingCategory;
  description: string;
  editable: boolean;
  created_at: string;
  updated_at: string;
}

export interface UpdateSettingRequest {
  value: string;
}

export interface BulkUpdateSettingsRequest {
  settings: Record<string, string>; // key -> value
}

export interface SettingsCategoriesResponse {
  categories: string[];
}

// Logging Target Types
export type LoggingTargetType = 'syslog' | 'http';

export interface LoggingTarget {
  id: string;
  name: string;
  type: LoggingTargetType;
  enabled: boolean;
  protocol: string;
  host: string;
  port: number;
  tag: string;
  format: string;
  tls_enabled: boolean;
  tls_cert: string;
  tls_key: string;
  tls_ca: string;
  tls_skip_verify: boolean;
  filter_level: string;
  auth_token: string;
  url: string;
  batch_size: number;
  flush_interval: number;
  created_at: string;
  updated_at: string;
}

export interface LoggingTargetsResponse {
  targets: LoggingTarget[];
  active_count: number;
}

// Replication Types
export type ReplicationMode = 'realtime' | 'scheduled' | 'batch';
export type ConflictResolution = 'last_write_wins' | 'version_based' | 'primary_wins';

export interface ReplicationRule {
  id: string;
  tenant_id: string;
  source_bucket: string;
  destination_endpoint: string;           // S3 endpoint URL
  destination_bucket: string;
  destination_access_key: string;
  destination_secret_key: string;
  destination_region?: string;
  prefix?: string;
  enabled: boolean;
  priority: number;
  mode: ReplicationMode;
  schedule_interval?: number;             // Interval in minutes for scheduled mode
  conflict_resolution: ConflictResolution;
  replicate_deletes: boolean;
  replicate_metadata: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateReplicationRuleRequest {
  destination_endpoint: string;
  destination_bucket: string;
  destination_access_key: string;
  destination_secret_key: string;
  destination_region?: string;
  prefix?: string;
  enabled: boolean;
  priority: number;
  mode: ReplicationMode;
  schedule_interval?: number;
  conflict_resolution: ConflictResolution;
  replicate_deletes: boolean;
  replicate_metadata: boolean;
}

export interface ReplicationMetrics {
  rule_id: string;
  total_objects: number;
  pending_objects: number;
  completed_objects: number;
  failed_objects: number;
  bytes_replicated: number;
  last_success?: string;
  last_failure?: string;
}

export interface ListReplicationRulesResponse {
  rules: ReplicationRule[];
}

// Cluster Types
export type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown';

export interface ClusterNode {
  id: string;
  name: string;
  endpoint: string;
  node_token?: string; // Only shown when creating/editing
  region?: string;
  priority: number;
  health_status: HealthStatus;
  last_health_check?: string;
  last_seen?: string;
  latency_ms: number;
  capacity_total: number;
  capacity_used: number;
  bucket_count: number;
  metadata?: string;
  created_at: string;
  updated_at: string;
}

export interface ClusterStatus {
  is_enabled: boolean;
  total_nodes: number;
  healthy_nodes: number;
  degraded_nodes: number;
  unavailable_nodes: number;
  total_buckets: number;
  replicated_buckets: number;
  local_buckets: number;
  last_updated: string;
}

export interface ClusterConfig {
  node_id: string;
  node_name: string;
  cluster_token?: string; // Only shown after initialization
  is_cluster_enabled: boolean;
  region?: string;
  created_at: string;
}

export interface InitializeClusterRequest {
  node_name: string;
  region?: string;
}

export interface InitializeClusterResponse {
  message: string;
  cluster_token: string;
  node_name: string;
  region?: string;
}

export interface JoinClusterRequest {
  cluster_token: string;
  node_endpoint: string;
}

export interface AddNodeRequest {
  endpoint: string;
  username: string;
  password: string;
}

export interface UpdateNodeRequest {
  name?: string;
  endpoint?: string;
  region?: string;
  priority?: number;
  metadata?: string;
}

export interface NodeHealthStatus {
  node_id: string;
  status: HealthStatus;
  latency_ms: number;
  last_check: string;
  error_message?: string;
}

export interface CacheStats {
  total_entries: number;
  expired_entries: number;
  valid_entries: number;
  ttl_seconds: number;
}

export interface ListNodesResponse {
  nodes: ClusterNode[];
  total: number;
}

// Cluster Replication Types
export interface ClusterReplicationRule {
  id: string;
  tenant_id: string;
  source_bucket: string;
  destination_node_id: string;
  destination_bucket: string;
  sync_interval_seconds: number;
  enabled: boolean;
  replicate_deletes: boolean;
  replicate_metadata: boolean;
  prefix?: string;
  priority: number;
  last_sync_at?: string;
  last_error?: string;
  objects_replicated: number;
  bytes_replicated: number;
  created_at: string;
  updated_at: string;
}

export interface CreateClusterReplicationRequest {
  tenant_id?: string;
  source_bucket: string;
  destination_node_id: string;
  destination_bucket: string;
  sync_interval_seconds: number;
  enabled: boolean;
  replicate_deletes: boolean;
  replicate_metadata: boolean;
  prefix?: string;
  priority?: number;
}

export interface UpdateClusterReplicationRequest {
  sync_interval_seconds?: number;
  enabled?: boolean;
  replicate_deletes?: boolean;
  replicate_metadata?: boolean;
  priority?: number;
}

export interface BulkClusterReplicationRequest {
  source_node_id?: string;
  destination_node_id: string;
  sync_interval_seconds: number;
  tenant_id?: string;
  enabled: boolean;
}

export interface ListClusterReplicationsResponse {
  rules: ClusterReplicationRule[];
  count: number;
}

// Cluster Migration Types
export type MigrationStatus = 'pending' | 'in_progress' | 'completed' | 'failed' | 'cancelled';

export interface MigrationJob {
  id: number;
  bucket_name: string;
  source_node_id: string;
  target_node_id: string;
  status: MigrationStatus;
  objects_total: number;
  objects_migrated: number;
  bytes_total: number;
  bytes_migrated: number;
  delete_source: boolean;
  verify_data: boolean;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  error_message?: string;
}

export interface MigrateBucketRequest {
  target_node_id: string;
  delete_source: boolean;
  verify_data: boolean;
}

export interface ListMigrationsResponse {
  migrations: MigrationJob[];
  count: number;
}

// Identity Provider Types
export type IDPType = 'ldap' | 'oauth2';
export type IDPStatus = 'active' | 'inactive' | 'testing';

export interface LDAPConfig {
  host: string;
  port: number;
  security: 'none' | 'tls' | 'starttls';
  bind_dn: string;
  bind_password: string;
  base_dn: string;
  user_search_base: string;
  user_filter: string;
  group_search_base: string;
  group_filter: string;
  attr_username: string;
  attr_email: string;
  attr_display_name: string;
  attr_member_of: string;
}

export interface OAuth2Config {
  preset: string;
  client_id: string;
  client_secret: string;
  auth_url: string;
  token_url: string;
  userinfo_url: string;
  scopes: string[];
  redirect_uri: string;
  claim_email: string;
  claim_name: string;
  claim_groups: string;
}

export interface IDPProviderConfig {
  ldap?: LDAPConfig;
  oauth2?: OAuth2Config;
}

export interface IdentityProvider {
  id: string;
  name: string;
  type: IDPType;
  tenantId: string;
  status: IDPStatus;
  config: IDPProviderConfig;
  created_by: string;
  created_at: number;
  updated_at: number;
}

export interface ExternalUser {
  external_id: string;
  username: string;
  email: string;
  display_name: string;
  groups: string[];
  raw_attrs: Record<string, string>;
}

export interface ExternalGroup {
  external_id: string;
  name: string;
  member_count: number;
}

export interface GroupMapping {
  id: string;
  provider_id: string;
  external_group: string;
  external_group_name: string;
  role: string;
  tenant_id: string;
  auto_sync: boolean;
  last_synced_at: number;
  created_at: number;
  updated_at: number;
}

export interface ImportUserEntry {
  external_id: string;
  username: string;
}

export interface ImportResult {
  imported: number;
  skipped: number;
  errors: { external_id: string; error: string }[];
}

export interface SyncResult {
  imported: number;
  updated: number;
  removed: number;
  errors: string[];
}

export interface OAuthProviderInfo {
  preset: string;
  name: string;
}

// Re-export common types
export type { FC, ReactNode, ComponentProps } from 'react';