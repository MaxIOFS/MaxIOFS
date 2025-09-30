'use client';

import React, { useState } from 'react';
import { useParams } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { DataTable, DataTableColumn } from '@/components/ui/DataTable';
import {
  ArrowLeft,
  Key,
  Plus,
  Copy,
  Trash2,
  Eye,
  EyeOff,
  Shield,
  Calendar,
  CheckCircle,
  XCircle,
  Settings,
  User,
  Mail,
  UserCheck,
  UserX
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User as UserType, AccessKey, CreateAccessKeyRequest } from '@/types';

export default function UserDetailsPage() {
  const params = useParams();
  const userId = params.user as string;
  const [isCreateKeyModalOpen, setIsCreateKeyModalOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [showSecretKeys, setShowSecretKeys] = useState<Record<string, boolean>>({});
  const [createdKey, setCreatedKey] = useState<AccessKey | null>(null);
  const queryClient = useQueryClient();

  const { data: user, isLoading: userLoading } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => APIClient.getUser(userId),
  });

  const { data: accessKeys, isLoading: keysLoading } = useQuery({
    queryKey: ['accessKeys', userId],
    queryFn: () => APIClient.getUserAccessKeys(userId),
  });

  const createAccessKeyMutation = useMutation({
    mutationFn: (data: CreateAccessKeyRequest) => APIClient.createAccessKey(data),
    onSuccess: (response) => {
      queryClient.invalidateQueries({ queryKey: ['accessKeys', userId] });
      setCreatedKey(response.data);
      setIsCreateKeyModalOpen(false);
      setNewKeyName('');
    },
  });

  const deleteAccessKeyMutation = useMutation({
    mutationFn: (keyId: string) => APIClient.deleteAccessKey(keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['accessKeys', userId] });
    },
  });

  const updateUserMutation = useMutation({
    mutationFn: (data: Partial<UserType>) => APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', userId] });
    },
  });

  const handleCreateAccessKey = (e: React.FormEvent) => {
    e.preventDefault();
    if (newKeyName.trim()) {
      createAccessKeyMutation.mutate({
        userId,
        name: newKeyName.trim(),
      });
    }
  };

  const handleDeleteAccessKey = (keyId: string, keyName: string) => {
    if (confirm(`Are you sure you want to delete access key "${keyName}"? This action cannot be undone.`)) {
      deleteAccessKeyMutation.mutate(keyId);
    }
  };

  const handleToggleUserStatus = () => {
    if (!user?.data) return;
    const newStatus = user.data.status === 'active' ? 'inactive' : 'active';
    updateUserMutation.mutate({ status: newStatus as 'active' | 'inactive' | 'suspended' });
  };

  const toggleSecretVisibility = (keyId: string) => {
    setShowSecretKeys(prev => ({
      ...prev,
      [keyId]: !prev[keyId]
    }));
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
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

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active': return 'bg-green-100 text-green-800';
      case 'inactive': return 'bg-gray-100 text-gray-800';
      case 'suspended': return 'bg-red-100 text-red-800';
      default: return 'bg-gray-100 text-gray-800';
    }
  };

  const getRoleColor = (role: string) => {
    switch (role) {
      case 'admin': return 'bg-red-100 text-red-800';
      case 'write': return 'bg-blue-100 text-blue-800';
      case 'read': return 'bg-green-100 text-green-800';
      default: return 'bg-gray-100 text-gray-800';
    }
  };

  const accessKeyColumns: DataTableColumn<AccessKey>[] = [
    {
      key: 'name',
      header: 'Name',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-2">
          <Key className="h-4 w-4 text-muted-foreground" />
          <span className="font-medium">{key.name}</span>
        </div>
      ),
    },
    {
      key: 'accessKey',
      header: 'Access Key',
      render: (key) => (
        <div className="flex items-center gap-2">
          <code className="px-2 py-1 bg-muted rounded text-sm font-mono">
            {key.accessKey}
          </code>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => copyToClipboard(key.accessKey)}
            className="h-6 w-6 p-0"
          >
            <Copy className="h-3 w-3" />
          </Button>
        </div>
      ),
    },
    {
      key: 'secretKey',
      header: 'Secret Key',
      render: (key) => (
        <div className="flex items-center gap-2">
          <code className="px-2 py-1 bg-muted rounded text-sm font-mono">
            {showSecretKeys[key.id] ? key.secretKey : '••••••••••••••••'}
          </code>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => toggleSecretVisibility(key.id)}
            className="h-6 w-6 p-0"
          >
            {showSecretKeys[key.id] ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
          </Button>
          {showSecretKeys[key.id] && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => copyToClipboard(key.secretKey)}
              className="h-6 w-6 p-0"
            >
              <Copy className="h-3 w-3" />
            </Button>
          )}
        </div>
      ),
    },
    {
      key: 'status',
      header: 'Status',
      sortable: true,
      render: (key) => (
        <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getStatusColor(key.status)}`}>
          {key.status === 'active' ? <CheckCircle className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
          {key.status}
        </span>
      ),
    },
    {
      key: 'createdAt',
      header: 'Created',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-1 text-sm text-muted-foreground">
          <Calendar className="h-3 w-3" />
          {formatDate(key.createdAt)}
        </div>
      ),
    },
  ];

  if (userLoading) {
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
            <h1 className="text-3xl font-bold tracking-tight">{user.data.username}</h1>
            <p className="text-muted-foreground">
              User details and access key management
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={handleToggleUserStatus}
            variant={user.data.status === 'active' ? 'outline' : 'default'}
            disabled={updateUserMutation.isPending}
            className="gap-2"
          >
            {user.data.status === 'active' ? (
              <>
                <UserX className="h-4 w-4" />
                Deactivate
              </>
            ) : (
              <>
                <UserCheck className="h-4 w-4" />
                Activate
              </>
            )}
          </Button>
          <Button
            variant="outline"
            onClick={() => window.location.href = `/users/${userId}/settings`}
            className="gap-2"
          >
            <Settings className="h-4 w-4" />
            Settings
          </Button>
        </div>
      </div>

      {/* User Info */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">User Status</CardTitle>
            <User className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getStatusColor(user.data.status)}`}>
              {user.data.status === 'active' ? <CheckCircle className="h-3 w-3 mr-1" /> : <XCircle className="h-3 w-3 mr-1" />}
              {user.data.status}
            </span>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Email</CardTitle>
            <Mail className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-sm">{user.data.email || 'Not provided'}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Roles</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="flex gap-1">
              {user.data.roles.map((role) => (
                <span
                  key={role}
                  className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${getRoleColor(role)}`}
                >
                  {role}
                </span>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Access Keys</CardTitle>
            <Key className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {accessKeys?.data?.length || 0}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Access Keys Management */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle>Access Keys</CardTitle>
          <Button onClick={() => setIsCreateKeyModalOpen(true)} className="gap-2">
            <Plus className="h-4 w-4" />
            Create Access Key
          </Button>
        </CardHeader>
        <CardContent>
          <DataTable
            data={accessKeys?.data || []}
            columns={accessKeyColumns}
            isLoading={keysLoading}
            emptyMessage="No access keys found"
            emptyIcon={<Key className="h-12 w-12 text-muted-foreground" />}
            emptyAction={
              <Button
                onClick={() => setIsCreateKeyModalOpen(true)}
                className="gap-2"
              >
                <Plus className="h-4 w-4" />
                Create First Access Key
              </Button>
            }
            actions={(key) => (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleDeleteAccessKey(key.id, key.name)}
                disabled={deleteAccessKeyMutation.isPending}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            )}
          />
        </CardContent>
      </Card>

      {/* Create Access Key Modal */}
      <Modal
        isOpen={isCreateKeyModalOpen}
        onClose={() => setIsCreateKeyModalOpen(false)}
        title="Create Access Key"
      >
        <form onSubmit={handleCreateAccessKey} className="space-y-4">
          <div>
            <label htmlFor="keyName" className="block text-sm font-medium mb-2">
              Access Key Name
            </label>
            <Input
              id="keyName"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              placeholder="my-access-key"
              required
            />
            <p className="text-xs text-muted-foreground mt-1">
              A descriptive name for this access key
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
              disabled={createAccessKeyMutation.isPending || !newKeyName.trim()}
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
            <div className="rounded-md bg-green-50 p-4">
              <div className="text-sm text-green-700">
                <strong>Important:</strong> This is the only time the secret key will be displayed.
                Please copy and store it securely.
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-2">Access Key ID</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-muted rounded text-sm font-mono">
                  {createdKey.accessKey}
                </code>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => copyToClipboard(createdKey.accessKey)}
                >
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-2">Secret Access Key</label>
              <div className="flex items-center gap-2">
                <code className="flex-1 px-3 py-2 bg-muted rounded text-sm font-mono">
                  {createdKey.secretKey}
                </code>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => copyToClipboard(createdKey.secretKey)}
                >
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>

            <div className="flex justify-end pt-4">
              <Button onClick={() => setCreatedKey(null)}>
                Done
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}