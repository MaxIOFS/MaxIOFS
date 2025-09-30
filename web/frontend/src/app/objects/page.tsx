'use client';

import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { DataTable, DataTableColumn } from '@/components/ui/DataTable';
import {
  FolderOpen,
  Search,
  Upload,
  Download,
  Trash2,
  File,
  Folder,
  Calendar,
  HardDrive,
  Filter
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { S3Object } from '@/types';

export default function ObjectsPage() {
  const [searchTerm, setSearchTerm] = useState('');
  const [selectedBucket, setSelectedBucket] = useState<string>('');

  const { data: buckets } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const { data: objects, isLoading } = useQuery({
    queryKey: ['allObjects', selectedBucket],
    queryFn: () => selectedBucket ? APIClient.getObjects(selectedBucket) : APIClient.getAllObjects(),
  });

  const filteredObjects = objects?.data?.filter(obj =>
    obj.key.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

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

  const handleDownload = (bucketName: string, objectKey: string) => {
    const downloadUrl = APIClient.getObjectUrl(bucketName, objectKey);
    const link = document.createElement('a');
    link.href = downloadUrl;
    link.download = objectKey.split('/').pop() || objectKey;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const isFolder = (obj: S3Object) => {
    return obj.key.endsWith('/');
  };

  const objectColumns: DataTableColumn<S3Object & { bucket: string }>[] = [
    {
      key: 'key',
      header: 'Name',
      sortable: true,
      render: (obj) => (
        <div className="flex items-center gap-2">
          {isFolder(obj) ? (
            <Folder className="h-4 w-4 text-blue-500" />
          ) : (
            <File className="h-4 w-4 text-muted-foreground" />
          )}
          <div>
            <div className="font-medium">{obj.key.split('/').pop() || obj.key}</div>
            <div className="text-xs text-muted-foreground">{obj.bucket}</div>
          </div>
        </div>
      ),
    },
    {
      key: 'size',
      header: 'Size',
      sortable: true,
      render: (obj) => (
        <span>{isFolder(obj) ? '-' : formatSize(obj.size)}</span>
      ),
    },
    {
      key: 'lastModified',
      header: 'Modified',
      sortable: true,
      render: (obj) => (
        <div className="flex items-center gap-1 text-sm text-muted-foreground">
          <Calendar className="h-3 w-3" />
          {formatDate(obj.lastModified)}
        </div>
      ),
    },
    {
      key: 'storageClass',
      header: 'Storage Class',
      render: (obj) => (
        <span className="px-2 py-1 bg-blue-100 text-blue-800 text-xs rounded-full">
          {obj.storageClass || 'STANDARD'}
        </span>
      ),
    },
  ];

  // Mock data for demonstration
  const mockObjects = [
    { key: 'documents/report.pdf', bucket: 'company-docs', size: 2456789, lastModified: new Date().toISOString(), storageClass: 'STANDARD' },
    { key: 'images/logo.png', bucket: 'assets', size: 45678, lastModified: new Date().toISOString(), storageClass: 'STANDARD' },
    { key: 'backups/', bucket: 'data-backup', size: 0, lastModified: new Date().toISOString(), storageClass: 'STANDARD' },
    { key: 'videos/intro.mp4', bucket: 'media', size: 156789012, lastModified: new Date().toISOString(), storageClass: 'IA' },
  ];

  const displayObjects = filteredObjects.length > 0 ? filteredObjects : mockObjects;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Objects</h1>
          <p className="text-muted-foreground">
            Browse and manage objects across all buckets
          </p>
        </div>
        <Button className="gap-2">
          <Upload className="h-4 w-4" />
          Upload Objects
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <File className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">1,584</div>
            <p className="text-xs text-muted-foreground">+12% from last month</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Size</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">256 GB</div>
            <p className="text-xs text-muted-foreground">+8% from last month</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Folders</CardTitle>
            <Folder className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">142</div>
            <p className="text-xs text-muted-foreground">+5 new this week</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Average Size</CardTitle>
            <FolderOpen className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">162 KB</div>
            <p className="text-xs text-muted-foreground">-3% from last month</p>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <div className="flex items-center space-x-4">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
          <Input
            placeholder="Search objects..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10"
          />
        </div>

        <div className="relative">
          <Filter className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
          <select
            value={selectedBucket}
            onChange={(e) => setSelectedBucket(e.target.value)}
            className="w-48 pl-10 pr-4 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
          >
            <option value="">All Buckets</option>
            {buckets?.data?.map((bucket) => (
              <option key={bucket.name} value={bucket.name}>
                {bucket.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Objects Table */}
      <DataTable
        data={displayObjects}
        columns={objectColumns}
        isLoading={isLoading}
        title={`Objects (${displayObjects.length})`}
        emptyMessage="No objects found"
        emptyIcon={<File className="h-12 w-12 text-muted-foreground" />}
        emptyAction={
          <Button className="gap-2">
            <Upload className="h-4 w-4" />
            Upload First Object
          </Button>
        }
        actions={(obj) => (
          <div className="flex items-center gap-2">
            {!isFolder(obj) && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleDownload(obj.bucket, obj.key)}
              >
                <Download className="h-4 w-4" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => console.log('Delete', obj.key)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        )}
      />
    </div>
  );
}