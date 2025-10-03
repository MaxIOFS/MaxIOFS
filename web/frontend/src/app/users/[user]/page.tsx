'use client';

import React, { useState, useEffect } from 'react';
import { useParams } from 'next/navigation';
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
  Copy
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User as UserType, AccessKey, EditUserForm } from '@/types';

export default function UserDetailsPage() {
  const params = useParams();
  const userId = params.user as string;
  const [isEditUserModalOpen, setIsEditUserModalOpen] = useState(false);
  const [isCreateKeyModalOpen, setIsCreateKeyModalOpen] = useState(false);
  const [editForm, setEditForm] = useState<EditUserForm>({
    email: '',
    roles: [],
    status: 'active',
  });
  const [newKeyName, setNewKeyName] = useState('');
  const [showSecretKeys, setShowSecretKeys] = useState<Record<string, boolean>>({});
  const [createdKey, setCreatedKey] = useState<AccessKey | null>(null);
  const queryClient = useQueryClient();

  // Fetch user data
  const { data: user, isLoading: userLoading } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => APIClient.getUser(userId),
  });

  // Fetch access keys
  const { data: accessKeys, isLoading: keysLoading } = useQuery({
    queryKey: ['accessKeys', userId],
    queryFn: () => APIClient.getUserAccessKeys(userId),
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
        createdAt: response.createdAt || response.created_at || new Date().toISOString(),
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

  // Initialize edit form when user data is loaded
  useEffect(() => {
    if (user) {
      setEditForm({
        email: user.email || '',
        roles: user.roles || [],
        status: user.status,
      });
    }
  }, [user]);

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

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
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

  if (!user) {
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
            <h1 className="text-3xl font-bold tracking-tight">{user.username}</h1>
            <p className="text-muted-foreground">
              User details and configuration
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
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
              <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getStatusColor(user.status)}`}>
                {user.status === 'active' ? <CheckCircle className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
                {user.status}
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
            <div className="text-sm">{user.email || 'Not provided'}</div>
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
              {user.roles && user.roles.length > 0 ? (
                user.roles.map((role: string) => (
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
            <div className="space-y-4">
              {accessKeys.map((key) => (
                <div key={key.id} className="border rounded-lg p-4">
                  <div className="flex items-center justify-between mb-3">
                    <div>
                      <span className="font-medium">{key.id}</span>
                      <span className={`ml-2 inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
                        key.status === 'active' ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'
                      }`}>
                        {key.status}
                      </span>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleDeleteAccessKey(key.id, key.id)}
                      className="text-red-600 hover:text-red-800"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  
                  <div className="space-y-2 text-sm">
                    <div className="flex items-center justify-between">
                      <span className="text-muted-foreground">Access Key:</span>
                      <div className="flex items-center gap-2">
                        <code className="bg-gray-100 px-2 py-1 rounded text-xs">
                          {key.accessKey}
                        </code>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => copyToClipboard(key.accessKey)}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                    
                    {key.secretKey && (
                      <div className="flex items-center justify-between">
                        <span className="text-muted-foreground">Secret Key:</span>
                        <div className="flex items-center gap-2">
                          <code className="bg-gray-100 px-2 py-1 rounded text-xs">
                            {showSecretKeys[key.id] ? key.secretKey : '••••••••••••••••'}
                          </code>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => toggleSecretVisibility(key.id)}
                          >
                            {showSecretKeys[key.id] ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                          </Button>
                          {showSecretKeys[key.id] && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => copyToClipboard(key.secretKey!)}
                            >
                              <Copy className="h-3 w-3" />
                            </Button>
                          )}
                        </div>
                      </div>
                    )}
                    
                    <div className="flex items-center justify-between">
                      <span className="text-muted-foreground">Created:</span>
                      <span>{formatDate(key.createdAt)}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
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

            <div className="flex justify-end">
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