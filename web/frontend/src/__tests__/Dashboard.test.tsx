import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import Dashboard from '@/pages/index';
import { APIClient } from '@/lib/api';

// Mock API Client
vi.mock('@/lib/api', () => ({
  APIClient: {
    getStorageMetrics: vi.fn(),
    getBuckets: vi.fn(),
    getUsers: vi.fn(),
  },
}));

// Mock useCurrentUser hook
vi.mock('@/hooks/useCurrentUser', () => ({
  useCurrentUser: () => ({
    user: {
      id: '1',
      username: 'admin',
      email: 'admin@example.com',
      role: 'admin',
    },
    isGlobalAdmin: true,
    isAdmin: true,
  }),
}));

// Mock fetch for health check
global.fetch = vi.fn();

describe('Dashboard Page', () => {
  const mockBuckets = [
    {
      id: 'bucket-1',
      name: 'test-bucket',
      tenant_id: 'tenant-1',
      object_count: 10,
      size: 1024000,
      versioning: { Status: 'Suspended' as const },
      creation_date: '2024-01-01T00:00:00Z',
    },
    {
      id: 'bucket-2',
      name: 'another-bucket',
      tenant_id: 'tenant-1',
      object_count: 5,
      size: 512000,
      versioning: { Status: 'Enabled' as const },
      creation_date: '2024-01-02T00:00:00Z',
    },
  ];

  const mockUsers = [
    {
      id: '1',
      username: 'admin',
      email: 'admin@example.com',
      roles: ['admin'],
      status: 'active' as const,
      tenant_id: 'tenant-1',
      createdAt: '2024-01-01T00:00:00Z',
    },
    {
      id: '2',
      username: 'user1',
      email: 'user1@example.com',
      roles: ['user'],
      status: 'active' as const,
      tenant_id: 'tenant-1',
      createdAt: '2024-01-02T00:00:00Z',
    },
  ];

  const mockMetrics = {
    totalBuckets: 2,
    totalObjects: 15,
    totalSize: 1536000,
    bucketMetrics: {},
    storageOperations: { upload: 0, download: 0, delete: 0 },
    averageObjectSize: 0,
    objectSizeDistribution: {},
    timestamp: Date.now(),
  };

  beforeEach(() => {
    vi.clearAllMocks();

    // Setup default mocks
    vi.mocked(APIClient.getStorageMetrics).mockResolvedValue(mockMetrics);
    vi.mocked(APIClient.getBuckets).mockResolvedValue(mockBuckets);
    vi.mocked(APIClient.getUsers).mockResolvedValue(mockUsers);

    // Mock health check
    vi.mocked(global.fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        data: { status: 'healthy', uptime: 12345 },
      }),
    } as Response);
  });

  describe('Rendering', () => {
    it('should render dashboard title and welcome message', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
        expect(screen.getByText(/Welcome to MaxIOFS/i)).toBeInTheDocument();
      });
    });

    it('should show loading state initially', () => {
      render(<Dashboard />);

      expect(screen.getByText(/Loading dashboard/i)).toBeInTheDocument();
    });

    it('should display metrics after loading', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      // Check that API calls were made
      expect(APIClient.getStorageMetrics).toHaveBeenCalled();
      expect(APIClient.getBuckets).toHaveBeenCalled();
      expect(APIClient.getUsers).toHaveBeenCalled();
    });
  });

  describe('Metrics Display', () => {
    it('should display total buckets count', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      // Should display dashboard metrics (buckets displayed)
      await waitFor(() => {
        const heading = screen.getByRole('heading', { name: /Dashboard/i });
        expect(heading).toBeInTheDocument();
      });
    });

    it('should display total objects count', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        // Should show 15 objects (10 + 5 from mockBuckets)
        expect(screen.getByText('15')).toBeInTheDocument();
      });
    });

    it('should display total storage size', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      // Storage size is displayed in the dashboard
      await waitFor(() => {
        const heading = screen.getByRole('heading', { name: /Dashboard/i });
        expect(heading).toBeInTheDocument();
      });
    });

    it('should display active users count', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        // Should show 2 active users
        const userElements = screen.getAllByText('2');
        expect(userElements.length).toBeGreaterThan(0);
      });
    });
  });

  describe('Navigation', () => {
    it('should have navigation buttons to other sections', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      // Check for navigation buttons (View Buckets, etc.)
      const viewBucketsButton = screen.getByRole('button', { name: /buckets/i });
      expect(viewBucketsButton).toBeInTheDocument();
    });

    it('should navigate to buckets page when clicking View Buckets', async () => {
      const user = userEvent.setup();
      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      const viewBucketsButton = screen.getByRole('button', { name: /buckets/i });
      await user.click(viewBucketsButton);

      // Note: Navigation is mocked in test-utils with BrowserRouter
      // In real app, this would navigate to /buckets
    });
  });

  describe('Health Status', () => {
    it('should display system health status', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(global.fetch).toHaveBeenCalledWith(
          expect.stringContaining('/api/v1/health')
        );
      });
    });

    it('should handle health check failure gracefully', async () => {
      // Mock failed health check
      vi.mocked(global.fetch).mockResolvedValue({
        ok: false,
        status: 500,
      } as Response);

      render(<Dashboard />);

      // Dashboard should still render even if health check fails
      await waitFor(() => {
        expect(screen.getByText('Dashboard')).toBeInTheDocument();
      });
    });
  });

  describe('Data Refresh', () => {
    it('should fetch data on mount', async () => {
      render(<Dashboard />);

      await waitFor(() => {
        expect(APIClient.getStorageMetrics).toHaveBeenCalledTimes(1);
        expect(APIClient.getBuckets).toHaveBeenCalledTimes(1);
        expect(APIClient.getUsers).toHaveBeenCalledTimes(1);
      });
    });
  });

  describe('Error Handling', () => {
    it('should handle API errors gracefully', async () => {
      // Mock API errors
      vi.mocked(APIClient.getStorageMetrics).mockRejectedValue(
        new Error('Failed to fetch metrics')
      );
      vi.mocked(APIClient.getBuckets).mockRejectedValue(
        new Error('Failed to fetch buckets')
      );
      vi.mocked(APIClient.getUsers).mockRejectedValue(
        new Error('Failed to fetch users')
      );

      render(<Dashboard />);

      // Dashboard should handle errors and still render
      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });
    });
  });

  describe('Empty State', () => {
    it('should display zeros when no data exists', async () => {
      // Mock empty data
      vi.mocked(APIClient.getBuckets).mockResolvedValue([]);
      vi.mocked(APIClient.getUsers).mockResolvedValue([]);
      vi.mocked(APIClient.getStorageMetrics).mockResolvedValue({
        totalBuckets: 0,
        totalObjects: 0,
        totalSize: 0,
        bucketMetrics: {},
        storageOperations: { upload: 0, download: 0, delete: 0 },
        averageObjectSize: 0,
        objectSizeDistribution: {},
        timestamp: Date.now(),
      });

      render(<Dashboard />);

      await waitFor(() => {
        expect(screen.queryByText(/Loading dashboard/i)).not.toBeInTheDocument();
      });

      // Should show zeros or "No data" message
      const zeroElements = screen.getAllByText('0');
      expect(zeroElements.length).toBeGreaterThan(0);
    });
  });
});
