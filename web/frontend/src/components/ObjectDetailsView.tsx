import React, { useState, useRef, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  HardDrive as HardDriveIcon,
  ChevronRight as ChevronRightIcon,
  ChevronDown as ChevronDownIcon,
  ArrowLeft as ArrowLeftIcon,
  Download as DownloadIcon,
  Copy,
  Check,
  Link as LinkIcon,
  Share2 as Share2Icon,
  Pencil as PencilIcon,
  Tag as TagIcon,
  Shield as ShieldIcon,
  Trash2 as Trash2Icon,
} from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/Table';
import { APIClient } from '@/lib/api';
import { useTranslation } from 'react-i18next';

type Tab = 'properties' | 'permissions' | 'versions';

export type ObjectViewCallbacks = {
  onDownload:            (key: string) => void;
  onCopyUrl:             (key: string) => void;
  onCopyS3Uri:           (key: string) => void;
  onShare:               (key: string) => void;
  onPresignedUrl:        (key: string) => void;
  onRename:              (key: string) => void;
  onEditTags:            (key: string) => void;
  onDelete:              (key: string) => void;
  onToggleLegalHold?:    (key: string, currentIsOn: boolean) => void;
  onNavigateToPrefix?:   (prefix: string) => void;
};

type Props = ObjectViewCallbacks & {
  bucketName:        string;
  bucketPath:        string;
  currentPrefix:     string;
  objectKey:         string;
  objectData:        Record<string, any>;
  bucketData?:       Record<string, any> | null;
  isReadOnly?:       boolean;
  objectLockEnabled?: boolean;
  tenantId?:         string;
  onBack:            () => void;
};

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatSize(bytes: number): string {
  if (!bytes) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) { value /= 1024; i++; }
  return `${value.toFixed(1)} ${units[i]}`;
}

function formatDate(d: string): string {
  if (!d) return '-';
  return new Date(d).toLocaleString('en-US', {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit',
  });
}

function fileExtension(key: string): string {
  const name = key.split('/').pop() ?? '';
  const idx = name.lastIndexOf('.');
  if (idx <= 0 || idx === name.length - 1) return '-';
  return name.slice(idx + 1).toUpperCase();
}

// ─── Copy button ─────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation('buckets');
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        });
      }}
      title={t('copy')}
      className="mr-1.5 inline-flex items-center shrink-0 text-muted-foreground hover:text-foreground transition-colors"
    >
      {copied
        ? <Check className="h-3.5 w-3.5 text-green-500" />
        : <Copy className="h-3.5 w-3.5" />
      }
    </button>
  );
}

// ─── Row helpers ─────────────────────────────────────────────────────────────

function InfoRow({ label, value, mono = false }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div className="space-y-0.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className={`text-sm break-all ${mono ? 'font-mono' : 'font-medium'}`}>{value || '-'}</p>
    </div>
  );
}

function CopyRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="space-y-0.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className={`flex items-start text-sm break-all ${mono ? 'font-mono' : 'font-medium'}`}>
        <CopyButton text={value} />
        <span className="break-all">{value || '-'}</span>
      </div>
    </div>
  );
}

// ─── Actions dropdown ────────────────────────────────────────────────────────

type ActionsMenuProps = ObjectViewCallbacks & {
  objectKey:          string;
  isReadOnly?:        boolean;
  objectLockEnabled?: boolean;
  legalHoldOn?:       boolean;
  onBack:             () => void;
};

