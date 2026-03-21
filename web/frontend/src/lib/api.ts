import axios, { AxiosInstance, AxiosResponse, AxiosError } from 'axios';
import { isErrorWithResponse } from '@/lib/utils';
import type {
  APIResponse,
  User,
  LoginRequest,
  LoginResponse,
  Bucket,
  S3Object,
  ListBucketsResponse,
  ListObjectsResponse,
  ListObjectsRequest,
  ListObjectVersionsResponse,
  GeneratePresignedURLRequest,
  GeneratePresignedURLResponse,
  UploadRequest,
  DownloadRequest,
  StorageMetrics,
  SystemMetrics,
  S3Metrics,
  ServerConfig,
  CreateBucketForm,
  EditBucketForm,
  CreateUserForm,
  CreateUserRequest,
  EditUserForm,
  APIError,
  AccessKey,
  CreateAccessKeyForm,
  Tenant,
  CreateTenantRequest,
  UpdateTenantRequest,
  BucketPermission,
  GrantPermissionRequest,
  Group,
  GroupMember,
  CreateGroupRequest,
  UpdateGroupRequest,
  AuditLog,
  AuditLogFilters,
  AuditLogsResponse,
  Setting,
  UpdateSettingRequest,
  BulkUpdateSettingsRequest,
  SettingsCategoriesResponse,
  ReplicationRule,
  CreateReplicationRuleRequest,
  ReplicationMetrics,
  ListReplicationRulesResponse,
  ClusterNode,
  ClusterStatus,
  ClusterConfig,
  InitializeClusterRequest,
  InitializeClusterResponse,
  JoinClusterRequest,
  AddNodeRequest,
  UpdateNodeRequest,
  NodeHealthStatus,
  CacheStats,
  ListNodesResponse,
  BucketWithReplication,
  ClusterReplicationRule,
  CreateClusterReplicationRequest,
  UpdateClusterReplicationRequest,
  BulkClusterReplicationRequest,
  ListClusterReplicationsResponse,
  MigrationJob,
  MigrateBucketRequest,
  ListMigrationsResponse,
  LatenciesResponse,
  ThroughputResponse,
  SearchObjectsRequest,
  IdentityProvider,
  ExternalUser,
  ExternalGroup,
  GroupMapping,
  ImportResult,
  SyncResult,
  OAuthProviderInfo,
  LoggingTarget,
  LoggingTargetsResponse,
  BucketIntegrityReport,
  LastIntegrityScan,
} from '@/types';

// API Configuration
// For monolithic deployment: Use relative URLs so frontend works with both HTTP and HTTPS
// The frontend is served from the same server as the API (port 8081)
import { getBasePath } from '@/lib/basePath';

const API_CONFIG = {
  baseURL: `${getBasePath()}/api/v1`, // Dynamic base URL based on public_console_url
  s3URL: typeof window !== 'undefined'
    ? `${window.location.protocol}//${window.location.hostname}:8080` // S3 API on port 8080 (auto-detects HTTP/HTTPS from browser)
    : 'https://localhost:8080', // Fallback to HTTPS for SSR/SSG (TLS is typically enabled in production)
  timeout: 30000,
  withCredentials: false, // Changed to false for development CORS
};

// Create axios instances
const apiClient: AxiosInstance = axios.create({
  baseURL: API_CONFIG.baseURL,
  timeout: API_CONFIG.timeout,
  withCredentials: false, // CORS support for development
  headers: {
    'Content-Type': 'application/json',
  },
});

const s3Client: AxiosInstance = axios.create({
  baseURL: API_CONFIG.s3URL,
  timeout: API_CONFIG.timeout,
  withCredentials: false, // CORS support for development
});

// Token management
class TokenManager {
  private static instance: TokenManager;
  private token: string | null = null;
  private refreshToken: string | null = null;
  private proactiveRefreshTimer: ReturnType<typeof setTimeout> | null = null;

  private constructor() {
    // Load tokens from localStorage if available
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('auth_token');
      this.refreshToken = localStorage.getItem('refresh_token');
      // Reschedule proactive refresh if we already have a token (e.g. page reload)
      if (this.token) {
        this.scheduleProactiveRefresh(this.token);
      }
    }
  }

  static getInstance(): TokenManager {
    if (!TokenManager.instance) {
      TokenManager.instance = new TokenManager();
    }
    return TokenManager.instance;
  }

  getToken(): string | null {
    return this.token;
  }

  getRefreshToken(): string | null {
    return this.refreshToken;
  }

  setTokens(token: string, refreshToken?: string): void {
    this.token = token;
    this.refreshToken = refreshToken || null;

    if (typeof window !== 'undefined') {
      localStorage.setItem('auth_token', token);
      if (refreshToken) {
        localStorage.setItem('refresh_token', refreshToken);
      }

      // Also set in cookies for middleware (24 hours max)
      // Secure: never sent over HTTP · SameSite=Strict: CSRF protection
      document.cookie = `auth_token=${token}; path=/; max-age=${24 * 60 * 60}; Secure; SameSite=Strict`;
      if (refreshToken) {
        document.cookie = `refresh_token=${refreshToken}; path=/; max-age=${24 * 60 * 60}; Secure; SameSite=Strict`;
      }
    }

    // Schedule a proactive refresh 2 min before the access token expires
    this.scheduleProactiveRefresh(token);
  }

  clearTokens(): void {
    this.token = null;
    this.refreshToken = null;

    if (this.proactiveRefreshTimer !== null) {
      clearTimeout(this.proactiveRefreshTimer);
      this.proactiveRefreshTimer = null;
    }

    if (typeof window !== 'undefined') {
      localStorage.removeItem('auth_token');
      localStorage.removeItem('refresh_token');

      // Also clear cookies
      document.cookie = 'auth_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT; Secure; SameSite=Strict';
      document.cookie = 'refresh_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT; Secure; SameSite=Strict';
    }
  }

  isAuthenticated(): boolean {
    return !!this.token;
  }

  // Proactively refresh the access token 2 min before it expires so the user
  // never hits a 401 mid-session.
  private scheduleProactiveRefresh(token: string): void {
    if (this.proactiveRefreshTimer !== null) {
      clearTimeout(this.proactiveRefreshTimer);
      this.proactiveRefreshTimer = null;
    }

    try {
      const parts = token.split('.');
      if (parts.length !== 3) return;
      const payload = JSON.parse(atob(parts[1]));
      const exp: number = payload.exp;
      if (!exp) return;

      const nowSecs = Math.floor(Date.now() / 1000);
      const ttl = exp - nowSecs;
      // Refresh 2 min before expiry; wait at least 5 s to avoid tight loops
      const refreshInMs = Math.max((ttl - 120) * 1000, 5000);

      this.proactiveRefreshTimer = setTimeout(async () => {
        const rt = this.refreshToken;
        if (!rt) return;
        try {
          const resp = await axios.post(
            `${API_CONFIG.baseURL}/auth/refresh`,
            { refresh_token: rt },
            { headers: { 'Content-Type': 'application/json' } }
          );
          const { access_token, refresh_token } = resp.data;
          if (access_token) {
            this.setTokens(access_token, refresh_token);
          }
        } catch {
          // Silent — the response interceptor will handle the eventual 401
        }
      }, refreshInMs);
    } catch {
      // Ignore JWT parse errors
    }
  }
}

const tokenManager = TokenManager.getInstance();

// Request interceptors
apiClient.interceptors.request.use(
  (config) => {
    // Don't add Authorization header to login/register/2fa-verify requests
    const isAuthRequest = config.url?.includes('/auth/login') ||
                         config.url?.includes('/auth/register') ||
                         config.url?.includes('/auth/2fa/verify');

    if (!isAuthRequest) {
      const token = tokenManager.getToken();
      if (token) {
        config.headers.Authorization = `Bearer ${token}`;
      }
    }
    return config;
  },
  (error) => Promise.reject(error)
);

