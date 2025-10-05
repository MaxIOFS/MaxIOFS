'use client';

import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
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
  Settings,
  Trash2,
  UserCheck,
  UserX,
  Shield,
  Calendar,
  Mail,
  Key,
  Building2
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User, CreateUserRequest, EditUserForm } from '@/types';
import SweetAlert from '@/lib/sweetalert';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function UsersPage() {
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
      queryClient.invalidateQueries({ queryKey: ['users'] });
      setIsCreateModalOpen(false);
      setNewUser({ roles: ['read'], status: 'active' });
      SweetAlert.successUserCreated(variables.username);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: ({ userId, data }: { userId: string; data: EditUserForm }) =>
      APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      SweetAlert.toast('success', 'User updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteUserMutation = useMutation({
    mutationFn: (userId: string) => APIClient.deleteUser(userId),
    onSuccess: () => {
      SweetAlert.close();
      queryClient.invalidateQueries({ queryKey: ['users'] });
      SweetAlert.toast('success', 'User deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.close();
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

  const getRoleColor = (role: string) => {
    switch (role) {
      case 'admin': return 'bg-red-100 text-red-800';
      case 'write': return 'bg-blue-100 text-blue-800';
      case 'read': return 'bg-green-100 text-green-800';
      default: return 'bg-gray-100 text-gray-800';
    }
  };

  const getUserAccessKeysCount = (userId: string) => {
    if (!allAccessKeys) return 0;
    return allAccessKeys.filter((key: any) => key.userId === userId).length;
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active': return 'bg-green-100 text-green-800';
      case 'inactive': return 'bg-gray-100 text-gray-800';
      case 'suspended': return 'bg-red-100 text-red-800';
      default: return 'bg-gray-100 text-gray-800';
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
          <h1 className="text-3xl font-bold tracking-tight">Users</h1>
          <p className="text-muted-foreground">
            Manage user accounts and their permissions
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)} className="gap-2">
          <Plus className="h-4 w-4" />
          Create User
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Users</CardTitle>
            <Users className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{filteredUsers.length}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Users</CardTitle>
            <UserCheck className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {filteredUsers.filter((user: User) => user.status === 'active').length}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Admin Users</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {filteredUsers.filter((user: User) => user.roles.includes('admin')).length}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Inactive Users</CardTitle>
            <UserX className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {filteredUsers.filter((user: User) => user.status !== 'active').length}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Search and Filters */}
      <div className="flex items-center space-x-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
          <Input
            placeholder="Search users..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10"
          />
        </div>
      </div>

      {/* Users Table */}
      <Card>
        <CardHeader>
          <CardTitle>Users ({filteredUsers.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {filteredUsers.length === 0 ? (
            <div className="text-center py-8">
              <Users className="mx-auto h-12 w-12 text-muted-foreground" />
              <h3 className="mt-4 text-lg font-semibold">No users found</h3>
              <p className="text-muted-foreground">
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
                            className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getRoleColor(role)}`}
                          >
                            {role}
                          </span>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getStatusColor(user.status)}`}>
                        {user.status}
                      </span>
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
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(user.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
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
                          onClick={() => window.location.href = `/users/${user.id}`}
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
        </CardContent>
      </Card>

      {/* Create User Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create New User"
      >
        <form onSubmit={handleCreateUser} className="space-y-4">
          <div>
            <label htmlFor="username" className="block text-sm font-medium mb-2">
              Username
            </label>
            <Input
              id="username"
              value={newUser.username || ''}
              onChange={(e) => updateNewUser('username', e.target.value)}
              placeholder="john.doe"
              required
            />
          </div>

          <div>
            <label htmlFor="email" className="block text-sm font-medium mb-2">
              Email (Optional)
            </label>
            <Input
              id="email"
              type="email"
              value={newUser.email || ''}
              onChange={(e) => updateNewUser('email', e.target.value)}
              placeholder="john.doe@example.com"
            />
          </div>

          <div>
            <label htmlFor="password" className="block text-sm font-medium mb-2">
              Password
            </label>
            <Input
              id="password"
              type="password"
              value={newUser.password || ''}
              onChange={(e) => updateNewUser('password', e.target.value)}
              placeholder="Enter password"
              required
            />
          </div>

          {/* Tenant selector - only for global admins */}
          {isGlobalAdmin ? (
            <div>
              <label className="block text-sm font-medium mb-2">
                Tenant (Optional)
              </label>
              <select
                value={newUser.tenantId || ''}
                onChange={(e) => updateNewUser('tenantId', e.target.value)}
                className="w-full border border-gray-300 rounded-md px-3 py-2"
              >
                <option value="">No Tenant (Global User)</option>
                {tenants?.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName} ({tenant.name})
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-500 mt-1">
                Global users can access all buckets. Tenant users are limited to their tenant's buckets.
              </p>
            </div>
          ) : currentUser?.tenantId && (
            <div>
              <label className="block text-sm font-medium mb-2">
                Tenant
              </label>
              <div className="w-full border border-gray-200 bg-gray-50 rounded-md px-3 py-2 text-gray-700">
                {tenants?.find(t => t.id === currentUser.tenantId)?.displayName || 'Your Tenant'}
              </div>
              <p className="text-xs text-gray-500 mt-1">
                Tenant admins can only create users within their own tenant.
              </p>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium mb-2">
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
                    className="rounded border-gray-300"
                  />
                  <span className="text-sm capitalize">{role}</span>
                </label>
              ))}
            </div>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium mb-2">
              Status
            </label>
            <select
              id="status"
              value={newUser.status || 'active'}
              onChange={(e) => updateNewUser('status', e.target.value)}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
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