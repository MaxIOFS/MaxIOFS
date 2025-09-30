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
  EditUserForm,
  APIError,
  AccessKey,
  CreateAccessKeyForm,
} from '@/types';

// API Configuration
const API_CONFIG = {
  baseURL: process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080',
  s3URL: process.env.NEXT_PUBLIC_S3_URL || 'http://localhost:8080/s3',
  timeout: 30000,
  withCredentials: true,
};

// Create axios instances
const apiClient: AxiosInstance = axios.create({
  baseURL: API_CONFIG.baseURL,
  timeout: API_CONFIG.timeout,
  withCredentials: API_CONFIG.withCredentials,
  headers: {
    'Content-Type': 'application/json',
  },
});

const s3Client: AxiosInstance = axios.create({
  baseURL: API_CONFIG.s3URL,
  timeout: API_CONFIG.timeout,
  withCredentials: API_CONFIG.withCredentials,
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
        if (typeof window !== 'undefined') {
          window.location.href = '/login';
        }
      }
    } else {
      // No refresh token, clear tokens and redirect to login
      tokenManager.clearTokens();
      if (typeof window !== 'undefined') {
        window.location.href = '/login';
      }
    }
  }

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
    const response = await apiClient.post<LoginResponse>('/auth/login', credentials);

    if (response.data.success && response.data.token) {
      tokenManager.setTokens(response.data.token, response.data.refreshToken);
    }

    return response.data;
  }

  static async logout(): Promise<void> {
    try {
      await apiClient.post('/auth/logout');
    } finally {
      tokenManager.clearTokens();
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

  static async createUser(userData: CreateUserForm): Promise<User> {
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

  // Access Keys Management
  static async getAccessKeys(userId?: string): Promise<AccessKey[]> {
    const url = userId ? `/users/${userId}/access-keys` : '/access-keys';
    const response = await apiClient.get<APIResponse<AccessKey[]>>(url);
    return response.data.data || [];
  }

  static async createAccessKey(keyData: CreateAccessKeyForm): Promise<AccessKey> {
    const response = await apiClient.post<APIResponse<AccessKey>>('/access-keys', keyData);
    return response.data.data!;
  }

  static async deleteAccessKey(keyId: string): Promise<void> {
    await apiClient.delete(`/access-keys/${keyId}`);
  }

  // Buckets Management
  static async getBuckets(): Promise<Bucket[]> {
    const response = await s3Client.get<ListBucketsResponse>('/');
    return response.data.buckets || [];
  }

  static async getBucket(bucketName: string): Promise<Bucket> {
    const response = await s3Client.get<Bucket>(`/${bucketName}`);
    return response.data;
  }

  static async createBucket(bucketData: CreateBucketForm): Promise<Bucket> {
    const response = await s3Client.put<Bucket>(`/${bucketData.name}`, {
      CreateBucketConfiguration: {
        LocationConstraint: bucketData.region,
      },
    });
    return response.data;
  }

  static async deleteBucket(bucketName: string): Promise<void> {
    await s3Client.delete(`/${bucketName}`);
  }

  static async updateBucketConfig(bucketName: string, config: EditBucketForm): Promise<Bucket> {
    const response = await s3Client.put<Bucket>(`/${bucketName}?configuration`, config);
    return response.data;
  }

  // Objects Management
  static async getObjects(request: ListObjectsRequest): Promise<ListObjectsResponse> {
    const params = new URLSearchParams();
    if (request.prefix) params.append('prefix', request.prefix);
    if (request.delimiter) params.append('delimiter', request.delimiter);
    if (request.maxKeys) params.append('max-keys', request.maxKeys.toString());
    if (request.continuationToken) params.append('continuation-token', request.continuationToken);
    if (request.startAfter) params.append('start-after', request.startAfter);

    const response = await s3Client.get<ListObjectsResponse>(
      `/${request.bucket}?list-type=2&${params.toString()}`
    );
    return response.data;
  }

  static async getObject(bucket: string, key: string, versionId?: string): Promise<S3Object> {
    const params = versionId ? `?versionId=${versionId}` : '';
    const response = await s3Client.get<S3Object>(`/${bucket}/${key}${params}`);
    return response.data;
  }

  static async uploadObject(request: UploadRequest): Promise<S3Object> {
    const formData = new FormData();
    formData.append('file', request.file);

    if (request.metadata) {
      Object.entries(request.metadata).forEach(([key, value]) => {
        formData.append(`x-amz-meta-${key}`, value);
      });
    }

    const config = {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
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

    const response = await s3Client.put<S3Object>(
      `/${request.bucket}/${request.key}`,
      formData,
      config
    );
    return response.data;
  }

  static async downloadObject(request: DownloadRequest): Promise<Blob> {
    const params = request.versionId ? `?versionId=${request.versionId}` : '';
    const config = {
      responseType: 'blob' as const,
      headers: request.range ? { Range: request.range } : undefined,
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

    const response = await s3Client.get<Blob>(
      `/${request.bucket}/${request.key}${params}`,
      config
    );
    return response.data;
  }

  static async deleteObject(bucket: string, key: string, versionId?: string): Promise<void> {
    const params = versionId ? `?versionId=${versionId}` : '';
    await s3Client.delete(`/${bucket}/${key}${params}`);
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
    const response = await apiClient.get<APIResponse<StorageMetrics>>('/metrics/storage');
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
  static async getUserAccessKeys(userId: string): Promise<APIResponse<any[]>> {
    const response = await apiClient.get<APIResponse<any[]>>(`/users/${userId}/access-keys`);
    return response.data;
  }

  static async createAccessKey(data: any): Promise<APIResponse<any>> {
    const response = await apiClient.post<APIResponse<any>>('/access-keys', data);
    return response.data;
  }

  static async deleteAccessKey(keyId: string): Promise<APIResponse<any>> {
    const response = await apiClient.delete<APIResponse<any>>(`/access-keys/${keyId}`);
    return response.data;
  }

  // Bucket Settings
  static async updateBucketSettings(bucketName: string, settings: any): Promise<APIResponse<any>> {
    const response = await apiClient.put<APIResponse<any>>(`/buckets/${bucketName}/settings`, settings);
    return response.data;
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