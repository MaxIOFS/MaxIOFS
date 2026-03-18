import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { render } from '@/test/utils/test-utils';
import { AuthContext } from '@/hooks/useAuth';
import { AppLayout } from '@/components/layout/AppLayout';
import { useNotifications } from '@/hooks/useNotifications';
import type { User } from '@/types';

// ─── Mocks ────────────────────────────────────────────────────────────────────

vi.mock('@/lib/api', () => {
  const mock = {
    getServerConfig: vi.fn(),
    getVersionCheck: vi.fn(),
    getTenant: vi.fn(),
    updateUserPreferences: vi.fn(),
    updateS3BaseUrl: vi.fn(),
  };
  return { default: mock, APIClient: mock };
});

vi.mock('@/hooks/useNotifications', () => ({
  useNotifications: vi.fn(),
}));

vi.mock('@/lib/modals', () => ({
  default: { confirmLogout: vi.fn(), loading: vi.fn(), close: vi.fn() },
  ModalRenderer: () => null,
  ToastNotifications: () => null,
}));

vi.mock('@/components/ui/BackgroundTaskBar', () => ({
  BackgroundTaskBar: () => null,
}));

// ─── Fixtures ─────────────────────────────────────────────────────────────────

const globalAdmin: User = {
  id: '1',
  username: 'admin',
  email: 'admin@example.com',
  roles: ['admin'],
  status: 'active',
  tenantId: undefined,
  createdAt: '2024-01-01T00:00:00Z',
};

const tenantAdmin: User = {
  ...globalAdmin,
  id: '2',
  username: 'tenantadmin',
  tenantId: 'tenant-1',
};

const regularUser: User = {
  ...globalAdmin,
  id: '3',
  username: 'user1',
  roles: ['user'],
  tenantId: undefined,
};

const mockServerConfig = {
  version: '1.0.0',
  commit: 'abc123',
  buildDate: '2026-03-02',
  server: {
    s3ApiPort: '8080',
    consoleApiPort: '8081',
    dataDir: './data',
    publicApiUrl: 'http://localhost:8080',
    publicConsoleUrl: 'http://localhost:8081',
    enableTls: false,
    logLevel: 'info',
  },
  storage: { backend: 'pebble', root: './data/storage', enableEncryption: false, enableObjectLock: false },
  auth: { enableAuth: true },
  metrics: { enable: true, path: '/metrics', interval: 60 },
  features: {
    multiTenancy: true, objectLock: true, versioning: true, encryption: true,
    multipart: true, presignedUrls: true, cors: true, lifecycle: true, tagging: true,
  },
  maintenanceMode: false,
};

// ─── Helpers ──────────────────────────────────────────────────────────────────

// Renders AppLayout wrapping around a simple child, with a specific auth user.
// The outer AuthContext.Provider from test-utils is overridden by the inner one.
function renderWithUser(user: User) {
  const authValue = {
    user,
    isAuthenticated: true,
    isLoading: false,
    error: null,
    login: vi.fn(),
    logout: vi.fn(),
    refreshAuth: vi.fn(),
    clearError: vi.fn(),
  };

  return render(
    <AuthContext.Provider value={authValue}>
      <AppLayout>
        <div>Page content</div>
      </AppLayout>
    </AuthContext.Provider>
  );
}

// ─── Setup ────────────────────────────────────────────────────────────────────

beforeEach(async () => {
  localStorage.clear();

  // Default: no notifications, no unread count
  vi.mocked(useNotifications).mockReturnValue({
    notifications: [],
    unreadCount: 0,
    connected: true, // Agregado para cumplir con el tipo esperado
    markAsRead: vi.fn(),
    markAllAsRead: vi.fn(),
    clearNotification: vi.fn(), // Agregado para cumplir con el tipo esperado
  });

  const APIClient = (await import('@/lib/api')).default;
  vi.mocked(APIClient.getServerConfig).mockResolvedValue(mockServerConfig);
  vi.mocked(APIClient.getVersionCheck).mockResolvedValue({ version: '1.0.0' });
  vi.mocked(APIClient.getTenant).mockResolvedValue({
    id: 'tenant-1',
    name: 'Tenant One',
    displayName: 'Tenant One', // Cambiado de display_name a displayName
    status: 'active',
    maxAccessKeys: 5,
    currentAccessKeys: 2,
    maxStorageBytes: 1000000000,
    currentStorageBytes: 500000000,
    maxBuckets: 10,
    currentBuckets: 5,
    createdAt: Date.now(), // Cambiado a número
    updatedAt: Date.now(),
  });
  vi.mocked(APIClient.updateUserPreferences).mockResolvedValue(undefined);
  vi.mocked(APIClient.updateS3BaseUrl).mockImplementation(() => {});
});

