import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import {
  Server,
  ArrowLeft,
  CheckCircle,
  XCircle,
  AlertTriangle,
  HelpCircle,
  Shield,
  Database,
  RefreshCw,
  Clock,
  PowerOff,
  Skull,
  Gauge,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import { formatBytes } from '@/lib/utils';

type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown' | 'dead' | 'storage_pressure';
type SyncStatus = 'running' | 'done' | 'failed';

function HealthBadge({ status }: { status: string }) {
  const { t } = useTranslation('cluster');
  const s = status as HealthStatus;
  if (s === 'healthy')
    return (
      <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400 text-sm font-medium">
        <CheckCircle className="h-4 w-4" /> {t('statusHealthy')}
      </span>
    );
  if (s === 'storage_pressure')
    return (
      <span className="inline-flex items-center gap-1 text-orange-600 dark:text-orange-400 text-sm font-medium">
        <Gauge className="h-4 w-4" /> {t('statusStoragePressure')}
      </span>
    );
  if (s === 'degraded')
    return (
      <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400 text-sm font-medium">
        <AlertTriangle className="h-4 w-4" /> {t('statusDegraded')}
      </span>
    );
  if (s === 'unavailable')
    return (
      <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400 text-sm font-medium">
        <XCircle className="h-4 w-4" /> {t('statusUnavailable')}
      </span>
    );
  if (s === 'dead')
    return (
      <span className="inline-flex items-center gap-1 text-red-700 dark:text-red-500 text-sm font-semibold">
        <Skull className="h-4 w-4" /> {t('statusDead')}
      </span>
    );
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground text-sm">
      <HelpCircle className="h-4 w-4" /> {t('statusUnknown')}
    </span>
  );
}

function SyncBadge({ status }: { status: string }) {
  const { t } = useTranslation('cluster');
  const s = status as SyncStatus;
  if (s === 'done')
    return (
      <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400 text-sm font-medium">
        <CheckCircle className="h-4 w-4" /> {t('syncReady')}
      </span>
    );
  if (s === 'running')
    return (
      <span className="inline-flex items-center gap-1 text-blue-600 dark:text-blue-400 text-sm font-medium">
        <RefreshCw className="h-4 w-4 animate-spin" /> {t('syncSyncing')}
      </span>
    );
  if (s === 'failed')
    return (
      <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400 text-sm font-medium">
        <XCircle className="h-4 w-4" /> {t('syncFailed')}
      </span>
    );
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground text-sm">
      <Clock className="h-4 w-4" /> {t('syncPending')}
    </span>
  );
}

