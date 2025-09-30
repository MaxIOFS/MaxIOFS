'use client';

import React, { useState } from 'react';
import { useParams } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { PermissionsEditor, Permission } from '@/components/ui/PermissionsEditor';
import {
  ArrowLeft,
  User,
  Shield,
  Save,
  AlertTriangle,
  Trash2,
  Mail,
  Key,
  Calendar,
  Settings
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User as UserType } from '@/types';

export default function UserSettingsPage() {
  const params = useParams();
  const userId = params.user as string;
  const queryClient = useQueryClient();

  const [isEditing, setIsEditing] = useState(false);
  const [userSettings, setUserSettings] = useState<Partial<UserType>>({});
  const [permissions, setPermissions] = useState<Permission[]>([]);

  const { data: user, isLoading } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => APIClient.getUser(userId),
    onSuccess: (data) => {
      if (data?.data) {
        setUserSettings(data.data);
      }
    },
  });

  const { data: userPermissions, isLoading: permissionsLoading } = useQuery({
    queryKey: ['userPermissions', userId],
    queryFn: () => APIClient.getUserPermissions(userId),
    onSuccess: (data) => {
      if (data?.data) {
        setPermissions(data.data);
      }
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: (data: Partial<UserType>) => APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', userId] });
      setIsEditing(false);
    },
  });

  const updatePermissionsMutation = useMutation({
    mutationFn: (permissions: Permission[]) => APIClient.updateUserPermissions(userId, permissions),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userPermissions', userId] });
    },
  });

  const deleteUserMutation = useMutation({
    mutationFn: () => APIClient.deleteUser(userId),
    onSuccess: () => {
      window.location.href = '/users';
    },
  });

  const handleSaveSettings = () => {
    updateUserMutation.mutate(userSettings);
  };

  const handleSavePermissions = () => {
    updatePermissionsMutation.mutate(permissions);
  };

  const handleDeleteUser = () => {
    if (confirm(`Are you sure you want to delete user "${user?.data?.username}"? This action cannot be undone and will delete all associated access keys and permissions.`)) {
      deleteUserMutation.mutate();
    }
  };

  const updateUserSetting = (key: keyof UserType, value: any) => {
    setUserSettings(prev => ({ ...prev, [key]: value }));
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  if (isLoading || permissionsLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (!user?.data) {
    return (
      <div className="rounded-md bg-red-50 p-4">
        <div className="text-sm text-red-700">
          User not found
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => window.history.back()}
            className="gap-2"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div>
            <h1 className="text-3xl font-bold tracking-tight">User Settings</h1>
            <p className="text-muted-foreground">
              Configure settings for {user.data.username}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {isEditing ? (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setIsEditing(false);
                  setUserSettings(user.data);
                }}
              >
                Cancel
              </Button>
              <Button
                onClick={handleSaveSettings}
                disabled={updateUserMutation.isPending}
                className="gap-2"
              >
                <Save className="h-4 w-4" />
                {updateUserMutation.isPending ? 'Saving...' : 'Save Changes'}
              </Button>
            </>
          ) : (
            <Button onClick={() => setIsEditing(true)} className="gap-2">
              <Settings className="h-4 w-4" />
              Edit Settings
            </Button>
          )}
        </div>
      </div>

      {/* User Information */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <User className="h-5 w-5" />
            User Information
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">Username</label>
              <Input
                value={userSettings.username || ''}
                onChange={(e) => updateUserSetting('username', e.target.value)}
                disabled={!isEditing}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Email</label>
              <Input
                type="email"
                value={userSettings.email || ''}
                onChange={(e) => updateUserSetting('email', e.target.value)}
                disabled={!isEditing}
                placeholder="user@example.com"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Status</label>
            <select
              value={userSettings.status || 'active'}
              onChange={(e) => updateUserSetting('status', e.target.value)}
              disabled={!isEditing}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            >
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="suspended">Suspended</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Roles</label>
            <div className="space-y-2">
              {['read', 'write', 'admin'].map((role) => (
                <label key={role} className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    checked={userSettings.roles?.includes(role) || false}
                    onChange={(e) => {
                      const currentRoles = userSettings.roles || [];
                      if (e.target.checked) {
                        updateUserSetting('roles', [...currentRoles, role]);
                      } else {
                        updateUserSetting('roles', currentRoles.filter(r => r !== role));
                      }
                    }}
                    disabled={!isEditing}
                    className="rounded border-gray-300"
                  />
                  <span className="text-sm capitalize">{role}</span>
                </label>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4 text-sm text-muted-foreground">
            <div>
              <label className="font-medium">Created</label>
              <div className="flex items-center gap-1 mt-1">
                <Calendar className="h-3 w-3" />
                {formatDate(user.data.createdAt)}
              </div>
            </div>
            <div>
              <label className="font-medium">Last Modified</label>
              <div className="flex items-center gap-1 mt-1">
                <Calendar className="h-3 w-3" />
                {formatDate(user.data.updatedAt || user.data.createdAt)}
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Password Management */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            Password Management
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {isEditing && (
            <>
              <div>
                <label className="block text-sm font-medium mb-2">New Password</label>
                <Input
                  type="password"
                  value={userSettings.password || ''}
                  onChange={(e) => updateUserSetting('password', e.target.value)}
                  placeholder="Leave empty to keep current password"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-2">Confirm Password</label>
                <Input
                  type="password"
                  placeholder="Confirm new password"
                />
              </div>
            </>
          )}

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="force-password-change"
              checked={userSettings.forcePasswordChange || false}
              onChange={(e) => updateUserSetting('forcePasswordChange', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="force-password-change" className="text-sm">
              Force password change on next login
            </label>
          </div>
        </CardContent>
      </Card>

      {/* Permissions */}
      <PermissionsEditor
        permissions={permissions}
        onChange={setPermissions}
        resourceType="bucket"
        disabled={!userSettings.roles?.includes('admin')}
      />

      {updatePermissionsMutation.isDirty && (
        <div className="flex justify-end">
          <Button
            onClick={handleSavePermissions}
            disabled={updatePermissionsMutation.isPending}
            className="gap-2"
          >
            <Save className="h-4 w-4" />
            {updatePermissionsMutation.isPending ? 'Saving...' : 'Save Permissions'}
          </Button>
        </div>
      )}

      {/* Danger Zone */}
      <Card className="border-red-200">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-red-600">
            <AlertTriangle className="h-5 w-5" />
            Danger Zone
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between p-4 border border-red-200 rounded-md">
            <div>
              <h3 className="font-semibold text-red-600">Delete User</h3>
              <p className="text-sm text-muted-foreground">
                Permanently delete this user account and all associated data. This action cannot be undone.
              </p>
            </div>
            <Button
              variant="destructive"
              onClick={handleDeleteUser}
              disabled={deleteUserMutation.isPending}
              className="gap-2"
            >
              <Trash2 className="h-4 w-4" />
              {deleteUserMutation.isPending ? 'Deleting...' : 'Delete User'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}