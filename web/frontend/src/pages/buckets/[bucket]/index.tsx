/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useState, useCallback, useEffect, useRef } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { Upload as UploadIcon } from 'lucide-react';
import { Download as DownloadIcon } from 'lucide-react';
import { Search as SearchIcon } from 'lucide-react';
import { Globe as GlobeIcon } from 'lucide-react';
import { Settings as SettingsIcon } from 'lucide-react';
import { Trash2 as Trash2Icon } from 'lucide-react';
import { File as FileIcon } from 'lucide-react';
import { Folder as FolderIcon, FolderOpen, FolderDown as FolderDownIcon } from 'lucide-react';
import { Calendar as CalendarIcon } from 'lucide-react';
import { HardDrive as HardDriveIcon } from 'lucide-react';
import { Lock as LockIcon } from 'lucide-react';
import { Shield as ShieldIcon } from 'lucide-react';
import { Share2 as Share2Icon } from 'lucide-react';
import { History as HistoryIcon } from 'lucide-react';
import { Link as LinkIcon } from 'lucide-react';
import { Filter as FilterIcon } from 'lucide-react';
import { RefreshCw as RefreshCwIcon } from 'lucide-react';
import { ChevronDown as ChevronDownIcon, ChevronRight as ChevronRightIcon } from 'lucide-react';
import { Copy as CopyIcon } from 'lucide-react';
import { Pencil as PencilIcon } from 'lucide-react';
import { Tag as TagIcon } from 'lucide-react';
import { Sigma as SigmaIcon } from 'lucide-react';
import { ExternalLink as ExternalLinkIcon } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient, s3Client } from '@/lib/api';
import { ObjectSearchFilter } from '@/types';
import ModalManager from '@/lib/modals';
import { isHttpStatus, escapeHtml } from '@/lib/utils';
import { BucketPermissionsModal } from '@/components/BucketPermissionsModal';
import { ObjectVersionsModal } from '@/components/ObjectVersionsModal';
import { PresignedURLModal } from '@/components/PresignedURLModal';
import { ObjectDetailsView, ObjectViewCallbacks } from '@/components/ObjectDetailsView';
import { ObjectFilterPanel } from '@/components/ObjectFilterPanel';
import { useAuth } from '@/hooks/useAuth';
import { useTranslation } from 'react-i18next';

// Helper function for responsive modal widths
const getResponsiveModalWidth = (baseWidth: number = 650): string => {
  const width = window.innerWidth;
  if (width >= 3840) return `${baseWidth * 1.4}px`; // 4K
  if (width >= 2560) return `${baseWidth * 1.2}px`; // 2K
  if (width >= 1920) return `${baseWidth * 1.1}px`; // Full HD+
  return `${baseWidth}px`;
};

