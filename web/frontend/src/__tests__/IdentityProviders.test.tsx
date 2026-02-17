import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import IdentityProvidersPage from '@/pages/identity-providers/index';
import { APIClient } from '@/lib/api';
import ModalManager from '@/lib/modals';
import type { IdentityProvider } from '@/types';

// Mock API Client
vi.mock('@/lib/api', () => ({
  APIClient: {
    listIDPs: vi.fn(),
    deleteIDP: vi.fn(),
    testIDPConnection: vi.fn(),
    createIDP: vi.fn(),
    updateIDP: vi.fn(),
    listGroupMappings: vi.fn(),
    createGroupMapping: vi.fn(),
    deleteGroupMapping: vi.fn(),
    syncGroupMapping: vi.fn(),
    syncAllMappings: vi.fn(),
    idpSearchUsers: vi.fn(),
    idpImportUsers: vi.fn(),
  },
}));

// Mock ModalManager
vi.mock('@/lib/modals', () => ({
  default: {
    confirmDelete: vi.fn(),
    success: vi.fn(),
    error: vi.fn(),
    apiError: vi.fn(),
    close: vi.fn(),
    loading: vi.fn(),
  },
}));

// Mock useCurrentUser hook - default to global admin
const mockCurrentUser = {
  user: {
    id: '1',
    username: 'admin',
    email: 'admin@example.com',
    roles: ['admin'],
    status: 'active' as const,
    tenantId: '',
    createdAt: '2024-01-01T00:00:00Z',
  },
  isGlobalAdmin: true,
  isTenantAdmin: false,
  isAdmin: true,
};

vi.mock('@/hooks/useCurrentUser', () => ({
  useCurrentUser: () => mockCurrentUser,
}));

describe('Identity Providers Page', () => {
  const mockIDPs: IdentityProvider[] = [
    {
      id: 'idp-1',
      name: 'Corporate AD',
      type: 'ldap',
      status: 'active',
      tenantId: '',
      config: {
        ldap: {
          host: 'ldap.corp.com',
          port: 636,
          security: 'tls',
          bind_dn: 'cn=readonly,dc=corp,dc=com',
          bind_password: '********',
          base_dn: 'dc=corp,dc=com',
          user_search_base: '',
          user_filter: '',
          group_search_base: '',
          group_filter: '',
          attr_username: 'sAMAccountName',
          attr_email: 'mail',
          attr_display_name: 'displayName',
          attr_member_of: 'memberOf',
        },
      },
      created_by: 'admin',
      created_at: 1707955200,
      updated_at: 1707955200,
    },
    {
      id: 'idp-2',
      name: 'Google SSO',
      type: 'oauth2',
      status: 'active',
      tenantId: '',
      config: {
        oauth2: {
          preset: 'google',
          client_id: 'google-client-id',
          client_secret: '********',
          auth_url: 'https://accounts.google.com/o/oauth2/v2/auth',
          token_url: 'https://oauth2.googleapis.com/token',
          userinfo_url: 'https://openidconnect.googleapis.com/v1/userinfo',
          scopes: ['openid', 'email', 'profile'],
          redirect_uri: '',
          claim_email: 'email',
          claim_name: 'name',
          claim_groups: '',
        },
      },
      created_by: 'admin',
      created_at: 1707955200,
      updated_at: 1707955200,
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.listIDPs).mockResolvedValue(mockIDPs);
  });

  it('renders the page title and description', async () => {
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Identity Providers')).toBeInTheDocument();
    });
    expect(screen.getByText(/Manage LDAP\/AD and OAuth2\/SSO/)).toBeInTheDocument();
  });

  it('shows the Add Provider button', async () => {
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Add Provider')).toBeInTheDocument();
    });
  });

  it('renders IDP list in table', async () => {
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });
    expect(screen.getByText('Google SSO')).toBeInTheDocument();
    expect(screen.getByText('LDAP/AD')).toBeInTheDocument();
    expect(screen.getByText('OAuth2')).toBeInTheDocument();
  });

  it('shows empty state when no providers', async () => {
    vi.mocked(APIClient.listIDPs).mockResolvedValue([]);

    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('No Identity Providers')).toBeInTheDocument();
    });
  });

  it('filters providers by search term', async () => {
    const user = userEvent.setup();
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });

    const searchInput = screen.getByPlaceholderText('Search providers...');
    await user.type(searchInput, 'Google');

    expect(screen.queryByText('Corporate AD')).not.toBeInTheDocument();
    expect(screen.getByText('Google SSO')).toBeInTheDocument();
  });

  it('calls delete mutation on confirm', async () => {
    vi.mocked(ModalManager.confirmDelete).mockResolvedValue({ isConfirmed: true } as any);
    vi.mocked(APIClient.deleteIDP).mockResolvedValue(undefined);

    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });

    // Find delete buttons (there are multiple, one per row)
    const deleteButtons = screen.getAllByTitle('Delete');
    await userEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(ModalManager.confirmDelete).toHaveBeenCalledWith('Corporate AD', 'identity provider');
    });

    await waitFor(() => {
      expect(APIClient.deleteIDP).toHaveBeenCalledWith('idp-1');
    });
  });

  it('calls test connection mutation', async () => {
    vi.mocked(APIClient.testIDPConnection).mockResolvedValue({ success: true, message: 'OK' });

    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });

    const testButtons = screen.getAllByTitle('Test Connection');
    await userEvent.click(testButtons[0]);

    await waitFor(() => {
      expect(APIClient.testIDPConnection).toHaveBeenCalledWith('idp-1');
    });
  });

  it('shows permission denied for non-admin users', () => {
    // Override the mock for this test
    const originalGlobalAdmin = mockCurrentUser.isGlobalAdmin;
    const originalTenantAdmin = mockCurrentUser.isTenantAdmin;
    mockCurrentUser.isGlobalAdmin = false;
    mockCurrentUser.isTenantAdmin = false;

    render(<IdentityProvidersPage />);

    expect(screen.getByText('You do not have permission to view this page.')).toBeInTheDocument();

    // Restore
    mockCurrentUser.isGlobalAdmin = originalGlobalAdmin;
    mockCurrentUser.isTenantAdmin = originalTenantAdmin;
  });

  it('shows LDAP Browse Users button only for LDAP providers', async () => {
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });

    // Browse Users button should exist (1 for LDAP, 0 for OAuth)
    const browseButtons = screen.getAllByTitle('Browse Users');
    expect(browseButtons).toHaveLength(1);
  });

  it('shows Group Mappings button for all providers', async () => {
    render(<IdentityProvidersPage />);

    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });

    const mappingButtons = screen.getAllByTitle('Group Mappings');
    expect(mappingButtons).toHaveLength(2);
  });
});

describe('Identity Providers Page - IDPStatusBadge', () => {
  it('is used in the Users page', async () => {
    // This is a smoke test to verify the IDPStatusBadge component exists and can be imported
    const { IDPStatusBadge } = await import('@/components/identity-providers/IDPStatusBadge');
    expect(IDPStatusBadge).toBeDefined();
  });
});