export default function ClusterHA() {
  const { t } = useTranslation('cluster');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedFactor, setSelectedFactor] = useState<number | null>(null);

  // Factor metadata — labels and descriptions come from i18n
  const FACTOR_INFO = {
    1: { labelKey: 'factorNoReplication', descKey: 'factorNoReplicationDesc', color: 'text-red-600 dark:text-red-400' },
    2: { labelKey: 'factorMirror',        descKey: 'factorMirrorDesc',        color: 'text-amber-600 dark:text-amber-400' },
    3: { labelKey: 'factorTriple',        descKey: 'factorTripleDesc',        color: 'text-green-600 dark:text-green-400' },
  } as const;

  const { data, isLoading, error } = useQuery({
    queryKey: ['cluster-ha'],
    queryFn: APIClient.getClusterHA,
    refetchInterval: 10000,
  });

  const { data: syncData } = useQuery({
    queryKey: ['cluster-ha-sync-jobs'],
    queryFn: APIClient.getClusterHASyncJobs,
    refetchInterval: 5000,
  });

  const { data: degraded } = useQuery({
    queryKey: ['cluster-degraded-state'],
    queryFn: APIClient.getClusterDegradedState,
    refetchInterval: 10000,
  });

  const { data: scrubStatus } = useQuery({
    queryKey: ['cluster-ha-scrub-status'],
    queryFn: APIClient.getHAScrubStatus,
    refetchInterval: 15000,
  });

  const setFactorMutation = useMutation({
    mutationFn: (factor: number) => APIClient.setClusterHA(factor),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['cluster-ha'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-ha-sync-jobs'] });
      setSelectedFactor(null);
      ModalManager.toast('success', t('factorChangedSuccess', { factor: result.new_factor }));
    },
    onError: (error: any) => {
      ModalManager.apiError(error);
    },
  });

  const drainMutation = useMutation({
    mutationFn: (vars: { nodeId: string; reason: string }) =>
      APIClient.drainClusterNode(vars.nodeId, vars.reason),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['cluster-ha'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-ha-sync-jobs'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-degraded-state'] });
      ModalManager.toast('success', t('drainNodeSuccess', { node: result.node_id }));
    },
    onError: (error: any) => {
      ModalManager.apiError(error);
    },
  });

  const handleDrainNode = (nodeId: string, nodeName: string) => {
    ModalManager.confirm(
      t('drainNodeConfirmTitle'),
      t('drainNodeConfirmBody', { node: nodeName }),
      () => drainMutation.mutate({ nodeId, reason: 'manual drain from HA admin page' }),
    );
  };

  const handleApplyFactor = () => {
    if (selectedFactor === null || !data) return;

    const info = FACTOR_INFO[selectedFactor as keyof typeof FACTOR_INFO];
    const isDowngrade = selectedFactor < data.replication_factor;
    const desc = t(info.descKey);

    ModalManager.confirm(
      isDowngrade ? t('confirmDowngradeTitle') : t('confirmChangeTitle'),
      isDowngrade
        ? t('confirmDowngradeBody', { from: data.replication_factor, to: selectedFactor, desc })
        : t('confirmChangeBody', { factor: selectedFactor, desc }),
      () => setFactorMutation.mutate(selectedFactor),
    );
  };

  if (isLoading) return <Loading />;

  if (error) {
    return (
      <div className="p-6">
        <div className="flex items-center gap-3 text-red-600 dark:text-red-400">
          <XCircle className="h-5 w-5" />
          <span>{t('haLoadError')}</span>
        </div>
      </div>
    );
  }

  if (!data) return null;

  const activeFactor = selectedFactor ?? data.replication_factor;
  const activeInfo = FACTOR_INFO[activeFactor as keyof typeof FACTOR_INFO];
  const hasChange = selectedFactor !== null && selectedFactor !== data.replication_factor;

  // Storage pressure: any node over 80%
  const pressureNodes = data.nodes.filter(
    (n) => n.capacity_total > 0 && n.capacity_used / n.capacity_total >= 0.8,
  );

  // Build per-node sync status from most-recent job per node
  const latestJobByNode: Record<string, (typeof syncData)['sync_jobs'][number]> = {};
  for (const job of syncData?.sync_jobs ?? []) {
    if (!latestJobByNode[job.target_node_id]) {
      latestJobByNode[job.target_node_id] = job;
    }
  }
  const syncRows = data.nodes
    .filter((n) => latestJobByNode[n.id])
    .map((n) => ({ node: n, job: latestJobByNode[n.id] }));

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/cluster')}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <Shield className="h-6 w-6" />
            {t('haReplication')}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {t('haReplicationDesc')}
          </p>
        </div>
      </div>

      {/* Cluster degraded banner — set by the dead-node reconciler when it
          refuses to mark a node dead because doing so would drop the count
          of non-dead nodes below the configured replication factor. */}
      {degraded?.degraded && (
        <div className="flex items-start gap-3 p-4 bg-red-50 dark:bg-red-900/20 border border-red-300 dark:border-red-800 rounded-lg">
          <XCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
          <div className="text-sm text-red-800 dark:text-red-300">
            <p className="font-semibold mb-1">{t('clusterDegradedTitle')}</p>
            <p>{degraded.reason || t('clusterDegradedFallback')}</p>
          </div>
        </div>
      )}

      {/* Storage pressure warning */}
      {pressureNodes.length > 0 && (
        <div className="flex items-start gap-3 p-4 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg">
          <AlertTriangle className="h-5 w-5 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
          <div className="text-sm text-amber-800 dark:text-amber-300">
            <p className="font-semibold mb-1">{t('storagePressureTitle')}</p>
            <p>
              {t(`storagePressureDesc_${pressureNodes.length === 1 ? 'one' : 'other'}`, {
                nodes: pressureNodes.map((n) => n.name).join(', '),
              })}
            </p>
          </div>
        </div>
      )}

      {/* Current status cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">{t('currentFactor')}</p>
          <p className={`text-2xl font-bold ${FACTOR_INFO[data.replication_factor as keyof typeof FACTOR_INFO].color}`}>
            ×{data.replication_factor}
          </p>
          <p className="text-xs text-muted-foreground mt-1">
            {t(FACTOR_INFO[data.replication_factor as keyof typeof FACTOR_INFO].labelKey)}
          </p>
        </div>
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">{t('usableCapacity')}</p>
          <p className="text-2xl font-bold text-foreground">{formatBytes(data.usable_bytes)}</p>
          <p className="text-xs text-muted-foreground mt-1">{t('ofTotal', { total: formatBytes(data.total_bytes) })}</p>
        </div>
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">{t('toleratedFailures')}</p>
          <p className="text-2xl font-bold text-foreground">{data.tolerated_failures}</p>
          <p className="text-xs text-muted-foreground mt-1">
            {data.tolerated_failures === 0
              ? t('noNodeCanFail')
              : t(`nodesCan_${data.tolerated_failures === 1 ? 'one' : 'other'}`, { count: data.tolerated_failures })}
          </p>
        </div>
      </div>

      {/* Factor selector */}
      <div className="bg-card border border-border rounded-lg p-6 shadow-sm space-y-4">
        <h2 className="text-base font-semibold text-foreground">{t('changeReplicationFactor')}</h2>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {([1, 2, 3] as const).map((f) => {
            const info = FACTOR_INFO[f];
            const isActive = f === data.replication_factor;
            const isSelected = f === selectedFactor;
            const notEnoughNodes = data.node_count < f;

            return (
              <button
                key={f}
                disabled={notEnoughNodes}
                onClick={() => setSelectedFactor(isSelected ? null : f)}
                className={`text-left p-4 rounded-lg border-2 transition-colors ${
                  isSelected
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                    : isActive
                    ? 'border-border bg-muted/40'
                    : 'border-border hover:border-muted-foreground'
                } ${notEnoughNodes ? 'opacity-40 cursor-not-allowed' : 'cursor-pointer'}`}
              >
                <div className="flex items-center justify-between mb-1">
                  <span className={`text-lg font-bold ${info.color}`}>×{f}</span>
                  {isActive && (
                    <span className="text-xs bg-muted text-muted-foreground px-2 py-0.5 rounded-full">
                      {t('factorCurrent')}
                    </span>
                  )}
                </div>
                <p className="text-sm font-medium text-foreground">{t(info.labelKey)}</p>
                <p className="text-xs text-muted-foreground mt-1">{t(info.descKey)}</p>
                {notEnoughNodes && (
                  <p className="text-xs text-red-500 mt-1">
                    {t(`factorRequires_${f === 1 ? 'one' : 'other'}`, { count: f, actual: data.node_count })}
                  </p>
                )}
              </button>
            );
          })}
        </div>

        {hasChange && (
          <div className="flex items-center gap-3 pt-2">
            <div className={`text-sm font-medium ${activeInfo.color}`}>
              {t(activeInfo.labelKey)} — {t(activeInfo.descKey)}
            </div>
            <div className="flex-1" />
            <Button variant="outline" onClick={() => setSelectedFactor(null)}>
              {t('cancel')}
            </Button>
            <Button
              onClick={handleApplyFactor}
              loading={setFactorMutation.isPending}
              disabled={setFactorMutation.isPending}
            >
              {t('applyFactor', { factor: activeFactor })}
            </Button>
          </div>
        )}
      </div>

      {/* Replica sync status */}
      {syncRows.length > 0 && (
        <div className="bg-card border border-border rounded-lg shadow-sm">
          <div className="px-6 py-4 border-b border-border">
            <h2 className="text-base font-semibold text-foreground flex items-center gap-2">
              <RefreshCw className="h-4 w-4" />
              {t('replicaSyncStatus')}
            </h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              {t('replicaSyncDesc')}
            </p>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/30">
                  <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colNode')}</th>
                  <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colSyncStatus')}</th>
                  <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colObjectsSynced')}</th>
                  <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colProgress')}</th>
                  <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colLastUpdated')}</th>
                </tr>
              </thead>
              <tbody>
                {syncRows.map(({ node, job }) => {
                  const completedAt = job.completed_at ? new Date(job.completed_at) : null;
                  const startedAt = new Date(job.started_at);
                  const isRunning = job.status === 'running';

                  return (
                    <tr key={node.id} className="border-b border-border last:border-0 hover:bg-muted/20">
                      <td className="px-6 py-4 font-medium text-foreground">
                        <div className="flex items-center gap-2">
                          <Database className="h-4 w-4 text-muted-foreground" />
                          {node.name}
                        </div>
                      </td>
                      <td className="px-6 py-4">
                        <SyncBadge status={job.status} />
                        {job.status === 'failed' && job.error_message && (
                          <p className="text-xs text-red-500 mt-1 max-w-xs truncate" title={job.error_message}>
                            {job.error_message}
                          </p>
                        )}
                      </td>
                      <td className="px-6 py-4 text-muted-foreground tabular-nums">
                        {job.objects_synced.toLocaleString()}
                      </td>
                      <td className="px-6 py-4 w-40">
                        {isRunning ? (
                          <div className="flex items-center gap-2">
                            <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                              <div className="h-2 bg-blue-500 rounded-full animate-pulse w-1/3" />
                            </div>
                            <span className="text-xs text-muted-foreground">{t('syncInProgress')}</span>
                          </div>
                        ) : job.status === 'done' ? (
                          <div className="flex items-center gap-2">
                            <div className="flex-1 bg-muted rounded-full h-2">
                              <div className="h-2 bg-green-500 rounded-full w-full" />
                            </div>
                            <span className="text-xs text-green-600 dark:text-green-400">100%</span>
                          </div>
                        ) : (
                          <div className="flex items-center gap-2">
                            <div className="flex-1 bg-muted rounded-full h-2">
                              <div className="h-2 bg-red-500 rounded-full w-full" />
                            </div>
                            <span className="text-xs text-red-500">{t('syncFailed')}</span>
                          </div>
                        )}
                      </td>
                      <td className="px-6 py-4 text-muted-foreground text-xs">
                        {completedAt
                          ? completedAt.toLocaleString()
                          : t('syncStartedAt', { time: startedAt.toLocaleString() })}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Node storage table */}
      <div className="bg-card border border-border rounded-lg shadow-sm">
        <div className="px-6 py-4 border-b border-border">
          <h2 className="text-base font-semibold text-foreground flex items-center gap-2">
            <Server className="h-4 w-4" />
            {t(`nodeStorage_${data.node_count === 1 ? 'one' : 'other'}`, { count: data.node_count })}
          </h2>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colNode')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colStatus')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colUsed')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colFree')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colTotal')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colUsage')}</th>
                <th className="text-right px-6 py-3 font-medium text-muted-foreground">{t('colActions')}</th>
              </tr>
            </thead>
            <tbody>
              {data.nodes.map((node) => {
                const usagePct =
                  node.capacity_total > 0
                    ? Math.round((node.capacity_used / node.capacity_total) * 100)
                    : 0;
                const isPressure = usagePct >= 80;
                const isLocal = node.id === data.local_node_id;
                const isDead = node.health_status === 'dead';
                const drainDisabled = isLocal || isDead || drainMutation.isPending;

                return (
                  <tr key={node.id} className="border-b border-border last:border-0 hover:bg-muted/20">
                    <td className="px-6 py-4 font-medium text-foreground">
                      <div className="flex items-center gap-2">
                        <Database className="h-4 w-4 text-muted-foreground" />
                        {node.name}
                        {isLocal && (
                          <span className="text-xs text-muted-foreground font-normal">{t('thisNode')}</span>
                        )}
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <HealthBadge status={node.health_status} />
                    </td>
                    <td className="px-6 py-4 text-muted-foreground">{formatBytes(node.capacity_used)}</td>
                    <td className="px-6 py-4 text-muted-foreground">{formatBytes(node.capacity_free)}</td>
                    <td className="px-6 py-4 text-muted-foreground">{formatBytes(node.capacity_total)}</td>
                    <td className="px-6 py-4 w-48">
                      <div className="flex items-center gap-2">
                        <div className="flex-1 bg-muted rounded-full h-2">
                          <div
                            className={`h-2 rounded-full transition-all ${
                              isPressure
                                ? 'bg-red-500'
                                : usagePct >= 60
                                ? 'bg-amber-500'
                                : 'bg-green-500'
                            }`}
                            style={{ width: `${usagePct}%` }}
                          />
                        </div>
                        <span className={`text-xs font-medium w-8 text-right ${isPressure ? 'text-red-500' : 'text-muted-foreground'}`}>
                          {usagePct}%
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4 text-right">
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={drainDisabled}
                        onClick={() => handleDrainNode(node.id, node.name)}
                        title={isLocal ? t('drainLocalDisabled') : isDead ? t('drainDeadDisabled') : t('drainNode')}
                      >
                        <PowerOff className="h-3.5 w-3.5 mr-1" />
                        {t('drainNode')}
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* Anti-entropy scrubber */}
      <ScrubberSection scrubStatus={scrubStatus} />
    </div>
  );
}

function ScrubberSection({
  scrubStatus,
}: {
  scrubStatus:
    | Awaited<ReturnType<typeof APIClient.getHAScrubStatus>>
    | undefined;
}) {
  const { t } = useTranslation('cluster');
  if (!scrubStatus) return null;

  const current = scrubStatus.current;
  const lastRun = scrubStatus.runs?.[0];
  const totalBuckets = current?.bucket_order?.length ?? 0;
  const progressPct =
    current && totalBuckets > 0
      ? Math.min(100, Math.round((current.current_bucket_idx / totalBuckets) * 100))
      : 0;

  return (
    <div className="bg-card border border-border rounded-lg shadow-sm">
      <div className="px-6 py-4 border-b border-border">
        <h2 className="text-base font-semibold text-foreground flex items-center gap-2">
          <RefreshCw className="h-4 w-4" />
          {t('scrubberTitle')}
        </h2>
        <p className="text-xs text-muted-foreground mt-0.5">{t('scrubberDesc')}</p>
      </div>

      <div className="p-6 space-y-4">
        {current ? (
          <div className="space-y-3">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium text-foreground">{t('scrubberCurrentCycle')}</span>
              <span className="text-xs text-muted-foreground">
                {t('scrubberStartedAt', { time: new Date(current.started_at).toLocaleString() })}
              </span>
            </div>
            <div className="flex items-center gap-3">
              <div className="flex-1 bg-muted rounded-full h-2 overflow-hidden">
                <div
                  className="h-2 bg-blue-500 rounded-full transition-all"
                  style={{ width: `${progressPct}%` }}
                />
              </div>
              <span className="text-xs text-muted-foreground tabular-nums w-24 text-right">
                {t('scrubberBucketsProgress', { current: current.current_bucket_idx, total: totalBuckets })}
              </span>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('scrubberObjectsCompared')}</p>
                <p className="font-medium text-foreground tabular-nums">{current.objects_compared.toLocaleString()}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('scrubberDivergencesFound')}</p>
                <p className="font-medium text-amber-600 dark:text-amber-400 tabular-nums">
                  {current.divergences_found.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('scrubberDivergencesFixed')}</p>
                <p className="font-medium text-green-600 dark:text-green-400 tabular-nums">
                  {current.divergences_fixed.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('scrubberBucketsScanned')}</p>
                <p className="font-medium text-foreground tabular-nums">{current.buckets_scanned.toLocaleString()}</p>
              </div>
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Clock className="h-4 w-4" />
            {t('scrubberIdle')}
          </div>
        )}

        {lastRun && (
          <div className="border-t border-border pt-4">
            <p className="text-xs text-muted-foreground uppercase tracking-wide mb-2">{t('scrubberLastRun')}</p>
            <div className="grid grid-cols-2 sm:grid-cols-5 gap-4 text-sm">
              <div>
                <p className="text-xs text-muted-foreground">{t('scrubberStatus')}</p>
                <p className="font-medium text-foreground capitalize">{lastRun.status}</p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">{t('scrubberCompletedAt')}</p>
                <p className="font-medium text-foreground text-xs">
                  {lastRun.completed_at ? new Date(lastRun.completed_at).toLocaleString() : '—'}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">{t('scrubberObjectsCompared')}</p>
                <p className="font-medium text-foreground tabular-nums">
                  {lastRun.objects_compared.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">{t('scrubberDivergencesFound')}</p>
                <p className="font-medium text-foreground tabular-nums">
                  {lastRun.divergences_found.toLocaleString()}
                </p>
              </div>
              <div>
                <p className="text-xs text-muted-foreground">{t('scrubberDivergencesFixed')}</p>
                <p className="font-medium text-foreground tabular-nums">
                  {lastRun.divergences_fixed.toLocaleString()}
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
