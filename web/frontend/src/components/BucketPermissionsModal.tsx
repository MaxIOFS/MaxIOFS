import React, { useState } from 'react';
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
  Shield,
  Plus,
  Trash2,
  Users,
  Building2,
  Calendar,
  AlertCircle,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { BucketPermission, GrantPermissionRequest } from '@/types';
import ModalManager from '@/lib/modals';

interface BucketPermissionsModalProps {
  isOpen: boolean;
  onClose: () => void;
  bucketName: string;
  tenantId?: string;
  readOnly?: boolean;
}

export function BucketPermissionsModal({
  isOpen,
  onClose,
  bucketName,
  tenantId,
  readOnly = false,
}: BucketPermissionsModalProps) {
  const [isAddPermissionOpen, setIsAddPermissionOpen] = useState(false);
  const [newPermission, setNewPermission] = useState<Partial<GrantPermissionRequest>>({
    permissionLevel: 'read',
    grantedBy: 'admin',
  });
  const queryClient = useQueryClient();

  // Scope rule: global bucket → global users only; tenant bucket → same-tenant users only
  const isGlobalBucket = !tenantId;

  // Fetch bucket permissions
  const { data: permissions, isLoading } = useQuery({
    queryKey: ['bucketPermissions', bucketName, tenantId],
    queryFn: () => APIClient.getBucketPermissions(bucketName, tenantId),
    enabled: isOpen,
  });

  // Fetch all users for dropdown and name resolution
  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: isOpen,
  });

  // Fetch tenants only for resolving names of existing (legacy) tenant-type permissions in the table
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
    enabled: isOpen,
  });

  // Strict scope filtering — no cross-scope grants allowed:
  // Global bucket → only non-admin global users (no tenantId)
  // Tenant bucket → only non-admin users belonging to exactly that tenant
  // Admins (global or tenant) already have full access and don't need explicit grants
  const selectableUsers = users?.filter(u => {
    if (u.roles?.includes('admin')) return false;
    return isGlobalBucket ? !u.tenantId : u.tenantId === tenantId;
  }) ?? [];

  const scopeLabel = isGlobalBucket
    ? 'Global users only'
    : `Users in this tenant only`;

  // Grant permission mutation
  const grantPermissionMutation = useMutation({
    mutationFn: (data: GrantPermissionRequest) =>
      APIClient.grantBucketPermission(bucketName, data, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucketPermissions', bucketName] });
      setIsAddPermissionOpen(false);
      setNewPermission({ permissionLevel: 'read', grantedBy: 'admin' });
      ModalManager.toast('success', 'Permission granted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Revoke permission mutation
  const revokePermissionMutation = useMutation({
    mutationFn: ({ userId, permissionTenantId }: { userId?: string; permissionTenantId?: string }) =>
      APIClient.revokeBucketPermission(bucketName, userId, permissionTenantId, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucketPermissions', bucketName] });
      ModalManager.toast('success', 'Permission revoked successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const handleGrantPermission = (e: React.FormEvent) => {
    e.preventDefault();

    if (!newPermission.userId) {
      ModalManager.toast('error', 'Please select a user');
      return;
    }

    const permission: GrantPermissionRequest = {
      userId: newPermission.userId,
      tenantId: undefined, // never grant by tenant — always by individual user
      permissionLevel: newPermission.permissionLevel || 'read',
      grantedBy: newPermission.grantedBy || 'admin',
      expiresAt: newPermission.expiresAt,
    };

    grantPermissionMutation.mutate(permission);
  };

  const handleRevokePermission = (permission: BucketPermission) => {
    const targetName = permission.userId
      ? getUserName(permission.userId)
      : permission.tenantId
        ? getTenantName(permission.tenantId)
        : 'Unknown';
    const targetKind = permission.userId ? 'user' : 'tenant';

    ModalManager.confirm(
      'Revoke Permission?',
      `Are you sure you want to revoke ${permission.permissionLevel} access for ${targetKind} "${targetName}"?`,
      () => revokePermissionMutation.mutate({
        userId: permission.userId || undefined,
        permissionTenantId: permission.tenantId || undefined,
      })
    );
  };

  const getPermissionLevelColor = (level: string) => {
    switch (level) {
      case 'admin':  return 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300';
      case 'write':  return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300';
      case 'read':   return 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300';
      default:       return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300';
    }
  };

  const formatDate = (timestamp: number) => {
    if (!timestamp) return 'N/A';
    return new Date(timestamp * 1000).toLocaleString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getUserName = (userId: string) => {
    const user = users?.find(u => u.id === userId);
    return user ? user.username : userId;
  };

  const getTenantName = (tId: string) => {
    const tenant = tenants?.find(t => t.id === tId);
    return tenant ? tenant.displayName : tId;
  };

  return (
    <>
      <Modal
        isOpen={isOpen}
        onClose={onClose}
        title={`Bucket Permissions: ${bucketName}`}
        size="lg"
      >
        <div className="space-y-4">
          {/* Header with scope badge and Add Button */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${
                isGlobalBucket
                  ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300'
                  : 'bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300'
              }`}>
                {isGlobalBucket ? <Users className="h-3 w-3" /> : <Building2 className="h-3 w-3" />}
                {isGlobalBucket ? 'Global bucket' : 'Tenant bucket'}
              </span>
              <span className="text-sm text-gray-500 dark:text-gray-400">
                {readOnly ? 'View access permissions' : 'Manage access permissions'}
              </span>
            </div>
            {!readOnly && (
              <Button
                onClick={() => setIsAddPermissionOpen(true)}
                size="sm"
                className="gap-2"
              >
                <Plus className="h-4 w-4" />
                Grant Permission
              </Button>
            )}
          </div>

          {/* Permissions Table */}
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : !permissions || permissions.length === 0 ? (
            <div className="text-center py-8">
              <Shield className="mx-auto h-12 w-12 text-gray-400" />
              <h3 className="mt-4 text-lg font-semibold">No permissions set</h3>
              <p className="text-gray-500 mt-1">
                {readOnly
                  ? 'No custom permissions have been granted for this bucket.'
                  : 'Grant permissions to users to control access to this bucket.'}
              </p>
              {!readOnly && (
                <Button
                  onClick={() => setIsAddPermissionOpen(true)}
                  className="mt-4 gap-2"
                >
                  <Plus className="h-4 w-4" />
                  Grant First Permission
                </Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Target</TableHead>
                  <TableHead>Permission Level</TableHead>
                  <TableHead>Granted By</TableHead>
                  <TableHead>Granted At</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {permissions.map((permission) => (
                  <TableRow key={permission.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {permission.userId ? (
                          <>
                            <Users className="h-4 w-4 text-gray-500" />
                            <span className="font-medium">{getUserName(permission.userId)}</span>
                            <span className="text-xs text-gray-500">(User)</span>
                          </>
                        ) : permission.tenantId ? (
                          <>
                            <Building2 className="h-4 w-4 text-gray-500" />
                            <span className="font-medium">{getTenantName(permission.tenantId)}</span>
                            <span className="text-xs text-gray-500">(Tenant)</span>
                          </>
                        ) : (
                          <span className="text-xs text-gray-400">Unknown</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getPermissionLevelColor(permission.permissionLevel)}`}>
                        {permission.permissionLevel}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-600 dark:text-gray-400">{getUserName(permission.grantedBy)}</span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-gray-600 dark:text-gray-400">
                        <Calendar className="h-3 w-3" />
                        {formatDate(permission.grantedAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      {permission.expiresAt ? (
                        <div className="flex items-center gap-1 text-sm text-gray-600 dark:text-gray-400">
                          <AlertCircle className="h-3 w-3 text-yellow-500" />
                          {formatDate(permission.expiresAt)}
                        </div>
                      ) : (
                        <span className="text-sm text-gray-400 italic">Never</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {!readOnly && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRevokePermission(permission)}
                          disabled={revokePermissionMutation.isPending}
                          title="Revoke permission"
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          {/* Close Button */}
          <div className="flex justify-end pt-4 border-t">
            <Button variant="outline" onClick={onClose}>
              Close
            </Button>
          </div>
        </div>
      </Modal>

      {/* Grant Permission Modal */}
      <Modal
        isOpen={isAddPermissionOpen}
        onClose={() => {
          setIsAddPermissionOpen(false);
          setNewPermission({ permissionLevel: 'read', grantedBy: 'admin' });
        }}
        title="Grant Bucket Permission"
      >
        <form onSubmit={handleGrantPermission} className="space-y-4">

          {/* Scope notice */}
          <div className={`flex items-start gap-2 p-3 rounded-lg text-sm ${
            isGlobalBucket
              ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-800 dark:text-blue-300'
              : 'bg-purple-50 dark:bg-purple-900/20 text-purple-800 dark:text-purple-300'
          }`}>
            <Shield className="h-4 w-4 mt-0.5 flex-shrink-0" />
            <span>
              <strong>{isGlobalBucket ? 'Global bucket:' : 'Tenant bucket:'}</strong>{' '}
              {scopeLabel}. Cross-scope grants are not allowed.
            </span>
          </div>

          {/* User selector */}
          <div>
            <label className="block text-sm font-medium mb-2">User</label>
            <select
              value={newPermission.userId || ''}
              onChange={(e) => setNewPermission({ ...newPermission, userId: e.target.value })}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
              required
            >
              <option value="">Select a user</option>
              {selectableUsers.map((user) => (
                <option key={user.id} value={user.id}>
                  {user.username}{user.email ? ` (${user.email})` : ''}
                </option>
              ))}
            </select>
            {selectableUsers.length === 0 && (
              <p className="text-xs text-amber-600 dark:text-amber-400 mt-1">
                No eligible users found for this bucket's scope.
              </p>
            )}
          </div>

          {/* Permission Level */}
          <div>
            <label className="block text-sm font-medium mb-2">Permission Level</label>
            <select
              value={newPermission.permissionLevel}
              onChange={(e) =>
                setNewPermission({ ...newPermission, permissionLevel: e.target.value as 'read' | 'write' | 'admin' })
              }
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="read">Read — View and download objects</option>
              <option value="write">Write — Upload, modify, and delete objects</option>
              <option value="admin">Admin — Full control including permissions</option>
            </select>
          </div>

          {/* Expiration */}
          <div>
            <label className="block text-sm font-medium mb-2">Expiration (Optional)</label>
            <Input
              type="datetime-local"
              value={
                newPermission.expiresAt
                  ? new Date(newPermission.expiresAt * 1000).toISOString().slice(0, 16)
                  : ''
              }
              onChange={(e) =>
                setNewPermission({
                  ...newPermission,
                  expiresAt: e.target.value
                    ? Math.floor(new Date(e.target.value).getTime() / 1000)
                    : undefined,
                })
              }
            />
            <p className="text-xs text-gray-500 mt-1">Leave empty for permanent access</p>
          </div>

          <div className="flex justify-end gap-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setIsAddPermissionOpen(false);
                setNewPermission({ permissionLevel: 'read', grantedBy: 'admin' });
              }}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={grantPermissionMutation.isPending || selectableUsers.length === 0}>
              {grantPermissionMutation.isPending ? 'Granting...' : 'Grant Permission'}
            </Button>
          </div>
        </form>
      </Modal>
    </>
  );
}
