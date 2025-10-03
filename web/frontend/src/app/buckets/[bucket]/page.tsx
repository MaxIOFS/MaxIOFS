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
import { ArrowLeft as ArrowLeftIcon } from 'lucide-react';
import { Upload as UploadIcon } from 'lucide-react';
import { Download as DownloadIcon } from 'lucide-react';
import { Search as SearchIcon } from 'lucide-react';
import { Settings as SettingsIcon } from 'lucide-react';
import { Trash2 as Trash2Icon } from 'lucide-react';
import { File as FileIcon } from 'lucide-react';
import { Folder as FolderIcon } from 'lucide-react';
import { Calendar as CalendarIcon } from 'lucide-react';
import { HardDrive as HardDriveIcon } from 'lucide-react';
import { MoreHorizontal as MoreHorizontalIcon } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { S3Object, UploadRequest } from '@/types';
import SweetAlert from '@/lib/sweetalert';

export default function BucketDetailsPage() {
  const params = useParams();
  const bucketName = params.bucket as string;
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPrefix, setCurrentPrefix] = useState('');
  const [isUploadModalOpen, setIsUploadModalOpen] = useState(false);
  const [isCreateFolderModalOpen, setIsCreateFolderModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [selectedFiles, setSelectedFiles] = useState<FileList | null>(null);
  const queryClient = useQueryClient();

  const { data: bucket, isLoading: bucketLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => APIClient.getBucket(bucketName),
  });

  const { data: objectsResponse, isLoading: objectsLoading } = useQuery({
    queryKey: ['objects', bucketName, currentPrefix],
    queryFn: () => APIClient.getObjects({
      bucket: bucketName,
      prefix: currentPrefix,
      delimiter: '/', // This groups objects by folder
    }),
  });

  const uploadMutation = useMutation({
    mutationFn: (data: UploadRequest) => APIClient.uploadObject(data),
    onSuccess: (response, variables) => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsUploadModalOpen(false);
      setSelectedFiles(null);
      
      // Mostrar notificación de éxito
      const fileName = variables.key.split('/').pop() || variables.key;
      SweetAlert.successUpload(fileName);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const createFolderMutation = useMutation({
    mutationFn: async (folderName: string) => {
      const folderKey = currentPrefix
        ? `${currentPrefix}${folderName}/`
        : `${folderName}/`;

      // Create an empty object with the folder name ending in /
      // This is the standard S3 way to create folders
      const emptyFile = new File([''], folderName, { type: 'application/octet-stream' });
      
      return APIClient.uploadObject({
        bucket: bucketName,
        key: folderKey,
        file: emptyFile,
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsCreateFolderModalOpen(false);
      setNewFolderName('');
      SweetAlert.toast('success', `Carpeta "${newFolderName}" creada exitosamente`);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteObjectMutation = useMutation({
    mutationFn: async ({ bucket, key }: { bucket: string; key: string }) => {
      // Check if it's a folder (ends with /)
      if (key.endsWith('/')) {
        // Check if folder has objects 
        const folderObjects = await APIClient.getObjects({
          bucket,
          prefix: key,
        });

        // Check if there are any actual files (not just the folder marker or system files)
        const actualObjects = folderObjects?.objects?.filter(obj => {
          // Exclude the folder marker itself
          if (obj.key === key) return false;
          
          // Exclude MaxIOFS system files (.maxiofs-folder, .metadata files, etc.)
          if (obj.key.includes('.maxiofs-')) return false;
          
          // Exclude other system/metadata files
          if (obj.key.endsWith('.metadata')) return false;
          
          return true;
        }) || [];

        if (actualObjects.length > 0) {
          throw new Error('Cannot delete folder: it contains objects. Delete all objects first.');
        }
      }

      return APIClient.deleteObject(bucket, key);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Objeto eliminado exitosamente');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // Process objects and common prefixes (folders)
  const objects = objectsResponse?.objects || [];
  const commonPrefixes = objectsResponse?.commonPrefixes || [];

  // Debug logging to see what we're receiving
  console.log('DEBUG Frontend - objectsResponse:', objectsResponse);
  console.log('DEBUG Frontend - objects:', objects);
  console.log('DEBUG Frontend - commonPrefixes:', commonPrefixes);

  // Combine folders and files
  // Filter out objects that are folder markers (empty files ending with / and size 0)
  // since they will already be in commonPrefixes
  const filteredObjects = objects.filter(obj => {
    // If it's a folder marker (ends with / and size is 0), skip it
    if (obj.key.endsWith('/') && obj.size === 0) {
      return false;
    }
    // Filter out MaxIOFS system files
    if (obj.key.includes('.maxiofs-')) {
      return false;
    }
    return true;
  });

  const allItems = [
    ...commonPrefixes.map(prefix => ({
      key: prefix,
      isFolder: true,
      size: 0,
      lastModified: '',
      etag: '',
      storageClass: '',
    })),
    ...filteredObjects,
  ];

  const filteredItems = allItems.filter(item =>
    item.key.toLowerCase().includes(searchTerm.toLowerCase())
  );

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedFiles || selectedFiles.length === 0) return;

    const totalFiles = selectedFiles.length;
    let successCount = 0;
    const errors: string[] = [];

    // Show loading indicator
    if (totalFiles === 1) {
      SweetAlert.loading('Subiendo archivo...', `Subiendo "${selectedFiles[0].name}"`);
    } else {
      SweetAlert.loading('Subiendo archivos...', `0 de ${totalFiles} archivos`);
    }

    // Upload files sequentially
    for (let i = 0; i < totalFiles; i++) {
      const file = selectedFiles[i];
      const key = currentPrefix
        ? `${currentPrefix.replace(/\/$/, '')}/${file.name}`
        : file.name;

      try {
        // Update progress message for multiple files
        if (totalFiles > 1) {
          SweetAlert.loading('Subiendo archivos...', `${i + 1} de ${totalFiles}: ${file.name}`);
        }

        await APIClient.uploadObject({
          bucket: bucketName,
          key,
          file,
        });

        successCount++;
      } catch (fileError: any) {
        const errorMsg = fileError?.response?.data?.error || fileError?.message || 'Error desconocido';
        errors.push(`${file.name}: ${errorMsg}`);
        console.error(`Error uploading ${file.name}:`, fileError);
      }
    }

    SweetAlert.close();

    // Show results
    if (totalFiles === 1) {
      if (successCount === 1) {
        SweetAlert.successUpload(selectedFiles[0].name);
      } else {
        SweetAlert.apiError(new Error(errors[0] || 'Error al subir archivo'));
      }
    } else {
      const failCount = totalFiles - successCount;
      if (failCount === 0) {
        SweetAlert.toast('success', `${totalFiles} archivos subidos exitosamente`);
      } else if (successCount > 0) {
        SweetAlert.fire({
          icon: 'warning',
          title: 'Upload parcialmente exitoso',
          html: `<p>Subidos: <strong>${successCount}</strong> / ${totalFiles}</p><p>Fallidos: <strong>${failCount}</strong></p>`,
        });
      } else {
        SweetAlert.fire({
          icon: 'error',
          title: 'Error al subir archivos',
          text: 'Todos los archivos fallaron',
        });
      }
    }

    // Refresh and close
    if (successCount > 0) {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
    }

    setIsUploadModalOpen(false);
    setSelectedFiles(null);
  };

  const handleCreateFolder = (e: React.FormEvent) => {
    e.preventDefault();
    if (newFolderName.trim()) {
      createFolderMutation.mutate(newFolderName.trim());
    }
  };

  const handleDeleteObject = async (key: string, isFolder: boolean) => {
    const itemType = isFolder ? 'carpeta' : 'archivo';
    
    try {
      const result = await SweetAlert.fire({
        icon: 'warning',
        title: `¿Eliminar ${itemType}?`,
        html: isFolder 
          ? `<p>Estás a punto de eliminar la carpeta <strong>"${key}"</strong></p>
             <p class="text-orange-600 mt-2">Esto fallará si la carpeta contiene objetos</p>`
          : `<p>Estás a punto de eliminar <strong>"${key}"</strong></p>
             <p class="text-red-600 mt-2">Esta acción no se puede deshacer</p>`,
        showCancelButton: true,
        confirmButtonText: 'Sí, eliminar',
        cancelButtonText: 'Cancelar',
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        SweetAlert.loading(`Eliminando ${itemType}...`, `Eliminando "${key}"`);
        deleteObjectMutation.mutate({ bucket: bucketName, key });
      }
    } catch (error) {
      SweetAlert.apiError(error);
    }
  };

  const handleDownloadObject = async (key: string) => {
    try {
      // Mostrar indicador de descarga
      SweetAlert.loading('Descargando archivo...', `Descargando "${key}"`);

      const blob = await APIClient.downloadObject({
        bucket: bucketName,
        key,
      });

      // Cerrar indicador de carga
      SweetAlert.close();

      // Create download link
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = key.split('/').pop() || key;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);

      // Mostrar mensaje de éxito
      SweetAlert.successDownload(key.split('/').pop() || key);
    } catch (error: any) {
      SweetAlert.close();
      SweetAlert.apiError(error);
    }
  };

  const navigateToFolder = (folderKey: string) => {
    setCurrentPrefix(folderKey);
    setSearchTerm('');
  };

  const navigateUp = () => {
    const parts = currentPrefix.split('/').filter(p => p);
    parts.pop(); // Remove last folder
    const newPrefix = parts.length > 0 ? parts.join('/') + '/' : '';
    setCurrentPrefix(newPrefix);
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

  const isFolder = (item: any) => {
    return item.isFolder || item.key.endsWith('/');
  };

  const getDisplayName = (item: any) => {
    if (isFolder(item)) {
      // Remove trailing slash and get last part
      const parts = item.key.replace(/\/$/, '').split('/');
      return parts[parts.length - 1];
    }
    return item.key.split('/').pop() || item.key;
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
            <ArrowLeftIcon className="h-4 w-4" />
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
            onClick={() => setIsCreateFolderModalOpen(true)}
            variant="outline"
            className="gap-2"
          >
            <FolderIcon className="h-4 w-4" />
            New Folder
          </Button>
          <Button
            onClick={() => setIsUploadModalOpen(true)}
            className="gap-2"
          >
            <UploadIcon className="h-4 w-4" />
            Upload Files
          </Button>
          <Button
            variant="outline"
            onClick={() => window.location.href = `/buckets/${bucketName}/settings`}
            className="gap-2"
          >
            <SettingsIcon className="h-4 w-4" />
            Settings
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <FileIcon className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {(bucket?.object_count || 0).toLocaleString()}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Files and folders
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Size</CardTitle>
            <HardDriveIcon className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {formatSize(bucket?.size || 0)}
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              Storage used
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Region</CardTitle>
            <SettingsIcon className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {bucket?.region || 'us-east-1'}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Search */}
      <div className="flex items-center space-x-4">
        <div className="relative flex-1 max-w-sm">
          <SearchIcon className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-4 w-4" />
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
            Objects ({filteredItems.length})
            {currentPrefix && ` in ${currentPrefix}`}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {objectsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : filteredItems.length === 0 ? (
            <div className="text-center py-8">
              <FileIcon className="mx-auto h-12 w-12 text-muted-foreground" />
              <h3 className="mt-4 text-lg font-semibold">No objects found</h3>
              <p className="text-muted-foreground">
                {searchTerm ? 'Try adjusting your search terms' : 'Create folders or upload files to get started'}
              </p>
              {!searchTerm && (
                <div className="flex gap-2 justify-center mt-4">
                  <Button
                    onClick={() => setIsCreateFolderModalOpen(true)}
                    variant="outline"
                    className="gap-2"
                  >
                    <FolderIcon className="h-4 w-4" />
                    New Folder
                  </Button>
                  <Button
                    onClick={() => setIsUploadModalOpen(true)}
                    className="gap-2"
                  >
                    <UploadIcon className="h-4 w-4" />
                    Upload Files
                  </Button>
                </div>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredItems.map((item) => (
                  <TableRow key={item.key}>
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        {isFolder(item) ? (
                          <>
                            <FolderIcon className="h-4 w-4 text-blue-500" />
                            <button
                              onClick={() => navigateToFolder(item.key)}
                              className="hover:underline text-blue-600"
                            >
                              {getDisplayName(item)}
                            </button>
                          </>
                        ) : (
                          <>
                            <FileIcon className="h-4 w-4 text-muted-foreground" />
                            <span>{getDisplayName(item)}</span>
                          </>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {isFolder(item) ? '-' : formatSize(item.size)}
                    </TableCell>
                    <TableCell>
                      {item.lastModified ? (
                        <div className="flex items-center gap-1 text-sm text-muted-foreground">
                          <CalendarIcon className="h-3 w-3" />
                          {formatDate(item.lastModified)}
                        </div>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                    <TableCell>
                      {isFolder(item) ? (
                        <span className="text-blue-600">Folder</span>
                      ) : (
                        item.storageClass || 'STANDARD'
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!isFolder(item) && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDownloadObject(item.key)}
                            title="Download"
                          >
                            <DownloadIcon className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteObject(item.key, isFolder(item))}
                          disabled={deleteObjectMutation.isPending}
                          title="Delete"
                        >
                          <Trash2Icon className="h-4 w-4" />
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

      {/* Create Folder Modal */}
      <Modal
        isOpen={isCreateFolderModalOpen}
        onClose={() => setIsCreateFolderModalOpen(false)}
        title="Create New Folder"
      >
        <form onSubmit={handleCreateFolder} className="space-y-4">
          <div className="bg-blue-50 border border-blue-200 rounded-md p-3 mb-4">
            <p className="text-sm text-blue-800">
              <strong>💡 About S3 Folders:</strong> In S3, folders are <strong>virtual</strong> - they don't physically exist.
              A folder is represented by adding "/" to object names (e.g., "photos/vacation.jpg").
              This is the standard S3 behavior used by AWS and all S3-compatible systems.
            </p>
          </div>

          <div>
            <label htmlFor="folderName" className="block text-sm font-medium mb-2">
              Folder Name *
            </label>
            <Input
              id="folderName"
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder="my-folder"
              required
              pattern="^[a-zA-Z0-9][a-zA-Z0-9\-_]{0,254}$"
              title="Folder name must be alphanumeric, hyphens, and underscores only"
            />
            {currentPrefix ? (
              <p className="text-xs text-muted-foreground mt-1">
                📁 Full path: <code className="bg-gray-100 px-1 rounded">{currentPrefix}/{newFolderName}/</code>
              </p>
            ) : (
              <p className="text-xs text-muted-foreground mt-1">
                📁 Full path: <code className="bg-gray-100 px-1 rounded">{newFolderName}/</code>
              </p>
            )}
          </div>

          <div className="flex justify-end space-x-2 pt-4 border-t">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateFolderModalOpen(false)}
              disabled={createFolderMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={createFolderMutation.isPending || !newFolderName.trim()}
            >
              {createFolderMutation.isPending ? 'Creating...' : 'Create Folder'}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}