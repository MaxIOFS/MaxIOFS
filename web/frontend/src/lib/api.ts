import axios, { AxiosInstance, AxiosResponse, AxiosError } from 'axios';
import type {
  APIResponse,
  User,
  AuthToken,
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
  AuditLog,
  AuditLogFilters,
  AuditLogsResponse,
  Setting,
  UpdateSettingRequest,
  BulkUpdateSettingsRequest,
  SettingsCategoriesResponse,
} from '@/types';

// API Configuration
// For monolithic deployment: Use relative URLs so frontend works with both HTTP and HTTPS
// The frontend is served from the same server as the API (port 8081)
// Get base path from window (injected by backend based on public_console_url)
const getBasePath = () => {
  if (typeof window !== 'undefined') {
    return ((window as any).BASE_PATH || '/').replace(/\/$/, '');
  }
  return '';
};

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

  private constructor() {
    // Load tokens from localStorage if available
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('auth_token');
      this.refreshToken = localStorage.getItem('refresh_token');
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

      // Also set in cookies for middleware (24 hours max, idle timeout handled by useIdleTimer)
      document.cookie = `auth_token=${token}; path=/; max-age=${24 * 60 * 60}`; // 24 hours
      if (refreshToken) {
        document.cookie = `refresh_token=${refreshToken}; path=/; max-age=${24 * 60 * 60}`;
      }
    }
  }

  clearTokens(): void {
    this.token = null;
    this.refreshToken = null;

    if (typeof window !== 'undefined') {
      localStorage.removeItem('auth_token');
      localStorage.removeItem('refresh_token');

      // Also clear cookies
      document.cookie = 'auth_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
      document.cookie = 'refresh_token=; path=/; expires=Thu, 01 Jan 1970 00:00:00 GMT';
    }
  }

  isAuthenticated(): boolean {
    return !!this.token;
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
const handleResponse = (response: AxiosResponse): AxiosResponse => response;

// Track if we're already redirecting to prevent loops
let isRedirectingToLogin = false;

const handleError = async (error: AxiosError): Promise<never> => {
  // Handle 401 errors - session expired or invalid token
  if (error.response?.status === 401) {
    // Don't clear tokens if this is a login request (user might have wrong password)
    const isLoginRequest = error.config?.url?.includes('/auth/login');

    if (!isLoginRequest) {
      // IMPORTANT: Clear tokens IMMEDIATELY to prevent retry loops
      tokenManager.clearTokens();

      // Prevent multiple redirects
      if (!isRedirectingToLogin && typeof window !== 'undefined') {
        isRedirectingToLogin = true;

        // Use setTimeout to ensure the redirect happens after current call stack
        setTimeout(() => {
          // Use BASE_PATH to respect proxy reverse configuration
          const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');
          window.location.replace(`${basePath}/login`);
        }, 100);
      }
    }

    return Promise.reject(error);
  }

  // For non-auth errors, we'll let individual components handle them
  // but we'll still format the error properly

  // Transform error to APIError format
  const apiError: APIError = {
    code: error.code || 'UNKNOWN_ERROR',
    message: error.message || 'An unknown error occurred',
    details: error.response?.data,
    requestId: error.response?.headers['x-request-id'],
    timestamp: new Date().toISOString(),
  };

  throw apiError;
};

apiClient.interceptors.response.use(handleResponse, handleError);
s3Client.interceptors.response.use(handleResponse, handleError);

// API Client Class
export class APIClient {
  // Authentication
  static async login(credentials: LoginRequest): Promise<LoginResponse> {
    const payload = {
      username: credentials.username,
      password: credentials.password,
    };

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
    };

    if (result.success && result.token) {
      tokenManager.setTokens(result.token, result.refreshToken);
    }

    return result;
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

  static async refreshToken(): Promise<AuthToken> {
    const refreshToken = tokenManager.getRefreshToken();
    if (!refreshToken) {
      throw new Error('No refresh token available');
    }

    const response = await apiClient.post<APIResponse<AuthToken>>('/auth/refresh', {
      refreshToken,
    });

    const authToken = response.data.data!;
    tokenManager.setTokens(authToken.token, authToken.refreshToken);

    return authToken;
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

  static async deleteBucket(bucketName: string, tenantId?: string): Promise<void> {
    const url = tenantId
      ? `/buckets/${bucketName}?tenantId=${encodeURIComponent(tenantId)}`
      : `/buckets/${bucketName}`;
    await apiClient.delete(url);
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

    // Read file as arrayBuffer for reliable transfer
    const fileBuffer = await request.file.arrayBuffer();

    // Send file directly in body instead of FormData (S3-style upload)
    const config = {
      headers: {
        'Content-Type': request.file.type || 'application/octet-stream',
        'Content-Length': request.file.size.toString(),
      } as Record<string, string>,
      timeout: 300000, // 5 minutes for large files
      maxContentLength: Infinity,
      maxBodyLength: Infinity,
      onUploadProgress: request.onProgress ? (progressEvent: any) => {
        const progress = {
          loaded: progressEvent.loaded,
          total: progressEvent.total,
          percentage: Math.round((progressEvent.loaded * 100) / progressEvent.total),
          speed: 0, // TODO: Calculate speed
          timeRemaining: 0, // TODO: Calculate time remaining
        };
        request.onProgress!(progress);
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
      fileBuffer, // Send file as ArrayBuffer
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

  static async deleteTenant(tenantId: string): Promise<void> {
    await apiClient.delete(`/tenants/${tenantId}`);
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

  static async revokeBucketPermission(bucketName: string, userId?: string, permissionTenantId?: string, bucketTenantId?: string): Promise<void> {
    const params = new URLSearchParams();
    if (userId) params.append('userId', userId);
    if (permissionTenantId) params.append('tenantId', permissionTenantId);
    if (bucketTenantId) params.append('bucketTenantId', bucketTenantId);
    await apiClient.delete(`/buckets/${bucketName}/permissions/revoke?${params.toString()}`);
  }

  static async updateBucketOwner(bucketName: string, ownerId: string, ownerType: 'user' | 'tenant'): Promise<void> {
    await apiClient.put(`/buckets/${bucketName}/owner`, { ownerId, ownerType });
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
      user: response.data.user,
      error: response.data.error,
    };

    if (result.success && result.token) {
      tokenManager.setTokens(result.token);
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