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
import { Database, Plus, Search, Settings, Trash2, Calendar, HardDrive } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Bucket, CreateBucketRequest } from '@/types';

export default function BucketsPage() {
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [newBucketName, setNewBucketName] = useState('');
  const [newBucketRegion, setNewBucketRegion] = useState('us-east-1');
  const queryClient = useQueryClient();

  const { data: buckets, isLoading, error } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const createBucketMutation = useMutation({
    mutationFn: (data: CreateBucketRequest) => APIClient.createBucket(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      setIsCreateModalOpen(false);
      setNewBucketName('');
      setNewBucketRegion('us-east-1');
    },
  });

  const deleteBucketMutation = useMutation({
    mutationFn: (bucketName: string) => APIClient.deleteBucket(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
    },
  });

  const filteredBuckets = buckets?.data?.filter(bucket =>
    bucket.name.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

  const handleCreateBucket = (e: React.FormEvent) => {
    e.preventDefault();
    if (newBucketName.trim()) {
      createBucketMutation.mutate({
        name: newBucketName.trim(),
        region: newBucketRegion,
      });
    }
  };

  const handleDeleteBucket = (bucketName: string) => {
    if (confirm(`Are you sure you want to delete bucket "${bucketName}"? This action cannot be undone.`)) {
      deleteBucketMutation.mutate(bucketName);
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
              {filteredBuckets.reduce((sum, bucket) => sum + (bucket.objectCount || 0), 0)}
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
              {formatSize(filteredBuckets.reduce((sum, bucket) => sum + (bucket.size || 0), 0))}
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
                    <TableCell>{bucket.objectCount?.toLocaleString() || '0'}</TableCell>
                    <TableCell>{formatSize(bucket.size || 0)}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(bucket.createdAt)}
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
          <div>
            <label htmlFor="bucketName" className="block text-sm font-medium mb-2">
              Bucket Name
            </label>
            <Input
              id="bucketName"
              value={newBucketName}
              onChange={(e) => setNewBucketName(e.target.value)}
              placeholder="my-bucket-name"
              required
              pattern="^[a-z0-9][a-z0-9\-]{1,61}[a-z0-9]$"
              title="Bucket name must be 3-63 characters, lowercase letters, numbers, and hyphens only"
            />
            <p className="text-xs text-muted-foreground mt-1">
              3-63 characters, lowercase letters, numbers, and hyphens only
            </p>
          </div>

          <div>
            <label htmlFor="bucketRegion" className="block text-sm font-medium mb-2">
              Region
            </label>
            <select
              id="bucketRegion"
              value={newBucketRegion}
              onChange={(e) => setNewBucketRegion(e.target.value)}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="us-east-1">US East (N. Virginia)</option>
              <option value="us-west-2">US West (Oregon)</option>
              <option value="eu-west-1">Europe (Ireland)</option>
              <option value="eu-central-1">Europe (Frankfurt)</option>
              <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
            </select>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createBucketMutation.isPending || !newBucketName.trim()}
            >
              {createBucketMutation.isPending ? 'Creating...' : 'Create Bucket'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}