s3Client.interceptors.request.use(
  (config) => {
    const token = tokenManager.getToken();
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

// Response interceptors
const handleResponse = (response: AxiosResponse): AxiosResponse => {
  return response;
};

// Track if we're already redirecting to prevent loops
let isRedirectingToLogin = false;
// Refresh-and-retry state
let isRefreshing = false;
let pendingRefreshCallbacks: ((token: string) => void)[] = [];

function doLogout(): void {
  tokenManager.clearTokens();
  if (!isRedirectingToLogin && typeof window !== 'undefined') {
    isRedirectingToLogin = true;
    setTimeout(() => window.location.replace(`${getBasePath()}/login`), 100);
  }
}

const handleError = async (error: AxiosError): Promise<unknown> => {
  // Handle 401 errors — attempt token refresh before logging out
  if (error.response?.status === 401) {
    const requestUrl = error.config?.url ?? '';
    const isAuthEndpoint =
      requestUrl.includes('/auth/login') || requestUrl.includes('/auth/refresh');

    if (!isAuthEndpoint) {
      const originalRequest = error.config!;

      if (!(originalRequest as any)._retry) {
        // If a refresh is already in flight, queue this request
        if (isRefreshing) {
          return new Promise((resolve, reject) => {
            pendingRefreshCallbacks.push((newToken: string) => {
              (originalRequest.headers as any)['Authorization'] = `Bearer ${newToken}`;
              apiClient(originalRequest).then(resolve as any).catch(reject);
            });
          });
        }

        (originalRequest as any)._retry = true;
        isRefreshing = true;

        try {
          const rt = tokenManager.getRefreshToken();
          if (!rt) {
            // No refresh token at all — hard logout, nothing to recover
            isRefreshing = false;
            pendingRefreshCallbacks = [];
            doLogout();
            return Promise.reject(error);
          }

          const resp = await axios.post(
            `${API_CONFIG.baseURL}/auth/refresh`,
            { refresh_token: rt },
            { headers: { 'Content-Type': 'application/json' } }
          );

          const { access_token, refresh_token } = resp.data;
          tokenManager.setTokens(access_token, refresh_token);
          isRefreshing = false;
          // Resume queued requests with the new token
          pendingRefreshCallbacks.forEach(cb => cb(access_token));
          pendingRefreshCallbacks = [];

          (originalRequest.headers as any)['Authorization'] = `Bearer ${access_token}`;
          return apiClient(originalRequest);
        } catch (refreshErr: unknown) {
          isRefreshing = false;
          pendingRefreshCallbacks = [];

          // Only log out if the refresh endpoint explicitly rejected the token
          // (401 / 403). Network errors or 5xx (server restarting) are transient
          // — do NOT log out; let the user retry.
          const refreshStatus = (refreshErr as any)?.response?.status;
          const isAuthRejection = refreshStatus === 401 || refreshStatus === 403;
          if (isAuthRejection) {
            doLogout();
          }
          return Promise.reject(error);
        }
      }

      // _retry already set means the refresh succeeded but the retried request
      // still returned 401 — legitimate auth failure, log out.
      doLogout();
    }

    return Promise.reject(error);
  }

  // Extract error info from response (Console API: { error }; S3/XML may be string)
  const data = error.response?.data as { code?: string; Code?: string; error?: string; Message?: string; message?: string } | undefined;
  const code = (data && typeof data === 'object' && (data.code ?? data.Code)) || error.code || 'UNKNOWN_ERROR';
  const rawMessage =
    (data && typeof data === 'object' && (data.error ?? data.Message ?? data.message)) ||
    error.message ||
    'An unknown error occurred';

  // Map known S3/API codes to user-friendly messages (use rawMessage if no mapping)
  const friendlyMessages: Record<string, string> = {
    QuotaExceeded: 'Storage quota exceeded',
    AccessDenied: 'Access denied',
    NoSuchBucket: 'The bucket does not exist',
    NoSuchKey: 'The object does not exist',
    NoSuchUpload: 'The multipart upload does not exist',
    InvalidPart: 'One or more parts could not be found',
    MalformedPolicy: 'Invalid policy document',
    InvalidTag: 'Invalid tag',
    IllegalVersioningConfigurationException: 'Invalid versioning configuration',
  };
  const message = friendlyMessages[code] ?? (typeof rawMessage === 'string' ? rawMessage : 'An unknown error occurred');

  const apiError: APIError = {
    code: String(code),
    message,
    details: data,
    requestId: error.response?.headers?.['x-request-id'] ?? error.response?.headers?.['X-Amz-Request-Id'],
    timestamp: new Date().toISOString(),
  };

  throw apiError;
};

apiClient.interceptors.response.use(handleResponse, handleError);
s3Client.interceptors.response.use(handleResponse, handleError);

// API Client Class
export class APIClient {
  // S3 base URL — updated at runtime from serverConfig
  static updateS3BaseUrl(url: string): void {
    s3Client.defaults.baseURL = url;
  }

  // Authentication
  static async login(credentials: LoginRequest): Promise<LoginResponse> {
    const payload = {
      username: credentials.username,
      password: credentials.password,
    };

    try {
      const response = await apiClient.post<any>('/auth/login', payload);

      const result: LoginResponse = {
        success: response.data.success,
        token: response.data.token,
        refreshToken: response.data.refreshToken,
        user: response.data.user,
        error: response.data.error,
        requires_2fa: response.data.requires_2fa,
        user_id: response.data.user_id,
        message: response.data.message,
        default_password: response.data.default_password,
        sso_hint: response.data.sso_hint,
      };

      if (result.success && result.token) {
        tokenManager.setTokens(result.token, result.refreshToken);
        // Track default password warning
        if (result.default_password) {
          localStorage.setItem('default_password_warning', 'true');
        } else {
          localStorage.removeItem('default_password_warning');
        }
      }

      return result;
    } catch (err: unknown) {
      // Extract sso_hint from error responses (400 status)
      if (isErrorWithResponse(err) && err.response?.data?.sso_hint) {
        return {
          success: false,
          error: err.response.data.error,
          sso_hint: true,
        };
      }
      throw err;
    }
  }

  static async logout(): Promise<void> {
    try {
      await apiClient.post('/auth/logout');
    } finally {
      tokenManager.clearTokens();
      // Clear auth cookie
      if (typeof document !== 'undefined') {
        document.cookie = 'auth_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
      }
    }
  }

  static async getCurrentUser(): Promise<User> {
    const response = await apiClient.get<APIResponse<User>>('/auth/me');
    return response.data.data!;
  }

  // Users Management
  static async getUsers(): Promise<User[]> {
    const response = await apiClient.get<APIResponse<User[]>>('/users');
    return response.data.data || [];
  }

  static async getUser(userId: string): Promise<User> {
    const response = await apiClient.get<APIResponse<User>>(`/users/${userId}`);
    return response.data.data!;
  }

  static async createUser(userData: CreateUserRequest): Promise<User> {
    const response = await apiClient.post<APIResponse<User>>('/users', userData);
    return response.data.data!;
  }

  static async updateUser(userId: string, userData: EditUserForm): Promise<User> {
    const response = await apiClient.put<APIResponse<User>>(`/users/${userId}`, userData);
    return response.data.data!;
  }

  static async deleteUser(userId: string): Promise<void> {
    await apiClient.delete(`/users/${userId}`);
  }

  static async changePassword(userId: string, currentPassword: string, newPassword: string): Promise<void> {
    await apiClient.put(`/users/${userId}/password`, {
      currentPassword,
      newPassword,
    });
    // Clear default password warning after successful password change
    localStorage.removeItem('default_password_warning');
    window.dispatchEvent(new Event('default-password-changed'));
  }

  static async updateUserPreferences(userId: string, themePreference: string, languagePreference: string): Promise<User> {
    const response = await apiClient.patch<User>(`/users/${userId}/preferences`, {
      themePreference,
      languagePreference,
    });
    return response.data;
  }

  static async unlockUser(userId: string): Promise<void> {
    await apiClient.post(`/users/${userId}/unlock`);
  }

  // Access Keys Management
  static async getAccessKeys(userId?: string): Promise<AccessKey[]> {
    const url = userId ? `/users/${userId}/access-keys` : '/access-keys';
    const response = await apiClient.get<APIResponse<AccessKey[]>>(url);
    return response.data.data || [];
  }

  static async createAccessKey(keyData: { userId: string }): Promise<AccessKey> {
    const response = await apiClient.post<APIResponse<any>>(`/users/${keyData.userId}/access-keys`);
    return response.data.data!;
  }

  static async deleteAccessKey(userId: string, keyId: string): Promise<void> {
    await apiClient.delete(`/users/${userId}/access-keys/${keyId}`);
  }

  // Buckets Management
  static async getBuckets(): Promise<Bucket[]> {
    const response = await apiClient.get<APIResponse<Bucket[]>>('/buckets');
    return response.data.data || [];
  }

  static async getBucket(bucketName: string, tenantId?: string): Promise<Bucket> {
    const url = tenantId
      ? `/buckets/${bucketName}?tenantId=${encodeURIComponent(tenantId)}`
      : `/buckets/${bucketName}`;
    const response = await apiClient.get<APIResponse<Bucket>>(url);
    return response.data.data!;
  }

  static async createBucket(bucketData: any): Promise<Bucket> {
    const response = await apiClient.post<APIResponse<Bucket>>('/buckets', bucketData);
    return response.data.data!;
  }

  static async deleteBucket(bucketName: string, tenantId?: string, force?: boolean): Promise<void> {
    let url = tenantId
      ? `/buckets/${bucketName}?tenantId=${encodeURIComponent(tenantId)}`
      : `/buckets/${bucketName}`;

    // Add force parameter if requested
    if (force) {
      url += tenantId ? '&force=true' : '?force=true';
    }

    await apiClient.delete(url);
  }

  static async verifyBucketIntegrity(
    bucketName: string,
    params: { prefix?: string; marker?: string; maxKeys?: number; tenantId?: string } = {}
  ): Promise<BucketIntegrityReport> {
    const query = new URLSearchParams();
    if (params.prefix)   query.set('prefix',   params.prefix);
    if (params.marker)   query.set('marker',   params.marker);
    if (params.maxKeys)  query.set('maxKeys',  String(params.maxKeys));
    if (params.tenantId) query.set('tenantId', params.tenantId);
    const qs = query.toString() ? `?${query.toString()}` : '';
    const response = await apiClient.post<APIResponse<BucketIntegrityReport>>(
      `/buckets/${bucketName}/verify-integrity${qs}`
    );
    return response.data.data!;
  }

  static async getIntegrityHistory(
    bucketName: string,
    tenantId?: string
  ): Promise<LastIntegrityScan[]> {
    const query = new URLSearchParams();
    if (tenantId) query.set('tenantId', tenantId);
    const qs = query.toString() ? `?${query.toString()}` : '';
    try {
      const response = await apiClient.get<APIResponse<LastIntegrityScan[]>>(
        `/buckets/${bucketName}/integrity-status${qs}`
      );
      return response.data.data ?? [];
    } catch (err: any) {
      if (err?.response?.status === 404) return [];
      throw err;
    }
  }

  static async saveIntegrityScan(
    bucketName: string,
    data: Omit<BucketIntegrityReport, 'bucket' | 'nextMarker'>,
    tenantId?: string
  ): Promise<void> {
    const query = new URLSearchParams();
    if (tenantId) query.set('tenantId', tenantId);
    const qs = query.toString() ? `?${query.toString()}` : '';
    await apiClient.post(`/buckets/${bucketName}/integrity-status${qs}`, data);
  }

  static async updateBucketConfig(bucketName: string, config: EditBucketForm): Promise<Bucket> {
    const response = await apiClient.put<APIResponse<Bucket>>(`/buckets/${bucketName}`, config);
    return response.data.data!;
  }

  // Objects Management
  static async getObjects(request: ListObjectsRequest): Promise<ListObjectsResponse> {
    const params = new URLSearchParams();
    if (request.prefix) params.append('prefix', request.prefix);
    if (request.delimiter) params.append('delimiter', request.delimiter);
    if (request.maxKeys) params.append('max_keys', request.maxKeys.toString());
    if (request.continuationToken) params.append('marker', request.continuationToken);
    if (request.tenantId) params.append('tenantId', request.tenantId);

    const response = await apiClient.get<APIResponse<ListObjectsResponse>>(
      `/buckets/${request.bucket}/objects?${params.toString()}`
    );
    return response.data.data!;
  }

  static async searchObjects(request: SearchObjectsRequest): Promise<ListObjectsResponse> {
    const params = new URLSearchParams();
    if (request.prefix) params.append('prefix', request.prefix);
    if (request.delimiter) params.append('delimiter', request.delimiter);
    if (request.maxKeys) params.append('max_keys', request.maxKeys.toString());
    if (request.marker) params.append('marker', request.marker);
    if (request.tenantId) params.append('tenantId', request.tenantId);

    if (request.filter) {
      const f = request.filter;
      if (f.contentTypes && f.contentTypes.length > 0) {
        params.append('content_type', f.contentTypes.join(','));
      }
      if (f.minSize !== undefined) params.append('min_size', f.minSize.toString());
      if (f.maxSize !== undefined) params.append('max_size', f.maxSize.toString());
      if (f.modifiedAfter) params.append('modified_after', f.modifiedAfter);
      if (f.modifiedBefore) params.append('modified_before', f.modifiedBefore);
      if (f.tags) {
        for (const [key, value] of Object.entries(f.tags)) {
          params.append('tag', `${key}:${value}`);
        }
      }
    }

    const response = await apiClient.get<APIResponse<ListObjectsResponse>>(
      `/buckets/${request.bucket}/objects/search?${params.toString()}`
    );
    return response.data.data!;
  }

  static async getObject(bucket: string, key: string, tenantId?: string, versionId?: string): Promise<S3Object> {
    const url = tenantId
      ? `/buckets/${bucket}/objects/${key}?tenantId=${encodeURIComponent(tenantId)}`
      : `/buckets/${bucket}/objects/${key}`;
    const response = await apiClient.get<APIResponse<S3Object>>(url);
    return response.data.data!;
  }

  static async uploadObject(request: UploadRequest): Promise<S3Object> {
    const uploadUrl = request.tenantId
      ? `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}?tenantId=${encodeURIComponent(request.tenantId)}`
      : `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}`;

    // Send the File/Blob directly — no arrayBuffer() — lets the browser stream
    // the file without loading it all into memory first.
    const config = {
      headers: {
        'Content-Type': request.file.type || 'application/octet-stream',
        'Content-Length': request.file.size.toString(),
      } as Record<string, string>,
      timeout: 0, // No timeout — large files can take minutes; server controls via context
      maxContentLength: Infinity,
      maxBodyLength: Infinity,
      onUploadProgress: request.onProgress ? (progressEvent: any) => {
        const total = progressEvent.total ?? request.file.size;
        request.onProgress!({
          loaded: progressEvent.loaded,
          total,
          percentage: total > 0 ? Math.round((progressEvent.loaded * 100) / total) : 0,
          speed: 0,
          timeRemaining: 0,
        });
      } : undefined,
    };

    // Add metadata headers if provided
    if (request.metadata) {
      Object.entries(request.metadata).forEach(([key, value]) => {
        config.headers[`x-amz-meta-${key}`] = value;
      });
    }

    const response = await apiClient.put<APIResponse<S3Object>>(
      uploadUrl,
      request.file, // Stream the File directly — no memory copy
      config
    );
    return response.data.data!;
  }

  static async downloadObject(request: DownloadRequest): Promise<Blob> {
    const url = request.tenantId
      ? `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}?tenantId=${encodeURIComponent(request.tenantId)}`
      : `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}`;

    const config = {
      responseType: 'blob' as const,
      headers: {
        ...(request.range ? { Range: request.range } : {}),
        // Don't send Accept: application/json for downloads
        'Accept': '*/*',
      },
      onDownloadProgress: request.onProgress ? (progressEvent: any) => {
        const progress = {
          loaded: progressEvent.loaded,
          total: progressEvent.total,
          percentage: Math.round((progressEvent.loaded * 100) / progressEvent.total),
          speed: 0, // TODO: Calculate speed
        };
        request.onProgress!(progress);
      } : undefined,
    };

    // Use API client with authentication instead of direct S3 client
    const response = await apiClient.get<Blob>(url, config);
    return response.data;
  }

  static async deleteObject(bucket: string, key: string, tenantId?: string, versionId?: string): Promise<void> {
    let url = `/buckets/${bucket}/objects/${key}`;
    const params = new URLSearchParams();

    if (tenantId) {
      params.append('tenantId', tenantId);
    }
    if (versionId) {
      params.append('versionId', versionId);
    }

    if (params.toString()) {
      url += `?${params.toString()}`;
    }

    await apiClient.delete(url);
  }

  static async shareObject(bucket: string, key: string, expiresIn: number | null = 3600, tenantId?: string): Promise<{ id: string; url: string; expiresAt?: string; createdAt: string; isExpired: boolean; existing: boolean }> {
    const url = tenantId 
      ? `/buckets/${bucket}/objects/${encodeURIComponent(key)}/share?tenantId=${tenantId}`
      : `/buckets/${bucket}/objects/${encodeURIComponent(key)}/share`;
    const response = await apiClient.post<APIResponse<{ id: string; url: string; expiresAt?: string; createdAt: string; isExpired: boolean; existing: boolean }>>(
      url,
      { expiresIn }
    );
    return response.data.data!;
  }

  static async getBucketShares(bucket: string, tenantId?: string): Promise<Record<string, any>> {
    const url = tenantId
      ? `/buckets/${bucket}/shares?tenantId=${tenantId}`
      : `/buckets/${bucket}/shares`;
    const response = await apiClient.get<APIResponse<Record<string, any>>>(url);
    return response.data.data || {};
  }

  static async deleteShare(bucket: string, key: string, tenantId?: string): Promise<void> {
    const url = tenantId
      ? `/buckets/${bucket}/objects/${encodeURIComponent(key)}/share?tenantId=${tenantId}`
      : `/buckets/${bucket}/objects/${encodeURIComponent(key)}/share`;
    await apiClient.delete(url);
  }

  // Object Versioning
  static async listObjectVersions(bucket: string, key: string, tenantId?: string): Promise<ListObjectVersionsResponse> {
    const url = tenantId
      ? `/buckets/${bucket}/objects/${encodeURIComponent(key)}/versions?tenantId=${tenantId}`
      : `/buckets/${bucket}/objects/${encodeURIComponent(key)}/versions`;

    const response = await apiClient.get<APIResponse<ListObjectVersionsResponse>>(url);
    return response.data.data!;
  }

  // Presigned URLs
  static async generatePresignedURL(request: GeneratePresignedURLRequest): Promise<GeneratePresignedURLResponse> {
    const url = request.tenantId
      ? `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}/presigned-url?tenantId=${request.tenantId}`
      : `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}/presigned-url`;

    const response = await apiClient.post<APIResponse<GeneratePresignedURLResponse>>(url, {
      expiresIn: request.expiresIn,
      method: request.method || 'GET'
    });
    return response.data.data!;
  }

  static async copyObject(
    sourceBucket: string,
    sourceKey: string,
    destBucket: string,
    destKey: string
  ): Promise<S3Object> {
    const response = await s3Client.put<S3Object>(`/${destBucket}/${destKey}`, null, {
      headers: {
        'x-amz-copy-source': `/${sourceBucket}/${sourceKey}`,
      },
    });
    return response.data;
  }

  // Metrics
  static async getStorageMetrics(): Promise<StorageMetrics> {
    const response = await apiClient.get<APIResponse<StorageMetrics>>('/metrics');
    return response.data.data!;
  }

  static async getSystemMetrics(): Promise<SystemMetrics> {
    const response = await apiClient.get<APIResponse<SystemMetrics>>('/metrics/system');
    return response.data.data!;
  }

  static async getS3Metrics(): Promise<S3Metrics> {
    const response = await apiClient.get<APIResponse<S3Metrics>>('/metrics/s3');
    return response.data.data!;
  }

  // Performance Metrics (from PerformanceCollector)
  static async getPerformanceLatencies(): Promise<LatenciesResponse> {
    const response = await apiClient.get<LatenciesResponse>('/metrics/performance/latencies');
    return response.data;
  }

  static async getPerformanceThroughput(): Promise<ThroughputResponse> {
    const response = await apiClient.get<ThroughputResponse>('/metrics/performance/throughput');
    return response.data;
  }

  // Historical Metrics
  static async getHistoricalMetrics(params: {
    type?: string;
    start?: number | string;
    end?: number | string;
  }): Promise<any> {
    const queryParams = new URLSearchParams();
    if (params.type) queryParams.append('type', params.type);
    if (params.start) queryParams.append('start', params.start.toString());
    if (params.end) queryParams.append('end', params.end.toString());

    const response = await apiClient.get<APIResponse<any>>(`/metrics/history?${queryParams.toString()}`);
    return response.data.data!;
  }

  static async getHistoryStats(): Promise<any> {
    const response = await apiClient.get<APIResponse<any>>('/metrics/history/stats');
    return response.data.data!;
  }

  // Metrics
  static async getMetrics(): Promise<APIResponse<any>> {
    const response = await apiClient.get<APIResponse<any>>('/metrics');
    return response.data;
  }

  // Server Configuration
  static async getServerConfig(): Promise<ServerConfig> {
    const response = await apiClient.get<APIResponse<ServerConfig>>('/config');
    return response.data.data!;
  }

  static async getVersion(): Promise<{ version: string }> {
    const response = await apiClient.get<APIResponse<{ version: string }>>('/version');
    return response.data.data!;
  }

  static async getVersionCheck(): Promise<{ version: string }> {
    const response = await apiClient.get<APIResponse<{ version: string }>>('/version-check');
    return response.data.data!;
  }

  // Objects - Additional methods
  static async getAllObjects(): Promise<APIResponse<any[]>> {
    const response = await apiClient.get<APIResponse<any[]>>('/objects');
    return response.data;
  }

  static getObjectUrl(bucket: string, key: string): string {
    return `${s3Client.defaults.baseURL}/${bucket}/${key}`;
  }

  // User Permissions
  static async getUserPermissions(userId: string): Promise<APIResponse<any[]>> {
    const response = await apiClient.get<APIResponse<any[]>>(`/users/${userId}/permissions`);
    return response.data;
  }

  static async updateUserPermissions(userId: string, permissions: any[]): Promise<APIResponse<any>> {
    const response = await apiClient.put<APIResponse<any>>(`/users/${userId}/permissions`, { permissions });
    return response.data;
  }

  // Access Keys
  static async getUserAccessKeys(userId: string): Promise<AccessKey[]> {
    const response = await apiClient.get<APIResponse<AccessKey[]>>(`/users/${userId}/access-keys`);
    return response.data.data || [];
  }

  // Bucket Settings
  static async updateBucketSettings(bucketName: string, settings: any): Promise<APIResponse<any>> {
    const response = await apiClient.put<APIResponse<any>>(`/buckets/${bucketName}/settings`, settings);
    return response.data;
  }

  // Bucket Versioning
  static async getBucketVersioning(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/versioning?tenantId=${tenantId}` : `/buckets/${bucketName}/versioning`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketVersioning(bucketName: string, enabled: boolean, tenantId?: string): Promise<void> {
    const status = enabled ? 'Enabled' : 'Suspended';
    const url = tenantId ? `/buckets/${bucketName}/versioning?tenantId=${tenantId}` : `/buckets/${bucketName}/versioning`;
    await apiClient.put(url, { status });
  }

  // Object Legal Hold
  static async getObjectLegalHold(bucketName: string, objectKey: string): Promise<{ status: string }> {
    const response = await apiClient.get(`/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}/legal-hold`);
    return response.data;
  }

  static async putObjectLegalHold(bucketName: string, objectKey: string, enabled: boolean): Promise<void> {
    const status = enabled ? 'ON' : 'OFF';
    await apiClient.put(`/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}/legal-hold`, { status });
  }

  // Bucket Policy
  static async getBucketPolicy(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/policy?tenantId=${tenantId}` : `/buckets/${bucketName}/policy`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketPolicy(bucketName: string, policy: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/policy?tenantId=${tenantId}` : `/buckets/${bucketName}/policy`;
    await apiClient.put(url, policy, {
      headers: { 'Content-Type': 'application/json' }
    });
  }

  static async deleteBucketPolicy(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/policy?tenantId=${tenantId}` : `/buckets/${bucketName}/policy`;
    await apiClient.delete(url);
  }

  // Bucket CORS
  static async getBucketCORS(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/cors?tenantId=${tenantId}` : `/buckets/${bucketName}/cors`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketCORS(bucketName: string, cors: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/cors?tenantId=${tenantId}` : `/buckets/${bucketName}/cors`;
    await apiClient.put(url, cors, {
      headers: { 'Content-Type': 'application/xml' }
    });
  }

  static async deleteBucketCORS(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/cors?tenantId=${tenantId}` : `/buckets/${bucketName}/cors`;
    await apiClient.delete(url);
  }

  // Bucket Tagging
  static async getBucketTagging(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/tagging?tenantId=${tenantId}` : `/buckets/${bucketName}/tagging`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketTagging(bucketName: string, tagging: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/tagging?tenantId=${tenantId}` : `/buckets/${bucketName}/tagging`;
    await apiClient.put(url, tagging, {
      headers: { 'Content-Type': 'application/xml' }
    });
  }

  static async deleteBucketTagging(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/tagging?tenantId=${tenantId}` : `/buckets/${bucketName}/tagging`;
    await apiClient.delete(url);
  }

  // Bucket ACL
  static async getBucketACL(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/acl?tenantId=${tenantId}` : `/buckets/${bucketName}/acl`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketACL(bucketName: string, acl: string | object, cannedACL?: string, tenantId?: string): Promise<void> {
    const headers: any = {};
    const url = tenantId ? `/buckets/${bucketName}/acl?tenantId=${tenantId}` : `/buckets/${bucketName}/acl`;

    if (cannedACL) {
      // Use canned ACL via header
      headers['x-amz-acl'] = cannedACL;
      await apiClient.put(url, '', { headers });
    } else {
      // Use custom ACL via XML body
      const aclXml = typeof acl === 'string' ? acl : this.convertACLToXML(acl);
      headers['Content-Type'] = 'application/xml';
      await apiClient.put(url, aclXml, { headers });
    }
  }

  // Object ACL
  static async getObjectACL(bucketName: string, objectKey: string): Promise<any> {
    const response = await apiClient.get(`/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}/acl`);
    return response.data;
  }

  static async putObjectACL(bucketName: string, objectKey: string, acl: string | object, cannedACL?: string): Promise<void> {
    const headers: any = {};

    if (cannedACL) {
      // Use canned ACL via header
      headers['x-amz-acl'] = cannedACL;
      await apiClient.put(`/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}/acl`, '', { headers });
    } else {
      // Use custom ACL via XML body
      const aclXml = typeof acl === 'string' ? acl : this.convertACLToXML(acl);
      headers['Content-Type'] = 'application/xml';
      await apiClient.put(`/buckets/${bucketName}/objects/${encodeURIComponent(objectKey)}/acl`, aclXml, { headers });
    }
  }

  // Helper to convert ACL object to XML
  private static convertACLToXML(acl: any): string {
    let xml = '<?xml version="1.0" encoding="UTF-8"?>\n<AccessControlPolicy>\n';

    // Owner
    xml += '  <Owner>\n';
    xml += `    <ID>${acl.owner?.id || acl.Owner?.ID || 'maxiofs'}</ID>\n`;
    xml += `    <DisplayName>${acl.owner?.displayName || acl.Owner?.DisplayName || 'MaxIOFS'}</DisplayName>\n`;
    xml += '  </Owner>\n';

    // Access Control List
    xml += '  <AccessControlList>\n';
    const grants = acl.grants || acl.Grants || acl.AccessControlList?.Grant || [];
    grants.forEach((grant: any) => {
      xml += '    <Grant>\n';

      // Grantee
      const grantee = grant.grantee || grant.Grantee;
      const granteeType = grantee.type || grantee.Type || 'CanonicalUser';
      xml += `      <Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="${granteeType}">\n`;

      if (granteeType === 'CanonicalUser') {
        xml += `        <ID>${grantee.id || grantee.ID}</ID>\n`;
        if (grantee.displayName || grantee.DisplayName) {
          xml += `        <DisplayName>${grantee.displayName || grantee.DisplayName}</DisplayName>\n`;
        }
      } else if (granteeType === 'AmazonCustomerByEmail') {
        xml += `        <EmailAddress>${grantee.emailAddress || grantee.EmailAddress}</EmailAddress>\n`;
      } else if (granteeType === 'Group') {
        xml += `        <URI>${grantee.uri || grantee.URI}</URI>\n`;
      }

      xml += '      </Grantee>\n';

      // Permission
      xml += `      <Permission>${grant.permission || grant.Permission}</Permission>\n`;
      xml += '    </Grant>\n';
    });
    xml += '  </AccessControlList>\n';
    xml += '</AccessControlPolicy>';

    return xml;
  }

  // Bucket Lifecycle
  static async getBucketLifecycle(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/lifecycle?tenantId=${tenantId}` : `/buckets/${bucketName}/lifecycle`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketLifecycle(bucketName: string, lifecycle: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/lifecycle?tenantId=${tenantId}` : `/buckets/${bucketName}/lifecycle`;
    await apiClient.put(url, lifecycle, {
      headers: { 'Content-Type': 'application/xml' }
    });
  }

  static async deleteBucketLifecycle(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/lifecycle?tenantId=${tenantId}` : `/buckets/${bucketName}/lifecycle`;
    await apiClient.delete(url);
  }

  // Bucket Notification Configuration
  static async getBucketNotification(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/notification?tenantId=${tenantId}` : `/buckets/${bucketName}/notification`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketNotification(bucketName: string, config: any, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/notification?tenantId=${tenantId}` : `/buckets/${bucketName}/notification`;
    await apiClient.put(url, config);
  }

  static async deleteBucketNotification(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/notification?tenantId=${tenantId}` : `/buckets/${bucketName}/notification`;
    await apiClient.delete(url);
  }

  // Bucket Inventory Configuration
  static async getBucketInventory(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/inventory?tenantId=${tenantId}` : `/buckets/${bucketName}/inventory`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putBucketInventory(bucketName: string, config: any, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/inventory?tenantId=${tenantId}` : `/buckets/${bucketName}/inventory`;
    await apiClient.put(url, config);
  }

  static async deleteBucketInventory(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/inventory?tenantId=${tenantId}` : `/buckets/${bucketName}/inventory`;
    await apiClient.delete(url);
  }

  // Bucket Website Hosting
  static async getBucketWebsite(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/website?tenantId=${tenantId}` : `/buckets/${bucketName}/website`;
    try {
      const response = await apiClient.get(url);
      return response.data;
    } catch (e: any) {
      if (e.response?.status === 404 || e.response?.status === 501) return null;
      throw e;
    }
  }

  static async putBucketWebsite(bucketName: string, config: { indexDocument: string; errorDocument?: string }, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/website?tenantId=${tenantId}` : `/buckets/${bucketName}/website`;
    await apiClient.put(url, config);
  }

  static async deleteBucketWebsite(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId ? `/buckets/${bucketName}/website?tenantId=${tenantId}` : `/buckets/${bucketName}/website`;
    await apiClient.delete(url);
  }

  static async listBucketInventoryReports(bucketName: string, limit?: number, offset?: number, tenantId?: string): Promise<any> {
    const params = new URLSearchParams();
    if (limit) params.append('limit', limit.toString());
    if (offset) params.append('offset', offset.toString());
    if (tenantId) params.append('tenantId', tenantId);

    const queryString = params.toString();
    const url = `/buckets/${bucketName}/inventory/reports${queryString ? `?${queryString}` : ''}`;
    const response = await apiClient.get(url);
    return response.data;
  }

  // Object Lock Configuration
  static async getObjectLockConfiguration(bucketName: string, tenantId?: string): Promise<any> {
    const url = tenantId ? `/buckets/${bucketName}/object-lock?tenantId=${tenantId}` : `/buckets/${bucketName}/object-lock`;
    const response = await apiClient.get(url);
    return response.data;
  }

  static async putObjectLockConfiguration(bucketName: string, config: string): Promise<void> {
    await apiClient.put(`/buckets/${bucketName}/object-lock`, config, {
      headers: { 'Content-Type': 'application/xml' }
    });
  }

  static async updateObjectLockConfiguration(
    bucketName: string,
    config: { mode: string; days?: number; years?: number }
  ): Promise<void> {
    // Enviar JSON directamente a la Console API
    await apiClient.put(`/buckets/${bucketName}/object-lock`, config);
  }

  // Tenant Management
  static async getTenants(): Promise<Tenant[]> {
    const response = await apiClient.get<APIResponse<Tenant[]>>('/tenants');

    // Handle double-wrapped response: response.data.data might be { success: true, data: [...] }
    let tenants: any;
    if (response.data.data && typeof response.data.data === 'object' && 'data' in response.data.data) {
      // Double wrapped: { success: true, data: { success: true, data: [...] } }
      tenants = response.data.data.data;
    } else if (Array.isArray(response.data.data)) {
      // Correct format: { success: true, data: [...] }
      tenants = response.data.data;
    } else {
      // Fallback
      tenants = response.data || [];
    }

    // Transform snake_case to camelCase
    const transformedTenants = Array.isArray(tenants) ? tenants.map((tenant: any) => ({
      id: tenant.id,
      name: tenant.name,
      displayName: tenant.display_name,
      description: tenant.description,
      status: tenant.status,
      maxAccessKeys: tenant.max_access_keys,
      maxStorageBytes: tenant.max_storage_bytes,
      currentStorageBytes: tenant.current_storage_bytes || 0,
      maxBuckets: tenant.max_buckets,
      currentBuckets: tenant.current_buckets || 0,
      currentAccessKeys: tenant.current_access_keys || 0,
      createdAt: tenant.created_at,
      updatedAt: tenant.updated_at,
    })) : [];

    return transformedTenants;
  }

  static async getTenant(tenantId: string): Promise<Tenant> {
    const response = await apiClient.get<APIResponse<Tenant>>(`/tenants/${tenantId}`);
    return response.data.data!;
  }

  static async createTenant(data: CreateTenantRequest): Promise<Tenant> {
    const response = await apiClient.post<APIResponse<Tenant>>('/tenants', data);
    return response.data.data!;
  }

  static async updateTenant(tenantId: string, data: UpdateTenantRequest): Promise<Tenant> {
    const response = await apiClient.put<APIResponse<Tenant>>(`/tenants/${tenantId}`, data);
    return response.data.data!;
  }

  static async deleteTenant(tenantId: string, force?: boolean): Promise<void> {
    const url = force ? `/tenants/${tenantId}?force=true` : `/tenants/${tenantId}`;
    await apiClient.delete(url);
  }

  static async getTenantUsers(tenantId: string): Promise<User[]> {
    const response = await apiClient.get<APIResponse<User[]>>(`/tenants/${tenantId}/users`);
    return response.data.data || [];
  }

  // Bucket Permissions
  static async getBucketPermissions(bucketName: string, tenantId?: string): Promise<BucketPermission[]> {
    const url = tenantId ? `/buckets/${bucketName}/permissions?tenantId=${tenantId}` : `/buckets/${bucketName}/permissions`;
    const response = await apiClient.get<APIResponse<BucketPermission[]>>(url);
    return response.data.data || [];
  }

  static async grantBucketPermission(bucketName: string, data: GrantPermissionRequest, bucketTenantId?: string): Promise<void> {
    const url = bucketTenantId ? `/buckets/${bucketName}/permissions?tenantId=${bucketTenantId}` : `/buckets/${bucketName}/permissions`;
    await apiClient.post(url, data);
  }

  static async revokeBucketPermission(bucketName: string, userId?: string, permissionTenantId?: string, bucketTenantId?: string, groupId?: string): Promise<void> {
    const params = new URLSearchParams();
    if (userId) params.append('userId', userId);
    if (permissionTenantId) params.append('tenantId', permissionTenantId);
    if (bucketTenantId) params.append('bucketTenantId', bucketTenantId);
    if (groupId) params.append('groupId', groupId);
    await apiClient.delete(`/buckets/${bucketName}/permissions/revoke?${params.toString()}`);
  }

  static async updateBucketOwner(bucketName: string, ownerId: string, ownerType: 'user' | 'tenant'): Promise<void> {
    await apiClient.put(`/buckets/${bucketName}/owner`, { ownerId, ownerType });
  }

  // Groups
  static async listGroups(tenantId?: string, scopeGlobal?: boolean): Promise<Group[]> {
    let url = '/groups';
    if (tenantId) url += `?tenantId=${tenantId}`;
    else if (scopeGlobal) url += '?scope=global';
    const response = await apiClient.get<{ groups: Group[]; total: number }>(url);
    return response.data.groups || [];
  }

  static async createGroup(data: CreateGroupRequest): Promise<Group> {
    const response = await apiClient.post<Group>('/groups', data);
    return response.data;
  }

  static async getGroup(groupId: string): Promise<Group> {
    const response = await apiClient.get<Group>(`/groups/${groupId}`);
    return response.data;
  }

  static async updateGroup(groupId: string, data: UpdateGroupRequest): Promise<Group> {
    const response = await apiClient.put<Group>(`/groups/${groupId}`, data);
    return response.data;
  }

  static async deleteGroup(groupId: string): Promise<void> {
    await apiClient.delete(`/groups/${groupId}`);
  }

  static async listGroupMembers(groupId: string): Promise<GroupMember[]> {
    const response = await apiClient.get<{ members: GroupMember[]; total: number }>(`/groups/${groupId}/members`);
    return response.data.members || [];
  }

  static async addGroupMember(groupId: string, userId: string): Promise<void> {
    await apiClient.post(`/groups/${groupId}/members`, { userId });
  }

  static async removeGroupMember(groupId: string, userId: string): Promise<void> {
    await apiClient.delete(`/groups/${groupId}/members/${userId}`);
  }

  static async listUserGroups(userId: string): Promise<Group[]> {
    const response = await apiClient.get<{ groups: Group[]; total: number }>(`/users/${userId}/groups`);
    return response.data.groups || [];
  }

  // Two-Factor Authentication methods
  static async setup2FA(): Promise<any> {
    const response = await apiClient.post<APIResponse<any>>('/auth/2fa/setup');
    return response.data.data;
  }

  static async enable2FA(code: string, secret: string): Promise<any> {
    const response = await apiClient.post<APIResponse<any>>('/auth/2fa/enable', { code, secret });
    return response.data.data;
  }

  static async disable2FA(userId?: string): Promise<any> {
    const response = await apiClient.post<APIResponse<any>>('/auth/2fa/disable', { user_id: userId });
    return response.data.data;
  }

  static async verify2FA(userId: string, code: string): Promise<LoginResponse> {
    const response = await apiClient.post<any>('/auth/2fa/verify', { user_id: userId, code });

    const result: LoginResponse = {
      success: response.data.success,
      token: response.data.token,
      refreshToken: response.data.refresh_token,
      user: response.data.user,
      error: response.data.error,
    };

    if (result.success && result.token) {
      tokenManager.setTokens(result.token, result.refreshToken);
    }

    return result;
  }

  static async regenerateBackupCodes(): Promise<any> {
    const response = await apiClient.post<APIResponse<any>>('/auth/2fa/backup-codes');
    return response.data.data;
  }

  static async get2FAStatus(userId?: string): Promise<any> {
    const url = userId ? `/auth/2fa/status?user_id=${userId}` : '/auth/2fa/status';
    const response = await apiClient.get<APIResponse<any>>(url);
    return response.data.data;
  }

  // Audit Logs
  static async getAuditLogs(filters?: AuditLogFilters): Promise<AuditLogsResponse> {
    const params = new URLSearchParams();
    if (filters?.tenantId) params.append('tenant_id', filters.tenantId);
    if (filters?.userId) params.append('user_id', filters.userId);
    if (filters?.eventType) params.append('event_type', filters.eventType);
    if (filters?.resourceType) params.append('resource_type', filters.resourceType);
    if (filters?.action) params.append('action', filters.action);
    if (filters?.status) params.append('status', filters.status);
    if (filters?.startDate) params.append('start_date', filters.startDate.toString());
    if (filters?.endDate) params.append('end_date', filters.endDate.toString());
    if (filters?.page) params.append('page', filters.page.toString());
    if (filters?.pageSize) params.append('page_size', filters.pageSize.toString());

    const response = await apiClient.get<APIResponse<AuditLogsResponse>>(`/audit-logs?${params.toString()}`);
    return response.data.data!;
  }

  static async getAuditLog(id: number): Promise<AuditLog> {
    const response = await apiClient.get<APIResponse<AuditLog>>(`/audit-logs/${id}`);
    return response.data.data!;
  }

  // Settings API
  static async listSettings(category?: string): Promise<Setting[]> {
    const params = category ? { category } : {};
    const response = await apiClient.get<APIResponse<Setting[]>>('/settings', { params });
    return response.data.data!;
  }

  static async getSettingCategories(): Promise<string[]> {
    const response = await apiClient.get<APIResponse<SettingsCategoriesResponse>>('/settings/categories');
    return response.data.data!.categories;
  }

  static async getSetting(key: string): Promise<Setting> {
    const response = await apiClient.get<APIResponse<Setting>>(`/settings/${key}`);
    return response.data.data!;
  }

  static async updateSetting(key: string, value: string): Promise<Setting> {
    const request: UpdateSettingRequest = { value };
    const response = await apiClient.put<APIResponse<Setting>>(`/settings/${key}`, request);
    return response.data.data!;
  }

  static async bulkUpdateSettings(settings: Record<string, string>): Promise<{ success: boolean; message: string; count: number }> {
    const request: BulkUpdateSettingsRequest = { settings };
    const response = await apiClient.post<APIResponse<{ success: boolean; message: string; count: number }>>('/settings/bulk', request);
    return response.data.data!;
  }

  static async testEmail(): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.post<APIResponse<{ success: boolean; message: string }>>('/settings/email/test', {});
    return response.data.data ?? response.data as unknown as { success: boolean; message: string };
  }

  // Logging Targets API
  static async listLoggingTargets(): Promise<LoggingTargetsResponse> {
    const response = await apiClient.get<APIResponse<LoggingTargetsResponse>>('/logs/targets');
    return response.data.data ?? response.data as unknown as LoggingTargetsResponse;
  }

  static async getLoggingTarget(id: string): Promise<LoggingTarget> {
    const response = await apiClient.get<APIResponse<LoggingTarget>>(`/logs/targets/${id}`);
    return response.data.data ?? response.data as unknown as LoggingTarget;
  }

  static async createLoggingTarget(target: Partial<LoggingTarget>): Promise<{ id: string; name: string; message: string }> {
    const response = await apiClient.post<APIResponse<{ id: string; name: string; message: string }>>('/logs/targets', target);
    return response.data.data ?? response.data as unknown as { id: string; name: string; message: string };
  }

  static async updateLoggingTarget(id: string, target: Partial<LoggingTarget>): Promise<{ id: string; message: string }> {
    const response = await apiClient.put<APIResponse<{ id: string; message: string }>>(`/logs/targets/${id}`, target);
    return response.data.data ?? response.data as unknown as { id: string; message: string };
  }

  static async deleteLoggingTarget(id: string): Promise<{ id: string; message: string }> {
    const response = await apiClient.delete<APIResponse<{ id: string; message: string }>>(`/logs/targets/${id}`);
    return response.data.data ?? response.data as unknown as { id: string; message: string };
  }

  static async testLoggingTarget(id: string): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.post<APIResponse<{ success: boolean; message: string }>>(`/logs/targets/${id}/test`);
    return response.data.data ?? response.data as unknown as { success: boolean; message: string };
  }

  static async testLoggingTargetConfig(target: Partial<LoggingTarget>): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.post<APIResponse<{ success: boolean; message: string }>>('/logs/targets/test', target);
    return response.data.data ?? response.data as unknown as { success: boolean; message: string };
  }

  // Replication API
  static async listReplicationRules(bucketName: string): Promise<ReplicationRule[]> {
    const response = await apiClient.get<ListReplicationRulesResponse>(`/buckets/${bucketName}/replication/rules`);
    return response.data.rules;
  }

  static async getReplicationRule(bucketName: string, ruleId: string): Promise<ReplicationRule> {
    const response = await apiClient.get<ReplicationRule>(`/buckets/${bucketName}/replication/rules/${ruleId}`);
    return response.data;
  }

  static async createReplicationRule(bucketName: string, request: CreateReplicationRuleRequest): Promise<ReplicationRule> {
    const response = await apiClient.post<ReplicationRule>(`/buckets/${bucketName}/replication/rules`, request);
    return response.data;
  }

  static async updateReplicationRule(bucketName: string, ruleId: string, request: CreateReplicationRuleRequest): Promise<ReplicationRule> {
    const response = await apiClient.put<ReplicationRule>(`/buckets/${bucketName}/replication/rules/${ruleId}`, request);
    return response.data;
  }

  static async deleteReplicationRule(bucketName: string, ruleId: string): Promise<void> {
    await apiClient.delete(`/buckets/${bucketName}/replication/rules/${ruleId}`);
  }

  static async getReplicationMetrics(bucketName: string, ruleId: string): Promise<ReplicationMetrics> {
    const response = await apiClient.get<ReplicationMetrics>(`/buckets/${bucketName}/replication/rules/${ruleId}/metrics`);
    return response.data;
  }

  static async triggerReplicationSync(bucketName: string, ruleId: string): Promise<{ success: boolean; message: string; queued_count: number; rule_id: string }> {
    const response = await apiClient.post<{ success: boolean; message: string; queued_count: number; rule_id: string }>(`/buckets/${bucketName}/replication/rules/${ruleId}/sync`);
    return response.data;
  }

  // Cluster API
  static async initializeCluster(request: InitializeClusterRequest): Promise<InitializeClusterResponse> {
    const response = await apiClient.post<APIResponse<InitializeClusterResponse>>('/cluster/initialize', request);
    return response.data.data!;
  }

  static async joinCluster(request: JoinClusterRequest): Promise<{ message: string }> {
    const response = await apiClient.post<APIResponse<{ message: string }>>('/cluster/join', request);
    return response.data.data!;
  }

  static async leaveCluster(): Promise<{ message: string }> {
    const response = await apiClient.post<APIResponse<{ message: string }>>('/cluster/leave');
    return response.data.data!;
  }

  static async getClusterStatus(): Promise<ClusterStatus> {
    const response = await apiClient.get<APIResponse<ClusterStatus>>('/cluster/status');
    return response.data.data!;
  }

  static async getClusterConfig(): Promise<ClusterConfig> {
    const response = await apiClient.get<APIResponse<ClusterConfig>>('/cluster/config');
    return response.data.data!;
  }

  static async getClusterToken(): Promise<{ cluster_token: string }> {
    const response = await apiClient.get<APIResponse<{ cluster_token: string }>>('/cluster/token');
    return response.data.data!;
  }

  static async listClusterNodes(): Promise<ClusterNode[]> {
    const response = await apiClient.get<APIResponse<ListNodesResponse>>('/cluster/nodes');
    return response.data.data!.nodes;
  }

  static async getClusterNode(nodeId: string): Promise<ClusterNode> {
    const response = await apiClient.get<APIResponse<ClusterNode>>(`/cluster/nodes/${nodeId}`);
    return response.data.data!;
  }

  static async addClusterNode(request: AddNodeRequest): Promise<{ message: string }> {
    const response = await apiClient.post<APIResponse<{ message: string }>>('/cluster/nodes', request);
    return response.data.data!;
  }

  static async updateClusterNode(nodeId: string, request: UpdateNodeRequest): Promise<{ message: string }> {
    const response = await apiClient.put<APIResponse<{ message: string }>>(`/cluster/nodes/${nodeId}`, request);
    return response.data.data!;
  }

  static async removeClusterNode(nodeId: string): Promise<{ message: string }> {
    const response = await apiClient.delete<APIResponse<{ message: string }>>(`/cluster/nodes/${nodeId}`);
    return response.data.data!;
  }

  static async checkNodeHealth(nodeId: string): Promise<NodeHealthStatus> {
    const response = await apiClient.get<APIResponse<NodeHealthStatus>>(`/cluster/nodes/${nodeId}/health`);
    return response.data.data!;
  }

  static async getCacheStats(): Promise<CacheStats> {
    const response = await apiClient.get<APIResponse<CacheStats>>('/cluster/cache/stats');
    return response.data.data!;
  }

  static async invalidateCache(bucket?: string): Promise<{ message: string }> {
    const response = await apiClient.post<APIResponse<{ message: string }>>('/cluster/cache/invalidate', { bucket });
    return response.data.data!;
  }

  static async getClusterBuckets(): Promise<{ buckets: BucketWithReplication[]; total: number }> {
    const response = await apiClient.get<APIResponse<{ buckets: BucketWithReplication[]; total: number }>>('/cluster/buckets');
    return response.data.data!;
  }

  static async getBucketReplicas(bucket: string): Promise<{ bucket: string; rules: any[]; total: number }> {
    const response = await apiClient.get<APIResponse<{ bucket: string; rules: any[]; total: number }>>(`/cluster/buckets/${bucket}/replicas`);
    return response.data.data!;
  }

  // Cluster Replication methods
  static async listClusterReplications(params?: { tenant_id?: string; bucket?: string }): Promise<ListClusterReplicationsResponse> {
    const queryParams = new URLSearchParams();
    if (params?.tenant_id) queryParams.append('tenant_id', params.tenant_id);
    if (params?.bucket) queryParams.append('bucket', params.bucket);
    const url = `/cluster/replication${queryParams.toString() ? `?${queryParams.toString()}` : ''}`;
    const response = await apiClient.get<APIResponse<ListClusterReplicationsResponse>>(url);
    return response.data.data!;
  }

  static async createClusterReplication(request: CreateClusterReplicationRequest): Promise<ClusterReplicationRule> {
    const response = await apiClient.post<APIResponse<ClusterReplicationRule>>('/cluster/replication', request);
    return response.data.data!;
  }

  static async updateClusterReplication(id: string, request: UpdateClusterReplicationRequest): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.put<APIResponse<{ success: boolean; message: string }>>(`/cluster/replication/${id}`, request);
    return response.data.data!;
  }

  static async deleteClusterReplication(id: string): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.delete<APIResponse<{ success: boolean; message: string }>>(`/cluster/replication/${id}`);
    return response.data.data!;
  }

  static async createBulkClusterReplication(request: BulkClusterReplicationRequest): Promise<{ success: boolean; rules_created: number; rules_failed: number; failed_buckets: string[]; message: string }> {
    const response = await apiClient.post<APIResponse<{ success: boolean; rules_created: number; rules_failed: number; failed_buckets: string[]; message: string }>>('/cluster/replication/bulk', request);
    return response.data.data!;
  }

  // Cluster Migration methods
  static async migrateBucket(bucket: string, request: MigrateBucketRequest): Promise<MigrationJob> {
    const response = await apiClient.post<APIResponse<MigrationJob>>(`/cluster/buckets/${bucket}/migrate`, request);
    return response.data.data!;
  }

  static async listMigrations(bucket?: string): Promise<ListMigrationsResponse> {
    const params = bucket ? { bucket } : {};
    const response = await apiClient.get<APIResponse<ListMigrationsResponse>>('/cluster/migrations', { params });
    return response.data.data!;
  }

  static async getMigration(id: number): Promise<MigrationJob> {
    const response = await apiClient.get<APIResponse<MigrationJob>>(`/cluster/migrations/${id}`);
    return response.data.data!;
  }

  // Identity Provider Management
  static async listIDPs(): Promise<IdentityProvider[]> {
    const response = await apiClient.get<APIResponse<IdentityProvider[]>>('/identity-providers');
    return response.data.data || [];
  }

  static async getIDP(id: string): Promise<IdentityProvider> {
    const response = await apiClient.get<APIResponse<IdentityProvider>>(`/identity-providers/${id}`);
    return response.data.data!;
  }

  static async createIDP(data: Partial<IdentityProvider>): Promise<IdentityProvider> {
    const response = await apiClient.post<APIResponse<IdentityProvider>>('/identity-providers', data);
    return response.data.data!;
  }

  static async updateIDP(id: string, data: Partial<IdentityProvider>): Promise<IdentityProvider> {
    const response = await apiClient.put<APIResponse<IdentityProvider>>(`/identity-providers/${id}`, data);
    return response.data.data!;
  }

  static async deleteIDP(id: string): Promise<void> {
    await apiClient.delete(`/identity-providers/${id}`);
  }

  static async testIDPConnection(id: string): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.post<APIResponse<{ success: boolean; message: string }>>(`/identity-providers/${id}/test`);
    return response.data.data!;
  }

  static async idpSearchUsers(id: string, query: string, limit?: number): Promise<ExternalUser[]> {
    const response = await apiClient.post<APIResponse<ExternalUser[]>>(`/identity-providers/${id}/search-users`, { query, limit: limit || 50 });
    return response.data.data || [];
  }

  static async idpSearchGroups(id: string, query: string, limit?: number): Promise<ExternalGroup[]> {
    const response = await apiClient.post<APIResponse<ExternalGroup[]>>(`/identity-providers/${id}/search-groups`, { query, limit: limit || 50 });
    return response.data.data || [];
  }

  static async idpGetGroupMembers(id: string, groupId: string): Promise<ExternalUser[]> {
    const response = await apiClient.post<APIResponse<ExternalUser[]>>(`/identity-providers/${id}/group-members`, { group_id: groupId });
    return response.data.data || [];
  }

  static async idpImportUsers(id: string, users: { external_id: string; username: string }[], role: string, tenantId?: string): Promise<ImportResult> {
    const response = await apiClient.post<APIResponse<ImportResult>>(`/identity-providers/${id}/import-users`, { users, role, tenant_id: tenantId });
    return response.data.data!;
  }

  // Group Mappings
  static async listGroupMappings(providerId: string): Promise<GroupMapping[]> {
    const response = await apiClient.get<APIResponse<GroupMapping[]>>(`/identity-providers/${providerId}/group-mappings`);
    return response.data.data || [];
  }

  static async createGroupMapping(providerId: string, data: Partial<GroupMapping>): Promise<GroupMapping> {
    const response = await apiClient.post<APIResponse<GroupMapping>>(`/identity-providers/${providerId}/group-mappings`, data);
    return response.data.data!;
  }

  static async updateGroupMapping(providerId: string, mapId: string, data: Partial<GroupMapping>): Promise<GroupMapping> {
    const response = await apiClient.put<APIResponse<GroupMapping>>(`/identity-providers/${providerId}/group-mappings/${mapId}`, data);
    return response.data.data!;
  }

  static async deleteGroupMapping(providerId: string, mapId: string): Promise<void> {
    await apiClient.delete(`/identity-providers/${providerId}/group-mappings/${mapId}`);
  }

  static async syncGroupMapping(providerId: string, mapId: string): Promise<SyncResult> {
    const response = await apiClient.post<APIResponse<SyncResult>>(`/identity-providers/${providerId}/group-mappings/${mapId}/sync`);
    return response.data.data!;
  }

  static async syncAllMappings(providerId: string): Promise<{ message: string }> {
    const response = await apiClient.post<APIResponse<{ message: string }>>(`/identity-providers/${providerId}/sync`);
    return response.data.data!;
  }

  // OAuth Providers (public)
  static async listOAuthProviders(): Promise<OAuthProviderInfo[]> {
    const response = await apiClient.get<APIResponse<OAuthProviderInfo[]>>('/auth/oauth/providers');
    return response.data.data || [];
  }

  // Utility methods
  static isAuthenticated(): boolean {
    return tokenManager.isAuthenticated();
  }

  static getToken(): string | null {
    return tokenManager.getToken();
  }

  static clearAuth(): void {
    tokenManager.clearTokens();
  }
}

// Export individual client instances for advanced usage
export { apiClient, s3Client };

// Export default instance
export default APIClient;