import React, { useState, useEffect } from 'react';
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
import { EmptyState } from '@/components/ui/EmptyState';

export default function UsersPage() {
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

  const createUserMutation = useMutation({
    mutationFn: (data: CreateUserRequest) => APIClient.createUser(data),
    onSuccess: (response, variables) => {
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      setIsCreateModalOpen(false);
      setNewUser({ roles: ['user'], status: 'active' });
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

  // Badge color helpers
  const getRoleBadgeClasses = (role: string) => {
    switch (role) {
      case 'admin':
        return 'bg-purple-100 text-purple-700 border-purple-300 dark:bg-purple-500/20 dark:text-purple-300 dark:border-purple-500/30';
      case 'user':
        return 'bg-blue-100 text-blue-700 border-blue-300 dark:bg-blue-500/20 dark:text-blue-300 dark:border-blue-500/30';
      case 'readonly':
        return 'bg-orange-100 text-orange-700 border-orange-300 dark:bg-orange-500/20 dark:text-orange-300 dark:border-orange-500/30';
      case 'guest':
        return 'bg-gray-100 text-gray-700 border-gray-300 dark:bg-gray-500/20 dark:text-gray-300 dark:border-gray-500/30';
      default:
        return 'bg-gray-100 text-gray-700 border-gray-300 dark:bg-gray-500/20 dark:text-gray-300 dark:border-gray-500/30';
    }
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
        <MetricCard
          title="Total Users"
          value={filteredUsers.length}
          icon={Users}
          description="Across all tenants"
          color="brand"
        />

        <MetricCard
          title="Active Users"
          value={filteredUsers.filter((user: User) => user.status === 'active').length}
          icon={UserCheck}
          description="Ready to use the system"
          color="success"
        />

        <MetricCard
          title="Admin Users"
          value={filteredUsers.filter((user: User) => user.roles.includes('admin')).length}
          icon={Shield}
          description="Users with admin access"
          color="blue-light"
        />

        <MetricCard
          title="Inactive Users"
          value={filteredUsers.filter((user: User) => user.status !== 'active').length}
          icon={UserX}
          description="Suspended or inactive"
          color="warning"
        />
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
            <EmptyState
              icon={Users}
              title="No users found"
              description={searchTerm ? "No users match your search criteria. Try adjusting your search terms." : "Get started by creating your first user to manage access."}
              actionLabel={!searchTerm ? "Create User" : undefined}
              onAction={!searchTerm ? () => setIsCreateModalOpen(true) : undefined}
              showAction={!searchTerm}
            />
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
                          <span className="flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border bg-red-100 text-red-700 border-red-300 dark:bg-red-500/20 dark:text-red-300 dark:border-red-500/30">
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
                      {user.twoFactorEnabled ? (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium border bg-cyan-100 text-cyan-700 border-cyan-300 dark:bg-cyan-500/20 dark:text-cyan-300 dark:border-cyan-500/30">
                          <KeyRound className="h-3 w-3" />
                          Enabled
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium border bg-gray-100 text-gray-600 border-gray-300 dark:bg-gray-500/20 dark:text-gray-400 dark:border-gray-500/30">
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
            <label htmlFor="role" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Role
            </label>
            <select
              id="role"
              value={newUser.roles?.[0] || 'user'}
              onChange={(e) => updateNewUser('roles', [e.target.value])}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="admin">Admin - Full access to manage the system</option>
              <option value="user">User - Standard user with normal access</option>
              <option value="readonly">Read Only - Can only view, cannot modify</option>
              <option value="guest">Guest - Limited access</option>
            </select>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Select the role for this user.
            </p>
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
