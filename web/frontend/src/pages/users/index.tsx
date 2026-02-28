import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import {
  Users,
  Plus,
  Search,
  Trash2,
  UserCheck,
  UserX,
  Calendar,
  Mail,
  Key,
  KeyRound,
  Building2,
  Lock,
  Unlock,
  Settings,
  UserStar
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User, CreateUserRequest, EditUserForm } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { EmptyState } from '@/components/ui/EmptyState';
import { IDPStatusBadge } from '@/components/identity-providers/IDPStatusBadge';

export default function UsersPage() {
  const { t } = useTranslation(['users', 'common']);
  const navigate = useNavigate();
  const { isGlobalAdmin, isTenantAdmin, user: currentUser } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [newUser, setNewUser] = useState<Partial<CreateUserRequest>>({
    roles: ['user'],
    status: 'active',
  });
  const queryClient = useQueryClient();

  // Only admins (global or tenant) can access this page
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;
  
  // Redirect non-admins to their profile page
  useEffect(() => {
    if (currentUser && !isAnyAdmin) {
      navigate(`/users/${currentUser.id}`);
    }
  }, [currentUser, isAnyAdmin, navigate]);

  const { data: users, isLoading, error } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: isAnyAdmin, // Only fetch if user is admin
  });

  // Fetch access keys for all users
  const { data: allAccessKeys } = useQuery({
    queryKey: ['accessKeys'],
    queryFn: () => APIClient.getAccessKeys(),
  });

  // Fetch tenants for assignment
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  // Fetch identity providers for SSO user creation
  const { data: idps } = useQuery({
    queryKey: ['identity-providers'],
    queryFn: APIClient.listIDPs,
    enabled: isAnyAdmin,
  });

  const oauthProviders = idps?.filter(p => p.type === 'oauth2' && p.status === 'active') || [];
  const isExternalUser = !!(newUser as any).authProvider && (newUser as any).authProvider !== 'local';

  const createUserMutation = useMutation({
    mutationFn: (data: CreateUserRequest) => APIClient.createUser(data),
    onSuccess: (response, variables) => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      setIsCreateModalOpen(false);
      setNewUser({ roles: ['user'], status: 'active', authProvider: undefined, externalId: undefined });
      ModalManager.successUserCreated(variables.username);
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: EditUserForm }) =>
      APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.toast('success', t('userUpdatedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteUserMutation = useMutation({
    mutationFn: (userId: string) => APIClient.deleteUser(userId),
    onSuccess: () => {
      ModalManager.close();
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.toast('success', t('userDeletedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.close();
      ModalManager.apiError(error);
    },
  });

  const unlockUserMutation = useMutation({
    mutationFn: (userId: string) => APIClient.unlockUser(userId),
    onSuccess: () => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['locked-users'] });
      ModalManager.toast('success', t('userUnlockedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const filteredUsers = users?.filter((user: User) =>
    user.username.toLowerCase().includes(searchTerm.toLowerCase()) ||
    user.email?.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

  const handleCreateUser = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newUser.username) return;
    if (!isExternalUser && !newUser.password) return;

    // Clean up empty string values - convert to undefined
    const userData: CreateUserRequest = {
      ...newUser as CreateUserRequest,
      tenantId: newUser.tenantId && newUser.tenantId !== '' ? newUser.tenantId : undefined,
      authProvider: isExternalUser ? newUser.authProvider : undefined,
      externalId: isExternalUser ? (newUser.externalId || newUser.username) : undefined,
      password: isExternalUser ? '' : newUser.password!,
    };
    createUserMutation.mutate(userData);
  };

  const handleDeleteUser = async (userId: string) => {
    const user = users?.find((u: User) => u.id === userId);

    try {
      const result = await ModalManager.confirmDeleteUser(user?.username || 'user');

      if (result.isConfirmed) {
        ModalManager.loading(t('deletingUser'), t('deletingUserMessage', { username: user?.username }));
        deleteUserMutation.mutate(userId);
      }
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const handleToggleUserStatus = (userId: string, currentStatus: string) => {
    const newStatus = currentStatus === 'active' ? 'inactive' : 'active';
    const user = users?.find((u: User) => u.id === userId);
    if (!user) return;

    updateUserMutation.mutate({
      userId,
      data: {
        status: newStatus as 'active' | 'inactive' | 'suspended',
        roles: user.roles,
        email: user.email
      }
    });
  };

  const handleUnlockUser = async (userId: string) => {
    const user = users?.find((u: User) => u.id === userId);
    if (!user) return;

    try {
      const result = await ModalManager.fire({
        title: t('unlockTitle'),
        text: t('unlockMessage', { username: user.username }),
        icon: 'question',
        showCancelButton: true,
        confirmButtonText: t('yesUnlock'),
        cancelButtonText: t('common:cancel'),
        confirmButtonColor: '#3b82f6',
      });

      if (result.isConfirmed) {
        unlockUserMutation.mutate(userId);
      }
    } catch (error) {
      ModalManager.apiError(error);
    }
  };

  const isUserLocked = (user: User): boolean => {
    if (!(user as any).lockedUntil) return false;
    const now = Math.floor(Date.now() / 1000);
    return (user as any).lockedUntil > now;
  };

  const updateNewUser = (field: keyof CreateUserRequest, value: any) => {
    setNewUser((prev: Partial<CreateUserRequest>) => ({ ...prev, [field]: value }));
  };

  const formatDate = (dateValue: string | number) => {
    // Si es un número, convertirlo de timestamp Unix (segundos) a milisegundos
    const date = typeof dateValue === 'number' ? new Date(dateValue * 1000) : new Date(dateValue);
    return date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getUserAccessKeysCount = (userId: string) => {
    if (!allAccessKeys) return 0;
    return allAccessKeys.filter((key: any) => key.userId === userId).length;
  };

  // Badge color helpers
  const getRoleBadgeClasses = (role: string) => {
    // All roles use gray - only differentiate admin with slightly darker shade
    if (role === 'admin') {
      return 'bg-gray-200 text-gray-800 border-gray-400 dark:bg-gray-600/30 dark:text-gray-200 dark:border-gray-500/40';
    }
    return 'bg-gray-100 text-gray-700 border-gray-300 dark:bg-gray-500/20 dark:text-gray-300 dark:border-gray-500/30';
  };

  const getStatusBadgeClasses = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-100 text-green-700 border-green-300 dark:bg-green-500/20 dark:text-green-300 dark:border-green-500/30';
      case 'suspended':
        return 'bg-red-100 text-red-700 border-red-300 dark:bg-red-500/20 dark:text-red-300 dark:border-red-500/30';
      case 'inactive':
        return 'bg-yellow-100 text-yellow-700 border-yellow-300 dark:bg-yellow-500/20 dark:text-yellow-300 dark:border-yellow-500/30';
      default:
        return 'bg-gray-100 text-gray-700 border-gray-300 dark:bg-gray-500/20 dark:text-gray-300 dark:border-gray-500/30';
    }
  };


  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-md bg-red-50 p-4">
        <div className="text-sm text-red-700">
          {t('errorLoadingUsers', { error: error instanceof Error ? error.message : 'Unknown error' })}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">{t('title')}</h1>
          <p className="text-gray-500 dark:text-gray-400">
            {t('manageUsers')}
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)} className="bg-brand-600 hover:bg-brand-700 text-white inline-flex items-center gap-2" variant="outline">
          <Plus className="h-4 w-4" />
          {t('createUser')}
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 md:gap-6">
        <MetricCard
          title={t('totalUsers')}
          value={filteredUsers.length}
          icon={Users}
          description={t('acrossAllTenants')}
          color="brand"
        />

        <MetricCard
          title={t('activeUsers')}
          value={filteredUsers.filter((user: User) => user.status === 'active').length}
          icon={UserCheck}
          description={t('readyToUse')}
          color="success"
        />

        <MetricCard
          title={t('adminUsers')}
          value={filteredUsers.filter((user: User) => user.roles.includes('admin')).length}
          icon={UserStar}
          description={t('usersWithAdminAccess')}
          color="blue-light"
        />

        <MetricCard
          title={t('inactiveUsers')}
          value={filteredUsers.filter((user: User) => user.status !== 'active').length}
          icon={UserX}
          description={t('suspendedOrInactive')}
          color="warning"
        />
      </div>

      {/* Users Table */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('allUsers', { count: filteredUsers.length })}</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{t('manageUserPermissions')}</p>

          {/* Search */}
          <div className="mt-4 relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 h-5 w-5" />
            <Input
              placeholder={t('searchUsers')}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-10 bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>
        </div>
        <div className="overflow-x-auto">
          {filteredUsers.length === 0 ? (
            <EmptyState
              icon={Users}
              title={t('noUsersFound')}
              description={searchTerm ? t('noUsersSearch') : t('noUsersEmpty')}
              actionLabel={!searchTerm ? t('createUser') : undefined}
              onAction={!searchTerm ? () => setIsCreateModalOpen(true) : undefined}
              showAction={!searchTerm}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('username')}</TableHead>
                  <TableHead>{t('email')}</TableHead>
                  <TableHead>{t('tenant')}</TableHead>
                  <TableHead>{t('roles')}</TableHead>
                  <TableHead>{t('status')}</TableHead>
                  <TableHead>Auth</TableHead>
                  <TableHead>2FA</TableHead>
                  <TableHead>{t('accessKeys')}</TableHead>
                  <TableHead>{t('created')}</TableHead>
                  <TableHead className="text-right">{t('actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredUsers.map((user: User) => (
                  <TableRow key={user.id}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <Users className="h-4 w-4 text-muted-foreground" />
                        {user.username}
                        {isUserLocked(user) && (
                          <span className="flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border bg-red-100 text-red-700 border-red-300 dark:bg-red-500/20 dark:text-red-300 dark:border-red-500/30">
                            <Lock className="h-3 w-3" />
                            {t('locked')}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        {user.email && <Mail className="h-3 w-3 text-muted-foreground" />}
                        {user.email || '-'}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        {user.tenantId ? (
                          <>
                            <Building2 className="h-3 w-3 text-muted-foreground" />
                            <span className="text-sm">
                              {tenants?.find(ten => ten.id === user.tenantId)?.displayName || user.tenantId}
                            </span>
                          </>
                        ) : (
                          <span className="text-sm text-muted-foreground italic">{t('global')}</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {user.roles.map((role: string) => (
                          <span
                            key={role}
                            className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium border ${getRoleBadgeClasses(role)}`}
                          >
                            {role}
                          </span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium border ${getStatusBadgeClasses(user.status)}`}>
                        {user.status}
                      </span>
                    </TableCell>
                    <TableCell>
                      <IDPStatusBadge authProvider={user.authProvider} />
                    </TableCell>
                    <TableCell>
                      {user.twoFactorEnabled ? (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium border bg-green-100 text-green-700 border-green-300 dark:bg-green-500/20 dark:text-green-300 dark:border-green-500/30">
                          <KeyRound className="h-3 w-3" />
                          {t('enabled')}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium border bg-gray-100 text-gray-600 border-gray-300 dark:bg-gray-500/20 dark:text-gray-400 dark:border-gray-500/30">
                          <KeyRound className="h-3 w-3" />
                          {t('disabled')}
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Key className="h-3 w-3 text-muted-foreground" />
                        <span className="text-sm">
                          {getUserAccessKeysCount(user.id)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                        <Calendar className="h-3 w-3" />
                        {formatDate(user.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {isUserLocked(user) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleUnlockUser(user.id)}
                            disabled={unlockUserMutation.isPending}
                            className="text-blue-600 hover:text-blue-700 hover:bg-blue-50"
                            title="Unlock account"
                          >
                            <Unlock className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggleUserStatus(user.id, user.status)}
                          disabled={updateUserMutation.isPending}
                        >
                          {user.status === 'active' ? (
                            <UserX className="h-4 w-4" />
                          ) : (
                            <UserCheck className="h-4 w-4" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => navigate(`/users/${user.id}`)}
                          title="User settings"
                        >
                          <Settings className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteUser(user.id)}
                          disabled={deleteUserMutation.isPending}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>

      {/* Create User Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title={t('createNewUser')}
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
          <div>
            <label htmlFor="username" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              {isExternalUser ? t('emailOptional') : t('username')}
            </label>
            <Input
              id="username"
              type={isExternalUser ? 'email' : 'text'}
              value={newUser.username || ''}
              onChange={(e) => updateNewUser('username', e.target.value)}
              placeholder={isExternalUser ? 'user@example.com' : 'john.doe'}
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              required
            />
          </div>

          <div>
            <label htmlFor="email" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              {t('emailOptional')}
            </label>
            <Input
              id="email"
              type="email"
              value={newUser.email || ''}
              onChange={(e) => updateNewUser('email', e.target.value)}
              placeholder="john.doe@example.com"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>

          {/* Auth Provider selector */}
          {oauthProviders.length > 0 && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Authentication
              </label>
              <select
                value={newUser.authProvider || 'local'}
                onChange={(e) => {
                  const value = e.target.value;
                  if (value === 'local') {
                    setNewUser(prev => ({ ...prev, authProvider: undefined, externalId: undefined }));
                  } else {
                    setNewUser(prev => ({ ...prev, authProvider: value, password: '' }));
                  }
                }}
                className="w-full border border-gray-200 dark:border-gray-700 rounded-md px-3 py-2 bg-white dark:bg-gray-900 text-gray-900 dark:text-white"
              >
                <option value="local">Local (password)</option>
                {oauthProviders.map((provider) => (
                  <option key={provider.id} value={`oauth:${provider.id}`}>
                    {provider.name} (SSO)
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {isExternalUser
                  ? 'This user will authenticate via SSO. Use their email as username.'
                  : 'This user will authenticate with a local password.'}
              </p>
            </div>
          )}

          {/* Password - only for local users */}
          {!isExternalUser && (
            <div>
              <label htmlFor="password" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                {t('password')}
              </label>
              <Input
                id="password"
                type="password"
                value={newUser.password || ''}
                onChange={(e) => updateNewUser('password', e.target.value)}
                placeholder={t('enterPassword')}
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                required
              />
            </div>
          )}

          {/* Tenant selector - only for global admins */}
          {isGlobalAdmin ? (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                {t('tenantOptional')}
              </label>
              <select
                value={newUser.tenantId || ''}
                onChange={(e) => updateNewUser('tenantId', e.target.value)}
                className="w-full border border-gray-200 dark:border-gray-700 rounded-md px-3 py-2 bg-white dark:bg-gray-900 text-gray-900 dark:text-white"
              >
                <option value="">{t('noTenantGlobal')}</option>
                {tenants?.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName} ({tenant.name})
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {t('globalUsersInfo')}
              </p>
            </div>
          ) : currentUser?.tenantId && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                {t('tenant')}
              </label>
              <div className="w-full border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 rounded-md px-3 py-2 text-gray-700 dark:text-gray-300">
                {tenants?.find(ten => ten.id === currentUser.tenantId)?.displayName || t('yourTenant')}
              </div>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {t('tenantAdminsInfo')}
              </p>
            </div>
          )}

          <div>
            <label htmlFor="role" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              {t('role')}
            </label>
            <select
              id="role"
              value={newUser.roles?.[0] || 'user'}
              onChange={(e) => updateNewUser('roles', [e.target.value])}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="admin">{t('adminRole')}</option>
              <option value="user">{t('userRole')}</option>
              <option value="readonly">{t('readonlyRole')}</option>
              <option value="guest">{t('guestRole')}</option>
            </select>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              {t('selectRole')}
            </p>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              {t('status')}
            </label>
            <select
              id="status"
              value={newUser.status || 'active'}
              onChange={(e) => updateNewUser('status', e.target.value)}
              className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="active">{t('active')}</option>
              <option value="inactive">{t('inactive')}</option>
              <option value="suspended">{t('suspended')}</option>
            </select>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateModalOpen(false)}
            >
              {t('common:cancel')}
            </Button>
            <Button
              type="submit"
              disabled={createUserMutation.isPending || !newUser.username || (!isExternalUser && !newUser.password)}
            >
              {createUserMutation.isPending ? t('creating') : t('createUser')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
