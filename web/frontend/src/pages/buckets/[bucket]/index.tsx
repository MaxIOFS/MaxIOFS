import React, { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
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
import { Lock as LockIcon } from 'lucide-react';
import { Shield as ShieldIcon } from 'lucide-react';
import { Clock as ClockIcon } from 'lucide-react';
import { Share2 as Share2Icon } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { S3Object, UploadRequest } from '@/types';
import SweetAlert from '@/lib/sweetalert';
import { BucketPermissionsModal } from '@/components/BucketPermissionsModal';

export default function BucketDetailsPage() {
  const { bucket, tenantId } = useParams<{ bucket: string; tenantId?: string }>();
  const navigate = useNavigate();
  const bucketName = bucket as string;
  const bucketPath = tenantId ? `/buckets/${tenantId}/${bucketName}` : `/buckets/${bucketName}`;
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPrefix, setCurrentPrefix] = useState('');
  const [isUploadModalOpen, setIsUploadModalOpen] = useState(false);
  const [isCreateFolderModalOpen, setIsCreateFolderModalOpen] = useState(false);
  const [isPermissionsModalOpen, setIsPermissionsModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [selectedFiles, setSelectedFiles] = useState<FileList | null>(null);
  const [selectedObjects, setSelectedObjects] = useState<Set<string>>(new Set());
  const queryClient = useQueryClient();

  const { data: bucketData, isLoading: bucketLoading } = useQuery({
    queryKey: ['bucket', bucketName, tenantId],
    queryFn: () => APIClient.getBucket(bucketName, tenantId || undefined),
  });

  const { data: objectsResponse, isLoading: objectsLoading } = useQuery({
    queryKey: ['objects', bucketName, currentPrefix, tenantId],
    queryFn: () => APIClient.getObjects({
      bucket: bucketName,
      ...(tenantId && { tenantId }),
      prefix: currentPrefix,
      delimiter: '/', // This groups objects by folder
    }),
  });

  const { data: sharesMap = {}, isLoading: sharesLoading } = useQuery({
    queryKey: ['shares', bucketName],
    queryFn: () => APIClient.getBucketShares(bucketName),
  });

  const uploadMutation = useMutation({
    mutationFn: (data: UploadRequest) => APIClient.uploadObject(data),
    onSuccess: (response, variables) => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsUploadModalOpen(false);
      setSelectedFiles(null);

      // Show success notification
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
        ...(tenantId && { tenantId }),
        key: folderKey,
        file: emptyFile,
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsCreateFolderModalOpen(false);
      setNewFolderName('');
      SweetAlert.toast('success', `Folder "${newFolderName}" created successfully`);
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
          ...(tenantId && { tenantId }),
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

      return APIClient.deleteObject(bucket, key, tenantId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Object deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteShareMutation = useMutation({
    mutationFn: ({ bucket, key }: { bucket: string; key: string }) => APIClient.deleteShare(bucket, key, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['shares', bucketName] });
      SweetAlert.toast('success', 'Share deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // Process objects and common prefixes (folders)
  const objects = objectsResponse?.objects || [];
  const commonPrefixes = objectsResponse?.commonPrefixes || [];

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
      SweetAlert.loading('Uploading file...', `Uploading "${selectedFiles[0].name}"`);
    } else {
      SweetAlert.loading('Uploading files...', `0 of ${totalFiles} files`);
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
          SweetAlert.loading('Uploading files...', `${i + 1} of ${totalFiles}: ${file.name}`);
        }

        await APIClient.uploadObject({
          bucket: bucketName,
          ...(tenantId && { tenantId }),
          key,
          file,
        });

        successCount++;
      } catch (fileError: any) {
        const errorMsg = fileError?.response?.data?.error || fileError?.message || 'Unknown error';
        errors.push(`${file.name}: ${errorMsg}`);
      }
    }

    SweetAlert.close();

    // Show results
    if (totalFiles === 1) {
      if (successCount === 1) {
        SweetAlert.successUpload(selectedFiles[0].name);
      } else {
        SweetAlert.apiError(new Error(errors[0] || 'Error uploading file'));
      }
    } else {
      const failCount = totalFiles - successCount;
      if (failCount === 0) {
        SweetAlert.toast('success', `${totalFiles} files uploaded successfully`);
      } else if (successCount > 0) {
        SweetAlert.fire({
          icon: 'warning',
          title: 'Partially successful upload',
          html: `<p>Uploaded: <strong>${successCount}</strong> / ${totalFiles}</p><p>Failed: <strong>${failCount}</strong></p>`,
        });
      } else {
        SweetAlert.fire({
          icon: 'error',
          title: 'Error uploading files',
          text: 'All files failed',
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
    const itemType = isFolder ? 'folder' : 'file';

    try {
      const result = await SweetAlert.fire({
        icon: 'warning',
        title: `Delete ${itemType}?`,
        html: isFolder
          ? `<p>You are about to delete the folder <strong>"${key}"</strong></p>
             <p class="text-orange-600 mt-2">This will fail if folder contains objects</p>`
          : `<p>You are about to delete <strong>"${key}"</strong></p>
             <p class="text-red-600 mt-2">This action cannot be undone</p>`,
        showCancelButton: true,
        confirmButtonText: 'Yes, delete',
        cancelButtonText: 'Cancel',
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        SweetAlert.loading(`Deleting ${itemType}...`, `Deleting "${key}"`);
        deleteObjectMutation.mutate({ bucket: bucketName, key });
      }
    } catch (error) {
      SweetAlert.apiError(error);
    }
  };

  const handleDownloadObject = async (key: string) => {
    try {
      // Show download indicator
      SweetAlert.loading('Downloading file...', `Downloading "${key}"`);

      const blob = await APIClient.downloadObject({
        bucket: bucketName,
        ...(tenantId && { tenantId }),
        key,
      });

      // Close loading indicator
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

      // Show success message
      SweetAlert.successDownload(key.split('/').pop() || key);
    } catch (error: any) {
      SweetAlert.close();
      SweetAlert.apiError(error);
    }
  };

  const handleShareObject = async (key: string) => {
    try {
      // Check if object is already shared
      const existingShare = sharesMap[key];

      if (existingShare) {
        // Object is already shared - show copy/unshare options
        const shareData = await APIClient.shareObject(bucketName, key, null, tenantId);

        let expirationInfo = '';
        if (shareData.expiresAt) {
          const expiresAt = new Date(shareData.expiresAt).toLocaleString();
          const expiresInMs = new Date(shareData.expiresAt).getTime() - Date.now();
          const expiresInDays = Math.floor(expiresInMs / (1000 * 60 * 60 * 24));
          const expiresInHours = Math.floor(expiresInMs / (1000 * 60 * 60));

          const expirationText = expiresInDays > 0
            ? `${expiresInDays} day${expiresInDays > 1 ? 's' : ''}`
            : `${expiresInHours} hour${expiresInHours > 1 ? 's' : ''}`;

          expirationInfo = `<p><strong>Expires:</strong> ${expiresAt} (in ${expirationText})</p>`;
        } else {
          expirationInfo = `<p><strong>Expiration:</strong> Never (permanent link)</p>`;
        }

        const result = await SweetAlert.fire({
          icon: 'info',
          title: 'Object Already Shared',
          html: `
            <div class="text-left space-y-4">
              <p class="text-gray-700">This object is already shared. You can:</p>
              <div>
                <p class="text-sm font-medium mb-2">Share this link:</p>
                <div class="bg-gray-50 p-3 rounded border border-gray-200">
                  <code class="text-xs break-all">${shareData.url}</code>
                </div>
              </div>
              <div class="text-sm text-gray-600">
                ${expirationInfo}
                <p><strong>Created:</strong> ${new Date(shareData.createdAt).toLocaleString()}</p>
              </div>
            </div>
          `,
          showCancelButton: true,
          showDenyButton: true,
          confirmButtonText: 'Copy Link',
          denyButtonText: 'Unshare',
          cancelButtonText: 'Close',
          width: '650px',
        });

        if (result.isConfirmed) {
          navigator.clipboard.writeText(shareData.url);
          SweetAlert.toast('success', 'Link copied to clipboard');
        } else if (result.isDenied) {
          // Unshare the object
          const confirmDelete = await SweetAlert.fire({
            icon: 'warning',
            title: 'Unshare Object?',
            text: 'This will delete the share link. The file itself will not be deleted.',
            showCancelButton: true,
            confirmButtonText: 'Yes, unshare',
            cancelButtonText: 'Cancel',
            confirmButtonColor: '#dc2626',
          });

          if (confirmDelete.isConfirmed) {
            deleteShareMutation.mutate({ bucket: bucketName, key });
          }
        }
        return;
      }

      // Object is not shared yet - show create dialog
      const result = await SweetAlert.fire({
        icon: 'info',
        title: 'Share Object',
        html: `
          <p class="mb-4">Generate a shareable link for <strong>"${key.split('/').pop()}"</strong></p>
          <div class="text-left">
            <label for="expiresIn" class="block text-sm font-medium mb-2">Link expires in:</label>
            <select id="expiresIn" class="w-full px-3 py-2 border border-gray-300 rounded-md">
              <option value="0">Never (permanent link)</option>
              <option value="3600">1 hour</option>
              <option value="21600">6 hours</option>
              <option value="43200">12 hours</option>
              <option value="86400" selected>24 hours</option>
              <option value="604800">7 days</option>
            </select>
          </div>
        `,
        showCancelButton: true,
        confirmButtonText: 'Generate Link',
        cancelButtonText: 'Cancel',
        preConfirm: () => {
          const select = document.getElementById('expiresIn') as HTMLSelectElement;
          const value = parseInt(select.value);
          return value === 0 ? null : value;
        }
      });

      if (!result.isConfirmed) return;

      const expiresIn = result.value as number | null;

      // Show loading indicator
      SweetAlert.loading('Generating shareable link...', `Creating link for "${key}"`);

      const shareData = await APIClient.shareObject(bucketName, key, expiresIn, tenantId);

      // Refresh shares list
      queryClient.invalidateQueries({ queryKey: ['shares', bucketName] });

      // Close loading indicator
      SweetAlert.close();

      // Prepare expiration info
      let expirationInfo = '';
      let statusBadge = '';

      if (shareData.expiresAt) {
        const expiresAt = new Date(shareData.expiresAt).toLocaleString();
        const expiresInMs = new Date(shareData.expiresAt).getTime() - Date.now();
        const expiresInDays = Math.floor(expiresInMs / (1000 * 60 * 60 * 24));
        const expiresInHours = Math.floor(expiresInMs / (1000 * 60 * 60));

        const expirationText = expiresInDays > 0
          ? `${expiresInDays} day${expiresInDays > 1 ? 's' : ''}`
          : `${expiresInHours} hour${expiresInHours > 1 ? 's' : ''}`;

        expirationInfo = `<p><strong>Expires:</strong> ${expiresAt} (in ${expirationText})</p>`;
        statusBadge = '<span class="inline-block px-2 py-1 bg-yellow-100 text-yellow-800 text-xs rounded">⏰ Temporary</span>';
      } else {
        expirationInfo = `<p><strong>Expiration:</strong> Never (permanent link)</p>`;
        statusBadge = '<span class="inline-block px-2 py-1 bg-green-100 text-green-800 text-xs rounded">∞ Permanent</span>';
      }

      await SweetAlert.fire({
        icon: 'success',
        title: 'Shareable Link Created',
        html: `
          <div class="text-left space-y-4">
            <div class="flex items-center gap-2 mb-2">
              ${statusBadge}
            </div>
            <div>
              <p class="text-sm font-medium mb-2">Share this link:</p>
              <div class="bg-gray-50 p-3 rounded border border-gray-200">
                <code class="text-xs break-all">${shareData.url}</code>
              </div>
            </div>
            <div class="text-sm text-gray-600">
              ${expirationInfo}
              <p><strong>Created:</strong> ${new Date(shareData.createdAt).toLocaleString()}</p>
            </div>
            <div class="bg-blue-50 border border-blue-200 rounded p-3 text-sm text-blue-800">
              <strong>ℹ️ Note:</strong> Anyone with this link can download the file${shareData.expiresAt ? ' until it expires' : ' (no expiration)'}.
            </div>
          </div>
        `,
        showCancelButton: true,
        confirmButtonText: 'Copy Link',
        cancelButtonText: 'Close',
        width: '650px',
      }).then((copyResult) => {
        if (copyResult.isConfirmed) {
          navigator.clipboard.writeText(shareData.url);
          SweetAlert.toast('success', 'Link copied to clipboard');
        }
      });
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
    // Clear selections when navigating
    setSelectedObjects(new Set());
  };

  // Bulk selection handlers
  const toggleObjectSelection = (key: string) => {
    const newSelection = new Set(selectedObjects);
    if (newSelection.has(key)) {
      newSelection.delete(key);
    } else {
      newSelection.add(key);
    }
    setSelectedObjects(newSelection);
  };

  const toggleSelectAll = () => {
    if (selectedObjects.size === filteredObjects.length) {
      // Deselect all
      setSelectedObjects(new Set());
    } else {
      // Select all
      const allKeys = new Set(filteredObjects.map(obj => obj.key));
      setSelectedObjects(allKeys);
    }
  };

  const handleBulkDelete = async () => {
    if (selectedObjects.size === 0) return;

    const objectNames = Array.from(selectedObjects).join(', ');
    const result = await SweetAlert.fire({
      icon: 'warning',
      title: `Delete ${selectedObjects.size} objects?`,
      html: `<p>You are about to delete <strong>${selectedObjects.size}</strong> objects</p>
             <p class="text-red-600 mt-2">This action cannot be undone</p>
             <p class="text-sm text-gray-600 mt-2">Some objects may be protected by Object Lock</p>`,
      showCancelButton: true,
      confirmButtonText: 'Yes, delete',
      cancelButtonText: 'Cancel',
      confirmButtonColor: '#dc2626',
    });

    if (!result.isConfirmed) return;

    // Show progress (don't await - it returns void)
    SweetAlert.progress(
      'Deleting objects...',
      `Processing ${selectedObjects.size} objects`
    );

    const selectedArray = Array.from(selectedObjects);
    let successCount = 0;
    let failCount = 0;
    const errors: { key: string; error: string }[] = [];

    for (let i = 0; i < selectedArray.length; i++) {
      const key = selectedArray[i];
      const progress = ((i + 1) / selectedArray.length) * 100;
      SweetAlert.updateProgress(progress);

      try {
        await APIClient.deleteObject(bucketName, key, tenantId);
        successCount++;
      } catch (error: any) {
        failCount++;
        const errorMsg = error?.details?.Error || error?.message || 'Unknown error';
        errors.push({ key, error: errorMsg });
      }
    }

    SweetAlert.close();

    // Show results
    if (failCount === 0) {
      SweetAlert.toast('success', `${successCount} objects deleted successfully`);
    } else if (successCount > 0) {
      const errorList = errors.map(e => `<li><strong>${e.key}</strong>: ${e.error}</li>`).join('');
      SweetAlert.fire({
        icon: 'warning',
        title: 'Partially successful deletion',
        html: `<p>Deleted: <strong>${successCount}</strong> / ${selectedArray.length}</p>
               <p>Failed: <strong>${failCount}</strong></p>
               <div class="mt-4 text-left max-h-64 overflow-y-auto">
                 <p class="font-semibold mb-2">Errors:</p>
                 <ul class="text-sm">${errorList}</ul>
               </div>`,
        width: '600px',
      });
    } else {
      const errorList = errors.map(e => `<li><strong>${e.key}</strong>: ${e.error}</li>`).join('');
      SweetAlert.fire({
        icon: 'error',
        title: 'Error deleting objects',
        html: `<p>All objects failed</p>
               <div class="mt-4 text-left max-h-64 overflow-y-auto">
                 <p class="font-semibold mb-2">Errors:</p>
                 <ul class="text-sm">${errorList}</ul>
               </div>`,
        width: '600px',
      });
    }

    // Refresh and clear selections
    if (successCount > 0) {
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
    }
    setSelectedObjects(new Set());
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

  const formatRetentionExpiration = (retainUntilDate?: string) => {
    if (!retainUntilDate) return null;

    const now = new Date();
    const expirationDate = new Date(retainUntilDate);
    const diffMs = expirationDate.getTime() - now.getTime();

    if (diffMs < 0) {
      return { text: 'Expired', color: 'text-green-600', expired: true };
    }

    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));
    const diffHours = Math.floor((diffMs % (1000 * 60 * 60 * 24)) / (1000 * 60 * 60));
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));

    let text = '';
    if (diffDays > 0) {
      text = `${diffDays}d ${diffHours}h`;
    } else if (diffHours > 0) {
      text = `${diffHours}h ${diffMinutes}m`;
    } else {
      text = `${diffMinutes}m`;
    }

    return {
      text: `Expires in ${text}`,
      color: 'text-yellow-600',
      expired: false,
      fullDate: formatDate(retainUntilDate)
    };
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
            onClick={() => navigate('/buckets')}
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
            onClick={() => setIsPermissionsModalOpen(true)}
            variant="outline"
            className="gap-2"
          >
            <ShieldIcon className="h-4 w-4" />
            Permissions
          </Button>
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
            onClick={() => window.location.href = `${bucketPath}/settings`}
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
              {(bucketData?.object_count || 0).toLocaleString()}
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
              {formatSize(bucketData?.size || 0)}
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
              {bucketData?.region || 'us-east-1'}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Object Lock Banner */}
      {bucketData?.objectLock?.objectLockEnabled && (
        <Card className="border-blue-200 bg-blue-50">
          <CardContent className="pt-6">
            <div className="flex items-start gap-4">
              <div className="flex-shrink-0">
                <div className="h-10 w-10 rounded-full bg-blue-100 flex items-center justify-center">
                  <LockIcon className="h-5 w-5 text-blue-600" />
                </div>
              </div>
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-2">
                  <h3 className="text-lg font-semibold text-blue-900">Object Lock Enabled (WORM)</h3>
                  {bucketData.objectLock.rule?.defaultRetention && (
                    <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800">
                      <ShieldIcon className="h-3 w-3" />
                      {bucketData.objectLock.rule.defaultRetention.mode}
                    </span>
                  )}
                </div>
                <p className="text-sm text-blue-800">
                  This bucket has WORM (Write Once Read Many) protection enabled. Objects are immutable and cannot be deleted until their retention expires.
                </p>
                {bucketData.objectLock.rule?.defaultRetention && (
                  <div className="mt-3 flex items-center gap-4 text-sm">
                    <div className="flex items-center gap-2 text-blue-700">
                      <ClockIcon className="h-4 w-4" />
                      <span className="font-medium">Default retention:</span>
                      <span>
                        {bucketData.objectLock.rule.defaultRetention.days
                          ? `${bucketData.objectLock.rule.defaultRetention.days} day${bucketData.objectLock.rule.defaultRetention.days !== 1 ? 's' : ''}`
                          : bucketData.objectLock.rule.defaultRetention.years
                          ? `${bucketData.objectLock.rule.defaultRetention.years} year${bucketData.objectLock.rule.defaultRetention.years !== 1 ? 's' : ''}`
                          : 'Not specified'
                        }
                      </span>
                    </div>
                    <div className="text-blue-600 text-xs">
                      {bucketData.objectLock.rule.defaultRetention.mode === 'COMPLIANCE'
                        ? '⚠️ COMPLIANCE: Cannot be deleted under any circumstances'
                        : '⚠️ GOVERNANCE: Requires special permissions to delete'
                      }
                    </div>
                  </div>
                )}
              </div>
            </div>
          </CardContent>
        </Card>
      )}

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
          <div className="flex items-center justify-between">
            <CardTitle>
              Objects ({filteredItems.length})
              {currentPrefix && ` in ${currentPrefix}`}
            </CardTitle>
            {selectedObjects.size > 0 && (
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">
                  {selectedObjects.size} selected
                </span>
                <Button
                  onClick={handleBulkDelete}
                  variant="destructive"
                  size="sm"
                  className="gap-2"
                >
                  <Trash2Icon className="h-4 w-4" />
                  Delete selected
                </Button>
              </div>
            )}
          </div>
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
                  <TableHead className="w-12">
                    <input
                      type="checkbox"
                      checked={selectedObjects.size === filteredItems.length && filteredItems.length > 0}
                      onChange={toggleSelectAll}
                      className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                    />
                  </TableHead>
                  <TableHead>Name</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead>Type</TableHead>
                  {bucketData?.objectLock?.objectLockEnabled && (
                    <TableHead>Retention</TableHead>
                  )}
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredItems.map((item) => (
                  <TableRow key={item.key}>
                    <TableCell>
                      <input
                        type="checkbox"
                        checked={selectedObjects.has(item.key)}
                        onChange={() => toggleObjectSelection(item.key)}
                        onClick={(e) => e.stopPropagation()}
                        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                      />
                    </TableCell>
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
                            {sharesMap[item.key] && (
                              <span title="This object is shared">
                                <Share2Icon className="h-4 w-4 text-green-600" />
                              </span>
                            )}
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
                    {bucketData?.objectLock?.objectLockEnabled && (
                      <TableCell>
                        {(() => {
                          if (isFolder(item)) {
                            return <span className="text-gray-400">-</span>;
                          }

                          if ('retention' in item && item.retention) {
                            const retentionInfo = formatRetentionExpiration(item.retention.retainUntilDate);
                            return retentionInfo ? (
                              <div className="flex items-center gap-1" title={retentionInfo.fullDate}>
                                <LockIcon className={`h-3 w-3 ${retentionInfo.color}`} />
                                <span className={`text-xs font-medium ${retentionInfo.color}`}>
                                  {retentionInfo.text}
                                </span>
                              </div>
                            ) : (
                              <span className="text-gray-400">-</span>
                            );
                          }

                          return <span className="text-gray-400">No retention</span>;
                        })()}
                      </TableCell>
                    )}
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!isFolder(item) && (
                          <>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleDownloadObject(item.key)}
                              title="Download"
                            >
                              <DownloadIcon className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleShareObject(item.key)}
                              title={sharesMap[item.key] ? "View/Copy share link" : "Share"}
                              className={sharesMap[item.key] ? "text-green-600 hover:text-green-700" : ""}
                            >
                              <Share2Icon className="h-4 w-4" />
                            </Button>
                          </>
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

      {/* Permissions Modal */}
      <BucketPermissionsModal
        isOpen={isPermissionsModalOpen}
        onClose={() => setIsPermissionsModalOpen(false)}
        bucketName={bucketName}
      />
    </div>
  );
}
