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
  email?: string;
  roles: string[];
  status: 'active' | 'inactive' | 'suspended';
  createdAt: string;
  lastLogin?: string;
  metadata?: Record<string, string>;
}

export interface AccessKey {
  id: string;
  accessKey: string;
  secretKey?: string; // Only shown when creating
  userId: string;
  status: 'active' | 'inactive';
  permissions: string[];
  createdAt: string;
  lastUsed?: string;
  expiresAt?: string;
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
}

// Bucket Types
export interface Bucket {
  name: string;
  creation_date: string; // Backend uses snake_case
  creationDate?: string; // Alias for compatibility
  region?: string;
  versioning?: VersioningConfig;
  cors?: CORSConfig;
  lifecycle?: LifecycleConfig;
  objectLock?: ObjectLockConfig;
  policy?: BucketPolicy;
  encryption?: EncryptionConfig;
  metadata?: Record<string, string>;
  object_count?: number; // Backend uses snake_case
  objectCount?: number; // Alias for compatibility
  size?: number; // Backend uses 'size'
  totalSize?: number; // Alias for compatibility
}

export interface VersioningConfig {
  status: 'Enabled' | 'Suspended';
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
  rules: LifecycleRule[];
}

export interface LifecycleRule {
  id: string;
  status: 'Enabled' | 'Disabled';
  filter?: {
    prefix?: string;
    tags?: Record<string, string>;
  };
  transitions?: LifecycleTransition[];
  expiration?: {
    days?: number;
    date?: string;
  };
}

export interface LifecycleTransition {
  days: number;
  storageClass: string;
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
  prefix?: string;
  delimiter?: string;
  maxKeys?: number;
  continuationToken?: string;
  fetchOwner?: boolean;
  startAfter?: string;
}

// Upload Types
export interface UploadRequest {
  bucket: string;
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
  largestObjectSize: number;
  smallestObjectSize: number;
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
  memoryUsagePercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes: number;
  diskUsagePercent: number;
  diskUsedBytes: number;
  diskTotalBytes: number;
  networkBytesIn: number;
  networkBytesOut: number;
  timestamp: number;
}

export interface S3Metrics {
  requestsTotal: Record<string, number>;
  errorsTotal: Record<string, number>;
  averageResponseTime: Record<string, number>;
  activeConnections: number;
  authSuccessRate: number;
  authFailures: Record<string, number>;
  timestamp: number;
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

export interface CreateUserForm {
  username: string;
  email?: string;
  password: string;
  confirmPassword: string;
  roles: string[];
  generateAccessKey?: boolean;
}

export interface EditUserForm {
  email?: string;
  roles: string[];
  status: 'active' | 'inactive' | 'suspended';
}

export interface CreateAccessKeyForm {
  userId: string;
  permissions: string[];
  expiresAt?: string;
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

// Re-export common types
export type { FC, ReactNode, ComponentProps } from 'react';