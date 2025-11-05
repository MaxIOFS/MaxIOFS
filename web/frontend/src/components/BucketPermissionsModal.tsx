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
import SweetAlert from '@/lib/sweetalert';

interface BucketPermissionsModalProps {
  isOpen: boolean;
  onClose: () => void;
  bucketName: string;
}

export function BucketPermissionsModal({
  isOpen,
  onClose,
  bucketName,
}: BucketPermissionsModalProps) {
  const [isAddPermissionOpen, setIsAddPermissionOpen] = useState(false);
  const [newPermission, setNewPermission] = useState<Partial<GrantPermissionRequest>>({
    permissionLevel: 'read',
    grantedBy: 'admin', // TODO: Get from authenticated user
  });
  const [targetType, setTargetType] = useState<'user' | 'tenant'>('user');
  const queryClient = useQueryClient();

  // Fetch bucket permissions
  const { data: permissions, isLoading } = useQuery({
    queryKey: ['bucketPermissions', bucketName],
    queryFn: () => APIClient.getBucketPermissions(bucketName),
    enabled: isOpen,
  });

  // Fetch users for dropdown and name resolution
  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: isOpen, // Load when modal opens, not just when adding
  });

  // Fetch tenants for dropdown and name resolution
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
    enabled: isOpen, // Load when modal opens, not just when adding
  });

  // Grant permission mutation
  const grantPermissionMutation = useMutation({
    mutationFn: (data: GrantPermissionRequest) =>
      APIClient.grantBucketPermission(bucketName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucketPermissions', bucketName] });
      setIsAddPermissionOpen(false);
      setNewPermission({ permissionLevel: 'read', grantedBy: 'admin' });
      SweetAlert.toast('success', 'Permission granted successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  // Revoke permission mutation
  const revokePermissionMutation = useMutation({
    mutationFn: ({ userId, tenantId }: { userId?: string; tenantId?: string }) =>
      APIClient.revokeBucketPermission(bucketName, userId, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucketPermissions', bucketName] });
      SweetAlert.toast('success', 'Permission revoked successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const handleGrantPermission = (e: React.FormEvent) => {
    e.preventDefault();

    if (targetType === 'user' && !newPermission.userId) {
      SweetAlert.toast('error', 'Please select a user');
      return;
    }

    if (targetType === 'tenant' && !newPermission.tenantId) {
      SweetAlert.toast('error', 'Please select a tenant');
      return;
    }

    const permission: GrantPermissionRequest = {
      userId: targetType === 'user' ? newPermission.userId : undefined,
      tenantId: targetType === 'tenant' ? newPermission.tenantId : undefined,
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

    const targetType = permission.userId ? 'user' : 'tenant';

    console.log('Revoking permission:', { userId: permission.userId, tenantId: permission.tenantId, permission });

    SweetAlert.confirm(
      'Revoke Permission?',
      `Are you sure you want to revoke ${permission.permissionLevel} access for ${targetType} "${targetName}"?`,
      () => revokePermissionMutation.mutate({
        userId: permission.userId || undefined,
        tenantId: permission.tenantId || undefined,
      })
    );
  };

  const getPermissionLevelColor = (level: string) => {
    switch (level) {
      case 'admin':
        return 'bg-red-100 text-red-800';
      case 'write':
        return 'bg-yellow-100 text-yellow-800';
      case 'read':
        return 'bg-blue-100 text-blue-800';
      default:
        return 'bg-gray-100 text-gray-800';
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
    return user ? `${user.username}` : userId;
  };

  const getTenantName = (tenantId: string) => {
    const tenant = tenants?.find(t => t.id === tenantId);
    return tenant ? tenant.displayName : tenantId;
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
          {/* Header with Add Button */}
          <div className="flex items-center justify-between">
            <div className="text-sm text-gray-600">
              Manage access permissions for this bucket
            </div>
            <Button
              onClick={() => setIsAddPermissionOpen(true)}
              size="sm"
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              Grant Permission
            </Button>
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
                Grant permissions to users or tenants to control access to this bucket.
              </p>
              <Button
                onClick={() => setIsAddPermissionOpen(true)}
                className="mt-4 gap-2"
              >
                <Plus className="h-4 w-4" />
                Grant First Permission
              </Button>
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
                      <span
                        className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getPermissionLevelColor(
                          permission.permissionLevel
                        )}`}
                      >
                        {permission.permissionLevel}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-600">{getUserName(permission.grantedBy)}</span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-gray-600">
                        <Calendar className="h-3 w-3" />
                        {formatDate(permission.grantedAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      {permission.expiresAt ? (
                        <div className="flex items-center gap-1 text-sm text-gray-600">
                          <AlertCircle className="h-3 w-3 text-yellow-500" />
                          {formatDate(permission.expiresAt)}
                        </div>
                      ) : (
                        <span className="text-sm text-gray-400 italic">Never</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleRevokePermission(permission)}
                        disabled={revokePermissionMutation.isPending}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
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

      {/* Add Permission Modal */}
      <Modal
        isOpen={isAddPermissionOpen}
        onClose={() => setIsAddPermissionOpen(false)}
        title="Grant Bucket Permission"
      >
        <form onSubmit={handleGrantPermission} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Target Type</label>
            <select
              value={targetType}
              onChange={(e) => setTargetType(e.target.value as 'user' | 'tenant')}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="user">User</option>
              <option value="tenant">Tenant</option>
            </select>
          </div>

          {targetType === 'user' ? (
            <div>
              <label className="block text-sm font-medium mb-2">User</label>
              <select
                value={newPermission.userId || ''}
                onChange={(e) =>
                  setNewPermission({ ...newPermission, userId: e.target.value })
                }
                className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
                required
              >
                <option value="">Select a user</option>
                {users?.map((user) => (
                  <option key={user.id} value={user.id}>
                    {user.username} ({user.email || 'no email'})
                  </option>
                ))}
              </select>
            </div>
          ) : (
            <div>
              <label className="block text-sm font-medium mb-2">Tenant</label>
              <select
                value={newPermission.tenantId || ''}
                onChange={(e) =>
                  setNewPermission({ ...newPermission, tenantId: e.target.value })
                }
                className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
                required
              >
                <option value="">Select a tenant</option>
                {tenants?.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName} ({tenant.name})
                  </option>
                ))}
              </select>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium mb-2">Permission Level</label>
            <select
              value={newPermission.permissionLevel}
              onChange={(e) =>
                setNewPermission({ ...newPermission, permissionLevel: e.target.value as 'read' | 'write' | 'admin' })
              }
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="read">Read - View and download objects</option>
              <option value="write">Write - Upload, modify, and delete objects</option>
              <option value="admin">Admin - Full control including permissions</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">
              Expiration (Optional)
            </label>
            <Input
              type="datetime-local"
              value={
                newPermission.expiresAt
                  ? new Date(newPermission.expiresAt * 1000)
                      .toISOString()
                      .slice(0, 16)
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
            <p className="text-xs text-gray-500 mt-1">
              Leave empty for permanent access
            </p>
          </div>

          <div className="flex justify-end gap-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsAddPermissionOpen(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={grantPermissionMutation.isPending}>
              {grantPermissionMutation.isPending ? 'Granting...' : 'Grant Permission'}
            </Button>
          </div>
        </form>
      </Modal>
    </>
  );
}
