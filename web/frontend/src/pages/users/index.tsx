import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
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
  Shield,
  Calendar,
  Mail,
  Key,
  KeyRound,
  Building2,
  Lock,
  Unlock,
  Settings
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User, CreateUserRequest, EditUserForm } from '@/types';
import SweetAlert from '@/lib/sweetalert';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function UsersPage() {
  const navigate = useNavigate();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [newUser, setNewUser] = useState<Partial<CreateUserRequest>>({
    roles: ['read'],
    status: 'active',
  });
  const queryClient = useQueryClient();

  const { data: users, isLoading, error } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
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

  const createUserMutation = useMutation({
    mutationFn: (data: CreateUserRequest) => APIClient.createUser(data),
    onSuccess: (response, variables) => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      setIsCreateModalOpen(false);
      setNewUser({ roles: ['read'], status: 'active' });
      SweetAlert.successUserCreated(variables.username);
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: EditUserForm }) =>
      APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      SweetAlert.toast('success', 'User updated successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteUserMutation = useMutation({
    mutationFn: (userId: string) => APIClient.deleteUser(userId),
    onSuccess: () => {
      SweetAlert.close();
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      SweetAlert.toast('success', 'User deleted successfully');
    },
    onError: (error: Error) => {
      SweetAlert.close();
      SweetAlert.apiError(error);
    },
  });

  const unlockUserMutation = useMutation({
    mutationFn: (userId: string) => APIClient.unlockUser(userId),
    onSuccess: () => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['locked-users'] });
      SweetAlert.toast('success', 'User unlocked successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const filteredUsers = users?.filter((user: User) =>
    user.username.toLowerCase().includes(searchTerm.toLowerCase()) ||
    user.email?.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

  const handleCreateUser = (e: React.FormEvent) => {
    e.preventDefault();
    if (newUser.username && newUser.password) {
      // Clean up empty string values - convert to undefined
      const userData: CreateUserRequest = {
        ...newUser as CreateUserRequest,
        tenantId: newUser.tenantId && newUser.tenantId !== '' ? newUser.tenantId : undefined,
      };
      createUserMutation.mutate(userData);
    }
  };

  const handleDeleteUser = async (userId: string) => {
    const user = users?.find((u: User) => u.id === userId);

    try {
      const result = await SweetAlert.confirmDeleteUser(user?.username || 'user');

      if (result.isConfirmed) {
        SweetAlert.loading('Deleting user...', `Deleting "${user?.username}"`);
        deleteUserMutation.mutate(userId);
      }
    } catch (error) {
      SweetAlert.close();
      SweetAlert.apiError(error);
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
      const result = await SweetAlert.fire({
        title: 'Unlock Account',
        text: `Are you sure you want to unlock "${user.username}"?`,
        icon: 'question',
        showCancelButton: true,
        confirmButtonText: 'Yes, unlock',
        cancelButtonText: 'Cancel',
        confirmButtonColor: '#3b82f6',
      });

      if (result.isConfirmed) {
        unlockUserMutation.mutate(userId);
      }
    } catch (error) {
      SweetAlert.apiError(error);
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
          Error loading users: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Users</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Manage user accounts and their permissions
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)} className="bg-brand-600 hover:bg-brand-700 text-white inline-flex items-center gap-2" variant="outline">
          <Plus className="h-4 w-4" />
          Create User
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 md:gap-6">
        {/* Total Users Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Users</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{filteredUsers.length}</h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
                Across all tenants
              </p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
              <Users className="h-7 w-7 text-brand-600 dark:text-brand-400" />
            </div>
          </div>
        </div>

        {/* Active Users Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Active Users</p>
              <h3 className="text-3xl font-bold text-success-600 dark:text-success-400">
                {filteredUsers.filter((user: User) => user.status === 'active').length}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
                Ready to use the system
              </p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-success-50 dark:bg-success-900/30">
              <UserCheck className="h-7 w-7 text-success-600 dark:text-success-400" />
            </div>
          </div>
        </div>

        {/* Admin Users Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Admin Users</p>
              <h3 className="text-3xl font-bold text-blue-light-600 dark:text-blue-light-400">
                {filteredUsers.filter((user: User) => user.roles.includes('admin')).length}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
                Users with admin access
              </p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-light-50 dark:bg-blue-light-900/30">
              <Shield className="h-7 w-7 text-blue-light-600 dark:text-blue-light-400" />
            </div>
          </div>
        </div>

        {/* Inactive Users Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Inactive Users</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">
                {filteredUsers.filter((user: User) => user.status !== 'active').length}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
                Suspended or inactive
              </p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-gray-100 dark:bg-gray-700">
              <UserX className="h-7 w-7 text-gray-600 dark:text-gray-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Users Table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">All Users ({filteredUsers.length})</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Manage user accounts and permissions</p>

          {/* Search */}
          <div className="mt-4 relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 h-5 w-5" />
            <Input
              placeholder="Search users by username or email..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-10 bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>
        </div>
        <div className="p-6">
          {filteredUsers.length === 0 ? (
            <div className="text-center py-8">
              <Users className="mx-auto h-12 w-12 text-gray-400 dark:text-gray-500" />
              <h3 className="mt-4 text-lg font-semibold text-gray-900 dark:text-white">No users found</h3>
              <p className="text-gray-500 dark:text-gray-400">
                {searchTerm ? 'Try adjusting your search terms' : 'Get started by creating your first user'}
              </p>
              {!searchTerm && (
                <Button
                  onClick={() => setIsCreateModalOpen(true)}
                  className="mt-4 gap-2"
                >
                  <Plus className="h-4 w-4" />
                  Create User
                </Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Username</TableHead>
                  <TableHead>Email</TableHead>
                  <TableHead>Tenant</TableHead>
                  <TableHead>Roles</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>2FA</TableHead>
                  <TableHead>Access Keys</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
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
                          <span className="flex items-center gap-1 px-2 py-0.5 bg-red-100 text-red-700 rounded-full text-xs font-medium">
                            <Lock className="h-3 w-3" />
                            Locked
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
                              {tenants?.find(t => t.id === user.tenantId)?.displayName || user.tenantId}
                            </span>
                          </>
                        ) : (
                          <span className="text-sm text-muted-foreground italic">Global</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {user.roles.map((role: string) => (
                          <span
                            key={role}
                            className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                              role === 'admin'
                                ? 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-400 border border-red-200 dark:border-red-800'
                                : role === 'write'
                                ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-400 border border-blue-200 dark:border-blue-800'
                                : 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border border-green-200 dark:border-green-800'
                            }`}
                          >
                            {role}
                          </span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                        user.status === 'active'
                          ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border border-green-200 dark:border-green-800'
                          : user.status === 'suspended'
                          ? 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-400 border border-red-200 dark:border-red-800'
                          : 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-400 border border-gray-200 dark:border-gray-600'
                      }`}>
                        {user.status}
                      </span>
                    </TableCell>
                    <TableCell>
                      {user.twoFactorEnabled ? (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border border-green-200 dark:border-green-800">
                          <KeyRound className="h-3 w-3" />
                          Enabled
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 border border-gray-200 dark:border-gray-600">
                          <KeyRound className="h-3 w-3" />
                          Disabled
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
        title="Create New User"
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
          <div>
            <label htmlFor="username" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Username
            </label>
            <Input
              id="username"
              value={newUser.username || ''}
              onChange={(e) => updateNewUser('username', e.target.value)}
              placeholder="john.doe"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              required
            />
          </div>

          <div>
            <label htmlFor="email" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Email (Optional)
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

          <div>
            <label htmlFor="password" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Password
            </label>
            <Input
              id="password"
              type="password"
              value={newUser.password || ''}
              onChange={(e) => updateNewUser('password', e.target.value)}
              placeholder="Enter password"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              required
            />
          </div>

          {/* Tenant selector - only for global admins */}
          {isGlobalAdmin ? (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Tenant (Optional)
              </label>
              <select
                value={newUser.tenantId || ''}
                onChange={(e) => updateNewUser('tenantId', e.target.value)}
                className="w-full border border-gray-200 dark:border-gray-700 rounded-md px-3 py-2 bg-white dark:bg-gray-900 text-gray-900 dark:text-white"
              >
                <option value="">No Tenant (Global User)</option>
                {tenants?.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName} ({tenant.name})
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Global users can access all buckets. Tenant users are limited to their tenant's buckets.
              </p>
            </div>
          ) : currentUser?.tenantId && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Tenant
              </label>
              <div className="w-full border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 rounded-md px-3 py-2 text-gray-700 dark:text-gray-300">
                {tenants?.find(t => t.id === currentUser.tenantId)?.displayName || 'Your Tenant'}
              </div>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Tenant admins can only create users within their own tenant.
              </p>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Roles
            </label>
            <div className="space-y-2">
              {['read', 'write', 'admin'].map((role) => (
                <label key={role} className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    checked={newUser.roles?.includes(role) || false}
                    onChange={(e) => {
                      const currentRoles = newUser.roles || [];
                      if (e.target.checked) {
                        updateNewUser('roles', [...currentRoles, role]);
                      } else {
                        updateNewUser('roles', currentRoles.filter((r: string) => r !== role));
                      }
                    }}
                    className="rounded border-gray-300 dark:border-gray-600"
                  />
                  <span className="text-sm capitalize text-gray-700 dark:text-gray-300">{role}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Status
            </label>
            <select
              id="status"
              value={newUser.status || 'active'}
              onChange={(e) => updateNewUser('status', e.target.value)}
              className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="suspended">Suspended</option>
            </select>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createUserMutation.isPending || !newUser.username || !newUser.password}
            >
              {createUserMutation.isPending ? 'Creating...' : 'Create User'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
