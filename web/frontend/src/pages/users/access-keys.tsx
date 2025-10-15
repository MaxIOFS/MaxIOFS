import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { DataTable, DataTableColumn } from '@/components/ui/DataTable';
import {
  Key,
  Search,
  Trash2,
  Calendar,
  User,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AccessKey } from '@/types';
import SweetAlert from '@/lib/sweetalert';

export default function AccessKeysPage() {
  const navigate = useNavigate();
  const [searchTerm, setSearchTerm] = useState('');
  const queryClient = useQueryClient();

  const { data: accessKeys, isLoading } = useQuery({
    queryKey: ['accessKeys'],
    queryFn: () => APIClient.getAccessKeys(),
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  const deleteAccessKeyMutation = useMutation({
    mutationFn: ({ userId, keyId }: { userId: string; keyId: string }) =>
      APIClient.deleteAccessKey(userId, keyId),
    onSuccess: () => {
      SweetAlert.close();
      queryClient.invalidateQueries({ queryKey: ['accessKeys'] });
      SweetAlert.toast('success', 'Access key deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.close();
      SweetAlert.apiError(error);
    },
  });

  const filteredKeys = (accessKeys || []).filter((key: AccessKey) => {
    const user = users?.find((u: any) => u.id === key.userId);
    const username = user?.username || '';
    return (
      key.id.toLowerCase().includes(searchTerm.toLowerCase()) ||
      username.toLowerCase().includes(searchTerm.toLowerCase())
    );
  });

  const handleDeleteKey = async (key: AccessKey) => {
    const user = users?.find((u: any) => u.id === key.userId);

    try {
      const result = await SweetAlert.fire({
        icon: 'warning',
        title: 'Delete Access Key',
        html: `<p>Are you sure you want to delete access key <strong>"${key.id}"</strong> for user <strong>"${user?.username || 'unknown'}"</strong>?</p>
               <p class="text-red-600 mt-2">This action cannot be undone</p>`,
        showCancelButton: true,
        confirmButtonText: 'Yes, delete',
        cancelButtonText: 'Cancel',
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        SweetAlert.loading('Deleting access key...', `Deleting "${key.id}"`);
        deleteAccessKeyMutation.mutate({ userId: key.userId, keyId: key.id });
      }
    } catch (error) {
      SweetAlert.close();
      SweetAlert.apiError(error);
    }
  };

  const formatDate = (timestamp: number | string) => {
    const date = typeof timestamp === 'string' 
      ? new Date(timestamp)
      : new Date(timestamp * 1000);
    return date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getUserName = (userId: string) => {
    const user = users?.find((u: any) => u.id === userId);
    return user?.username || 'Unknown User';
  };

  const accessKeyColumns: DataTableColumn<AccessKey>[] = [
    {
      key: 'id',
      header: 'Access Key ID',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-2">
          <Key className="h-4 w-4 text-gray-400 dark:text-gray-500" />
          <code className="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{key.id}</code>
        </div>
      ),
    },
    {
      key: 'userId',
      header: 'User',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-2">
          <User className="h-4 w-4 text-gray-400 dark:text-gray-500" />
          <span>{getUserName(key.userId)}</span>
        </div>
      ),
    },
    {
      key: 'createdAt',
      header: 'Created',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
          <Calendar className="h-3 w-3" />
          {formatDate(key.createdAt)}
        </div>
      ),
    },
    {
      key: 'lastUsed',
      header: 'Last Used',
      sortable: true,
      render: (key) => (
        <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
          {key.lastUsed ? (
            <>
              <Calendar className="h-3 w-3" />
              {formatDate(key.lastUsed)}
            </>
          ) : (
            <span className="text-gray-400 dark:text-gray-500">Never</span>
          )}
        </div>
      ),
    },
  ];

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Access Keys</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage S3 API access keys for all users
          </p>
        </div>
      </div>

      {/* Search */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-4">
        <div className="relative max-w-md">
          <Search className="absolute left-4 top-1/2 transform -translate-y-1/2 text-gray-400 h-5 w-5" />
          <Input
            placeholder="Search by key ID or username..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-12 bg-gray-50 dark:bg-gray-900 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white focus:ring-brand-500 focus:border-brand-500"
          />
        </div>
      </div>

      {/* Access Keys Table */}
      <DataTable
        data={filteredKeys}
        columns={accessKeyColumns}
        isLoading={isLoading}
        title={`Access Keys (${filteredKeys.length})`}
        emptyMessage="No access keys found"
        emptyIcon={<Key className="h-12 w-12 text-gray-400 dark:text-gray-500" />}
        actions={(key) => (
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate(`/users/${key.userId}`)}
              title="View user details"
            >
              <User className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleDeleteKey(key)}
              disabled={deleteAccessKeyMutation.isPending}
              title="Delete access key"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        )}
      />
    </div>
  );
}
