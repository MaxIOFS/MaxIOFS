import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Loading } from '@/components/ui/Loading';
import { Database, Plus, Search, Settings, Trash2, Calendar, HardDrive, Lock, Shield, Building2, Users } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Bucket } from '@/types';
import SweetAlert from '@/lib/sweetalert';

export default function BucketsPage() {
  const navigate = useNavigate();
  const [searchTerm, setSearchTerm] = useState('');
  const queryClient = useQueryClient();

  const { data: buckets, isLoading, error } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  const deleteBucketMutation = useMutation({
    mutationFn: (bucketName: string) => APIClient.deleteBucket(bucketName),
    onSuccess: (response, bucketName) => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      SweetAlert.successBucketDeleted(bucketName);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const filteredBuckets = buckets?.filter(bucket =>
    bucket.name.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

  const handleDeleteBucket = async (bucketName: string) => {
    try {
      const result = await SweetAlert.confirmDeleteBucket(bucketName);
      if (result.isConfirmed) {
        SweetAlert.loading('Deleting bucket...', `Deleting "${bucketName}" and all its data`);
        deleteBucketMutation.mutate(bucketName);
      }
    } catch (error) {
      SweetAlert.close();
      SweetAlert.apiError(error);
    }
  };

  const formatSize = (bytes: number) => {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = bytes;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    return `${size.toFixed(1)} ${units[unitIndex]}`;
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

  const getOwnerDisplay = (bucket: Bucket) => {
    const ownerId = bucket.owner_id || bucket.ownerId;
    const ownerType = bucket.owner_type || bucket.ownerType;
    if (!ownerId || !ownerType) {
      return { type: 'global', name: 'Global', icon: Shield };
    }
    if (ownerType === 'user') {
      const user = users?.find(u => u.id === ownerId);
      return { type: 'user', name: user?.username || ownerId, icon: Users };
    }
    if (ownerType === 'tenant') {
      const tenant = tenants?.find(t => t.id === ownerId);
      return { type: 'tenant', name: tenant?.displayName || ownerId, icon: Building2 };
    }
    return { type: 'unknown', name: 'Unknown', icon: Shield };
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
      <div className="rounded-lg bg-error-50 dark:bg-error-900/30 border border-error-200 dark:border-error-800 p-4">
        <div className="text-sm text-error-700 dark:text-error-400 font-medium">
          Error loading buckets: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Buckets</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage your S3 buckets and their configurations
          </p>
        </div>
        <Button 
          onClick={() => navigate('/buckets/create')} 
          className="bg-brand-600 hover:bg-brand-700 text-white inline-flex items-center gap-2"
        >
          <Plus className="h-4 w-4" />
          Create Bucket
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400">Total Buckets</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white mt-2">{filteredBuckets.length}</h3>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
              <Database className="h-7 w-7 text-brand-600 dark:text-brand-400" />
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400">Total Objects</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white mt-2">
                {filteredBuckets.reduce((sum, bucket) => sum + (bucket.object_count || bucket.objectCount || 0), 0).toLocaleString()}
              </h3>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-light-50 dark:bg-blue-light-900/30">
              <HardDrive className="h-7 w-7 text-blue-light-600 dark:text-blue-light-400" />
            </div>
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400">Total Size</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white mt-2">
                {formatSize(filteredBuckets.reduce((sum, bucket) => sum + (bucket.size || bucket.totalSize || 0), 0))}
              </h3>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-orange-50 dark:bg-orange-900/30">
              <HardDrive className="h-7 w-7 text-orange-600 dark:text-orange-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Search Bar */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-4">
        <div className="relative max-w-md">
          <Search className="absolute left-4 top-1/2 transform -translate-y-1/2 text-gray-400 h-5 w-5" />
          <Input
            placeholder="Search buckets..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-12 bg-gray-50 dark:bg-gray-900 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white focus:ring-brand-500 focus:border-brand-500"
          />
        </div>
      </div>

      {/* Buckets Table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
            All Buckets ({filteredBuckets.length})
          </h3>
        </div>

        <div className="overflow-x-auto">
          {filteredBuckets.length === 0 ? (
            <div className="text-center py-12 px-4">
              <div className="flex items-center justify-center w-16 h-16 mx-auto rounded-full bg-gray-100 dark:bg-gray-700 mb-4">
                <Database className="h-8 w-8 text-gray-400 dark:text-gray-500" />
              </div>
              <h3 className="text-base font-medium text-gray-900 dark:text-white mb-1">No buckets found</h3>
              <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
                {searchTerm ? 'Try adjusting your search terms' : 'Get started by creating your first bucket'}
              </p>
              {!searchTerm && (
                <Button
                  onClick={() => navigate('/buckets/create')}
                  className="bg-brand-600 hover:bg-brand-700 text-white inline-flex items-center gap-2"
                >
                  <Plus className="h-4 w-4" />
                  Create Bucket
                </Button>
              )}
            </div>
          ) : (
            <table className="w-full">
              <thead className="bg-gray-50 dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Name
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Region
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Owner
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Objects
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Size
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Created
                  </th>
                  <th className="px-6 py-3 text-right text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                {filteredBuckets.map((bucket) => {
                  const tenantId = bucket.tenant_id || bucket.tenantId;
                  const bucketPath = tenantId ? `/buckets/${tenantId}/${bucket.name}` : `/buckets/${bucket.name}`;
                  const owner = getOwnerDisplay(bucket);
                  const OwnerIcon = owner.icon;

                  return (
                    <tr key={`${tenantId || 'global'}-${bucket.name}`} className="hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-2">
                          <div className="flex items-center justify-center w-8 h-8 rounded bg-brand-50 dark:bg-brand-900/30">
                            <Database className="h-4 w-4 text-brand-600 dark:text-brand-400" />
                          </div>
                          <div>
                            <Link
                              to={bucketPath}
                              className="text-sm font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300"
                            >
                              {bucket.name}
                            </Link>
                            {bucket.objectLock?.objectLockEnabled && (
                              <div className="flex items-center gap-1 mt-1">
                                <span className="inline-flex items-center gap-1 bg-blue-light-100 dark:bg-blue-light-900/30 text-blue-light-700 dark:text-blue-light-400 px-2 py-0.5 rounded text-xs font-medium">
                                  <Lock className="h-3 w-3" />
                                  WORM
                                </span>
                              </div>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className="text-sm text-gray-900 dark:text-gray-300">{bucket.region || 'us-east-1'}</span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-2">
                          <OwnerIcon className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                          <span className={owner.type === 'global' ? 'text-sm text-gray-500 dark:text-gray-400 italic' : 'text-sm text-gray-900 dark:text-gray-300'}>
                            {owner.name}
                          </span>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className="text-sm text-gray-900 dark:text-gray-300">
                          {(bucket.object_count || bucket.objectCount || 0).toLocaleString()}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className="text-sm text-gray-900 dark:text-gray-300">
                          {formatSize(bucket.size || bucket.totalSize || 0)}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                          <Calendar className="h-3 w-3" />
                          {formatDate(bucket.creation_date || bucket.creationDate || '')}
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => navigate(`${bucketPath}/settings`)}
                            className="p-2 text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
                            title="Settings"
                          >
                            <Settings className="h-4 w-4" />
                          </button>
                          <button
                            onClick={() => handleDeleteBucket(bucket.name)}
                            disabled={deleteBucketMutation.isPending}
                            className="p-2 text-gray-600 dark:text-gray-400 hover:text-error-600 dark:hover:text-error-400 hover:bg-error-50 dark:hover:bg-error-900/30 rounded-lg transition-colors disabled:opacity-50"
                            title="Delete"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}
