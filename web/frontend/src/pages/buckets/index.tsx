import React, { useState } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
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

  // Fetch users and tenants for ownership display
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
        // Show loading indicator
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
      <div className="rounded-md bg-red-50 p-4">
        <div className="text-sm text-red-700">
          Error loading buckets: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Buckets</h1>
          <p className="text-muted-foreground">
            Manage your S3 buckets and their configurations
          </p>
        </div>
        <Button onClick={() => navigate('/buckets/create')} className="gap-2">
          <Plus className="h-4 w-4" />
          Create Bucket
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Buckets</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{filteredBuckets.length}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {filteredBuckets.reduce((sum, bucket) => sum + (bucket.object_count || bucket.objectCount || 0), 0).toLocaleString()}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Size</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {formatSize(filteredBuckets.reduce((sum, bucket) => sum + (bucket.size || bucket.totalSize || 0), 0))}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Search and Filters */}
      <div className="flex items-center space-x-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
          <Input
            placeholder="Search buckets..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10"
          />
        </div>
      </div>

      {/* Buckets Table */}
      <Card>
        <CardHeader>
          <CardTitle>Buckets ({filteredBuckets.length})</CardTitle>
        </CardHeader>
        <CardContent>
          {filteredBuckets.length === 0 ? (
            <div className="text-center py-8">
              <Database className="mx-auto h-12 w-12 text-muted-foreground" />
              <h3 className="mt-4 text-lg font-semibold">No buckets found</h3>
              <p className="text-muted-foreground">
                {searchTerm ? 'Try adjusting your search terms' : 'Get started by creating your first bucket'}
              </p>
              {!searchTerm && (
                <Button
                  onClick={() => navigate('/buckets/create')}
                  className="mt-4 gap-2"
                >
                  <Plus className="h-4 w-4" />
                  Create Bucket
                </Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Region</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>Objects</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredBuckets.map((bucket) => (
                  <TableRow key={bucket.name}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <Database className="h-4 w-4 text-muted-foreground" />
                        <Link
                          to={`/buckets/${bucket.name}`}
                          className="hover:underline text-blue-600"
                        >
                          {bucket.name}
                        </Link>
                        {bucket.objectLock?.objectLockEnabled && (
                          <div className="flex items-center gap-1 bg-blue-100 text-blue-700 px-2 py-0.5 rounded text-xs font-medium" title="Object Lock enabled">
                            <Lock className="h-3 w-3" />
                            <span>WORM</span>
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>{bucket.region || 'us-east-1'}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {(() => {
                          const owner = getOwnerDisplay(bucket);
                          const Icon = owner.icon;
                          return (
                            <>
                              <Icon className="h-4 w-4 text-muted-foreground" />
                              <span className={owner.type === 'global' ? 'text-sm text-muted-foreground italic' : 'text-sm'}>
                                {owner.name}
                              </span>
                            </>
                          );
                        })()}
                      </div>
                    </TableCell>
                    <TableCell>{(bucket.object_count || bucket.objectCount || 0).toLocaleString()}</TableCell>
                    <TableCell>{formatSize(bucket.size || bucket.totalSize || 0)}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(bucket.creation_date || bucket.creationDate || '')}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => navigate(`/buckets/${bucket.name}/settings`)}
                        >
                          <Settings className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteBucket(bucket.name)}
                          disabled={deleteBucketMutation.isPending}
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
    </div>
  );
}
