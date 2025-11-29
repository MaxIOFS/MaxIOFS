import { vi } from 'vitest';
import type { LoginResponse, User, Bucket, APIResponse } from '@/types';

// Mock successful login response
export const mockLoginSuccess: LoginResponse = {
  success: true,
  token: 'mock-jwt-token-12345',
  user: {
    id: '1',
    username: 'admin',
    email: 'admin@example.com',
    roles: ['admin'],
    status: 'active' as const,
    tenantId: 'tenant-1',
    createdAt: new Date().toISOString(),
  },
  requires_2fa: false,
};

// Mock 2FA required response
export const mockLogin2FARequired: LoginResponse = {
  success: false,
  requires_2fa: true,
  user_id: '1',
};

// Mock failed login response
export const mockLoginFailure: LoginResponse = {
  success: false,
  error: 'Invalid credentials',
};

// Mock current user
export const mockCurrentUser: User = {
  id: '1',
  username: 'admin',
  email: 'admin@example.com',
  roles: ['admin'],
  tenantId: 'tenant-1',
  createdAt: new Date().toISOString(),
  status: 'active' as const,
  twoFactorEnabled: false,
};

// Mock buckets list
export const mockBuckets: Bucket[] = [
  {
    name: 'test-bucket',
    tenantId: 'tenant-1',
    versioning: { Status: 'Suspended' as const },
    creation_date: new Date().toISOString(),
    objectCount: 10,
    totalSize: 1024000,
  },
  {
    name: 'another-bucket',
    tenantId: 'tenant-1',
    versioning: { Status: 'Enabled' as const },
    creation_date: new Date().toISOString(),
    objectCount: 5,
    totalSize: 512000,
  },
];

// Mock API Client methods
export const mockAPIClient = {
  login: vi.fn(),
  verify2FA: vi.fn(),
  getCurrentUser: vi.fn(),
  listBuckets: vi.fn(),
  createBucket: vi.fn(),
  deleteBucket: vi.fn(),
  listUsers: vi.fn(),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  updateUser: vi.fn(),
};

// Helper to setup successful auth state
export const setupAuthenticatedState = () => {
  localStorage.setItem('auth_token', 'mock-jwt-token-12345');
  mockAPIClient.getCurrentUser.mockResolvedValue({
    success: true,
    data: mockCurrentUser,
  });
};

// Helper to clear auth state
export const clearAuthState = () => {
  localStorage.removeItem('auth_token');
  mockAPIClient.getCurrentUser.mockRejectedValue({
    response: { status: 401 },
  });
};

// Helper to reset all mocks
export const resetAllMocks = () => {
  Object.values(mockAPIClient).forEach((mock) => mock.mockReset());
  localStorage.clear();
  sessionStorage.clear();
};
