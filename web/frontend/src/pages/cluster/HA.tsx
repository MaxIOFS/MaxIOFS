import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
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
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import { formatBytes } from '@/lib/utils';

type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown';

function HealthBadge({ status }: { status: string }) {
  const s = status as HealthStatus;
  if (s === 'healthy')
    return (
      <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400 text-sm font-medium">
        <CheckCircle className="h-4 w-4" /> Healthy
      </span>
    );
  if (s === 'degraded')
    return (
      <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400 text-sm font-medium">
        <AlertTriangle className="h-4 w-4" /> Degraded
      </span>
    );
  if (s === 'unavailable')
    return (
      <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400 text-sm font-medium">
        <XCircle className="h-4 w-4" /> Unavailable
      </span>
    );
  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground text-sm">
      <HelpCircle className="h-4 w-4" /> Unknown
    </span>
  );
}

const FACTOR_INFO = {
  1: { label: 'No replication', description: 'Single copy — no redundancy. Cluster fails if any node goes down.', color: 'text-red-600 dark:text-red-400' },
  2: { label: 'Mirror (×2)', description: 'Two complete copies. Tolerates 1 simultaneous node failure.', color: 'text-amber-600 dark:text-amber-400' },
  3: { label: 'Triple copy (×3)', description: 'Three complete copies. Tolerates 2 simultaneous node failures.', color: 'text-green-600 dark:text-green-400' },
};

export default function ClusterHA() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [selectedFactor, setSelectedFactor] = useState<number | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['cluster-ha'],
    queryFn: APIClient.getClusterHA,
    refetchInterval: 10000,
  });

  const setFactorMutation = useMutation({
    mutationFn: (factor: number) => APIClient.setClusterHA(factor),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['cluster-ha'] });
      setSelectedFactor(null);
      ModalManager.toast('success', `Replication factor changed to ×${result.new_factor}`);
    },
    onError: (error: any) => {
      ModalManager.apiError(error);
    },
  });

  const handleApplyFactor = () => {
    if (selectedFactor === null || !data) return;

    const info = FACTOR_INFO[selectedFactor as keyof typeof FACTOR_INFO];
    const isDowngrade = selectedFactor < data.replication_factor;

    ModalManager.confirm(
      isDowngrade ? 'Downgrade replication factor?' : 'Change replication factor?',
      isDowngrade
        ? `You are reducing the replication factor from ×${data.replication_factor} to ×${selectedFactor}. ${info.description} This will remove redundant copies from some nodes in the background.`
        : `You are changing the replication factor to ×${selectedFactor}. ${info.description} The cluster will sync all data to new replicas in the background.`,
      () => setFactorMutation.mutate(selectedFactor),
    );
  };

  if (isLoading) return <Loading />;

  if (error) {
    return (
      <div className="p-6">
        <div className="flex items-center gap-3 text-red-600 dark:text-red-400">
          <XCircle className="h-5 w-5" />
          <span>Failed to load cluster HA status. Is the cluster enabled?</span>
        </div>
      </div>
    );
  }

  if (!data) return null;

  const activeFactor = selectedFactor ?? data.replication_factor;
  const factorInfo = FACTOR_INFO[activeFactor as keyof typeof FACTOR_INFO];
  const hasChange = selectedFactor !== null && selectedFactor !== data.replication_factor;

  // Storage pressure: any node over 80%
  const pressureNodes = data.nodes.filter(
    (n) => n.capacity_total > 0 && n.capacity_used / n.capacity_total >= 0.8,
  );

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
            HA Replication
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Cluster-wide replication factor — applies to all buckets on all nodes
          </p>
        </div>
      </div>

      {/* Storage pressure warning */}
      {pressureNodes.length > 0 && (
        <div className="flex items-start gap-3 p-4 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg">
          <AlertTriangle className="h-5 w-5 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
          <div className="text-sm text-amber-800 dark:text-amber-300">
            <p className="font-semibold mb-1">Storage pressure detected</p>
            <p>
              {pressureNodes.map((n) => n.name).join(', ')}{' '}
              {pressureNodes.length === 1 ? 'is' : 'are'} above 80% capacity. Consider expanding
              storage or reducing the replication factor.
            </p>
          </div>
        </div>
      )}

      {/* Current status cards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Current factor</p>
          <p className={`text-2xl font-bold ${FACTOR_INFO[data.replication_factor as keyof typeof FACTOR_INFO].color}`}>
            ×{data.replication_factor}
          </p>
          <p className="text-xs text-muted-foreground mt-1">
            {FACTOR_INFO[data.replication_factor as keyof typeof FACTOR_INFO].label}
          </p>
        </div>
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Usable capacity</p>
          <p className="text-2xl font-bold text-foreground">{formatBytes(data.usable_bytes)}</p>
          <p className="text-xs text-muted-foreground mt-1">of {formatBytes(data.total_bytes)} total</p>
        </div>
        <div className="bg-card border border-border rounded-lg p-4 shadow-sm">
          <p className="text-xs text-muted-foreground uppercase tracking-wide mb-1">Tolerated failures</p>
          <p className="text-2xl font-bold text-foreground">{data.tolerated_failures}</p>
          <p className="text-xs text-muted-foreground mt-1">
            {data.tolerated_failures === 0
              ? 'No node can fail'
              : `${data.tolerated_failures} node${data.tolerated_failures > 1 ? 's' : ''} can fail simultaneously`}
          </p>
        </div>
      </div>

      {/* Factor selector */}
      <div className="bg-card border border-border rounded-lg p-6 shadow-sm space-y-4">
        <h2 className="text-base font-semibold text-foreground">Change replication factor</h2>

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
                      Current
                    </span>
                  )}
                </div>
                <p className="text-sm font-medium text-foreground">{info.label}</p>
                <p className="text-xs text-muted-foreground mt-1">{info.description}</p>
                {notEnoughNodes && (
                  <p className="text-xs text-red-500 mt-1">
                    Requires {f} healthy node{f > 1 ? 's' : ''} — cluster has {data.node_count}
                  </p>
                )}
              </button>
            );
          })}
        </div>

        {hasChange && (
          <div className="flex items-center gap-3 pt-2">
            <div className={`text-sm font-medium ${factorInfo.color}`}>
              {factorInfo.label} — {factorInfo.description}
            </div>
            <div className="flex-1" />
            <Button variant="outline" onClick={() => setSelectedFactor(null)}>
              Cancel
            </Button>
            <Button
              onClick={handleApplyFactor}
              loading={setFactorMutation.isPending}
              disabled={setFactorMutation.isPending}
            >
              Apply ×{activeFactor}
            </Button>
          </div>
        )}
      </div>

      {/* Node status table */}
      <div className="bg-card border border-border rounded-lg shadow-sm">
        <div className="px-6 py-4 border-b border-border">
          <h2 className="text-base font-semibold text-foreground flex items-center gap-2">
            <Server className="h-4 w-4" />
            Node storage ({data.node_count} node{data.node_count !== 1 ? 's' : ''})
          </h2>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Node</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Status</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Used</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Free</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Total</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">Usage</th>
              </tr>
            </thead>
            <tbody>
              {data.nodes.map((node) => {
                const usagePct =
                  node.capacity_total > 0
                    ? Math.round((node.capacity_used / node.capacity_total) * 100)
                    : 0;
                const isPressure = usagePct >= 80;

                return (
                  <tr key={node.id} className="border-b border-border last:border-0 hover:bg-muted/20">
                    <td className="px-6 py-4 font-medium text-foreground">
                      <div className="flex items-center gap-2">
                        <Database className="h-4 w-4 text-muted-foreground" />
                        {node.name}
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
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
