import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
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
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Box,
  Building2,
  Calendar,
  ChevronLeft,
  ChevronRight,
  Clock,
  HardDrive,
  Lock,
  Plus,
  Search,
  Settings,
  Shield,
  ShieldAlert,
  ShieldCheck,
  ShieldOff,
  Trash2,
  Users,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Bucket, IntegrityResult } from '@/types';
import ModalManager from '@/lib/modals';
import { EmptyState } from '@/components/ui/EmptyState';
import {
  BucketIntegrityModal,
  BucketScanState,
} from '@/components/BucketIntegrityModal';

type SortField = 'name' | 'creationDate' | 'objectCount' | 'size';
type SortOrder = 'asc' | 'desc';

// ── Helpers ────────────────────────────────────────────────────────────────────

function getBucketKey(bucket: Bucket): string {
  const tid = bucket.tenant_id || bucket.tenantId;
  return tid ? `${tid}/${bucket.name}` : bucket.name;
}

// ── Page component ─────────────────────────────────────────────────────────────

export default function BucketsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // ── Table state ──────────────────────────────────────────────────────────────
  const [searchTerm, setSearchTerm]   = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [itemsPerPage]                = useState(10);
  const [sortField, setSortField]     = useState<SortField>('creationDate');
  const [sortOrder, setSortOrder]     = useState<SortOrder>('desc');

  // ── Integrity scan state ─────────────────────────────────────────────────────
  const [scanStates, setScanStates] = useState<Record<string, BucketScanState>>({});
  const abortRefs = useRef<Record<string, AbortController>>({});
  const [integrityBucket, setIntegrityBucket] = useState<Bucket | null>(null);

  // ── Queries ──────────────────────────────────────────────────────────────────
  const { data: buckets, isLoading, error } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  });
  const { data: users }   = useQuery({ queryKey: ['users'],   queryFn: APIClient.getUsers });
  const { data: tenants } = useQuery({ queryKey: ['tenants'], queryFn: APIClient.getTenants });

  // Fetch the persisted scan history whenever the modal is open.
  const integrityBucketKey = integrityBucket ? getBucketKey(integrityBucket) : null;
  const { data: integrityHistory = [] } = useQuery({
    queryKey: ['integrity-status', integrityBucketKey],
    queryFn: () => APIClient.getIntegrityHistory(
      integrityBucket!.name,
      integrityBucket!.tenant_id || integrityBucket!.tenantId
    ),
    enabled: !!integrityBucket,
    retry: false,
    staleTime: 30000,
    refetchOnWindowFocus: false,
  });

  // Seed the scan state from the latest persisted record when no in-session scan exists.
  useEffect(() => {
    if (!integrityHistory.length || !integrityBucketKey || scanStates[integrityBucketKey]) return;
    const latest = integrityHistory[0];
    const scannedAt = new Date(latest.scannedAt);
    setScanStates(prev => ({
      ...prev,
      [integrityBucketKey]: {
        phase: 'done',
        checked:    latest.checked,
        ok:         latest.ok,
        corrupted:  latest.corrupted,
        skipped:    latest.skipped,
        errors:     latest.errors,
        issues:     latest.issues ?? [],
        duration:   latest.duration,
        startedAt:  scannedAt,
        finishedAt: scannedAt,
        source:     latest.source,
      },
    }));
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [integrityHistory, integrityBucketKey]);

  // Rate limit: compute when the next manual scan is allowed based on history.
  const getRateLimitUntil = (bucketKey: string): Date | null => {
    const lastManual = integrityHistory.find(h => h.source === 'manual');
    if (!lastManual) return null;
    const nextAt = new Date(new Date(lastManual.scannedAt).getTime() + 60 * 60 * 1000);
    return nextAt > new Date() ? nextAt : null;
  };

  // ── Delete mutation ──────────────────────────────────────────────────────────
  const deleteBucketMutation = useMutation({
    mutationFn: ({ bucketName, tenantId, force }: { bucketName: string; tenantId?: string; force?: boolean }) =>
      APIClient.deleteBucket(bucketName, tenantId, force),
    onSuccess: (_r, { bucketName }) => {
      queryClient.refetchQueries({ queryKey: ['buckets'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.successBucketDeleted(bucketName);
    },
    onError: async (err: any, variables) => {
      ModalManager.close();
      const msg = err?.response?.data?.error || err?.message || String(err);
      if (msg.includes('not empty')) {
        const result = await ModalManager.fire({
          title: 'Bucket is not empty',
          text: `The bucket "${variables.bucketName}" contains objects. Do you want to force delete it and all its contents? This action cannot be undone.`,
          icon: 'warning',
          showCancelButton: true,
          confirmButtonText: 'Yes, force delete',
          cancelButtonText: 'Cancel',
          confirmButtonColor: '#dc2626',
        });
        if (result.isConfirmed) {
          ModalManager.loading('Force deleting bucket...', `Deleting "${variables.bucketName}" and all its objects`);
          deleteBucketMutation.mutate({ bucketName: variables.bucketName, tenantId: variables.tenantId, force: true });
        }
      } else {
        ModalManager.apiError(err);
      }
    },
  });

  // ── Integrity scan management ────────────────────────────────────────────────

  const startScan = useCallback(async (bucket: Bucket) => {
    const key = getBucketKey(bucket);
    const tenantId = bucket.tenant_id || bucket.tenantId;

    // Cancel any in-progress scan for this bucket
    abortRefs.current[key]?.abort();
    const controller = new AbortController();
    abortRefs.current[key] = controller;

    setScanStates(prev => ({
      ...prev,
      [key]: {
        phase: 'running',
        checked: 0, ok: 0, corrupted: 0, skipped: 0, errors: 0,
        issues: [],
        duration: '',
        startedAt: new Date(),
      },
    }));

    const startTime = Date.now();
    let marker = '';
    let acc = {
      checked: 0, ok: 0, corrupted: 0, skipped: 0, errors: 0,
      issues: [] as IntegrityResult[],
    };

    try {
      do {
        if (controller.signal.aborted) return;

        const report = await APIClient.verifyBucketIntegrity(bucket.name, {
          marker,
          maxKeys: 500,
          tenantId,
        });

        if (controller.signal.aborted) return;

        acc = {
          checked:   acc.checked   + report.checked,
          ok:        acc.ok        + report.ok,
          corrupted: acc.corrupted + report.corrupted,
          skipped:   acc.skipped   + report.skipped,
          errors:    acc.errors    + report.errors,
          issues:    [...acc.issues, ...(report.issues ?? [])],
        };

        setScanStates(prev => ({
          ...prev,
          [key]: { ...prev[key], ...acc },
        }));

        marker = report.nextMarker ?? '';
      } while (marker !== '');

    } catch (err: any) {
      if (controller.signal.aborted) return;
      setScanStates(prev => ({
        ...prev,
        [key]: {
          ...prev[key],
          phase: 'error',
          errorMessage: err?.response?.data?.error ?? err?.message ?? 'Unknown error',
          finishedAt: new Date(),
        },
      }));
      return;
    }

    if (controller.signal.aborted) return;

    const duration = ((Date.now() - startTime) / 1000).toFixed(2) + 's';
    const finishedAt = new Date();

    setScanStates(prev => ({
      ...prev,
      [key]: { ...prev[key], ...acc, phase: 'done', duration, finishedAt },
    }));

    // Persist the result and refresh history from the server.
    try {
      await APIClient.saveIntegrityScan(bucket.name, {
        duration,
        checked:   acc.checked,
        ok:        acc.ok,
        corrupted: acc.corrupted,
        skipped:   acc.skipped,
        errors:    acc.errors,
        issues:    acc.issues,
      }, tenantId);
      queryClient.invalidateQueries({ queryKey: ['integrity-status', key] });
    } catch {
      // Non-fatal — UI already updated, just couldn't persist.
    }
  }, [queryClient]);

  const cancelScan = useCallback((bucket: Bucket) => {
    const key = getBucketKey(bucket);
    abortRefs.current[key]?.abort();
    delete abortRefs.current[key];
    setScanStates(prev => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }, []);

  // ── Filtering / sorting / pagination ────────────────────────────────────────
  const filteredBuckets = useMemo(
    () => (buckets ?? []).filter(b => b.name.toLowerCase().includes(searchTerm.toLowerCase())),
    [buckets, searchTerm],
  );

  const sortedBuckets = useMemo(() => {
    const sorted = [...filteredBuckets];
    sorted.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case 'name':        cmp = a.name.localeCompare(b.name); break;
        case 'creationDate':
          cmp = new Date(a.creation_date || a.creationDate || '').getTime()
              - new Date(b.creation_date || b.creationDate || '').getTime(); break;
        case 'objectCount': cmp = (a.object_count || a.objectCount || 0) - (b.object_count || b.objectCount || 0); break;
        case 'size':        cmp = (a.size || a.totalSize || 0) - (b.size || b.totalSize || 0); break;
      }
      return sortOrder === 'asc' ? cmp : -cmp;
    });
    return sorted;
  }, [filteredBuckets, sortField, sortOrder]);

  const totalPages   = Math.ceil(sortedBuckets.length / itemsPerPage);
  const startIndex   = (currentPage - 1) * itemsPerPage;
  const paginatedBuckets = sortedBuckets.slice(startIndex, startIndex + itemsPerPage);

  React.useEffect(() => { setCurrentPage(1); }, [searchTerm]);

  // ── Helpers ──────────────────────────────────────────────────────────────────
  const handleSort = (field: SortField) => {
    if (sortField === field) setSortOrder(o => o === 'asc' ? 'desc' : 'asc');
    else { setSortField(field); setSortOrder('asc'); }
  };

  const sortIcon = (field: SortField) => {
    if (sortField !== field) return <ArrowUpDown className="h-3 w-3 text-gray-400" />;
    return sortOrder === 'asc'
      ? <ArrowUp className="h-3 w-3 text-brand-600 dark:text-brand-400" />
      : <ArrowDown className="h-3 w-3 text-brand-600 dark:text-brand-400" />;
  };

  const handleDeleteBucket = async (bucketName: string) => {
    try {
      const result = await ModalManager.confirmDeleteBucket(bucketName);
      if (result.isConfirmed) {
        ModalManager.loading('Deleting bucket...', `Deleting "${bucketName}" and all its data`);
        const bucket = buckets?.find(b => b.name === bucketName);
        deleteBucketMutation.mutate({ bucketName, tenantId: bucket?.tenant_id || bucket?.tenantId });
      }
    } catch (err) {
      ModalManager.close();
      ModalManager.apiError(err);
    }
  };

  const formatSize = (bytes: number) => {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = bytes, ui = 0;
    while (size >= 1024 && ui < units.length - 1) { size /= 1024; ui++; }
    return `${size.toFixed(1)} ${units[ui]}`;
  };

  const formatDate = (ds: string) =>
    new Date(ds).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

  const getOwnerDisplay = (bucket: Bucket) => {
    const ownerId = bucket.owner_id || bucket.ownerId;
    const ownerType = bucket.owner_type || bucket.ownerType;
    if (!ownerId || !ownerType) return { type: 'global', name: 'Global', icon: Shield };
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

  // ── Integrity button per row ─────────────────────────────────────────────────
  const getIntegrityButton = (bucket: Bucket) => {
    const key = getBucketKey(bucket);
    const state = scanStates[key];
    const objCount = bucket.object_count ?? bucket.objectCount ?? 0;
    const pct = state?.phase === 'running' && objCount > 0
      ? Math.min(100, Math.round((state.checked / objCount) * 100))
      : null;

    if (!state) {
      return {
        icon: <ShieldCheck className="h-4 w-4" />,
        className: 'p-2 text-gray-500 dark:text-gray-400 hover:text-success-600 dark:hover:text-success-400 hover:bg-success-50 dark:hover:bg-success-900/20 rounded-lg transition-all duration-200',
        title: 'Verify integrity',
        badge: null,
      };
    }
    if (state.phase === 'running') {
      return {
        icon: <ShieldCheck className="h-4 w-4 text-amber-500 animate-pulse" />,
        className: 'p-2 rounded-lg transition-all duration-200 bg-amber-50 dark:bg-amber-900/20',
        title: `Scanning… ${pct !== null ? pct + '%' : ''}`,
        badge: pct !== null
          ? <span className="absolute -top-1 -right-1 text-[9px] font-bold leading-none bg-amber-500 text-white rounded-full px-1 py-0.5">{pct}%</span>
          : null,
      };
    }
    if (state.phase === 'done' && state.corrupted === 0 && state.errors === 0) {
      return {
        icon: <ShieldCheck className="h-4 w-4 text-success-600 dark:text-success-400" />,
        className: 'p-2 rounded-lg transition-all duration-200 bg-success-50 dark:bg-success-900/20 hover:bg-success-100 dark:hover:bg-success-900/30',
        title: `Last scan: clean (${state.checked.toLocaleString()} objects in ${state.duration})`,
        badge: null,
      };
    }
    if (state.phase === 'done' && (state.corrupted > 0 || state.errors > 0)) {
      return {
        icon: <ShieldAlert className="h-4 w-4 text-error-600 dark:text-error-400" />,
        className: 'p-2 rounded-lg transition-all duration-200 bg-error-50 dark:bg-error-900/20 hover:bg-error-100 dark:hover:bg-error-900/30',
        title: `Last scan: ${state.corrupted} issue${state.corrupted !== 1 ? 's' : ''} found`,
        badge: <span className="absolute -top-1 -right-1 text-[9px] font-bold leading-none bg-error-500 text-white rounded-full px-1 py-0.5">{state.corrupted}</span>,
      };
    }
    // error
    return {
      icon: <ShieldOff className="h-4 w-4 text-gray-400 dark:text-gray-500" />,
      className: 'p-2 rounded-lg transition-all duration-200 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700',
      title: 'Last scan failed — click to retry',
      badge: null,
    };
  };

  // ── Render ───────────────────────────────────────────────────────────────────
  if (isLoading) {
    return <div className="flex items-center justify-center h-64"><Loading size="lg" /></div>;
  }
  if (error) {
    return (
      <div className="rounded-lg bg-error-50 dark:bg-error-900/30 border border-error-200 dark:border-error-800 p-4">
        <div className="text-sm text-error-700 dark:text-error-400 font-medium">
          Error loading buckets: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  // Buckets actively running a scan in background (for the toast-style indicator)
  const runningScanKeys = Object.entries(scanStates)
    .filter(([, s]) => s.phase === 'running')
    .map(([k]) => k);

  return (
    <>
      <div className="space-y-6">

        {/* ── Header ── */}
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Buckets</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
              Manage your S3 buckets and their configurations
            </p>
          </div>
          <Button
            onClick={() => navigate('/buckets/create')}
            className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md hover:shadow-lg transition-all duration-200 inline-flex items-center gap-2"
          >
            <Plus className="h-4 w-4" />
            Create Bucket
          </Button>
        </div>

        {/* ── Background scan indicator bar ── */}
        {runningScanKeys.length > 0 && (
          <div className="flex items-center gap-3 px-4 py-3 rounded-xl bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-sm text-amber-800 dark:text-amber-300">
            <Clock className="h-4 w-4 animate-pulse shrink-0" />
            <span>
              Integrity scan running in background on{' '}
              <strong>{runningScanKeys.length}</strong> bucket{runningScanKeys.length !== 1 ? 's' : ''}.
              Click the <ShieldCheck className="h-3.5 w-3.5 inline text-amber-600" /> icon on a bucket to view progress.
            </span>
          </div>
        )}

        {/* ── Stats Cards ── */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 md:gap-6">
          <MetricCard
            title="Total Buckets"
            value={sortedBuckets.length}
            icon={Box}
            description="Active storage containers"
            color="brand"
          />
          <MetricCard
            title="Total Objects"
            value={sortedBuckets.reduce((s, b) => s + (b.object_count || b.objectCount || 0), 0).toLocaleString()}
            icon={HardDrive}
            description="Stored across all buckets"
            color="blue-light"
          />
          <MetricCard
            title="Total Size"
            value={formatSize(sortedBuckets.reduce((s, b) => s + (b.size || b.totalSize || 0), 0))}
            icon={HardDrive}
            description="Storage consumption"
            color="warning"
          />
        </div>

        {/* ── Search ── */}
        <div className="bg-gradient-to-r from-white to-gray-50 dark:from-gray-800 dark:to-gray-800/50 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm p-4">
          <div className="relative max-w-md">
            <div className="absolute left-4 top-1/2 transform -translate-y-1/2">
              <Search className="text-gray-400 dark:text-gray-500 h-5 w-5" />
            </div>
            <Input
              placeholder="Search buckets..."
              value={searchTerm}
              onChange={e => setSearchTerm(e.target.value)}
              className="pl-12 bg-white dark:bg-gray-900 border-gray-300 dark:border-gray-600 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500 focus:border-brand-500 rounded-lg shadow-sm"
            />
          </div>
        </div>

        {/* ── Table ── */}
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md overflow-hidden">
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Box className="h-5 w-5 text-brand-600 dark:text-brand-400" />
              All Buckets ({sortedBuckets.length})
            </h3>
          </div>

          <div className="overflow-x-auto">
            {paginatedBuckets.length === 0 ? (
              <EmptyState
                icon={Box}
                title="No buckets found"
                description={searchTerm
                  ? 'No buckets match your search criteria.'
                  : 'Get started by creating your first bucket.'}
                actionLabel={!searchTerm ? 'Create Bucket' : undefined}
                onAction={!searchTerm ? () => navigate('/buckets/create') : undefined}
                showAction={!searchTerm}
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <button onClick={() => handleSort('name')} className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors">
                        Name {sortIcon('name')}
                      </button>
                    </TableHead>
                    <TableHead>Region</TableHead>
                    <TableHead>Node</TableHead>
                    <TableHead>Owner</TableHead>
                    <TableHead>
                      <button onClick={() => handleSort('objectCount')} className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors">
                        Objects {sortIcon('objectCount')}
                      </button>
                    </TableHead>
                    <TableHead>
                      <button onClick={() => handleSort('size')} className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors">
                        Size {sortIcon('size')}
                      </button>
                    </TableHead>
                    <TableHead>
                      <button onClick={() => handleSort('creationDate')} className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors">
                        Created {sortIcon('creationDate')}
                      </button>
                    </TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {paginatedBuckets.map(bucket => {
                    const tenantId   = bucket.tenant_id || bucket.tenantId;
                    const bucketPath = tenantId ? `/buckets/${tenantId}/${bucket.name}` : `/buckets/${bucket.name}`;
                    const owner      = getOwnerDisplay(bucket);
                    const OwnerIcon  = owner.icon;
                    const ib         = getIntegrityButton(bucket);
                    const scanState  = scanStates[getBucketKey(bucket)];

                    return (
                      <TableRow key={`${tenantId || 'global'}-${bucket.name}`}>

                        {/* Name */}
                        <TableCell className="whitespace-nowrap">
                          <div className="flex items-center gap-3">
                            <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gradient-to-br from-brand-50 to-blue-50 dark:from-brand-900/30 dark:to-blue-900/30 shadow-sm">
                              <Box className="h-4 w-4 text-brand-600 dark:text-brand-400" />
                            </div>
                            <div>
                              <div className="flex items-center gap-2">
                                <Link
                                  to={bucketPath}
                                  className="text-sm font-semibold text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 transition-colors"
                                >
                                  {bucket.name}
                                </Link>
                                {/* Scanning badge inline with name */}
                                {scanState?.phase === 'running' && (
                                  <span className="inline-flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400">
                                    <Clock className="h-2.5 w-2.5 animate-spin" />
                                    {(() => {
                                      const cnt = bucket.object_count ?? bucket.objectCount ?? 0;
                                      return cnt > 0
                                        ? `${Math.min(100, Math.round(((scanState.checked) / cnt) * 100))}%`
                                        : 'scanning';
                                    })()}
                                  </span>
                                )}
                                {scanState?.phase === 'done' && scanState.corrupted > 0 && (
                                  <span className="inline-flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded bg-error-100 dark:bg-error-900/40 text-error-700 dark:text-error-400">
                                    <AlertTriangle className="h-2.5 w-2.5" />
                                    {scanState.corrupted} issue{scanState.corrupted !== 1 ? 's' : ''}
                                  </span>
                                )}
                              </div>
                              {bucket.objectLock?.objectLockEnabled && (
                                <span className="inline-flex items-center gap-1 bg-gradient-to-r from-blue-100 to-cyan-100 dark:from-blue-900/40 dark:to-cyan-900/40 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded-md text-xs font-medium shadow-sm mt-1">
                                  <Lock className="h-3 w-3" />
                                  WORM
                                </span>
                              )}
                            </div>
                          </div>
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          <span className="text-sm">{bucket.region || 'us-east-1'}</span>
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          {bucket.node_name || bucket.nodeName ? (
                            <div className="flex items-center gap-2">
                              <div className={`w-2 h-2 rounded-full ${(bucket.node_status || bucket.nodeStatus) === 'healthy' ? 'bg-green-500' : 'bg-yellow-500'}`} />
                              <span className="text-sm">{bucket.node_name || bucket.nodeName}</span>
                            </div>
                          ) : (
                            <span className="text-xs text-gray-400 dark:text-gray-500 italic">Local</span>
                          )}
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          <div className="flex items-center gap-2">
                            <OwnerIcon className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                            <span className={owner.type === 'global' ? 'text-sm text-gray-500 dark:text-gray-400 italic' : 'text-sm'}>
                              {owner.name}
                            </span>
                          </div>
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          <span className="text-sm">
                            {(bucket.object_count || bucket.objectCount || 0).toLocaleString()}
                          </span>
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          <span className="text-sm">{formatSize(bucket.size || bucket.totalSize || 0)}</span>
                        </TableCell>

                        <TableCell className="whitespace-nowrap">
                          <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                            <Calendar className="h-3 w-3" />
                            {formatDate(bucket.creation_date || bucket.creationDate || '')}
                          </div>
                        </TableCell>

                        {/* Actions */}
                        <TableCell className="whitespace-nowrap text-right">
                          <div className="flex items-center justify-end gap-2">
                            {/* Integrity button — dynamic appearance */}
                            <button
                              onClick={() => setIntegrityBucket(bucket)}
                              className={`relative ${ib.className}`}
                              title={ib.title}
                            >
                              {ib.icon}
                              {ib.badge}
                            </button>

                            <button
                              onClick={() => navigate(`${bucketPath}/settings`)}
                              className="p-2 text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 hover:bg-brand-50 dark:hover:bg-brand-900/20 rounded-lg transition-all duration-200"
                              title="Settings"
                            >
                              <Settings className="h-4 w-4" />
                            </button>

                            <button
                              onClick={() => handleDeleteBucket(bucket.name)}
                              disabled={deleteBucketMutation.isPending}
                              className="p-2 text-gray-600 dark:text-gray-400 hover:text-error-600 dark:hover:text-error-400 hover:bg-error-50 dark:hover:bg-error-900/20 rounded-lg transition-all duration-200 disabled:opacity-50"
                              title="Delete"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        </TableCell>

                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </div>

          {/* Pagination */}
          {sortedBuckets.length > 0 && (
            <div className="px-6 py-4 border-t border-gray-200 dark:border-gray-700 bg-gradient-to-r from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 flex items-center justify-between">
              <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
                Showing{' '}
                <span className="text-brand-600 dark:text-brand-400 font-semibold">{startIndex + 1}</span>
                {' '}to{' '}
                <span className="text-brand-600 dark:text-brand-400 font-semibold">{Math.min(startIndex + itemsPerPage, sortedBuckets.length)}</span>
                {' '}of{' '}
                <span className="text-brand-600 dark:text-brand-400 font-semibold">{sortedBuckets.length}</span>
                {' '}buckets
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  onClick={() => setCurrentPage(p => p - 1)}
                  disabled={currentPage === 1}
                  className="inline-flex items-center gap-1 bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 shadow-sm"
                >
                  <ChevronLeft className="h-4 w-4" />
                  Previous
                </Button>

                <div className="flex items-center gap-1">
                  {Array.from({ length: totalPages }, (_, i) => i + 1)
                    .filter(p => p === 1 || p === totalPages || (p >= currentPage - 1 && p <= currentPage + 1))
                    .map((page, idx, arr) => (
                      <React.Fragment key={page}>
                        {idx > 0 && page - arr[idx - 1] > 1 && (
                          <span className="px-2 text-gray-500 dark:text-gray-400">…</span>
                        )}
                        <button
                          onClick={() => setCurrentPage(page)}
                          className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-all duration-200 ${
                            currentPage === page
                              ? 'bg-gradient-to-r from-brand-600 to-brand-700 text-white shadow-md'
                              : 'bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 border border-gray-300 dark:border-gray-600 shadow-sm hover:bg-brand-50 dark:hover:bg-brand-900/20'
                          }`}
                        >
                          {page}
                        </button>
                      </React.Fragment>
                    ))}
                </div>

                <Button
                  variant="outline"
                  onClick={() => setCurrentPage(p => p + 1)}
                  disabled={currentPage === totalPages}
                  className="inline-flex items-center gap-1 bg-white dark:bg-gray-800 border-gray-300 dark:border-gray-600 shadow-sm"
                >
                  Next
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* ── Integrity modal ── */}
      {integrityBucket && (
        <BucketIntegrityModal
          isOpen={!!integrityBucket}
          bucket={integrityBucket}
          objectCount={integrityBucket.object_count ?? integrityBucket.objectCount ?? 0}
          scanState={scanStates[getBucketKey(integrityBucket)] ?? null}
          history={integrityHistory}
          rateLimitUntil={getRateLimitUntil(getBucketKey(integrityBucket))}
          onStart={() => startScan(integrityBucket)}
          onCancel={() => cancelScan(integrityBucket)}
          onHide={() => setIntegrityBucket(null)}
        />
      )}
    </>
  );
}
