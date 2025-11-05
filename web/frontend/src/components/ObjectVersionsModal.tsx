import React from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { Download as DownloadIcon, Trash2 as Trash2Icon, RotateCcw as RotateCcwIcon } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

interface ObjectVersionsModalProps {
  isOpen: boolean;
  onClose: () => void;
  bucketName: string;
  objectKey: string;
  tenantId?: string;
}

export function ObjectVersionsModal({
  isOpen,
  onClose,
  bucketName,
  objectKey,
  tenantId,
}: ObjectVersionsModalProps) {
  const queryClient = useQueryClient();

  const { data: versionsData, isLoading } = useQuery({
    queryKey: ['objectVersions', bucketName, objectKey, tenantId],
    queryFn: () => APIClient.listObjectVersions(bucketName, objectKey, tenantId),
    enabled: isOpen,
  });

  const deleteVersionMutation = useMutation({
    mutationFn: ({ versionId, isLatest }: { versionId: string; isLatest: boolean }) => {
      // If deleting the latest version (and not a delete marker), create a Delete Marker by not passing versionId
      // Otherwise, permanently delete the specific version
      if (isLatest) {
        return APIClient.deleteObject(bucketName, objectKey, tenantId); // No versionId = Create Delete Marker
      } else {
        return APIClient.deleteObject(bucketName, objectKey, tenantId, versionId); // With versionId = Permanent delete
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['objectVersions', bucketName, objectKey] });
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      SweetAlert.toast('success', 'Version deleted successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const handleDeleteVersion = async (versionId: string, isDeleteMarker: boolean, isLatest: boolean) => {
    const result = await SweetAlert.fire({
      icon: 'warning',
      title: `Delete ${isDeleteMarker ? 'Delete Marker' : 'Version'}?`,
      html: isDeleteMarker
        ? `<p>Deleting this Delete Marker will <strong>restore</strong> the previous version of the object.</p>`
        : isLatest
          ? `<p>This will create a <strong>Delete Marker</strong> for this object.</p><p class="text-gray-600 mt-2">The version will still exist and can be recovered</p>`
          : `<p>This will <strong>permanently</strong> delete this version.</p><p class="text-red-600 mt-2">This action cannot be undone</p>`,
      showCancelButton: true,
      confirmButtonText: isDeleteMarker ? 'Yes, restore object' : isLatest ? 'Yes, mark as deleted' : 'Yes, delete permanently',
      cancelButtonText: 'Cancel',
      confirmButtonColor: isDeleteMarker ? '#10b981' : '#dc2626',
    });

    if (result.isConfirmed) {
      deleteVersionMutation.mutate({ versionId, isLatest });
    }
  };

  const handleDownloadVersion = async (versionId: string) => {
    try {
      SweetAlert.loading('Downloading version...', `Downloading version ${versionId}`);

      const blob = await APIClient.downloadObject({
        bucket: bucketName,
        key: objectKey,
        tenantId,
        versionId,
      });

      SweetAlert.close();

      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = objectKey.split('/').pop() || objectKey;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);

      SweetAlert.toast('success', 'Version downloaded successfully');
    } catch (error: any) {
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
    return new Date(dateString).toLocaleString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const allVersions = [
    ...(versionsData?.versions || []),
    ...(versionsData?.deleteMarkers || []),
  ].sort((a, b) => new Date(b.lastModified).getTime() - new Date(a.lastModified).getTime());

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={`Versions: ${objectKey}`} size="xl">
      <div className="space-y-4">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loading size="md" />
          </div>
        ) : allVersions.length === 0 ? (
          <div className="text-center py-8">
            <p className="text-gray-500">No versions found</p>
          </div>
        ) : (
          <div className="overflow-x-auto max-h-96">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Version ID</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {allVersions.map((version) => (
                  <TableRow key={version.versionId}>
                    <TableCell className="font-mono text-xs">
                      {version.versionId.substring(0, 16)}...
                    </TableCell>
                    <TableCell className="text-sm">
                      {formatDate(version.lastModified)}
                    </TableCell>
                    <TableCell>
                      {version.isDeleteMarker ? '-' : formatSize(version.size)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {version.isLatest && (
                          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800">
                            Latest
                          </span>
                        )}
                        {version.isDeleteMarker && (
                          <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-100 text-red-800">
                            Delete Marker
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        {!version.isDeleteMarker && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleDownloadVersion(version.versionId)}
                            title="Download this version"
                          >
                            <DownloadIcon className="h-4 w-4" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteVersion(version.versionId, !!version.isDeleteMarker, !!version.isLatest)}
                          disabled={deleteVersionMutation.isPending}
                          title={version.isDeleteMarker ? 'Delete marker (restores object)' : version.isLatest ? 'Create delete marker' : 'Delete this version permanently'}
                          className={version.isDeleteMarker ? 'text-green-600 hover:text-green-700' : 'text-red-600 hover:text-red-700'}
                        >
                          {version.isDeleteMarker ? <RotateCcwIcon className="h-4 w-4" /> : <Trash2Icon className="h-4 w-4" />}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        <div className="flex justify-end gap-2 pt-4 border-t">
          <Button variant="outline" onClick={onClose}>
            Close
          </Button>
        </div>
      </div>
    </Modal>
  );
}