// ─── Tests ────────────────────────────────────────────────────────────────────

describe('AppLayout', () => {
  describe('Nav visibility by role', () => {
    it('shows Metrics, Security, Cluster and Settings to global admins', async () => {
      renderWithUser(globalAdmin);

      await waitFor(() => {
        expect(screen.getByText('Metrics')).toBeInTheDocument();
        expect(screen.getByText('Security')).toBeInTheDocument();
        expect(screen.getByText('Cluster')).toBeInTheDocument();
        expect(screen.getByText('Settings')).toBeInTheDocument();
      });
    });

    it('shows Audit Logs to global admins', async () => {
      renderWithUser(globalAdmin);

      await waitFor(() => {
        expect(screen.getByText('Audit Logs')).toBeInTheDocument();
      });
    });

    it('hides Metrics, Security, Cluster and Settings from tenant admins', async () => {
      renderWithUser(tenantAdmin);

      await waitFor(() => {
        expect(screen.getByText('Buckets')).toBeInTheDocument(); // sidebar is rendered
      });

      expect(screen.queryByText('Metrics')).not.toBeInTheDocument();
      expect(screen.queryByText('Security')).not.toBeInTheDocument();
      expect(screen.queryByText('Cluster')).not.toBeInTheDocument();
      expect(screen.queryByText('Settings')).not.toBeInTheDocument();
    });

    it('shows Audit Logs to tenant admins', async () => {
      renderWithUser(tenantAdmin);

      await waitFor(() => {
        expect(screen.getByText('Audit Logs')).toBeInTheDocument();
      });
    });

    it('hides Audit Logs from regular users', async () => {
      renderWithUser(regularUser);

      await waitFor(() => {
        expect(screen.getByText('Buckets')).toBeInTheDocument();
      });

      expect(screen.queryByText('Audit Logs')).not.toBeInTheDocument();
    });

    it('always shows Dashboard and Buckets regardless of role', async () => {
      renderWithUser(regularUser);

      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
        expect(screen.getByText('Buckets')).toBeInTheDocument();
      });
    });
  });

  describe('Maintenance banner', () => {
    it('shows the banner when server is in maintenance mode', async () => {
      const APIClient = (await import('@/lib/api')).default;
      vi.mocked(APIClient.getServerConfig).mockResolvedValue({
        ...mockServerConfig,
        maintenanceMode: true,
      });

      renderWithUser(globalAdmin);

      await waitFor(() => {
        expect(screen.getByText(/maintenance mode/i)).toBeInTheDocument();
      });
    });

    it('does not show the banner when server is not in maintenance mode', async () => {
      renderWithUser(globalAdmin);

      // Wait for serverConfig to load
      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
      });

      expect(screen.queryByText(/maintenance mode/i)).not.toBeInTheDocument();
    });
  });

  describe('Notification badge', () => {
    it('shows the unread count on the notification bell', async () => {
      vi.mocked(useNotifications).mockReturnValue({
        notifications: [],
        unreadCount: 5,
        connected: true,
        markAsRead: vi.fn(),
        markAllAsRead: vi.fn(),
        clearNotification: vi.fn(),
      });

      renderWithUser(globalAdmin);

      await waitFor(() => {
        expect(screen.getByText('5')).toBeInTheDocument();
      });
    });

    it('does not show a badge when there are no unread notifications', async () => {
      renderWithUser(globalAdmin);

      // Wait for render to stabilise
      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
      });

      // Badge only appears when totalUnread > 0
      expect(screen.queryByLabelText('Open notifications')?.querySelector('.bg-error-600')).not.toBeInTheDocument();
    });

    it('includes the default-password warning in the badge count', async () => {
      // Simulate the default password warning flag set in localStorage
      localStorage.setItem('default_password_warning', 'true');

      vi.mocked(useNotifications).mockReturnValue({
        notifications: [],
        unreadCount: 2,
        connected: true,
        markAsRead: vi.fn(),
        markAllAsRead: vi.fn(),
        clearNotification: vi.fn(),
      });

      renderWithUser(globalAdmin);

      // totalUnread = unreadCount (2) + hasDefaultPassword (1) = 3
      await waitFor(() => {
        expect(screen.getByText('3')).toBeInTheDocument();
      });
    });
  });

  describe('Content area', () => {
    it('renders the child content', async () => {
      renderWithUser(globalAdmin);

      await waitFor(() => {
        expect(screen.getByText('Page content')).toBeInTheDocument();
      });
    });
  });
});