export default function BucketDetailsPage() {
  const { t } = useTranslation('buckets');
  const { bucket } = useParams<{ bucket: string }>();
  const location = useLocation();
  const tenantId = (location.state as any)?.tenantId || undefined;
  const navigate = useNavigate();
  const { user } = useAuth();
  const bucketName = bucket as string;
  const bucketPath = `/buckets/${bucketName}`;
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPrefix, setCurrentPrefix] = useState('');
  const [isUploadModalOpen, setIsUploadModalOpen] = useState(false);
  const [isCreateFolderModalOpen, setIsCreateFolderModalOpen] = useState(false);
  const [isPermissionsModalOpen, setIsPermissionsModalOpen] = useState(false);
  const [isVersionsModalOpen, setIsVersionsModalOpen] = useState(false);
  const [isPresignedURLModalOpen, setIsPresignedURLModalOpen] = useState(false);
  const [detailsObjectKey, setDetailsObjectKey] = useState<string>('');
  const [detailsObjectData, setDetailsObjectData] = useState<Record<string, any>>({});
  // ref so mutation callbacks (closures) can check if we're in detail view
  const detailsObjectKeyRef = useRef<string>('');
  const [selectedObjectKey, setSelectedObjectKey] = useState<string>('');
  const [newFolderName, setNewFolderName] = useState('');
  const [uploadMode, setUploadMode] = useState<'files' | 'folder'>('files');
  const [uploadFiles, setUploadFiles] = useState<Array<{ file: File; path: string }>>([]);
  const [isFolderScanning, setIsFolderScanning] = useState(false);
  const [selectedObjects, setSelectedObjects] = useState<Set<string>>(new Set());
  const [objectFilter, setObjectFilter] = useState<ObjectSearchFilter>({});
  const [isFilterPanelOpen, setIsFilterPanelOpen] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const [actionsDropUp, setActionsDropUp] = useState(false);
  const actionsRef = useRef<HTMLDivElement>(null);
  const [isRenameModalOpen, setIsRenameModalOpen] = useState(false);
  const [renameKey, setRenameKey] = useState('');
  const [renameNewName, setRenameNewName] = useState('');
  const [isEditTagsModalOpen, setIsEditTagsModalOpen] = useState(false);
  const [editTagsKey, setEditTagsKey] = useState('');
  const [editTags, setEditTags] = useState<Array<{ key: string; value: string }>>([]);
  const [showVersions, setShowVersions] = useState(false);
  const queryClient = useQueryClient();

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (actionsRef.current && !actionsRef.current.contains(e.target as Node)) {
        setActionsOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Keep ref in sync so mutation callbacks (closures) can read current value
  useEffect(() => { detailsObjectKeyRef.current = detailsObjectKey; }, [detailsObjectKey]);

  // Check if user is global admin (no tenantId) accessing a tenant bucket
  // Global admins should only have read-only access to tenant buckets
  const isGlobalAdminInTenantBucket = user && !user.tenantId && !!tenantId;

  const { data: bucketData, isLoading: bucketLoading } = useQuery({
    queryKey: ['bucket', bucketName, tenantId],
    queryFn: () => APIClient.getBucket(bucketName, tenantId),
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  });

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true);
    await queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
    await queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
    await queryClient.invalidateQueries({ queryKey: ['shares', bucketName, tenantId] });
    setTimeout(() => setIsRefreshing(false), 600);
  }, [bucketName, tenantId, queryClient]);

  const hasActiveFilters = !!(
    (objectFilter.contentTypes && objectFilter.contentTypes.length > 0) ||
    objectFilter.minSize !== undefined ||
    objectFilter.maxSize !== undefined ||
    objectFilter.modifiedAfter ||
    objectFilter.modifiedBefore ||
    (objectFilter.tags && Object.keys(objectFilter.tags).length > 0)
  );

  const activeFilterCount = [
    objectFilter.contentTypes && objectFilter.contentTypes.length > 0,
    objectFilter.minSize !== undefined,
    objectFilter.maxSize !== undefined,
    objectFilter.modifiedAfter,
    objectFilter.modifiedBefore,
    objectFilter.tags && Object.keys(objectFilter.tags).length > 0,
  ].filter(Boolean).length;

  const { data: objectsResponse, isLoading: objectsLoading } = useQuery({
    queryKey: ['objects', bucketName, currentPrefix, tenantId, hasActiveFilters ? objectFilter : null],
    queryFn: () => hasActiveFilters
      ? APIClient.searchObjects({
          bucket: bucketName,
          ...(tenantId && { tenantId }),
          prefix: currentPrefix,
          delimiter: '/',
          filter: objectFilter,
        })
      : APIClient.getObjects({
          bucket: bucketName,
          ...(tenantId && { tenantId }),
          prefix: currentPrefix,
          delimiter: '/',
        }),
  });

  const { data: sharesMap = {} } = useQuery({
    queryKey: ['shares', bucketName, tenantId],
    queryFn: () => APIClient.getBucketShares(bucketName, tenantId),
  });

  const { data: versionsData, isLoading: versionsLoading } = useQuery({
    queryKey: ['bucket-versions', bucketName, currentPrefix, tenantId],
    queryFn: () => APIClient.listBucketVersions(bucketName, currentPrefix || undefined, tenantId),
    enabled: showVersions,
  });

  // Server config and website — needed for bucket info in Actions dropdown
  const { data: serverConfig } = useQuery({
    queryKey: ['server-config'],
    queryFn: APIClient.getServerConfig,
    staleTime: 5 * 60 * 1000,
  });
  const { data: websiteConfig } = useQuery({
    queryKey: ['bucket-website', bucketName, tenantId],
    queryFn: () => APIClient.getBucketWebsite(bucketName, tenantId),
    retry: false,
    staleTime: 60 * 1000,
  });

  // Derived bucket info values for the Actions dropdown
  const s3BaseUrl = s3Client.defaults.baseURL as string;
  const bucketS3Uri = `s3://${bucketName}`;
  const bucketArn = `arn:aws:s3:::${bucketName}`;
  const bucketEndpointUrl = `${s3BaseUrl}/${bucketName}`;
  const websiteHostname = serverConfig?.server?.websiteHostname;
  const bucketWebsiteUrl = websiteHostname && websiteConfig?.indexDocument
    ? `${window.location.protocol}//${bucketName}.${websiteHostname}`
    : null;

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
      // Invalidate ALL object queries for this bucket (any prefix)
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      // Invalidate bucket metadata with specific tenantId
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      // Invalidate buckets list to update counters
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      setIsCreateFolderModalOpen(false);
      setNewFolderName('');
      ModalManager.toast('success', `Folder "${newFolderName}" created successfully`);
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Run at most `limit` async tasks concurrently from `items`.
  const runConcurrent = async <T,>(
    items: T[],
    fn: (item: T) => Promise<void>,
    limit: number
  ): Promise<void> => {
    let idx = 0;
    const worker = async () => {
      while (idx < items.length) {
        const item = items[idx++];
        await fn(item);
      }
    };
    await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  };

  // Recursively deletes a folder and all its contents at any depth.
  //
  // Uses delimiter="/" so that implicit folder markers (which are invisible to
  // flat/no-delimiter listings) appear as commonPrefixes and can be recursed into.
  // Concurrency is capped at CONCURRENT_DELETES to avoid overwhelming SQLite
  // with simultaneous write transactions on large bulk operations.
  const CONCURRENT_DELETES = 8;
  const deleteFolderRecursive = async (bucket: string, folderKey: string): Promise<void> => {
    let marker = '';
    while (true) {
      const page = await APIClient.getObjects({
        bucket,
        ...(tenantId && { tenantId }),
        prefix: folderKey,
        delimiter: '/',
        maxKeys: 1000,
        ...(marker && { continuationToken: marker }),
      });

      // Recurse into subfolders with bounded concurrency
      const subfolders = page?.commonPrefixes || [];
      if (subfolders.length > 0) {
        await runConcurrent(subfolders, sub => deleteFolderRecursive(bucket, sub), CONCURRENT_DELETES);
      }

      // Delete objects at this level with bounded concurrency
      const objects = (page?.objects || []).filter(obj =>
        obj.key !== folderKey &&
        !obj.key.includes('.maxiofs-') &&
        !obj.key.endsWith('.metadata')
      );
      if (objects.length > 0) {
        await runConcurrent(
          objects,
          obj => APIClient.deleteObject(bucket, obj.key, tenantId),
          CONCURRENT_DELETES
        );
      }

      if (!page?.isTruncated || !page?.nextContinuationToken) break;
      marker = page.nextContinuationToken;
    }

    // Finally delete the folder marker itself (ignore 404 — virtual folders have none)
    try {
      await APIClient.deleteObject(bucket, folderKey, tenantId);
    } catch (error: unknown) {
      if (!isHttpStatus(error, 404)) throw error;
    }
  };

  const deleteObjectMutation = useMutation({
    mutationFn: async ({ bucket, key }: { bucket: string; key: string }) => {
      if (key.endsWith('/')) {
        return deleteFolderRecursive(bucket, key);
      }
      return APIClient.deleteObject(bucket, key, tenantId);
    },
    onSuccess: () => {
      ModalManager.close();
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      ModalManager.toast('success', 'Object deleted successfully');
      // If we were in the object detail view, go back to the list
      if (detailsObjectKeyRef.current) {
        setDetailsObjectKey('');
        setDetailsObjectData({});
      }
    },
    onError: (error: Error) => {
      ModalManager.close();
      ModalManager.apiError(error);
    },
  });

  const deleteShareMutation = useMutation({
    mutationFn: ({ bucket, key }: { bucket: string; key: string }) => APIClient.deleteShare(bucket, key, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['shares', bucketName, tenantId] });
      ModalManager.toast('success', 'Share deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const toggleLegalHoldMutation = useMutation({
    mutationFn: ({ key, enabled }: { key: string; enabled: boolean }) =>
      APIClient.putObjectLegalHold(bucketName, key, enabled, tenantId),
    onSuccess: (_, variables) => {
      // Invalidate objects to refresh Legal Hold status
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      ModalManager.toast('success', `Legal Hold ${variables.enabled ? 'enabled' : 'disabled'} successfully`);
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
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

  // Normalize API response fields:
  // - console API returns snake_case (e.g. last_modified)
  // - UI uses camelCase (lastModified)
  // We keep other fields untouched to avoid breaking other UI logic.
  const normalizedFilteredObjects = filteredObjects.map((obj: any) => ({
    ...obj,
    lastModified: obj.lastModified ?? obj.last_modified ?? '',
    storageClass: obj.storageClass ?? obj.storage_class ?? '',
  }));

  const allItems = [
    ...commonPrefixes.map(prefix => ({
      key: prefix,
      isFolder: true,
      size: 0,
      lastModified: '',
      etag: '',
      storageClass: '',
    })),
    ...normalizedFilteredObjects,
  ];

  const filteredItems = allItems.filter(item =>
    item.key.toLowerCase().includes(searchTerm.toLowerCase())
  );

  // Traverse a FileSystemEntry tree (drag & drop API — works in all browsers, no dialogs)
  const traverseEntry = (entry: any, prefix: string): Promise<Array<{ file: File; path: string }>> => {
    return new Promise((resolve) => {
      if (entry.isFile) {
        entry.getFile((file: File) => {
          resolve([{ file, path: prefix ? `${prefix}/${entry.name}` : entry.name }]);
        });
      } else if (entry.isDirectory) {
        const reader = entry.createReader();
        const results: Array<{ file: File; path: string }> = [];
        const dirPath = prefix ? `${prefix}/${entry.name}` : entry.name;

        // readEntries returns max 100 at a time — must loop until empty
        const readAll = () => {
          reader.readEntries(async (entries: any[]) => {
            if (entries.length === 0) {
              resolve(results);
              return;
            }
            for (const child of entries) {
              const sub = await traverseEntry(child, dirPath);
              results.push(...sub);
            }
            readAll();
          });
        };
        readAll();
      } else {
        resolve([]);
      }
    });
  };

  // Traverse FileSystemDirectoryHandle tree (showDirectoryPicker API — Chrome/Edge/Safari)
  const traverseHandle = async (
    handle: any,
    prefix: string
  ): Promise<Array<{ file: File; path: string }>> => {
    const results: Array<{ file: File; path: string }> = [];
    for await (const entry of handle.values()) {
      const entryPath = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.kind === 'file') {
        results.push({ file: await entry.getFile(), path: entryPath });
      } else if (entry.kind === 'directory') {
        results.push(...await traverseHandle(entry, entryPath));
      }
    }
    return results;
  };

  const handleBrowseFolder = async () => {
    try {
      setIsFolderScanning(true);
      const dirHandle = await (window as any).showDirectoryPicker();
      const files = await traverseHandle(dirHandle, dirHandle.name);
      setUploadFiles(files);
    } catch (err: any) {
      if (err?.name !== 'AbortError') console.warn('[FolderUpload]', err);
    } finally {
      setIsFolderScanning(false);
    }
  };

  const handleFolderDrop = async (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsFolderScanning(true);
    const items = Array.from(e.dataTransfer.items);
    const results: Array<{ file: File; path: string }> = [];
    for (const item of items) {
      const entry = item.webkitGetAsEntry?.();
      if (entry) {
        const files = await traverseEntry(entry, '');
        results.push(...files);
      }
    }
    setUploadFiles(results);
    setIsFolderScanning(false);
  };

  const resetUploadModal = () => {
    setIsUploadModalOpen(false);
    setUploadFiles([]);
    setUploadMode('files');
  };

  const handleUpload = (e: React.FormEvent) => {
    e.preventDefault();
    if (uploadFiles.length === 0) return;

    const totalFiles = uploadFiles.length;
    const files = [...uploadFiles];

    // Close modal immediately so the user can keep working
    resetUploadModal();

    // Start background task — progress bar appears bottom-right
    const taskId = ModalManager.startBgTask(
      `Uploading ${totalFiles} file${totalFiles !== 1 ? 's' : ''}`,
      totalFiles
    );

    // Run uploads in background (fire-and-forget)
    (async () => {
      let successCount = 0;
      let failCount = 0;

      for (const { file, path } of files) {
        const key = currentPrefix
          ? `${currentPrefix.replace(/\/$/, '')}/${path}`
          : path;

        try {
          await APIClient.uploadObject({
            bucket: bucketName,
            ...(tenantId && { tenantId }),
            key,
            file,
            onProgress: ({ percentage }) => {
              ModalManager.updateBgTaskProgress(taskId, percentage);
            },
          });
          successCount++;
        } catch {
          failCount++;
        }
        // tick resets subPct to 0, ready for the next file
        ModalManager.tickBgTask(taskId, successCount, failCount);
      }

      ModalManager.finishBgTask(taskId, successCount, failCount);

      if (successCount > 0) {
        queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
        queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
        queryClient.invalidateQueries({ queryKey: ['buckets'] });
      }
    })();
  };

  const handleCreateFolder = (e: React.FormEvent) => {
    e.preventDefault();
    if (newFolderName.trim()) {
      createFolderMutation.mutate(newFolderName.trim());
    }
  };

  const handleDeleteObject = async (key: string, isFolder: boolean) => {
    const itemTypeLabel = isFolder ? t('folderType') : t('fileType', 'file');

    // Block deletion if the object is under active retention or legal hold
    if (!isFolder) {
      const item = filteredItems.find(i => i.key === key);
      if (item && isObjectLocked(item)) {
        const hasLegalHold = 'legalHold' in item && (item as any).legalHold?.status === 'ON';
        ModalManager.toast('error', hasLegalHold
          ? t('deleteBlockedLegalHold')
          : t('deleteBlockedRetention'));
        return;
      }
    }

    try {
      const result = await ModalManager.fire({
        icon: 'warning',
        title: isFolder ? t('deleteFolderTitle') : t('deleteFileTitle'),
        html: isFolder
          ? `<p>${t('deleteFolderConfirm', { key: escapeHtml(key) })}</p>
             <p class="text-red-600 mt-2">${t('deleteFolderRecursiveWarning')}</p>`
          : `<p>${t('deleteFileConfirm', { key: escapeHtml(key) })}</p>
             <p class="text-red-600 mt-2">${t('deleteIrreversibleWarning')}</p>`,
        showCancelButton: true,
        confirmButtonText: t('confirmDelete'),
        cancelButtonText: t('cancel'),
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        ModalManager.loading(t('deletingItem', { itemType: itemTypeLabel }), t('deletingKey', { key }));
        deleteObjectMutation.mutate({ bucket: bucketName, key });
      }
    } catch (error) {
      ModalManager.apiError(error);
    }
  };

  const handleDownloadObject = async (key: string) => {
    try {
      // Show download indicator
      ModalManager.loading(t('downloadingFile'), t('downloadingKey', { key }));

      const blob = await APIClient.downloadObject({
        bucket: bucketName,
        ...(tenantId && { tenantId }),
        key,
      });

      // Close loading indicator
      ModalManager.close();

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
      ModalManager.successDownload(key.split('/').pop() || key);
    } catch (error: unknown) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const handleDownloadFolderZip = async (key: string) => {
    const folderName = key.replace(/\/$/, '').split('/').pop() || key;
    try {
      ModalManager.loading(t('downloadingFolder'), t('downloadingFolderKey', { prefix: folderName }));
      const blob = await APIClient.downloadFolderAsZip(bucketName, key, tenantId);
      ModalManager.close();
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `${folderName}.zip`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
      ModalManager.successDownload(`${folderName}.zip`);
    } catch (error: unknown) {
      ModalManager.close();
      ModalManager.apiError(error);
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

        const result = await ModalManager.fire({
          icon: 'info',
          title: 'Object Already Shared',
          html: `
            <div class="text-left space-y-4">
              <p class="text-gray-700">This object is already shared. You can:</p>
              <div>
                <p class="text-sm font-medium mb-2">Share this link:</p>
                <div class="bg-gray-50 p-3 rounded border border-gray-200">
                  <code class="text-xs break-all">${escapeHtml(shareData.url)}</code>
                </div>
              </div>
              <div class="text-sm text-gray-600">
                ${expirationInfo}
                <p><strong>Created:</strong> ${escapeHtml(new Date(shareData.createdAt).toLocaleString())}</p>
              </div>
            </div>
          `,
          showCancelButton: true,
          showDenyButton: true,
          confirmButtonText: 'Copy Link',
          denyButtonText: 'Unshare',
          cancelButtonText: 'Close',
          width: getResponsiveModalWidth(650),
        });

        if (result.isConfirmed) {
          navigator.clipboard.writeText(shareData.url);
          ModalManager.toast('success', t('linkCopied'));
        } else if (result.isDenied) {
          // Unshare the object
          const confirmDelete = await ModalManager.fire({
            icon: 'warning',
            title: t('unshareObject'),
            text: t('unshareObjectText'),
            showCancelButton: true,
            confirmButtonText: t('yesUnshare'),
            cancelButtonText: t('cancel'),
            confirmButtonColor: '#dc2626',
          });

          if (confirmDelete.isConfirmed) {
            deleteShareMutation.mutate({ bucket: bucketName, key });
          }
        }
        return;
      }

      // Object is not shared yet - show create dialog
      const result = await ModalManager.fire({
        icon: 'info',
        title: t('shareObject'),
        html: `
          <p class="mb-4">Generate a shareable link for <strong>"${escapeHtml(key.split('/').pop() ?? '')}"</strong></p>
          <div class="text-left">
            <label for="expiresIn" class="block text-sm font-medium mb-2">${t('linkExpiresIn')}</label>
            <select id="expiresIn" class="w-full px-3 py-2 border border-gray-300 rounded-md">
              <option value="0">${t('never')}</option>
              <option value="3600">1 hour</option>
              <option value="21600">6 hours</option>
              <option value="43200">12 hours</option>
              <option value="86400" selected>24 hours</option>
              <option value="604800">7 days</option>
            </select>
          </div>
        `,
        showCancelButton: true,
        confirmButtonText: t('generateLink'),
        cancelButtonText: t('cancel'),
        preConfirm: () => {
          const select = document.getElementById('expiresIn') as HTMLSelectElement;
          const value = parseInt(select.value);
          return value === 0 ? null : value;
        }
      });

      if (!result.isConfirmed) return;

      const expiresIn = result.value as number | null;

      // Show loading indicator
      ModalManager.loading(t('generatingLink'), t('creatingLinkFor', { key }));

      const shareData = await APIClient.shareObject(bucketName, key, expiresIn, tenantId);

      // Refresh shares list
      queryClient.invalidateQueries({ queryKey: ['shares', bucketName, tenantId] });

      // Close loading indicator
      ModalManager.close();

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

      await ModalManager.fire({
        icon: 'success',
        title: t('shareableLinkCreated'),
        html: `
          <div class="text-left space-y-4">
            <div class="flex items-center gap-2 mb-2">
              ${statusBadge}
            </div>
            <div>
              <p class="text-sm font-medium mb-2">${t('shareThisLink')}</p>
              <div class="bg-gray-50 p-3 rounded border border-gray-200">
                <code class="text-xs break-all">${escapeHtml(shareData.url)}</code>
              </div>
            </div>
            <div class="text-sm text-gray-600">
              ${expirationInfo}
              <p><strong>Created:</strong> ${escapeHtml(new Date(shareData.createdAt).toLocaleString())}</p>
            </div>
            <div class="bg-blue-50 border border-blue-200 rounded p-3 text-sm text-blue-800">
              <strong>ℹ️ Note:</strong> ${t('noteAnyone', { expiry: shareData.expiresAt ? t('noteExpiry') : t('noteNoExpiry') })}
            </div>
          </div>
        `,
        showCancelButton: true,
        confirmButtonText: t('copyLink'),
        cancelButtonText: t('cancel'),
        width: '650px',
      }).then((copyResult) => {
        if (copyResult.isConfirmed) {
          navigator.clipboard.writeText(shareData.url);
          ModalManager.toast('success', 'Link copied to clipboard');
        }
      });
    } catch (error: unknown) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const handleViewVersions = (key: string) => {
    setSelectedObjectKey(key);
    setIsVersionsModalOpen(true);
  };

  const handleViewObjectDetails = (key: string, data: Record<string, any>) => {
    setDetailsObjectKey(key);
    setDetailsObjectData(data);
  };

  const handleGeneratePresignedURL = (key: string) => {
    setSelectedObjectKey(key);
    setIsPresignedURLModalOpen(true);
  };

  const handleToggleLegalHold = async (key: string, currentStatus: boolean) => {
    const action = currentStatus ? 'disable' : 'enable';
    const result = await ModalManager.fire({
      icon: 'warning',
      title: `${action === 'enable' ? 'Enable' : 'Disable'} Legal Hold?`,
      text: `Are you sure you want to ${action} Legal Hold on this object? ${
        action === 'enable'
          ? 'The object will not be deletable until Legal Hold is removed.'
          : 'The object will become deletable again (if not under retention).'
      }`,
      showCancelButton: true,
      confirmButtonText: action === 'enable' ? 'Enable' : 'Disable',
      cancelButtonText: 'Cancel',
    });

    if (result.isConfirmed) {
      toggleLegalHoldMutation.mutate({ key, enabled: !currentStatus });
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
    if (selectedObjects.size === filteredItems.length) {
      // Deselect all
      setSelectedObjects(new Set());
    } else {
      // Select all — includes both files and folders
      const allKeys = new Set(filteredItems.map(item => item.key));
      setSelectedObjects(allKeys);
    }
  };

  const handleBulkDelete = async () => {
    if (selectedObjects.size === 0) return;

    // Separate locked from deletable items
    const allSelected = filteredItems.filter(item => selectedObjects.has(item.key));
    const lockedItems = allSelected.filter(isObjectLocked);
    const deletableItems = allSelected.filter(item => !isObjectLocked(item));

    if (lockedItems.length === allSelected.length) {
      // Everything is locked — nothing to delete
      ModalManager.toast('error', t('deleteBlockedAllLocked'));
      return;
    }

    const total = deletableItems.length;
    const lockedNote = lockedItems.length > 0
      ? `<p class="text-yellow-600 mt-2">${lockedItems.length} item${lockedItems.length !== 1 ? 's are' : ' is'} protected by Object Lock and will be skipped.</p>`
      : '';

    const result = await ModalManager.fire({
      icon: 'warning',
      title: `Delete ${total} item${total !== 1 ? 's' : ''}?`,
      html: `<p>You are about to delete <strong>${total}</strong> item${total !== 1 ? 's' : ''}</p>
             <p class="text-red-600 mt-2">This action cannot be undone</p>
             ${lockedNote}`,
      showCancelButton: true,
      confirmButtonText: 'Yes, delete',
      cancelButtonText: 'Cancel',
      confirmButtonColor: '#dc2626',
    });

    if (!result.isConfirmed) return;

    // Only delete non-locked items; clear full selection
    const selectedArray = deletableItems.map(item => item.key);
    setSelectedObjects(new Set());

    // Start background task — dialog is now closed, bar appears bottom-right
    const taskId = ModalManager.startBgTask(
      `Deleting ${total} item${total !== 1 ? 's' : ''}`,
      total
    );

    let successCount = 0;
    let failCount = 0;

    await runConcurrent(
      selectedArray,
      async (key: string) => {
        try {
          if (key.endsWith('/')) {
            await deleteFolderRecursive(bucketName, key);
          } else {
            await APIClient.deleteObject(bucketName, key, tenantId);
          }
          successCount++;
        } catch {
          failCount++;
        }
        ModalManager.tickBgTask(taskId, successCount, failCount);
      },
      CONCURRENT_DELETES
    );

    ModalManager.finishBgTask(taskId, successCount, failCount);

    // Refresh queries after completion
    if (successCount > 0) {
      queryClient.refetchQueries({ queryKey: ['objects', bucketName] });
      queryClient.refetchQueries({ queryKey: ['bucket', bucketName, tenantId] });
      queryClient.refetchQueries({ queryKey: ['buckets'] });
    }
  };

  const handleCopyS3Uri = (key: string) => {
    navigator.clipboard.writeText(`s3://${bucketName}/${key}`);
    ModalManager.toast('success', t('copyS3UriSuccess'));
    setActionsOpen(false);
  };

  const handleCopyObjectUrl = (key: string) => {
    navigator.clipboard.writeText(APIClient.getObjectUrl(bucketName, key));
    ModalManager.toast('success', t('copyObjectUrlSuccess'));
    setActionsOpen(false);
  };

  const handleCalculateFolderSize = async (key: string) => {
    setActionsOpen(false);
    try {
      ModalManager.loading(t('calculatingSize'), key);
      const result = await APIClient.getFolderSize(bucketName, key, tenantId);
      ModalManager.close();
      await ModalManager.fire({
        icon: 'info',
        title: t('folderSizeResult'),
        html: `<p><strong>${t('size')}:</strong> ${formatSize(result.size)}</p><p><strong>${t('objectsLabel')}:</strong> ${result.count.toLocaleString()}</p>`,
      });
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const openRenameModal = (key: string) => {
    const currentName = key.split('/').pop() || key;
    setRenameKey(key);
    setRenameNewName(currentName);
    setIsRenameModalOpen(true);
    setActionsOpen(false);
  };

  const handleRenameConfirm = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = renameNewName.trim();
    if (!trimmed) return;
    const prefix = renameKey.substring(0, renameKey.lastIndexOf('/') + 1);
    const newKey = prefix + trimmed;
    if (newKey === renameKey) { setIsRenameModalOpen(false); return; }
    try {
      await APIClient.renameObject(bucketName, renameKey, newKey, tenantId);
      setIsRenameModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      setSelectedObjects(new Set());
      ModalManager.toast('success', t('renameSuccess'));
    } catch (error) {
      ModalManager.apiError(error);
    }
  };

  const openEditTagsModal = async (key: string) => {
    setActionsOpen(false);
    try {
      ModalManager.loading(t('editTags'), key);
      const tagData = await APIClient.getObjectTags(bucketName, key, tenantId);
      ModalManager.close();
      setEditTagsKey(key);
      setEditTags(tagData.tags || []);
      setIsEditTagsModalOpen(true);
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const handleEditTagsSave = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await APIClient.setObjectTags(bucketName, editTagsKey, editTags, tenantId);
      setIsEditTagsModalOpen(false);
      ModalManager.toast('success', t('tagsUpdated'));
    } catch (error) {
      ModalManager.apiError(error);
    }
  };

  const handleRestoreVersion = async (key: string, versionId: string, isDeleteMarker: boolean) => {
    const titleKey = isDeleteMarker ? 'restoreDeleteMarkerTitle' : 'restoreVersionTitle';
    const msgKey = isDeleteMarker ? 'restoreDeleteMarkerMsg' : 'restoreVersionMsg';
    const result = await ModalManager.fire({
      icon: 'question',
      title: t(titleKey),
      text: t(msgKey, { key: key.split('/').pop() || key, versionId: versionId.substring(0, 8) + '…' }),
      showCancelButton: true,
      confirmButtonText: t('restoreVersion'),
      cancelButtonText: t('cancel'),
    });
    if (!result.isConfirmed) return;
    try {
      ModalManager.loading(t('restoring'), key);
      await APIClient.restoreObjectVersion(bucketName, key, versionId, isDeleteMarker, tenantId);
      ModalManager.close();
      ModalManager.toast('success', isDeleteMarker ? t('deleteMarkerRemoved') : t('versionRestored'));
      queryClient.invalidateQueries({ queryKey: ['bucket-versions', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const handleDeleteVersion = async (key: string, versionId: string) => {
    const result = await ModalManager.fire({
      icon: 'warning',
      title: t('deleteVersionTitle'),
      text: t('deleteVersionMsg', { key: key.split('/').pop() || key, versionId: versionId.substring(0, 8) + '…' }),
      showCancelButton: true,
      confirmButtonText: t('deleteVersion'),
      cancelButtonText: t('cancel'),
      confirmButtonColor: '#dc2626',
    });
    if (!result.isConfirmed) return;
    try {
      await APIClient.deleteObjectVersion(bucketName, key, versionId, tenantId);
      ModalManager.toast('success', t('versionDeleted'));
      queryClient.invalidateQueries({ queryKey: ['bucket-versions', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['objects', bucketName] });
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
    } catch (error) {
      ModalManager.apiError(error);
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

  // File "Type" column (AWS-style): extension without dot.
  const getFileExtension = (key: string) => {
    const fileName = key.split('/').pop() || key;
    const idx = fileName.lastIndexOf('.');
    if (idx <= 0 || idx === fileName.length - 1) return '';
    return fileName.substring(idx + 1);
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

    let text: string;
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

  // Returns true if the item has an active (non-expired) retention period or an ON legal hold.
  // Used to block deletion from the console — the S3 API also enforces this server-side.
  const isObjectLocked = (item: any): boolean => {
    if (isFolder(item)) return false;
    if ('legalHold' in item && item.legalHold?.status === 'ON') return true;
    if ('retention' in item && item.retention?.retainUntilDate) {
      const info = formatRetentionExpiration(item.retention.retainUntilDate);
      if (info && !info.expired) return true;
    }
    return false;
  };

  const getDisplayName = (item: any) => {
    if (isFolder(item)) {
      // Remove trailing slash and get last part
      const parts = item.key.replace(/\/$/, '').split('/');
      return parts[parts.length - 1];
    }
    return item.key.split('/').pop() || item.key;
  };

  // Actions dropdown helpers
  const selectedItemsList = filteredItems.filter(item => selectedObjects.has(item.key));
  const isSingleSelection = selectedItemsList.length === 1;
  const singleItem = isSingleSelection ? selectedItemsList[0] : null;
  const singleIsFolder = singleItem ? isFolder(singleItem) : false;
  const singleIsFile = singleItem ? !isFolder(singleItem) : false;
  // True when any selected file has active retention or legal hold — blocks delete
  const hasLockedItems = selectedItemsList.some(isObjectLocked);

  if (bucketLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  // Helper shared by both the detail view's onBack and any internal close
  const closeObjectDetails = () => {
    setDetailsObjectKey('');
    setDetailsObjectData({});
  };

  // Callbacks passed to ObjectDetailsView
  const objectViewCallbacks: ObjectViewCallbacks = {
    onDownload:           (key) => handleDownloadObject(key),
    onCopyUrl:            (key) => handleCopyObjectUrl(key),
    onCopyS3Uri:          (key) => handleCopyS3Uri(key),
    onShare:              (key) => handleShareObject(key),
    onPresignedUrl:       (key) => { setSelectedObjectKey(key); setIsPresignedURLModalOpen(true); },
    onRename:             (key) => openRenameModal(key),
    onEditTags:           (key) => openEditTagsModal(key),
    onDelete:             (key) => handleDeleteObject(key, false),
    onToggleLegalHold:    (key, isOn) => handleToggleLegalHold(key, isOn),
    onNavigateToPrefix:   (prefix) => { setCurrentPrefix(prefix); setSelectedObjects(new Set()); },
  };

  return (
    <div className="space-y-6">

      {/* ── Object detail view (AWS-style) — replaces the table ── */}
      {detailsObjectKey && (
        <ObjectDetailsView
          bucketName={bucketName}
          bucketPath={bucketPath}
          currentPrefix={currentPrefix}
          objectKey={detailsObjectKey}
          objectData={detailsObjectData}
          bucketData={bucketData}
          isReadOnly={!!isGlobalAdminInTenantBucket}
          objectLockEnabled={!!bucketData?.objectLock?.objectLockEnabled}
          tenantId={tenantId}
          onBack={closeObjectDetails}
          {...objectViewCallbacks}
        />
      )}

      {/* ── Bucket list view — hidden while object detail is open ── */}
      {!detailsObjectKey && (
      <>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            {/* Breadcrumb */}
            <nav className="flex items-center gap-1 text-sm flex-wrap mb-1">
              <HardDriveIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
              <button
                onClick={() => navigate('/buckets')}
                className="text-blue-600 hover:underline"
              >
                {t('title')}
              </button>
              <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
              {currentPrefix ? (
                <button
                  onClick={() => { setCurrentPrefix(''); setSelectedObjects(new Set()); }}
                  className="text-blue-600 hover:underline"
                >
                  {bucketName}
                </button>
              ) : (
                <span className="text-foreground font-medium">{bucketName}</span>
              )}
              {currentPrefix.split('/').filter(p => p).map((segment, index, parts) => {
                const prefixUpTo = parts.slice(0, index + 1).join('/') + '/';
                const isLast = index === parts.length - 1;
                return (
                  <React.Fragment key={prefixUpTo}>
                    <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                    {isLast ? (
                      <span className="text-foreground font-medium">{segment}</span>
                    ) : (
                      <button
                        onClick={() => { setCurrentPrefix(prefixUpTo); setSelectedObjects(new Set()); }}
                        className="text-blue-600 hover:underline"
                      >
                        {segment}
                      </button>
                    )}
                  </React.Fragment>
                );
              })}
            </nav>
            {/* Page title — shows current folder or bucket */}
            <h1 className="text-2xl font-bold text-foreground">
              {currentPrefix
                ? currentPrefix.split('/').filter(p => p).pop()
                : bucketName}
            </h1>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon"
              onClick={handleRefresh}
              disabled={isRefreshing}
              title={t('refreshObjects')}
              className="bg-card hover:bg-secondary border-border"
            >
              <RefreshCwIcon className="h-4 w-4" />
            </Button>
            <Button
              onClick={() => setIsPermissionsModalOpen(true)}
              variant="outline"
              className="gap-2 hover:bg-gradient-to-r hover:from-purple-50 hover:to-violet-50 dark:hover:from-purple-900/30 dark:hover:to-violet-900/30 transition-all duration-200"
            >
              <ShieldIcon className="h-4 w-4" />
              {t('permissions')}
            </Button>
            <Button
              onClick={() => setIsCreateFolderModalOpen(true)}
              variant="outline"
              className="gap-2 hover:bg-gradient-to-r hover:from-blue-50 hover:to-cyan-50 dark:hover:from-blue-900/30 dark:hover:to-cyan-900/30 transition-all duration-200"
              disabled={isGlobalAdminInTenantBucket}
              title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : t('newFolder')}
            >
              <FolderIcon className="h-4 w-4" />
              {t('newFolder')}
            </Button>
            <Button
              onClick={() => setIsUploadModalOpen(true)}
              variant="outline"
              className="gap-2 hover:bg-gradient-to-r hover:from-green-50 hover:to-emerald-50 dark:hover:from-green-900/30 dark:hover:to-emerald-900/30 transition-all duration-200"
              disabled={isGlobalAdminInTenantBucket}
              title={isGlobalAdminInTenantBucket ? t('globalAdminCannotUpload') : t('uploadFilesTitle')}
            >
              <UploadIcon className="h-4 w-4" />
              {t('uploadFiles')}
            </Button>
            <Button
              variant="outline"
              onClick={() => navigate(`${bucketPath}/settings`, { state: { tenantId } })}
              className="gap-2 hover:bg-secondary transition-all duration-200"
            >
              <SettingsIcon className="h-4 w-4" />
              {t('settings')}
            </Button>
          </div>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard compact
          title={t('totalObjects')}
          value={(bucketData?.object_count || 0).toLocaleString()}
          icon={FileIcon}
          description={t('filesAndFolders')}
          color="brand"
        />

        <MetricCard compact
          title={t('totalSize')}
          value={formatSize(bucketData?.size || 0)}
          icon={HardDriveIcon}
          description={t('storageUsed')}
          color="warning"
        />

        <MetricCard compact
          title={t('region')}
          value={bucketData?.region || 'us-east-1'}
          icon={GlobeIcon}
          description={t('storageRegion')}
          color="success"
        />
      </div>

      {/* Object Lock — compact strip */}
      {bucketData?.objectLock?.objectLockEnabled && (
        <div className="flex items-center gap-2 px-3 py-2 bg-blue-50 dark:bg-blue-900/30 rounded-lg border border-blue-200 dark:border-blue-800 text-xs">
          <LockIcon className="h-3.5 w-3.5 text-blue-600 dark:text-blue-400 shrink-0" />
          <span className="font-semibold text-blue-900 dark:text-blue-100">{t('objectLockEnabled')}</span>
          {bucketData.objectLock.rule?.defaultRetention && (
            <>
              <span className="text-blue-400 dark:text-blue-500">·</span>
              <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full font-medium bg-blue-100 dark:bg-blue-800 text-blue-800 dark:text-blue-200">
                <ShieldIcon className="h-3 w-3" />
                {bucketData.objectLock.rule.defaultRetention.mode}
              </span>
              <span className="text-blue-400 dark:text-blue-500">·</span>
              <span className="text-blue-700 dark:text-blue-300">
                {t('defaultRetention')}:{' '}
                {bucketData.objectLock.rule.defaultRetention.days
                  ? t('retentionDay', { count: bucketData.objectLock.rule.defaultRetention.days })
                  : bucketData.objectLock.rule.defaultRetention.years
                  ? t('retentionYear', { count: bucketData.objectLock.rule.defaultRetention.years })
                  : t('notSpecified')}
              </span>
            </>
          )}
          <span className="ml-auto text-blue-500 dark:text-blue-400">{t('objectLockDesc')}</span>
        </div>
      )}

      {/* Objects Table — search and filters integrated at the top (AWS S3 style) */}
      <div className="bg-card rounded-xl border border-border shadow-md">
        {/* Search + filter toolbar */}
        <div className="px-4 py-2.5 border-b border-border flex items-center gap-2">
          <div className="relative flex-1 max-w-sm">
            <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
            <Input
              placeholder={t('searchObjects')}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-9 h-8 text-sm bg-transparent"
            />
          </div>
          <Button
            onClick={() => setIsFilterPanelOpen(!isFilterPanelOpen)}
            variant={isFilterPanelOpen ? 'default' : 'outline'}
            size="sm"
            className="gap-1.5 h-8 relative"
          >
            <FilterIcon className="h-3.5 w-3.5" />
            {t('filters')}
            {activeFilterCount > 0 && (
              <span className="absolute -top-1.5 -right-1.5 bg-brand-600 text-white text-[10px] font-bold w-4 h-4 rounded-full flex items-center justify-center">
                {activeFilterCount}
              </span>
            )}
          </Button>
        </div>

        {/* Expandable filter panel */}
        {isFilterPanelOpen && (
          <div className="border-b border-border">
            <ObjectFilterPanel
              filters={objectFilter}
              onFiltersChange={setObjectFilter}
              onClear={() => setObjectFilter({})}
            />
          </div>
        )}

        <div className="px-6 border-b border-border flex items-center justify-between h-14 shrink-0">
          <h3 className="text-lg font-semibold text-foreground flex items-center gap-2 min-w-0 mr-4">
            {showVersions ? (
              <>
                <HistoryIcon className="h-5 w-5 text-brand-600 dark:text-brand-400 shrink-0" />
                <span className="truncate">{t('versionsView')}{currentPrefix && ` ${t('inPath', { path: currentPrefix })}`}</span>
              </>
            ) : (
              <>
                <FileIcon className="h-5 w-5 text-brand-600 dark:text-brand-400 shrink-0" />
                <span className="truncate">{t('objectsLabel')} ({filteredItems.length}){currentPrefix && ` ${t('inPath', { path: currentPrefix })}`}</span>
              </>
            )}
          </h3>
          {/* Right-side toolbar: always visible */}
          <div className="flex items-center gap-2 shrink-0">
            {selectedObjects.size > 0 && (
              <span className="text-sm text-muted-foreground hidden sm:inline">
                {t('selectedCount', { count: selectedObjects.size })}
              </span>
            )}
            {bucketData?.versioning?.Status === 'Enabled' && (
              <Button
                variant={showVersions ? 'default' : 'outline'}
                size="sm"
                className="gap-2"
                onClick={() => setShowVersions(v => !v)}
              >
                <HistoryIcon className="h-4 w-4" />
                <span className="hidden sm:inline">{showVersions ? t('hideVersions') : t('showVersions')}</span>
              </Button>
            )}
            <div className="relative" ref={actionsRef}>
                <Button
                  variant="outline"
                  size="sm"
                  className="gap-1"
                  onClick={() => {
                    if (!actionsOpen && actionsRef.current) {
                      const rect = actionsRef.current.getBoundingClientRect();
                      setActionsDropUp(window.innerHeight - rect.bottom < 520);
                    }
                    setActionsOpen(o => !o);
                  }}
                  disabled={isGlobalAdminInTenantBucket || showVersions}
                >
                  {t('actions')}
                  <ChevronDownIcon className="h-4 w-4" />
                </Button>
                {actionsOpen && (
                  <div className={`absolute right-0 w-64 rounded-md shadow-lg bg-card border border-border z-50 ${actionsDropUp ? 'bottom-full mb-1' : 'mt-1'}`}>
                    <div className="py-1">
                      {selectedObjects.size === 0 ? (
                        /* ── No selection: show bucket info ── */
                        <>
                          <div className="px-3 pt-2 pb-1">
                            <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">{t('bucketInfo')}</p>
                          </div>
                          <button
                            className="w-full text-left flex items-start gap-2 px-3 py-1.5 hover:bg-secondary"
                            onClick={() => { navigator.clipboard.writeText(bucketS3Uri); ModalManager.toast('success', t('copyS3BucketUriSuccess')); setActionsOpen(false); }}
                          >
                            <CopyIcon className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                            <div className="min-w-0">
                              <div className="text-xs font-medium">{t('copyS3BucketUri')}</div>
                              <div className="text-[10px] text-muted-foreground truncate">{bucketS3Uri}</div>
                            </div>
                          </button>
                          <button
                            className="w-full text-left flex items-start gap-2 px-3 py-1.5 hover:bg-secondary"
                            onClick={() => { navigator.clipboard.writeText(bucketArn); ModalManager.toast('success', t('copyBucketArnSuccess')); setActionsOpen(false); }}
                          >
                            <CopyIcon className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                            <div className="min-w-0">
                              <div className="text-xs font-medium">{t('copyBucketArn')}</div>
                              <div className="text-[10px] text-muted-foreground truncate">{bucketArn}</div>
                            </div>
                          </button>
                          <button
                            className="w-full text-left flex items-start gap-2 px-3 py-1.5 hover:bg-secondary"
                            onClick={() => { navigator.clipboard.writeText(bucketEndpointUrl); ModalManager.toast('success', t('copyBucketEndpointSuccess')); setActionsOpen(false); }}
                          >
                            <LinkIcon className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                            <div className="min-w-0">
                              <div className="text-xs font-medium">{t('copyBucketEndpoint')}</div>
                              <div className="text-[10px] text-muted-foreground truncate">{bucketEndpointUrl}</div>
                            </div>
                          </button>
                          {bucketWebsiteUrl && (
                            <button
                              className="w-full text-left flex items-start gap-2 px-3 py-1.5 hover:bg-secondary"
                              onClick={() => { window.open(bucketWebsiteUrl, '_blank'); setActionsOpen(false); }}
                            >
                              <ExternalLinkIcon className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
                              <div className="min-w-0">
                                <div className="text-xs font-medium">{t('openWebsiteUrl')}</div>
                                <div className="text-[10px] text-muted-foreground truncate">{bucketWebsiteUrl}</div>
                              </div>
                            </button>
                          )}
                        </>
                      ) : (
                        /* ── Objects selected: show object actions ── */
                        <>
                          <div className="px-3 pt-2 pb-1">
                            <p className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">{t('objectActions')}</p>
                          </div>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!isSingleSelection}
                            onClick={() => singleItem && handleCopyS3Uri(singleItem.key)}
                          >
                            <CopyIcon className="h-4 w-4" />
                            {t('copyS3Uri')}
                          </button>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile}
                            onClick={() => singleItem && handleCopyObjectUrl(singleItem.key)}
                          >
                            <LinkIcon className="h-4 w-4" />
                            {t('copyObjectUrl')}
                          </button>
                          <div className="my-1 border-t border-border" />
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile || isGlobalAdminInTenantBucket}
                            onClick={() => { if (singleItem) handleDownloadObject(singleItem.key); setActionsOpen(false); }}
                          >
                            <DownloadIcon className="h-4 w-4" />
                            {t('download')}
                          </button>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFolder || isGlobalAdminInTenantBucket}
                            onClick={() => { if (singleItem) { handleDownloadFolderZip(singleItem.key); setActionsOpen(false); } }}
                          >
                            <FolderDownIcon className="h-4 w-4" />
                            {t('downloadFolderZip')}
                          </button>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFolder}
                            onClick={() => singleItem && handleCalculateFolderSize(singleItem.key)}
                          >
                            <SigmaIcon className="h-4 w-4" />
                            {t('calculateSize')}
                          </button>
                          <div className="my-1 border-t border-border" />
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile || isGlobalAdminInTenantBucket}
                            onClick={() => { if (singleItem) { handleShareObject(singleItem.key); setActionsOpen(false); } }}
                          >
                            <Share2Icon className="h-4 w-4" />
                            {t('sharePublicLink')}
                          </button>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile || isGlobalAdminInTenantBucket}
                            onClick={() => { if (singleItem) { handleGeneratePresignedURL(singleItem.key); setActionsOpen(false); } }}
                          >
                            <LinkIcon className="h-4 w-4" />
                            {t('generatePresignedUrl')}
                          </button>
                          {bucketData?.versioning?.Status === 'Enabled' && (
                            <button
                              className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                              disabled={!singleIsFile}
                              onClick={() => { if (singleItem) { handleViewVersions(singleItem.key); setActionsOpen(false); } }}
                            >
                              <HistoryIcon className="h-4 w-4" />
                              {t('viewVersions')}
                            </button>
                          )}
                          {bucketData?.objectLock?.objectLockEnabled && (
                            <button
                              className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                              disabled={!singleIsFile || isGlobalAdminInTenantBucket || toggleLegalHoldMutation.isPending}
                              onClick={() => {
                                if (singleItem) {
                                  handleToggleLegalHold(singleItem.key, ('legalHold' in singleItem && (singleItem as any).legalHold?.status === 'ON'));
                                  setActionsOpen(false);
                                }
                              }}
                            >
                              <ShieldIcon className="h-4 w-4" />
                              {singleItem && 'legalHold' in singleItem && (singleItem as any).legalHold?.status === 'ON' ? t('disableLegalHold') : t('enableLegalHold')}
                            </button>
                          )}
                          <div className="my-1 border-t border-border" />
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile || isGlobalAdminInTenantBucket}
                            onClick={() => singleItem && openRenameModal(singleItem.key)}
                          >
                            <PencilIcon className="h-4 w-4" />
                            {t('renameObject')}
                          </button>
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm hover:bg-secondary disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={!singleIsFile || isGlobalAdminInTenantBucket}
                            onClick={() => singleItem && openEditTagsModal(singleItem.key)}
                          >
                            <TagIcon className="h-4 w-4" />
                            {t('editTags')}
                          </button>
                          <div className="my-1 border-t border-border" />
                          <button
                            className="w-full text-left flex items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-950/40 disabled:opacity-40 disabled:cursor-not-allowed"
                            disabled={isGlobalAdminInTenantBucket || deleteObjectMutation.isPending || hasLockedItems}
                            onClick={() => { handleBulkDelete(); setActionsOpen(false); }}
                          >
                            <Trash2Icon className="h-4 w-4" />
                            {t('deleteSelected')}
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                )}
              </div>
          </div>
        </div>
        <div className="overflow-x-auto overflow-hidden rounded-b-xl">
          {showVersions ? (
            versionsLoading ? (
              <div className="flex items-center justify-center py-8">
                <Loading size="md" />
                <span className="ml-3 text-sm text-muted-foreground">{t('loadingVersions')}</span>
              </div>
            ) : !versionsData?.versions?.length ? (
              <div className="text-center py-12 px-4">
                <div className="flex items-center justify-center w-16 h-16 mx-auto rounded-full bg-gray-100 dark:bg-gray-700 mb-4">
                  <HistoryIcon className="h-8 w-8 text-muted-foreground" />
                </div>
                <h3 className="text-base font-medium text-foreground mb-1">{t('noVersionsFound')}</h3>
                <p className="text-sm text-muted-foreground">{t('noVersionsDesc')}</p>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('name')}</TableHead>
                    <TableHead>{t('versionType')}</TableHead>
                    <TableHead>{t('tableModified')}</TableHead>
                    <TableHead>{t('size')}</TableHead>
                    <TableHead>{t('versionId')}</TableHead>
                    <TableHead>{t('tableActions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {versionsData.versions.map((v) => {
                    const displayName = v.key.split('/').pop() || v.key;
                    const isLatest = v.isLatest;
                    const isDM = v.isDeleteMarker;
                    return (
                      <TableRow key={`${v.key}-${v.versionId}`} className={`h-[37px] [&>td]:overflow-hidden [&>td]:max-h-[37px] ${isDM ? 'bg-red-50/30 dark:bg-red-900/10' : ''}`}>
                        <TableCell className="font-medium">
                          <div className="flex items-center gap-2">
                            {isDM
                              ? <Trash2Icon className="h-4 w-4 text-red-400 shrink-0" />
                              : <FileIcon className="h-4 w-4 text-muted-foreground shrink-0" />
                            }
                            <span className="truncate">{displayName}</span>
                            {v.key.includes('/') && (
                              <span className="text-xs text-muted-foreground truncate hidden sm:inline">({v.key})</span>
                            )}
                            {isLatest && (
                              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400 shrink-0">
                                {t('versionIsLatest')}
                              </span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell>
                          {isDM
                            ? <span className="inline-flex items-center gap-1 text-xs text-red-600"><Trash2Icon className="h-3 w-3" />{t('versionTypeDeleteMarker')}</span>
                            : <span className="text-xs text-muted-foreground">{t('versionTypeVersion')}</span>
                          }
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1 text-sm text-muted-foreground">
                            <CalendarIcon className="h-3 w-3 shrink-0" />
                            {formatDate(v.lastModified)}
                          </div>
                        </TableCell>
                        <TableCell>{isDM ? '-' : formatSize(v.size)}</TableCell>
                        <TableCell>
                          <span className="font-mono text-xs text-muted-foreground">
                            {v.versionId ? v.versionId.substring(0, 12) + '…' : '-'}
                          </span>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1">
                            {/* Download: available for all content versions */}
                            {!isDM && (
                              <button
                                title={t('download')}
                                className="p-1 rounded hover:bg-secondary text-muted-foreground hover:text-foreground"
                                onClick={() => handleDownloadObject(v.key)}
                              >
                                <DownloadIcon className="h-4 w-4" />
                              </button>
                            )}
                            {/* Restore: for delete markers (removes marker) and non-latest content versions (copies to current) */}
                            {(isDM || (!isDM && !isLatest)) && (
                              <button
                                title={t('restoreVersion')}
                                className="p-1 rounded hover:bg-secondary text-blue-600 hover:text-blue-700"
                                onClick={() => handleRestoreVersion(v.key, v.versionId, isDM)}
                              >
                                <HistoryIcon className="h-4 w-4" />
                              </button>
                            )}
                            {/* Delete version: for any version with a versionId, not allowed for global admin in tenant */}
                            {!isGlobalAdminInTenantBucket && v.versionId && (
                              <button
                                title={t('deleteVersion')}
                                className="p-1 rounded hover:bg-red-50 dark:hover:bg-red-950/30 text-red-500 hover:text-red-700"
                                onClick={() => handleDeleteVersion(v.key, v.versionId)}
                              >
                                <Trash2Icon className="h-4 w-4" />
                              </button>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )
          ) : objectsLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : filteredItems.length === 0 ? (
            <div className="text-center py-12 px-4">
              <div className="flex items-center justify-center w-16 h-16 mx-auto rounded-full bg-gray-100 dark:bg-gray-700 mb-4">
                <FileIcon className="h-8 w-8 text-muted-foreground" />
              </div>
              <h3 className="text-base font-medium text-foreground mb-1">{t('noObjectsFound')}</h3>
              <p className="text-sm text-muted-foreground mb-4">
                {searchTerm ? t('tryAdjustingSearch') : t('emptyBucketHint')}
              </p>
              {!searchTerm && !isGlobalAdminInTenantBucket && (
                <div className="flex gap-2 justify-center mt-4">
                  <Button
                    onClick={() => setIsCreateFolderModalOpen(true)}
                    variant="outline"
                    className="gap-2"
                  >
                    <FolderIcon className="h-4 w-4" />
                    {t('newFolder')}
                  </Button>
                  <Button
                    onClick={() => setIsUploadModalOpen(true)}
                    className="gap-2"
                  >
                    <UploadIcon className="h-4 w-4" />
                    {t('uploadFiles')}
                  </Button>
                </div>
              )}
              {!searchTerm && isGlobalAdminInTenantBucket && (
                <p className="text-xs text-muted-foreground mt-2">
                  {t('globalAdminReadOnlyHint')}
                </p>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  {!isGlobalAdminInTenantBucket && (
                    <TableHead className="w-12">
                      <input
                        type="checkbox"
                        checked={selectedObjects.size === filteredItems.length && filteredItems.length > 0}
                        ref={el => { if (el) el.indeterminate = selectedObjects.size > 0 && selectedObjects.size < filteredItems.length; }}
                        onChange={toggleSelectAll}
                        className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                      />
                    </TableHead>
                  )}
                  <TableHead>{t('name')}</TableHead>
                  <TableHead>{t('tableType')}</TableHead>
                  <TableHead>{t('tableModified')}</TableHead>
                  <TableHead>{t('size')}</TableHead>
                  <TableHead>{t('tableStorageClass')}</TableHead>
                  {bucketData?.objectLock?.objectLockEnabled && (
                    <>
                      <TableHead>{t('tableRetention')}</TableHead>
                      <TableHead>{t('tableLegalHold')}</TableHead>
                    </>
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {currentPrefix && (
                  <TableRow
                    key="../"
                    className="cursor-pointer hover:bg-slate-100 dark:hover:bg-slate-700/40 h-[37px] [&>td]:overflow-hidden [&>td]:max-h-[37px]"
                    onClick={navigateUp}
                  >
                    {!isGlobalAdminInTenantBucket && <TableCell />}
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <FolderIcon className="h-4 w-4 text-blue-500" />
                        <span className="text-blue-600">..</span>
                      </div>
                    </TableCell>
                    {/* AWS column order: Name, Type, Last Modified, Size, Storage Class */}
                    <TableCell>-</TableCell> {/* Type */}
                    <TableCell>-</TableCell> {/* Last modified */}
                    <TableCell>-</TableCell> {/* Size */}
                    <TableCell>-</TableCell> {/* Storage class */}
                    {bucketData?.objectLock?.objectLockEnabled && (
                      <>
                        <TableCell>-</TableCell>
                        <TableCell>-</TableCell>
                      </>
                    )}
                  </TableRow>
                )}
                {filteredItems.map((item) => (
                  <TableRow key={item.key} className="h-[37px] [&>td]:overflow-hidden [&>td]:max-h-[37px]">
                    {!isGlobalAdminInTenantBucket && (
                      <TableCell>
                        <input
                          type="checkbox"
                          checked={selectedObjects.has(item.key)}
                          onChange={() => toggleObjectSelection(item.key)}
                          onClick={(e) => e.stopPropagation()}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                      </TableCell>
                    )}
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        {isFolder(item) ? (
                          <>
                            <FolderIcon className="h-4 w-4 text-blue-500" />
                            <span
                              onClick={() => navigateToFolder(item.key)}
                              role="button"
                              tabIndex={0}
                              onKeyDown={(e) => e.key === 'Enter' && navigateToFolder(item.key)}
                              className="hover:underline text-blue-600 cursor-pointer"
                            >
                              {getDisplayName(item)}
                            </span>
                          </>
                        ) : (
                          <>
                            <FileIcon className="h-4 w-4 text-muted-foreground" />
                            <span
                              role="button"
                              tabIndex={0}
                              onKeyDown={(e) => e.key === 'Enter' && handleViewObjectDetails(item.key, item)}
                              onClick={() => handleViewObjectDetails(item.key, item)}
                              className="hover:underline text-blue-600 cursor-pointer"
                            >
                              {getDisplayName(item)}
                            </span>
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
                      {isFolder(item) ? t('folderType') : (getFileExtension(item.key) || '-')}
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
                      {isFolder(item) ? '-' : formatSize(item.size)}
                    </TableCell>
                    <TableCell>
                      {isFolder(item) ? '-' : (item.storageClass || 'STANDARD')}
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

                          return <span className="text-gray-400">{t('noRetention')}</span>;
                        })()}
                      </TableCell>
                    )}
                    {bucketData?.objectLock?.objectLockEnabled && (
                      <TableCell>
                        {(() => {
                          if (isFolder(item)) {
                            return <span className="text-gray-400">-</span>;
                          }

                          if ('legalHold' in item && item.legalHold?.status === 'ON') {
                            return (
                              <div className="flex items-center gap-1" title={t('legalHoldActiveTitle')}>
                                <ShieldIcon className="h-3 w-3 text-yellow-600" />
                                <span className="text-xs font-medium text-yellow-600">
                                  ON
                                </span>
                              </div>
                            );
                          }

                          return <span className="text-gray-400 text-xs">OFF</span>;
                        })()}
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
      </>
      )} {/* end !detailsObjectKey */}

      {/* Upload Modal */}
      <Modal
        isOpen={isUploadModalOpen}
        onClose={resetUploadModal}
        title={t('uploadFilesModalTitle')}
      >
        <form onSubmit={handleUpload} className="space-y-4">
          {/* Mode tabs */}
          <div className="flex rounded-lg border border-border overflow-hidden">
            <button
              type="button"
              onClick={() => { setUploadMode('files'); setUploadFiles([]); }}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium transition-colors ${
                uploadMode === 'files'
                  ? 'bg-brand-600 text-white'
                  : 'bg-card text-muted-foreground hover:bg-secondary'
              }`}
            >
              <UploadIcon className="h-4 w-4" />
              {t('uploadModeFiles')}
            </button>
            <button
              type="button"
              onClick={() => { setUploadMode('folder'); setUploadFiles([]); }}
              className={`flex-1 flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium transition-colors border-l border-border ${
                uploadMode === 'folder'
                  ? 'bg-brand-600 text-white'
                  : 'bg-card text-muted-foreground hover:bg-secondary'
              }`}
            >
              <FolderOpen className="h-4 w-4" />
              {t('uploadModeFolder')}
            </button>
          </div>

          {/* Picker */}
          {uploadMode === 'files' ? (
            <div>
              {/* Hidden native input — triggered by the styled button below */}
              <input
                id="upload-input"
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  const list = e.target.files;
                  if (!list) return;
                  setUploadFiles(Array.from(list).map(f => ({ file: f, path: f.name })));
                }}
              />
              {uploadFiles.length > 0 ? (
                /* Compact bar — replaces the drop zone once files are chosen */
                <div className="flex items-center justify-between px-3 py-2 border border-border rounded-lg bg-secondary">
                  <div className="flex items-center gap-2 text-sm text-foreground">
                    <UploadIcon className="h-4 w-4 text-brand-600 shrink-0" />
                    <span className="font-medium">{t('selectedFilesCount', { count: uploadFiles.length })}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => document.getElementById('upload-input')?.click()}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-card hover:bg-border text-foreground border border-border transition-colors"
                  >
                    <UploadIcon className="h-3 w-3" />
                    {t('changeFilesLabel')}
                  </button>
                </div>
              ) : (
                /* Full drop zone — shown when nothing is selected yet */
                <div
                  onDrop={(e) => {
                    e.preventDefault();
                    e.currentTarget.classList.remove('border-brand-500', 'bg-brand-50', 'dark:bg-brand-950/20');
                    const list = e.dataTransfer.files;
                    if (!list || list.length === 0) return;
                    setUploadFiles(Array.from(list).filter(f => f.size > 0).map(f => ({ file: f, path: f.name })));
                  }}
                  onDragOver={(e) => e.preventDefault()}
                  onDragEnter={(e) => e.currentTarget.classList.add('border-brand-500', 'bg-brand-50', 'dark:bg-brand-950/20')}
                  onDragLeave={(e) => e.currentTarget.classList.remove('border-brand-500', 'bg-brand-50', 'dark:bg-brand-950/20')}
                  className="w-full flex flex-col items-center justify-center gap-3 px-4 py-10 border-2 border-dashed border-border rounded-lg text-center transition-colors"
                >
                  <UploadIcon className="h-8 w-8 text-muted-foreground" />
                  <p className="text-sm font-medium text-foreground">{t('dropFilesHere')}</p>
                  <p className="text-xs text-muted-foreground">{t('dropFilesHint')}</p>
                  <button
                    type="button"
                    onClick={() => document.getElementById('upload-input')?.click()}
                    className="mt-1 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-brand-600 hover:bg-brand-700 text-white transition-colors"
                  >
                    <UploadIcon className="h-3.5 w-3.5" />
                    {t('browseFilesLabel')}
                  </button>
                </div>
              )}
              {currentPrefix && (
                <p className="text-xs text-muted-foreground mt-2">
                  {t('filesUploadedTo', { path: currentPrefix })}
                </p>
              )}
            </div>
          ) : (
            <div>
              {uploadFiles.length > 0 && !isFolderScanning ? (
                /* Compact bar — replaces drop zone once folder is loaded */
                <div className="flex items-center justify-between px-3 py-2 border border-border rounded-lg bg-secondary">
                  <div className="flex items-center gap-2 text-sm text-foreground">
                    <FolderOpen className="h-4 w-4 text-brand-600 shrink-0" />
                    <span className="font-medium">{t('selectedFolderLabel', { count: uploadFiles.length })}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setUploadFiles([])}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-card hover:bg-border text-foreground border border-border transition-colors"
                  >
                    {t('changeFolderLabel')}
                  </button>
                </div>
              ) : (
                /* Full drop zone */
                <div
                  onDrop={handleFolderDrop}
                  onDragOver={(e) => e.preventDefault()}
                  onDragEnter={(e) => e.currentTarget.classList.add('border-brand-500', 'bg-brand-50', 'dark:bg-brand-950/20')}
                  onDragLeave={(e) => e.currentTarget.classList.remove('border-brand-500', 'bg-brand-50', 'dark:bg-brand-950/20')}
                  className="w-full flex flex-col items-center justify-center gap-3 px-4 py-10 border-2 border-dashed border-border rounded-lg text-center transition-colors"
                >
                  {isFolderScanning ? (
                    <>
                      <svg className="animate-spin h-8 w-8 text-brand-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                      </svg>
                      <p className="text-sm text-muted-foreground">{t('scanningFolder')}</p>
                    </>
                  ) : (
                    <>
                      <FolderOpen className="h-8 w-8 text-muted-foreground" />
                      <p className="text-sm font-medium text-foreground">{t('dropFolderHere')}</p>
                      <p className="text-xs text-muted-foreground">{t('dropFolderHint')}</p>
                      {'showDirectoryPicker' in window ? (
                        <button
                          type="button"
                          onClick={handleBrowseFolder}
                          className="mt-1 inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-brand-600 hover:bg-brand-700 text-white transition-colors"
                        >
                          <FolderOpen className="h-3.5 w-3.5" />
                          {t('browseFolderLabel')}
                        </button>
                      ) : (
                        <p className="mt-1 text-xs text-muted-foreground italic">{t('browseFolderUnsupported')}</p>
                      )}
                    </>
                  )}
                </div>
              )}
              {currentPrefix && (
                <p className="text-xs text-muted-foreground mt-2">
                  {t('filesUploadedTo', { path: currentPrefix })}
                </p>
              )}
            </div>
          )}

          {/* Preview list */}
          {uploadFiles.length > 0 && (
            <div>
              <h4 className="text-sm font-medium mb-2">
                {uploadMode === 'folder'
                  ? t('selectedFolderLabel', { count: uploadFiles.length })
                  : t('selectedFilesCount', { count: uploadFiles.length })}
              </h4>
              <ul className="text-sm space-y-1 max-h-48 overflow-y-auto pr-1">
                {uploadFiles.map(({ file, path }, index) => (
                  <li key={index} className="flex justify-between gap-2">
                    <span className="truncate text-foreground">{path}</span>
                    <span className="text-muted-foreground shrink-0">{formatSize(file.size)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex justify-end space-x-2 pt-4">
            <Button type="button" variant="outline" onClick={resetUploadModal}>
              {t('cancel')}
            </Button>
            <Button type="submit" disabled={uploadFiles.length === 0}>
              {uploadMode === 'folder' ? t('uploadFolder') : t('uploadFiles')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Folder Modal */}
      <Modal
        isOpen={isCreateFolderModalOpen}
        onClose={() => setIsCreateFolderModalOpen(false)}
        title={t('createNewFolder')}
      >
        <form onSubmit={handleCreateFolder} className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4 mb-4">
            <p className="text-sm text-blue-800 dark:text-blue-200"
              dangerouslySetInnerHTML={{ __html: t('s3FoldersInfo') }}
            />
          </div>

          <div>
            <label htmlFor="folderName" className="block text-sm font-medium text-foreground mb-2">
              {t('folderNameLabel')} *
            </label>
            <Input
              id="folderName"
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder={t('folderNamePlaceholder')}
              required
              pattern="^[a-zA-Z0-9][a-zA-Z0-9\-_]{0,254}$"
              title={t('folderNameValidation')}
              className="bg-card border-border text-foreground"
            />
            {currentPrefix ? (
              <p className="text-xs text-muted-foreground mt-2">
                {t('fullPathWithPrefix', { prefix: currentPrefix, name: newFolderName })}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground mt-2">
                {t('fullPath', { name: newFolderName })}
              </p>
            )}
          </div>

          <div className="flex justify-end space-x-2 pt-4 border-t border-border">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateFolderModalOpen(false)}
              disabled={createFolderMutation.isPending}
              className="bg-card hover:bg-secondary border-border text-foreground"
            >
              {t('cancel')}
            </Button>
            <Button
              type="submit"
              disabled={createFolderMutation.isPending || !newFolderName.trim()}
              className="bg-brand-600 hover:bg-brand-700 text-white"
            >
              {createFolderMutation.isPending ? t('creating') : t('createFolder')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Permissions Modal */}
      <BucketPermissionsModal
        isOpen={isPermissionsModalOpen}
        onClose={() => setIsPermissionsModalOpen(false)}
        bucketName={bucketName}
        tenantId={tenantId}
        readOnly={isGlobalAdminInTenantBucket}
      />

      {/* Object Versions Modal */}
      <ObjectVersionsModal
        isOpen={isVersionsModalOpen}
        onClose={() => setIsVersionsModalOpen(false)}
        bucketName={bucketName}
        objectKey={selectedObjectKey}
        tenantId={tenantId}
      />

      {/* Presigned URL Modal */}
      <PresignedURLModal
        key={selectedObjectKey} // Force remount when object changes to reset state
        isOpen={isPresignedURLModalOpen}
        onClose={() => setIsPresignedURLModalOpen(false)}
        bucketName={bucketName}
        objectKey={selectedObjectKey}
        tenantId={tenantId}
      />

      {/* Rename Modal */}
      <Modal
        isOpen={isRenameModalOpen}
        onClose={() => setIsRenameModalOpen(false)}
        title={t('renameObject')}
      >
        <form onSubmit={handleRenameConfirm} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-foreground mb-2">
              {t('newName')}
            </label>
            <Input
              value={renameNewName}
              onChange={(e) => setRenameNewName(e.target.value)}
              autoFocus
              className="bg-card border-border text-foreground"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" variant="outline" onClick={() => setIsRenameModalOpen(false)}>
              {t('cancel')}
            </Button>
            <Button type="submit" disabled={!renameNewName.trim()}>
              {t('rename')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Tags Modal */}
      <Modal
        isOpen={isEditTagsModalOpen}
        onClose={() => setIsEditTagsModalOpen(false)}
        title={t('editTags')}
      >
        <form onSubmit={handleEditTagsSave} className="space-y-4">
          {editTags.length === 0 && (
            <p className="text-sm text-muted-foreground">{t('noTagsSet')}</p>
          )}
          <div className="space-y-2">
            {editTags.map((tag, idx) => (
              <div key={idx} className="flex items-center gap-2">
                <Input
                  placeholder={t('tagKey')}
                  value={tag.key}
                  onChange={(e) => {
                    const updated = [...editTags];
                    updated[idx] = { ...updated[idx], key: e.target.value };
                    setEditTags(updated);
                  }}
                  className="bg-card border-border text-foreground flex-1"
                />
                <Input
                  placeholder={t('tagValue')}
                  value={tag.value}
                  onChange={(e) => {
                    const updated = [...editTags];
                    updated[idx] = { ...updated[idx], value: e.target.value };
                    setEditTags(updated);
                  }}
                  className="bg-card border-border text-foreground flex-1"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setEditTags(editTags.filter((_, i) => i !== idx))}
                  className="hover:text-red-600"
                >
                  <Trash2Icon className="h-4 w-4" />
                </Button>
              </div>
            ))}
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setEditTags([...editTags, { key: '', value: '' }])}
          >
            {t('addTag')}
          </Button>
          <div className="flex justify-end gap-2 pt-2 border-t border-border">
            <Button type="button" variant="outline" onClick={() => setIsEditTagsModalOpen(false)}>
              {t('cancel')}
            </Button>
            <Button type="submit">
              {t('save')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  );
}
