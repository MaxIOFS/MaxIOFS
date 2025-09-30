'use client';

import React, { useState } from 'react';
import { useParams } from 'next/navigation';
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
import {
  ArrowLeft,
  Upload,
  Download,
  Search,
  Settings,
  Trash2,
  File,
  Folder,
  Calendar,
  HardDrive,
  MoreHorizontal
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { S3Object, UploadRequest } from '@/types';

export default function BucketDetailsPage() {
  const params = useParams();
  const bucketName = params.bucket as string;
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPrefix, setCurrentPrefix] = useState('');
  const [isUploadModalOpen, setIsUploadModalOpen] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState<FileList | null>(null);
  const queryClient = useQueryClient();

  const { data: bucket, isLoading: bucketLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => APIClient.getBucket(bucketName),
  });

  const { data: objects, isLoading: objectsLoading } = useQuery({
    queryKey: ['objects', bucketName, currentPrefix],
    queryFn: () => APIClient.getObjects(bucketName, { prefix: currentPrefix }),
  });

  const uploadMutation = useMutation({
    mutationFn: (data: UploadRequest) => APIClient.uploadObject(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      setIsUploadModalOpen(false);
      setSelectedFiles(null);
    },
  });

  const deleteObjectMutation = useMutation({
    mutationFn: ({ bucket, key }: { bucket: string; key: string }) =>
      APIClient.deleteObject(bucket, key),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
    },
  });

  const filteredObjects = objects?.data?.filter(obj =>
    obj.key.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedFiles || selectedFiles.length === 0) return;

    for (let i = 0; i < selectedFiles.length; i++) {
      const file = selectedFiles[i];
      const key = currentPrefix ? `${currentPrefix}/${file.name}` : file.name;

      await uploadMutation.mutateAsync({
        bucket: bucketName,
        key,
        file,
      });
    }
  };

  const handleDeleteObject = (key: string) => {
    if (confirm(`Are you sure you want to delete "${key}"? This action cannot be undone.`)) {
      deleteObjectMutation.mutate({ bucket: bucketName, key });
    }
  };

  const handleDownloadObject = (key: string) => {
    // Create download link
    const downloadUrl = APIClient.getObjectUrl(bucketName, key);
    const link = document.createElement('a');
    link.href = downloadUrl;
    link.download = key.split('/').pop() || key;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const navigateToFolder = (folderKey: string) => {
    setCurrentPrefix(folderKey);
    setSearchTerm('');
  };

  const navigateUp = () => {
    const parts = currentPrefix.split('/');
    parts.pop();
    setCurrentPrefix(parts.join('/'));
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

  const isFolder = (obj: S3Object) => {
    return obj.key.endsWith('/');
  };

  if (bucketLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
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
            <h1 className="text-3xl font-bold tracking-tight">{bucketName}</h1>
            <div className="flex items-center gap-4 text-sm text-muted-foreground">
              {currentPrefix && (
                <>
                  <span>Path: /{currentPrefix}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={navigateUp}
                    className="text-blue-600 hover:text-blue-800"
                  >
                    ← Up
                  </Button>
                </>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={() => setIsUploadModalOpen(true)}
            className="gap-2"
          >
            <Upload className="h-4 w-4" />
            Upload
          </Button>
          <Button
            variant="outline"
            onClick={() => window.location.href = `/buckets/${bucketName}/settings`}
            className="gap-2"
          >
            <Settings className="h-4 w-4" />
            Settings
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <File className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {bucket?.data?.objectCount?.toLocaleString() || '0'}
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
              {formatSize(bucket?.data?.size || 0)}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Region</CardTitle>
            <Settings className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {bucket?.data?.region || 'us-east-1'}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Search */}
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
      </div>

      {/* Objects Table */}
      <Card>
        <CardHeader>
          <CardTitle>
            Objects ({filteredObjects.length})
            {currentPrefix && ` in ${currentPrefix}`}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {objectsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : filteredObjects.length === 0 ? (
            <div className="text-center py-8">
              <File className="mx-auto h-12 w-12 text-muted-foreground" />
              <h3 className="mt-4 text-lg font-semibold">No objects found</h3>
              <p className="text-muted-foreground">
                {searchTerm ? 'Try adjusting your search terms' : 'Upload files to get started'}
              </p>
              {!searchTerm && (
                <Button
                  onClick={() => setIsUploadModalOpen(true)}
                  className="mt-4 gap-2"
                >
                  <Upload className="h-4 w-4" />
                  Upload Files
                </Button>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead>Storage Class</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredObjects.map((object) => (
                  <TableRow key={object.key}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        {isFolder(object) ? (
                          <>
                            <Folder className="h-4 w-4 text-blue-500" />
                            <button
                              onClick={() => navigateToFolder(object.key)}
                              className="hover:underline text-blue-600"
                            >
                              {object.key.split('/').slice(-2, -1)[0] || object.key}
                            </button>
                          </>
                        ) : (
                          <>
                            <File className="h-4 w-4 text-muted-foreground" />
                            <span>{object.key.split('/').pop() || object.key}</span>
                          </>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {isFolder(object) ? '-' : formatSize(object.size)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(object.lastModified)}
                      </div>
                    </TableCell>
                    <TableCell>{object.storageClass || 'STANDARD'}</TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!isFolder(object) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDownloadObject(object.key)}
                          >
                            <Download className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteObject(object.key)}
                          disabled={deleteObjectMutation.isPending}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                        <Button variant="ghost" size="sm">
                          <MoreHorizontal className="h-4 w-4" />
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

      {/* Upload Modal */}
      <Modal
        isOpen={isUploadModalOpen}
        onClose={() => setIsUploadModalOpen(false)}
        title="Upload Files"
      >
        <form onSubmit={handleUpload} className="space-y-4">
          <div>
            <label htmlFor="files" className="block text-sm font-medium mb-2">
              Select Files
            </label>
            <input
              id="files"
              type="file"
              multiple
              onChange={(e) => setSelectedFiles(e.target.files)}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
            />
            {currentPrefix && (
              <p className="text-xs text-muted-foreground mt-1">
                Files will be uploaded to: {currentPrefix}/
              </p>
            )}
          </div>

          {selectedFiles && selectedFiles.length > 0 && (
            <div>
              <h4 className="text-sm font-medium mb-2">Selected Files:</h4>
              <ul className="text-sm space-y-1">
                {Array.from(selectedFiles).map((file, index) => (
                  <li key={index} className="flex justify-between">
                    <span>{file.name}</span>
                    <span className="text-muted-foreground">{formatSize(file.size)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsUploadModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={uploadMutation.isPending || !selectedFiles || selectedFiles.length === 0}
            >
              {uploadMutation.isPending ? 'Uploading...' : 'Upload Files'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}