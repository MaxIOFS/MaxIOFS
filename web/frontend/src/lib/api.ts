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
  UploadRequest,
  DownloadRequest,
  StorageMetrics,
  SystemMetrics,
  S3Metrics,
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
} from '@/types';
import SweetAlert from '@/lib/sweetalert';

// API Configuration
const API_CONFIG = {
  baseURL: process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8081/api/v1',
  s3URL: process.env.NEXT_PUBLIC_S3_URL || 'http://localhost:8080',
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
    }
  }

  clearTokens(): void {
    this.token = null;
    this.refreshToken = null;

    if (typeof window !== 'undefined') {
      localStorage.removeItem('auth_token');
      localStorage.removeItem('refresh_token');
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
    const token = tokenManager.getToken();
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
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

const handleError = async (error: AxiosError): Promise<never> => {
  // Handle 401 errors specially (authentication)
  if (error.response?.status === 401) {
    // Try to refresh token
    const refreshToken = tokenManager.getRefreshToken();
    if (refreshToken) {
      try {
        const response = await apiClient.post('/auth/refresh', {
          refreshToken,
        });

        const { token, refreshToken: newRefreshToken } = response.data;
        tokenManager.setTokens(token, newRefreshToken);

        // Retry the original request
        if (error.config) {
          error.config.headers.Authorization = `Bearer ${token}`;
          return axios.request(error.config);
        }
      } catch (refreshError) {
        // Refresh failed, clear tokens and redirect to login
        tokenManager.clearTokens();
        await SweetAlert.error(
          'Session expired',
          'Your session has expired. You will be redirected to login.'
        );
        if (typeof window !== 'undefined') {
          window.location.href = '/login';
        }
      }
    } else {
      // No refresh token, clear tokens and redirect to login
      tokenManager.clearTokens();
      await SweetAlert.error(
        'Session expired',
        'Your session has expired. You will be redirected to login.'
      );
      if (typeof window !== 'undefined') {
        window.location.href = '/login';
      }
    }
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
    // Backend expects username and password
    const payload = {
      username: credentials.username,
      password: credentials.password,
    };

    const response = await apiClient.post<APIResponse<any>>('/auth/login', payload);

    // Backend returns: {"success":true,"data":{"token":"...","user":{...}}}
    const result: LoginResponse = {
      success: response.data.success,
      token: response.data.data?.token,
      refreshToken: response.data.data?.refreshToken,
      user: response.data.data?.user,
      error: response.data.error,
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

  static async getBucket(bucketName: string): Promise<Bucket> {
    const response = await apiClient.get<APIResponse<Bucket>>(`/buckets/${bucketName}`);
    return response.data.data!;
  }

  static async createBucket(bucketData: any): Promise<Bucket> {
    const response = await apiClient.post<APIResponse<Bucket>>('/buckets', bucketData);
    return response.data.data!;
  }

  static async deleteBucket(bucketName: string): Promise<void> {
    await apiClient.delete(`/buckets/${bucketName}`);
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

    const response = await apiClient.get<APIResponse<ListObjectsResponse>>(
      `/buckets/${request.bucket}/objects?${params.toString()}`
    );
    return response.data.data!;
  }

  static async getObject(bucket: string, key: string, versionId?: string): Promise<S3Object> {
    const response = await apiClient.get<APIResponse<S3Object>>(`/buckets/${bucket}/objects/${key}`);
    return response.data.data!;
  }

  static async uploadObject(request: UploadRequest): Promise<S3Object> {
    const uploadUrl = `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}`;

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
    const response = await apiClient.get<Blob>(
      `/buckets/${request.bucket}/objects/${encodeURIComponent(request.key)}`,
      config
    );
    return response.data;
  }

  static async deleteObject(bucket: string, key: string, versionId?: string): Promise<void> {
    await apiClient.delete(`/buckets/${bucket}/objects/${key}`);
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
    const response = await apiClient.get<APIResponse<S3Metrics>>('/metrics');
    return response.data.data!;
  }

  // System Configuration
  static async getSystemConfig(): Promise<APIResponse<any>> {
    const response = await apiClient.get<APIResponse<any>>('/system/config');
    return response.data;
  }

  static async updateSystemConfig(config: any): Promise<APIResponse<any>> {
    const response = await apiClient.put<APIResponse<any>>('/system/config', config);
    return response.data;
  }

  static async testStorageConnection(): Promise<APIResponse<any>> {
    const response = await apiClient.post<APIResponse<any>>('/system/test-storage', {});
    return response.data;
  }

  // Metrics
  static async getMetrics(): Promise<APIResponse<any>> {
    const response = await apiClient.get<APIResponse<any>>('/metrics');
    return response.data;
  }

  // Security
  static async getSecurityStatus(): Promise<APIResponse<any>> {
    const response = await apiClient.get<APIResponse<any>>('/security/status');
    return response.data;
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

  // Tenant Management
  static async getTenants(): Promise<Tenant[]> {
    const response = await apiClient.get<APIResponse<Tenant[]>>('/tenants');
    return response.data.data || [];
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
  static async getBucketPermissions(bucketName: string): Promise<BucketPermission[]> {
    const response = await apiClient.get<APIResponse<BucketPermission[]>>(`/buckets/${bucketName}/permissions`);
    return response.data.data || [];
  }

  static async grantBucketPermission(bucketName: string, data: GrantPermissionRequest): Promise<void> {
    await apiClient.post(`/buckets/${bucketName}/permissions`, data);
  }

  static async revokeBucketPermission(bucketName: string, permissionId: string, userId?: string, tenantId?: string): Promise<void> {
    const params = new URLSearchParams();
    if (userId) params.append('userId', userId);
    if (tenantId) params.append('tenantId', tenantId);
    await apiClient.delete(`/buckets/${bucketName}/permissions/${permissionId}?${params.toString()}`);
  }

  static async updateBucketOwner(bucketName: string, ownerId: string, ownerType: 'user' | 'tenant'): Promise<void> {
    await apiClient.put(`/buckets/${bucketName}/owner`, { ownerId, ownerType });
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