function ActionsMenu({
  objectKey, isReadOnly, objectLockEnabled, legalHoldOn,
  onCopyUrl, onCopyS3Uri, onShare, onPresignedUrl,
  onRename, onEditTags, onDelete, onToggleLegalHold,
}: ActionsMenuProps) {
  const { t } = useTranslation('buckets');
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  const item = (
    icon: React.ReactNode,
    label: string,
    onClick: () => void,
    disabled = false,
    danger = false,
  ) => (
    <button
      type="button"
      disabled={disabled}
      onClick={() => { onClick(); setOpen(false); }}
      className={[
        'w-full text-left flex items-center gap-2 px-3 py-2 text-sm transition-colors',
        danger
          ? 'text-red-600 hover:bg-red-50 dark:hover:bg-red-950/40'
          : 'hover:bg-secondary',
        disabled ? 'opacity-40 cursor-not-allowed' : '',
      ].join(' ')}
    >
      {icon}
      {label}
    </button>
  );

  const sep = <div className="my-1 border-t border-border" />;

  return (
    <div className="relative" ref={ref}>
      <Button
        variant="outline"
        size="sm"
        className="gap-1"
        onClick={() => setOpen(o => !o)}
        disabled={isReadOnly}
      >
        {t('actions')}
        <ChevronDownIcon className="h-4 w-4" />
      </Button>

      {open && (
        <div className="absolute right-0 mt-1 w-56 rounded-md shadow-lg bg-card border border-border z-50">
          <div className="py-1">
            {item(<Copy className="h-4 w-4" />,       t('copyS3Uri'),            () => onCopyS3Uri(objectKey))}
            {item(<LinkIcon className="h-4 w-4" />,   t('copyObjectUrl'),        () => onCopyUrl(objectKey))}
            {sep}
            {item(<Share2Icon className="h-4 w-4" />, t('sharePublicLink'),      () => onShare(objectKey),        isReadOnly)}
            {item(<LinkIcon className="h-4 w-4" />,   t('generatePresignedUrl'), () => onPresignedUrl(objectKey), isReadOnly)}
            {objectLockEnabled && onToggleLegalHold && (
              item(
                <ShieldIcon className="h-4 w-4" />,
                legalHoldOn ? t('disableLegalHold') : t('enableLegalHold'),
                () => onToggleLegalHold(objectKey, !!legalHoldOn),
                isReadOnly,
              )
            )}
            {sep}
            {item(<PencilIcon className="h-4 w-4" />, t('renameObject'), () => onRename(objectKey),   isReadOnly)}
            {item(<TagIcon className="h-4 w-4" />,    t('editTags'),     () => onEditTags(objectKey), isReadOnly)}
            {sep}
            {/* Note: onBack is NOT called here — deleteObjectMutation.onSuccess handles
                closing the detail view via detailsObjectKeyRef once the delete is confirmed */}
            {item(
              <Trash2Icon className="h-4 w-4" />,
              t('deleteObject'),
              () => onDelete(objectKey),
              isReadOnly,
              true,
            )}
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Component ───────────────────────────────────────────────────────────────

export function ObjectDetailsView({
  bucketName, bucketPath, currentPrefix, objectKey,
  objectData, bucketData, isReadOnly, objectLockEnabled, tenantId, onBack,
  onDownload, onCopyUrl, onCopyS3Uri, onShare, onPresignedUrl,
  onRename, onEditTags, onDelete, onToggleLegalHold, onNavigateToPrefix,
}: Props) {
  const { t } = useTranslation('buckets');
  const navigate = useNavigate();
  const [activeTab, setActiveTab] = useState<Tab>('properties');

  const obj = {
    key:          objectData.key ?? objectKey,
    size:         Number(objectData.size ?? 0),
    lastModified: objectData.lastModified ?? objectData.last_modified ?? '',
    etag:         objectData.etag ?? objectData.ETag ?? '',
    contentType:  objectData.contentType ?? objectData.content_type ?? '',
    storageClass: objectData.storageClass || objectData.storage_class || 'STANDARD',
    metadata:     (objectData.metadata ?? {}) as Record<string, string>,
    retention:    objectData.retention ?? null,
    legalHold:    objectData.legalHold ?? null,
  };

  const region     = (bucketData as any)?.region ?? '-';
  const s3Uri      = `s3://${bucketName}/${objectKey}`;
  const arn        = `arn:aws:s3:::${bucketName}/${objectKey}`;
  const objectUrl  = APIClient.getObjectUrl(bucketName, objectKey);
  const objectName = objectKey.split('/').pop() ?? objectKey;
  const legalHoldOn = obj.legalHold?.status === 'ON' || obj.legalHold?.Status === 'ON';

  const prefixSegments = currentPrefix.split('/').filter(Boolean);

  // ACL — lazy
  const aclQuery = useQuery({
    queryKey: ['objectACL', bucketName, objectKey, tenantId],
    enabled: activeTab === 'permissions',
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: () => APIClient.getObjectACL(bucketName, objectKey, tenantId),
  });

  // Versions — lazy
  const versionsQuery = useQuery({
    queryKey: ['objectVersionsView', bucketName, objectKey, tenantId],
    enabled: activeTab === 'versions',
    retry: false,
    refetchOnWindowFocus: false,
    queryFn: () => APIClient.listObjectVersions(bucketName, objectKey, tenantId),
  });

  const aclData   = aclQuery.data as any;
  const ownerName = aclData?.owner?.display_name ?? aclData?.owner?.id ?? '-';
  const grants    = (aclData?.grants ?? []) as any[];

  const versionsData = versionsQuery.data as any;
  const allVersions  = [
    ...(versionsData?.versions ?? []),
    ...(versionsData?.deleteMarkers ?? []),
  ].sort((a: any, b: any) =>
    new Date(b.lastModified).getTime() - new Date(a.lastModified).getTime()
  );

  const tabs: { id: Tab; label: string }[] = [
    { id: 'properties',  label: t('tabProperties') },
    { id: 'permissions', label: t('tabPermissions') },
    { id: 'versions',    label: t('tabVersions') },
  ];

  return (
    <div className="space-y-6">

      {/* ── Header ── */}
      <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
        <div>
          {/* Breadcrumb */}
          <nav className="flex items-center gap-1 text-sm flex-wrap mb-2">
            <HardDriveIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <button onClick={() => navigate('/buckets')} className="text-blue-600 hover:underline">
              {t('title')}
            </button>
            <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            {/* Bucket name — clicking goes back to bucket root */}
            <button
              onClick={() => {
                if (onNavigateToPrefix) onNavigateToPrefix('');
                onBack();
              }}
              className="text-blue-600 hover:underline"
            >
              {bucketName}
            </button>
            {prefixSegments.map((segment, i) => {
              const prefixUpTo = prefixSegments.slice(0, i + 1).join('/') + '/';
              return (
                <React.Fragment key={i}>
                  <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <button
                    onClick={() => {
                      if (onNavigateToPrefix) onNavigateToPrefix(prefixUpTo);
                      onBack();
                    }}
                    className="text-blue-600 hover:underline"
                  >
                    {segment}
                  </button>
                </React.Fragment>
              );
            })}
            <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <span className="text-foreground font-medium">{objectName}</span>
          </nav>

          {/* Title */}
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onBack}
              title={t('back')}
              className="p-1 rounded hover:bg-secondary text-muted-foreground hover:text-foreground transition-colors"
            >
              <ArrowLeftIcon className="h-5 w-5" />
            </button>
            <h1 className="text-2xl font-bold text-foreground">{objectName}</h1>
          </div>
        </div>

        {/* Action buttons */}
        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="outline"
            size="sm"
            className="gap-2"
            onClick={() => onDownload(objectKey)}
            disabled={isReadOnly}
          >
            <DownloadIcon className="h-4 w-4" />
            {t('download')}
          </Button>

          <ActionsMenu
            objectKey={objectKey}
            isReadOnly={isReadOnly}
            objectLockEnabled={objectLockEnabled}
            legalHoldOn={legalHoldOn}
            onBack={onBack}
            onDownload={onDownload}
            onCopyUrl={onCopyUrl}
            onCopyS3Uri={onCopyS3Uri}
            onShare={onShare}
            onPresignedUrl={onPresignedUrl}
            onRename={onRename}
            onEditTags={onEditTags}
            onDelete={onDelete}
            onToggleLegalHold={onToggleLegalHold}
          />
        </div>
      </div>

      {/* ── Tab card ── */}
      <div className="bg-card rounded-lg border border-border">
        {/* Tab bar */}
        <div className="flex border-b border-border overflow-x-auto px-2">
          {tabs.map(tab => (
            <button
              key={tab.id}
              type="button"
              onClick={() => setActiveTab(tab.id)}
              className={[
                'px-4 py-3 text-sm font-medium whitespace-nowrap transition-colors border-b-2 -mb-px',
                activeTab === tab.id
                  ? 'border-blue-600 text-blue-600'
                  : 'border-transparent text-muted-foreground hover:text-foreground',
              ].join(' ')}
            >
              {tab.label}
            </button>
          ))}
        </div>

        <div className="p-6">

          {/* ── Properties ── */}
          {activeTab === 'properties' && (
            <div className="space-y-6">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-x-8 gap-y-5">
                <InfoRow label={t('objLastModified')} value={formatDate(obj.lastModified)} />
                <InfoRow label={t('size')}            value={formatSize(obj.size)} />
                <InfoRow label={t('tableType')}       value={fileExtension(obj.key)} />
                <InfoRow label={t('objContentType')}  value={obj.contentType} />
                <InfoRow label={t('tableStorageClass')} value={obj.storageClass} />
                <InfoRow label={t('region')}          value={region} />
                <InfoRow label="ETag"                 value={obj.etag} mono />
              </div>

              <hr className="border-border" />

              <div className="grid grid-cols-1 gap-4">
                <CopyRow label={t('objKey')}    value={obj.key}   mono />
                <CopyRow label="S3 URI"         value={s3Uri}     mono />
                <CopyRow label="ARN"            value={arn}       mono />
                <CopyRow label={t('objUrl')}    value={objectUrl} mono />
              </div>

              {Object.keys(obj.metadata).length > 0 && (
                <>
                  <hr className="border-border" />
                  <div>
                    <p className="text-sm font-semibold mb-3">{t('objMetadata')}</p>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{t('objMetaKey')}</TableHead>
                          <TableHead>{t('objMetaValue')}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {Object.entries(obj.metadata).map(([k, v]) => (
                          <TableRow key={k}>
                            <TableCell className="font-mono text-xs">{k}</TableCell>
                            <TableCell className="font-mono text-xs">{v}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                </>
              )}

              {(obj.legalHold || obj.retention) && (
                <>
                  <hr className="border-border" />
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-5">
                    {obj.legalHold && (
                      <InfoRow label={t('tableLegalHold')} value={String(obj.legalHold?.status ?? obj.legalHold?.Status ?? '-')} />
                    )}
                    {obj.retention && (
                      <>
                        <InfoRow label={t('objRetentionMode')}  value={String(obj.retention?.mode ?? obj.retention?.Mode ?? '-')} />
                        <InfoRow label={t('objRetainUntil')}    value={formatDate(String(obj.retention?.retainUntilDate ?? obj.retention?.RetainUntilDate ?? ''))} />
                      </>
                    )}
                  </div>
                </>
              )}
            </div>
          )}

          {/* ── Permissions ── */}
          {activeTab === 'permissions' && (
            <div className="space-y-4">
              {aclQuery.isLoading && <div className="flex justify-center py-8"><Loading size="md" /></div>}
              {aclQuery.isError && (
                <p className="text-sm text-muted-foreground text-center py-6">
                  {t('objAclLoadError')}
                </p>
              )}
              {aclQuery.isSuccess && (
                <>
                  <InfoRow label={t('objOwner')} value={ownerName} />
                  <div>
                    <p className="text-sm font-semibold mb-2">{t('objGrants')}</p>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{t('objGrantee')}</TableHead>
                          <TableHead>{t('objPermission')}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {grants.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={2} className="text-center text-sm text-muted-foreground">
                              {t('objNoGrants')}
                            </TableCell>
                          </TableRow>
                        ) : grants.map((g: any, i: number) => {
                          const grantee = g?.grantee ?? g?.Grantee ?? {};
                          const label = [
                            grantee.type ?? grantee.Type,
                            grantee.display_name ?? grantee.DisplayName,
                            grantee.id ?? grantee.ID,
                          ].filter(Boolean).join(' · ');
                          return (
                            <TableRow key={i}>
                              <TableCell className="text-xs font-mono">{label || '-'}</TableCell>
                              <TableCell className="text-xs">{String(g?.permission ?? g?.Permission ?? '-')}</TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  </div>
                </>
              )}
            </div>
          )}

          {/* ── Versions ── */}
          {activeTab === 'versions' && (
            <div className="space-y-4">
              {versionsQuery.isLoading && <div className="flex justify-center py-8"><Loading size="md" /></div>}
              {versionsQuery.isError && (
                <p className="text-sm text-muted-foreground text-center py-6">
                  {t('objVersionsLoadError')}
                </p>
              )}
              {versionsQuery.isSuccess && allVersions.length === 0 && (
                <p className="text-sm text-muted-foreground text-center py-6">
                  {t('objNoVersions')}
                </p>
              )}
              {versionsQuery.isSuccess && allVersions.length > 0 && (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('objVersionId')}</TableHead>
                      <TableHead>{t('tableModified')}</TableHead>
                      <TableHead>{t('size')}</TableHead>
                      <TableHead>{t('objVersionStatus')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {allVersions.map((v: any) => (
                      <TableRow key={v.versionId ?? v.VersionId}>
                        <TableCell className="font-mono text-xs">
                          {String(v.versionId ?? v.VersionId ?? '').slice(0, 16)}…
                        </TableCell>
                        <TableCell className="text-xs">{formatDate(v.lastModified ?? v.LastModified)}</TableCell>
                        <TableCell className="text-xs">
                          {v.isDeleteMarker || v.IsDeleteMarker ? '-' : formatSize(v.size ?? v.Size ?? 0)}
                        </TableCell>
                        <TableCell className="text-xs space-x-1">
                          {(v.isLatest ?? v.IsLatest) && (
                            <span className="inline-flex px-2 py-0.5 rounded bg-green-100 text-green-800">Latest</span>
                          )}
                          {(v.isDeleteMarker ?? v.IsDeleteMarker) && (
                            <span className="inline-flex px-2 py-0.5 rounded bg-red-100 text-red-800">Delete marker</span>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          )}

        </div>
      </div>
    </div>
  );
}
