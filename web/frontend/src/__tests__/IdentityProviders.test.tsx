import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
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
      createdBy: 'admin',
      createdAt: 1707955200,
      updatedAt: 1707955200,
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
      createdBy: 'admin',
      createdAt: 1707955200,
      updatedAt: 1707955200,
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

  it('shows each step of the connection test, with how it went', async () => {
    vi.mocked(APIClient.testIDPConnection).mockResolvedValue({
      success: false,
      message: 'Service account: The directory refused cn=readonly,dc=corp,dc=com',
      checks: [
        { name: 'Connection', status: 'passed', detail: 'Connected to ldap.corp.com:636 over TLS' },
        { name: 'Service account', status: 'failed', detail: 'The directory refused cn=readonly,dc=corp,dc=com' },
      ],
    });

    render(<IdentityProvidersPage />);
    await waitFor(() => {
      expect(screen.getByText('Corporate AD')).toBeInTheDocument();
    });
    await userEvent.click(screen.getAllByTitle('Test Connection')[0]);

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Connection Failed')).toBeInTheDocument();
    expect(within(dialog).getByText('Connected to ldap.corp.com:636 over TLS')).toBeInTheDocument();
    const failed = within(dialog).getByText('Service account').closest('li');
    expect(failed).toHaveAttribute('data-status', 'failed');
    expect(within(failed!).getByText('The directory refused cn=readonly,dc=corp,dc=com')).toBeInTheDocument();
    expect(ModalManager.error).not.toHaveBeenCalled();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });
  });

  it('shows a passed connection test with its warnings', async () => {
    vi.mocked(APIClient.testIDPConnection).mockResolvedValue({
      success: true,
      message: 'Connection successful',
      checks: [
        { name: 'Sign-in page', status: 'passed', detail: 'The provider shows its sign-in page' },
        { name: 'Client secret', status: 'warning', detail: 'The provider answered invalid_request' },
      ],
    });

    render(<IdentityProvidersPage />);
    await waitFor(() => {
      expect(screen.getByText('Google SSO')).toBeInTheDocument();
    });
    await userEvent.click(screen.getAllByTitle('Test Connection')[1]);

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('Connection OK')).toBeInTheDocument();
    expect(within(dialog).getByText('Client secret').closest('li')).toHaveAttribute('data-status', 'warning');
    expect(ModalManager.success).not.toHaveBeenCalled();
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

  // The server's users and mappings, as it sends them.
  it('imports only the user that is ticked, with its DN and email', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.idpSearchUsers).mockResolvedValue([
      { externalId: 'uid=anabel,ou=people,dc=example,dc=org', username: 'anabel', email: 'anabel@example.org', displayName: 'Anabel Ruiz', groups: [], rawAttrs: {} },
      { externalId: 'uid=ana,ou=people,dc=example,dc=org', username: 'ana', email: 'ana@example.org', displayName: 'Ana Torres', groups: [], rawAttrs: {} },
    ]);
    vi.mocked(APIClient.idpImportUsers).mockResolvedValue({ imported: 1, skipped: 0, errors: 0, results: [] });
    render(<IdentityProvidersPage />);

    await user.click(await screen.findByTitle('Browse Users'));
    await user.type(screen.getByPlaceholderText('Search by name, email, or username...'), 'ana');
    await user.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Ana Torres')).toBeInTheDocument();
    await user.click(within(screen.getByText('ana@example.org').closest('tr')!).getByRole('checkbox'));
    expect(within(screen.getByText('anabel@example.org').closest('tr')!).getByRole('checkbox')).not.toBeChecked();
    await user.click(screen.getByRole('button', { name: /Import Selected/i }));

    await waitFor(() => expect(APIClient.idpImportUsers).toHaveBeenCalledWith(
      'idp-1', [{ external_id: 'uid=ana,ou=people,dc=example,dc=org', username: 'ana', email: 'ana@example.org' }], 'user'));
  });

  it('shows the back-channel logout URL of an OAuth provider with an issuer', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.listIDPs).mockResolvedValue([{
      ...mockIDPs[1], id: 'idp-kc', name: 'Keycloak', config: { oauth2: { ...mockIDPs[1].config.oauth2!, preset: 'custom', issuer: 'https://sso.example.com/realms/company' } },
    }]);
    render(<IdentityProvidersPage />);

    await user.click(await screen.findByTitle('Edit'));
    expect(await screen.findByText('http://localhost:8081/api/v1/auth/oauth/idp-kc/backchannel-logout')).toBeInTheDocument();
  });

  it('creates an OAuth provider with its issuer', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.createIDP).mockResolvedValue(mockIDPs[1]);
    render(<IdentityProvidersPage />);

    await user.click(await screen.findByRole('button', { name: /Add Provider/i }));
    await user.type(screen.getByPlaceholderText('e.g., Corporate AD, Google SSO'), 'Keycloak');
    await user.click(screen.getByRole('button', { name: /OAuth2/i }));
    await user.type(screen.getByPlaceholderText('https://sso.example.com/realms/company'), 'https://sso.example.com/realms/company');
    expect(screen.getByText('The back-channel logout URL is shown once the provider is saved.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /^Create$/ }));

    await waitFor(() => expect(APIClient.createIDP).toHaveBeenCalledWith(expect.objectContaining({
      config: { oauth2: expect.objectContaining({ issuer: 'https://sso.example.com/realms/company' }) },
    })));
  });

  it('creates a group mapping of the group it is given', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.listGroupMappings).mockResolvedValue([]);
    vi.mocked(APIClient.createGroupMapping).mockResolvedValue({
      id: 'gmap-1', providerId: 'idp-1', externalGroup: 'cn=admins,ou=groups,dc=example,dc=org', externalGroupName: '',
      role: 'user', tenantId: '', autoSync: false, lastSyncedAt: 0, createdAt: 0, updatedAt: 0,
    });
    render(<IdentityProvidersPage />);

    await user.click((await screen.findAllByTitle('Group Mappings'))[0]);
    await user.click(screen.getAllByRole('button', { name: /Add Mapping/i })[0]);
    await user.type(screen.getByPlaceholderText('CN=Domain Admins,OU=Groups,DC=company,DC=com'), 'cn=admins,ou=groups,dc=example,dc=org');
    await user.click(screen.getByRole('button', { name: /^Create$/ }));

    await waitFor(() => expect(APIClient.createGroupMapping).toHaveBeenCalledWith(
      'idp-1', expect.objectContaining({ externalGroup: 'cn=admins,ou=groups,dc=example,dc=org', role: 'user' })));
  });
});

describe('Identity Providers Page - IDPStatusBadge', () => {
  it('is used in the Users page', async () => {
    // This is a smoke test to verify the IDPStatusBadge component exists and can be imported
    const { IDPStatusBadge } = await import('@/components/identity-providers/IDPStatusBadge');
    expect(IDPStatusBadge).toBeDefined();
  });
});
