import React, { useState, useEffect } from 'react';
import { useRouter } from 'next/router';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import SweetAlert from '@/lib/sweetalert';
import {
  ArrowLeft,
  User as UserIcon,
  Mail,
  Shield,
  Settings,
  Edit,
  CheckCircle,
  XCircle,
  Key,
  Plus,
  Trash2,
  Eye,
  EyeOff,
  Copy,
  Download,
  Calendar
} from 'lucide-react';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User as UserType, AccessKey, EditUserForm } from '@/types';

export default function UserDetailsPage() {
  const router = useRouter();
  const { user } = router.query;
  const userId = user as string;
  const [isEditUserModalOpen, setIsEditUserModalOpen] = useState(false);
  const [isCreateKeyModalOpen, setIsCreateKeyModalOpen] = useState(false);
  const [editForm, setEditForm] = useState<EditUserForm>({
    email: '',
    roles: [],
    status: 'active',
    tenantId: undefined,
  });
  const [newKeyName, setNewKeyName] = useState('');
  const [showSecretKeys, setShowSecretKeys] = useState<Record<string, boolean>>({});
  const [createdKey, setCreatedKey] = useState<AccessKey | null>(null);
  const [isChangePasswordOpen, setIsChangePasswordOpen] = useState(false);
  const [passwordForm, setPasswordForm] = useState({
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
  });
  const queryClient = useQueryClient();

  // Fetch user data
  const { data: userData, isLoading: userLoading } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => APIClient.getUser(userId),
  });

  // Fetch access keys
  const { data: accessKeys, isLoading: keysLoading } = useQuery({
    queryKey: ['accessKeys', userId],
    queryFn: () => APIClient.getUserAccessKeys(userId),
  });

  // Fetch tenants for assignment
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  // Update user mutation
  const updateUserMutation = useMutation({
    mutationFn: (data: EditUserForm) => APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', userId] });
      setIsEditUserModalOpen(false);
      SweetAlert.toast('success', 'User updated successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  // Create access key mutation
  const createAccessKeyMutation = useMutation({
    mutationFn: () => APIClient.createAccessKey({ userId }),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['accessKeys', userId] });
      // Transform backend response to match expected format
      const transformedKey: AccessKey = {
        id: response.id || response.access_key_id || response.accessKey,
        accessKey: response.id || response.access_key_id || response.accessKey,
        secretKey: response.secret || response.secret_access_key || response.secretKey,
        userId: response.userId || response.user_id,
        status: response.status || 'active',
        permissions: [],
        createdAt: response.createdAt || response.created_at || Date.now() / 1000,
      };
      setCreatedKey(transformedKey);
      setIsCreateKeyModalOpen(false);
      setNewKeyName('');
      SweetAlert.toast('success', 'Access key created successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  // Delete access key mutation
  const deleteAccessKeyMutation = useMutation({
    mutationFn: (keyId: string) => APIClient.deleteAccessKey(userId, keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accessKeys', userId] });
      SweetAlert.toast('success', 'Access key deleted successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  // Change password mutation
  const changePasswordMutation = useMutation({
    mutationFn: (data: { currentPassword: string; newPassword: string }) =>
      APIClient.changePassword(userId, data.currentPassword, data.newPassword),
    onSuccess: () => {
      setIsChangePasswordOpen(false);
      setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
      SweetAlert.toast('success', 'Password changed successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  // Initialize edit form when user data is loaded
  useEffect(() => {
    if (userData) {
      setEditForm({
        email: userData.email || '',
        roles: userData.roles || [],
        status: userData.status,
        tenantId: userData.tenantId || undefined,
      });
    }
  }, [userData]);

  // Handlers
  const handleEditUser = (e: React.FormEvent) => {
    e.preventDefault();
    updateUserMutation.mutate(editForm);
  };

  const handleCreateAccessKey = (e: React.FormEvent) => {
    e.preventDefault();
    createAccessKeyMutation.mutate();
  };

  const handleDeleteAccessKey = async (keyId: string, keyDescription: string) => {
    try {
      const result = await SweetAlert.fire({
        icon: 'warning',
        title: 'Delete access key?',
        html: `<p>You are about to delete the access key <strong>"${keyDescription}"</strong></p>
               <p class="text-red-600 mt-2">This action cannot be undone</p>`,
        showCancelButton: true,
        confirmButtonText: 'Yes, delete',
        cancelButtonText: 'Cancel',
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        SweetAlert.loading('Deleting access key...', `Deleting "${keyDescription}"`);
        deleteAccessKeyMutation.mutate(keyId);
      }
    } catch (error) {
      SweetAlert.apiError(error);
    }
  };

  const toggleSecretVisibility = (keyId: string) => {
    setShowSecretKeys(prev => ({
      ...prev,
      [keyId]: !prev[keyId]
    }));
  };

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      SweetAlert.toast('success', 'Copied to clipboard');
    } catch (err) {
      SweetAlert.toast('error', 'Error copying');
    }
  };

  const downloadAsCSV = (key: AccessKey) => {
    const csvContent = `Access Key ID,Secret Access Key,Status,Created At\n${key.accessKey},${key.secretKey || 'N/A'},${key.status},${formatDate(key.createdAt)}`;
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `access-key-${key.accessKey}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    SweetAlert.toast('success', 'CSV downloaded successfully');
  };

  const handleChangePassword = () => {
    if (!passwordForm.currentPassword || !passwordForm.newPassword || !passwordForm.confirmPassword) {
      SweetAlert.toast('error', 'All password fields are required');
      return;
    }

    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      SweetAlert.toast('error', 'New passwords do not match');
      return;
    }

    if (passwordForm.newPassword.length < 6) {
      SweetAlert.toast('error', 'Password must be at least 6 characters');
      return;
    }

    changePasswordMutation.mutate({
      currentPassword: passwordForm.currentPassword,
      newPassword: passwordForm.newPassword,
    });
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-100 text-green-800';
      case 'inactive':
        return 'bg-gray-100 text-gray-800';
      case 'suspended':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  const formatDate = (date: string | number) => {
    // If it's a number (Unix timestamp in seconds), convert to milliseconds
    const timestamp = typeof date === 'number' ? date * 1000 : new Date(date).getTime();
    return new Date(timestamp).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  if (userLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (!userData) {
    return (
      <div className="text-center py-8">
        <h3 className="text-lg font-semibold">User not found</h3>
        <p className="text-muted-foreground">The requested user does not exist.</p>
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
            <h1 className="text-3xl font-bold tracking-tight">{userData.username}</h1>
            <p className="text-muted-foreground">
              User details and configuration
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={() => setIsChangePasswordOpen(true)}
            variant="outline"
            className="gap-2"
          >
            <Key className="h-4 w-4" />
            Change Password
          </Button>
          <Button
            onClick={() => setIsEditUserModalOpen(true)}
            variant="outline"
            className="gap-2"
          >
            <Edit className="h-4 w-4" />
            Edit User
          </Button>
          <Button
            onClick={() => setIsCreateKeyModalOpen(true)}
            className="gap-2"
          >
            <Plus className="h-4 w-4" />
            New Access Key
          </Button>
        </div>
      </div>

      {/* User Info Cards */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Status Card */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Status</CardTitle>
            <UserIcon className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getStatusColor(userData.status)}`}>
                {userData.status === 'active' ? <CheckCircle className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                {userData.status}
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Email Card */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Email</CardTitle>
            <Mail className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-sm">{userData.email || 'Not provided'}</div>
          </CardContent>
        </Card>

        {/* Roles Card */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Roles</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-1">
              {userData.roles && userData.roles.length > 0 ? (
                userData.roles.map((role: string) => (
                  <span
                    key={role}
                    className="inline-flex items-center px-2 py-1 rounded-md text-xs font-medium bg-blue-100 text-blue-800"
                  >
                    {role}
                  </span>
                ))
              ) : (
                <span className="text-xs text-muted-foreground">No roles assigned</span>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Password Management - Modal */}
      <Modal
        isOpen={isChangePasswordOpen}
        onClose={() => {
          setIsChangePasswordOpen(false);
          setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
        }}
        title="Change Password"
      >
        <div className="space-y-4">
          <div>
            <label className="text-sm font-medium mb-2 block">Current Password</label>
            <Input
              type="password"
              placeholder="Enter current password"
              value={passwordForm.currentPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, currentPassword: e.target.value })}
            />
          </div>
          <div>
            <label className="text-sm font-medium mb-2 block">New Password</label>
            <Input
              type="password"
              placeholder="Enter new password (min 6 characters)"
              value={passwordForm.newPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, newPassword: e.target.value })}
            />
          </div>
          <div>
            <label className="text-sm font-medium mb-2 block">Confirm New Password</label>
            <Input
              type="password"
              placeholder="Confirm new password"
              value={passwordForm.confirmPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, confirmPassword: e.target.value })}
            />
          </div>
          <div className="flex gap-2 justify-end mt-6">
            <Button
              variant="outline"
              onClick={() => {
                setIsChangePasswordOpen(false);
                setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
              }}
            >
              Cancel
            </Button>
            <Button
              onClick={handleChangePassword}
              disabled={changePasswordMutation.isPending}
            >
              {changePasswordMutation.isPending ? 'Changing...' : 'Change Password'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Access Keys */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            Access Keys ({accessKeys?.length || 0})
          </CardTitle>
        </CardHeader>
        <CardContent>
          {keysLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : !accessKeys || accessKeys.length === 0 ? (
            <div className="text-center py-8">
              <Key className="mx-auto h-12 w-12 text-muted-foreground" />
              <h3 className="mt-4 text-lg font-semibold">No access keys</h3>
              <p className="text-muted-foreground">
                Create an access key to allow programmatic access
              </p>
              <Button
                onClick={() => setIsCreateKeyModalOpen(true)}
                className="mt-4 gap-2"
              >
                <Plus className="h-4 w-4" />
                Create Access Key
              </Button>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Access Key ID</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {accessKeys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Key className="h-4 w-4 text-muted-foreground" />
                        <code className="text-sm bg-gray-100 px-2 py-1 rounded">{key.id}</code>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => copyToClipboard(key.id)}
                          title="Copy Access Key"
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                        key.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'
                      }`}>
                        {key.status}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(key.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteAccessKey(key.id, key.id)}
                          title="Delete access key"
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

      {/* Edit User Modal */}
      <Modal
        isOpen={isEditUserModalOpen}
        onClose={() => setIsEditUserModalOpen(false)}
        title="Edit User"
      >
        <form onSubmit={handleEditUser} className="space-y-4">
          <div>
            <label htmlFor="email" className="block text-sm font-medium mb-2">
              Email
            </label>
            <Input
              id="email"
              type="email"
              value={editForm.email}
              onChange={(e) => setEditForm(prev => ({ ...prev, email: e.target.value }))}
              placeholder="user@example.com"
            />
          </div>

          <div>
            <label htmlFor="tenant" className="block text-sm font-medium mb-2">
              Tenant (Optional)
            </label>
            <select
              id="tenant"
              value={editForm.tenantId || ''}
              onChange={(e) => setEditForm(prev => ({ ...prev, tenantId: e.target.value || undefined }))}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="">No Tenant (Global User)</option>
              {tenants?.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.displayName} ({tenant.name})
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground mt-1">
              Global users can access all buckets. Tenant users are limited to their tenant's buckets.
            </p>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium mb-2">
              Status
            </label>
            <select
              id="status"
              value={editForm.status}
              onChange={(e) => setEditForm(prev => ({ ...prev, status: e.target.value as any }))}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="suspended">Suspended</option>
            </select>
          </div>

          <div>
            <label htmlFor="roles" className="block text-sm font-medium mb-2">
              Roles (comma separated)
            </label>
            <Input
              id="roles"
              value={editForm.roles.join(', ')}
              onChange={(e) => setEditForm(prev => ({
                ...prev,
                roles: e.target.value.split(',').map(r => r.trim()).filter(r => r)
              }))}
              placeholder="admin, user, guest"
            />
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsEditUserModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={updateUserMutation.isPending}
            >
              {updateUserMutation.isPending ? 'Saving...' : 'Save Changes'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Access Key Modal */}
      <Modal
        isOpen={isCreateKeyModalOpen}
        onClose={() => setIsCreateKeyModalOpen(false)}
        title="Create New Access Key"
      >
        <form onSubmit={handleCreateAccessKey} className="space-y-4">
          <div className="bg-blue-50 border border-blue-200 rounded-md p-3">
            <p className="text-sm text-blue-800">
              <strong>ℹ️ Information:</strong> An access key and secret key pair will be automatically generated for this user.
            </p>
          </div>

          <div className="bg-yellow-50 border border-yellow-200 rounded-md p-3">
            <p className="text-sm text-yellow-800">
              <strong>⚠️ Important:</strong> The secret key will only be displayed once after creation.
              Make sure to copy and store it in a safe place.
            </p>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateKeyModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createAccessKeyMutation.isPending}
            >
              {createAccessKeyMutation.isPending ? 'Creating...' : 'Create Access Key'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Created Key Modal */}
      {createdKey && (
        <Modal
          isOpen={!!createdKey}
          onClose={() => setCreatedKey(null)}
          title="Access Key Created"
        >
          <div className="space-y-4">
            <div className="bg-green-50 border border-green-200 rounded-md p-3">
              <p className="text-sm text-green-800">
                <strong>✅ Access Key created successfully!</strong>
              </p>
            </div>

            <div className="space-y-3">
              <div>
                <label className="block text-sm font-medium mb-1">Access Key ID:</label>
                <div className="flex items-center gap-2">
                  <code className="bg-gray-100 px-3 py-2 rounded text-sm flex-1">
                    {createdKey.accessKey}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => copyToClipboard(createdKey.accessKey)}
                    title="Copy to clipboard"
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              {createdKey.secretKey && (
                <div>
                  <label className="block text-sm font-medium mb-1">Secret Access Key:</label>
                  <div className="flex items-center gap-2">
                    <code className="bg-gray-100 px-3 py-2 rounded text-sm flex-1">
                      {createdKey.secretKey}
                    </code>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => copyToClipboard(createdKey.secretKey!)}
                      title="Copy to clipboard"
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              )}
            </div>

            <div className="bg-red-50 border border-red-200 rounded-md p-3">
              <p className="text-sm text-red-800">
                <strong>⚠️ Important:</strong> This is the only time the secret key will be displayed.
                Copy and store it in a safe place before closing this window.
              </p>
            </div>

            <div className="flex justify-between items-center">
              <Button
                variant="outline"
                onClick={() => downloadAsCSV(createdKey)}
                className="gap-2"
              >
                <Download className="h-4 w-4" />
                Download CSV
              </Button>
              <Button onClick={() => setCreatedKey(null)}>
                Got it
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
