import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Routes, Route } from 'react-router-dom';
import { render } from '@/test/utils/test-utils';
import UserDetailsPage from '@/pages/users/[user]/index';
import { APIClient } from '@/lib/api';

vi.mock('@/lib/api', () => ({
  APIClient: {
    getCurrentUser: vi.fn(),
    getUser: vi.fn(),
    getUserAccessKeys: vi.fn(),
    getTenants: vi.fn(),
    getUsers: vi.fn(),
    get2FAStatus: vi.fn(),
    listIAMRoles: vi.fn(),
    listUserGroups: vi.fn(),
    listGroups: vi.fn(),
    updateUser: vi.fn(),
  },
}));

vi.mock('@/lib/modals', () => ({
  default: { toast: vi.fn(), apiError: vi.fn(), success: vi.fn(), error: vi.fn(), confirm: vi.fn(), close: vi.fn(), loading: vi.fn() },
}));

const admin = { id: 'admin', username: 'admin', email: '', roles: ['admin'], status: 'active' as const, createdAt: 0 };

function renderUser(user: Record<string, unknown>) {
  vi.mocked(APIClient.getCurrentUser).mockResolvedValue(admin as any);
  vi.mocked(APIClient.getUser).mockResolvedValue({ status: 'active', roles: ['user'], createdAt: 0, ...user } as any);
  // The tests' window.location is a plain object; the router reads its path.
  (window.location as { pathname: string }).pathname = `/users/${user.id}`;
  return render(
    <Routes>
      <Route path="/users/:user" element={<UserDetailsPage />} />
    </Routes>,
  );
}

describe('User details', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.getUserAccessKeys).mockResolvedValue([]);
    vi.mocked(APIClient.getTenants).mockResolvedValue([]);
    vi.mocked(APIClient.getUsers).mockResolvedValue([]);
    vi.mocked(APIClient.get2FAStatus).mockResolvedValue({ enabled: false } as any);
    vi.mocked(APIClient.listIAMRoles).mockResolvedValue([]);
    vi.mocked(APIClient.listUserGroups).mockResolvedValue([]);
    vi.mocked(APIClient.listGroups).mockResolvedValue([]);
    vi.mocked(APIClient.updateUser).mockResolvedValue({} as any);
  });

  // An administrator marks a user of an identity provider as one whose role
  // and access follow the provider's groups.
  it('saves whether an external user follows its groups', async () => {
    const user = userEvent.setup();
    renderUser({ id: 'user-1', username: 'alice@example.org', authProvider: 'oauth:idp-1', managedByGroups: false });

    await user.click(await screen.findByRole('button', { name: /Edit User/i }));
    const box = await screen.findByRole('checkbox', { name: /Follows the provider's groups/i });
    expect(box).not.toBeChecked();
    await user.click(box);
    await user.click(screen.getByRole('button', { name: /Save Changes/i }));

    await waitFor(() => expect(APIClient.updateUser).toHaveBeenCalledWith(
      'user-1', expect.objectContaining({ managedByGroups: true })));
  });

  it('offers no group mark for a local user', async () => {
    const user = userEvent.setup();
    renderUser({ id: 'carla', username: 'carla', authProvider: 'local' });

    await user.click(await screen.findByRole('button', { name: /Edit User/i }));
    await screen.findByRole('button', { name: /Save Changes/i });
    expect(screen.queryByRole('checkbox', { name: /Follows the provider's groups/i })).toBeNull();
  });
});
