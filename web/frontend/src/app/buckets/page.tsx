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
import { Database, Plus, Search, Settings, Trash2, Calendar, HardDrive, Lock, Shield } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Bucket, CreateBucketForm } from '@/types';
import SweetAlert from '@/lib/sweetalert';

export default function BucketsPage() {
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [formData, setFormData] = useState<CreateBucketForm>({
    name: '',
    region: 'us-east-1',
    versioning: false,
    objectLock: false,
    encryption: {
      enabled: false,
      algorithm: 'AES256',
      keySource: 'server',
    },
  });
  const queryClient = useQueryClient();

  const { data: buckets, isLoading, error } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const createBucketMutation = useMutation({
    mutationFn: (data: CreateBucketForm) => APIClient.createBucket(data),
    onSuccess: (response, variables) => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      setIsCreateModalOpen(false);
      setFormData({
        name: '',
        region: 'us-east-1',
        versioning: false,
        objectLock: false,
        encryption: {
          enabled: false,
          algorithm: 'AES256',
          keySource: 'server',
        },
      });
      // Mostrar notificación de éxito
      SweetAlert.successBucketCreated(variables.name);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
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

  const handleCreateBucket = (e: React.FormEvent) => {
    e.preventDefault();
    if (formData.name.trim()) {
      createBucketMutation.mutate(formData);
    }
  };

  const handleDeleteBucket = async (bucketName: string) => {
    try {
      const result = await SweetAlert.confirmDeleteBucket(bucketName);
      
      if (result.isConfirmed) {
        // Mostrar indicador de carga
        SweetAlert.loading('Eliminando bucket...', `Eliminando "${bucketName}" y todos sus datos`);
        
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
        <Button onClick={() => setIsCreateModalOpen(true)} className="gap-2">
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
                  onClick={() => setIsCreateModalOpen(true)}
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
                        <a
                          href={`/buckets/${bucket.name}`}
                          className="hover:underline text-blue-600"
                        >
                          {bucket.name}
                        </a>
                      </div>
                    </TableCell>
                    <TableCell>{bucket.region || 'us-east-1'}</TableCell>
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
                          onClick={() => window.location.href = `/buckets/${bucket.name}/settings`}
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

      {/* Create Bucket Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create New Bucket"
      >
        <form onSubmit={handleCreateBucket} className="space-y-4">
          {/* Bucket Name */}
          <div>
            <label htmlFor="bucketName" className="block text-sm font-medium mb-2">
              Bucket Name *
            </label>
            <Input
              id="bucketName"
              value={formData.name}
              onChange={(e) => setFormData({ ...formData, name: e.target.value })}
              placeholder="my-bucket-name"
              required
              pattern="^[a-z0-9][a-z0-9\-]{1,61}[a-z0-9]$"
              title="Bucket name must be 3-63 characters, lowercase letters, numbers, and hyphens only"
            />
            <p className="text-xs text-muted-foreground mt-1">
              3-63 characters, lowercase letters, numbers, and hyphens only
            </p>
          </div>

          {/* Region */}
          <div>
            <label htmlFor="bucketRegion" className="block text-sm font-medium mb-2">
              Region
            </label>
            <select
              id="bucketRegion"
              value={formData.region}
              onChange={(e) => setFormData({ ...formData, region: e.target.value })}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="us-east-1">US East (N. Virginia)</option>
              <option value="us-west-2">US West (Oregon)</option>
              <option value="eu-west-1">Europe (Ireland)</option>
              <option value="eu-central-1">Europe (Frankfurt)</option>
              <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
            </select>
          </div>

          {/* Object Lock */}
          <div className="border border-yellow-200 bg-yellow-50 rounded-md p-4">
            <div className="flex items-start space-x-3">
              <input
                type="checkbox"
                id="objectLock"
                checked={formData.objectLock}
                onChange={(e) => {
                  const objectLock = e.target.checked;
                  setFormData({
                    ...formData,
                    objectLock,
                    // Object Lock requires versioning
                    versioning: objectLock ? true : formData.versioning,
                  });
                }}
                className="mt-1"
              />
              <div className="flex-1">
                <label htmlFor="objectLock" className="text-sm font-medium flex items-center gap-2 cursor-pointer">
                  <Lock className="h-4 w-4" />
                  Enable Object Lock
                </label>
                <p className="text-xs text-muted-foreground mt-1">
                  Permanently enable object lock to prevent objects from being deleted or overwritten.
                  <strong className="text-yellow-700"> Cannot be disabled after bucket creation!</strong>
                  <br />
                  Object Lock requires versioning to be enabled.
                </p>
              </div>
            </div>
          </div>

          {/* Versioning */}
          <div className="border border-gray-200 rounded-md p-4">
            <div className="flex items-start space-x-3">
              <input
                type="checkbox"
                id="versioning"
                checked={formData.versioning || formData.objectLock}
                onChange={(e) => setFormData({ ...formData, versioning: e.target.checked })}
                disabled={formData.objectLock}
                className="mt-1"
              />
              <div className="flex-1">
                <label htmlFor="versioning" className="text-sm font-medium flex items-center gap-2 cursor-pointer">
                  <Shield className="h-4 w-4" />
                  Enable Versioning
                </label>
                <p className="text-xs text-muted-foreground mt-1">
                  Keep multiple versions of objects in the same bucket. Can be enabled/disabled later.
                  {formData.objectLock && (
                    <span className="text-blue-600 font-medium"> (Required for Object Lock)</span>
                  )}
                </p>
              </div>
            </div>
          </div>

          {/* Encryption */}
          <div className="border border-gray-200 rounded-md p-4">
            <div className="flex items-start space-x-3">
              <input
                type="checkbox"
                id="encryption"
                checked={formData.encryption?.enabled}
                onChange={(e) => setFormData({
                  ...formData,
                  encryption: { ...formData.encryption!, enabled: e.target.checked }
                })}
                className="mt-1"
              />
              <div className="flex-1">
                <label htmlFor="encryption" className="text-sm font-medium cursor-pointer">
                  Enable Server-Side Encryption
                </label>
                <p className="text-xs text-muted-foreground mt-1">
                  Automatically encrypt objects when stored (AES-256)
                </p>
              </div>
            </div>
          </div>

          <div className="flex justify-end space-x-2 pt-4 border-t">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateModalOpen(false)}
              disabled={createBucketMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createBucketMutation.isPending || !formData.name.trim()}
            >
              {createBucketMutation.isPending ? 'Creating...' : 'Create Bucket'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}