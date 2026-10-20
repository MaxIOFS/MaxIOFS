import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Layers, Plus, Trash2, AlertTriangle, XCircle, Database, Shield, Network } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import { formatBytes } from '@/lib/utils';
import type { StorageClassState } from '@/types';

// Protection is how a class keeps its data: whole copies, or parity.
type Protection = 1 | 2 | 3 | 'parity';
const PROTECTIONS: Protection[] = [1, 2, 3, 'parity'];
const STANDARD = 'STANDARD';

const PROTECTION_KEYS: Record<string, { labelKey: string; descKey: string }> = {
  '1': { labelKey: 'factorNoReplication', descKey: 'factorNoReplicationDesc' },
  '2': { labelKey: 'factorMirror', descKey: 'factorMirrorDesc' },
  '3': { labelKey: 'factorTriple', descKey: 'factorTripleDesc' },
  parity: { labelKey: 'factorParity', descKey: 'factorParityDesc' },
};

function protectionOf(c: StorageClassState): Protection {
  return c.parity ? 'parity' : (c.factor as Protection);
}

function parseProtection(value: string): Protection {
  return value === 'parity' ? 'parity' : (Number(value) as Protection);
}

// layoutOf is how a class keeps a write now: ×N, or data+parity shards.
function layoutOf(c: StorageClassState): string {
  if (!c.parity) return `×${c.factor}`;
  return c.data_shards > 0 ? `${c.data_shards}+${c.parity_shards}` : '×2';
}

function ProtectionSelect({ value, onChange, disabled, label }: {
  value: Protection;
  onChange: (p: Protection) => void;
  disabled?: boolean;
  label: string;
}) {
  const { t } = useTranslation('cluster');
  return (
    <select
      aria-label={label}
      value={String(value)}
      disabled={disabled}
      onChange={(e) => onChange(parseProtection(e.target.value))}
      className="px-3 py-2 border border-border bg-card text-foreground rounded-md text-sm"
    >
      {PROTECTIONS.map((p) => (
        <option key={String(p)} value={String(p)}>
          {t(PROTECTION_KEYS[String(p)].labelKey)}
        </option>
      ))}
    </select>
  );
}

function ClassCard({ c, onProtection, onDelete, busy }: {
  c: StorageClassState;
  onProtection: (p: Protection) => void;
  onDelete: () => void;
  busy: boolean;
}) {
  const { t } = useTranslation('cluster');
  const navigate = useNavigate();
  const isStandard = c.name === STANDARD;
  return (
    <div className="bg-card border border-border rounded-lg p-5 shadow-sm space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-foreground flex items-center gap-2">
            <Layers className="h-4 w-4 text-muted-foreground" />
            {c.name}
            {isStandard && (
              <span className="text-xs bg-muted text-muted-foreground px-2 py-0.5 rounded-full font-normal">
                {t('storageClassDefault')}
              </span>
            )}
          </h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t(`storageClassNodes_${c.node_count === 1 ? 'one' : 'other'}`, { count: c.node_count })}
          </p>
        </div>
        <span className="text-xl font-bold text-foreground">{layoutOf(c)}</span>
      </div>

      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('usableCapacity')}</p>
          <p className="font-medium text-foreground">{formatBytes(c.usable_bytes)}</p>
          <p className="text-xs text-muted-foreground">{t('ofTotal', { total: formatBytes(c.total_bytes) })}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground uppercase tracking-wide">{t('toleratedFailures')}</p>
          <p className="font-medium text-foreground">{c.tolerated_failures}</p>
        </div>
      </div>

      {c.below_protection && (
        <div className="flex items-start gap-2 p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md">
          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
          <p className="text-xs text-amber-800 dark:text-amber-300">
            {c.node_count === 0
              ? t('storageClassNoNodes')
              : t('storageClassBelowProtection', { count: c.takers, required: Math.max(c.required_nodes, c.data_shards + c.parity_shards) })}
          </p>
        </div>
      )}

      <div className="flex items-center gap-2 pt-1 border-t border-border">
        <p className="text-xs text-muted-foreground uppercase tracking-wide flex-1 pt-2">{t('storageClassProtection')}</p>
        {isStandard ? (
          <Button size="sm" variant="outline" className="mt-2" onClick={() => navigate('/cluster/ha')}>
            <Shield className="h-3.5 w-3.5" />
            {t('storageClassSetOnHA')}
          </Button>
        ) : (
          <div className="flex items-center gap-2 pt-2">
            <ProtectionSelect
              label={t('storageClassProtectionOf', { name: c.name })}
              value={protectionOf(c)}
              onChange={onProtection}
              disabled={busy}
            />
            <Button size="sm" variant="outline" onClick={onDelete} disabled={busy} title={t('storageClassDelete')}>
              <Trash2 className="h-3.5 w-3.5" />
              {t('storageClassDelete')}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

export default function StorageClasses() {
  const { t } = useTranslation('cluster');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [newName, setNewName] = useState('');
  const [newProtection, setNewProtection] = useState<Protection>(2);

  const { data: clusterConfig, isLoading: isConfigLoading, error: configError } = useQuery({
    queryKey: ['cluster-config'],
    queryFn: APIClient.getClusterConfig,
  });
  const isClusterEnabled = clusterConfig?.is_cluster_enabled === true;

  const { data, isLoading, error } = useQuery({
    queryKey: ['cluster-storage-classes'],
    queryFn: APIClient.getStorageClasses,
    refetchInterval: 10000,
    enabled: isClusterEnabled,
  });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['cluster-storage-classes'] });
    queryClient.invalidateQueries({ queryKey: ['cluster-ha'] });
    queryClient.invalidateQueries({ queryKey: ['serverConfig'] });
  };

  const setClass = useMutation({
    mutationFn: (v: { name: string; protection: Protection }) => APIClient.setStorageClass(v.name, v.protection),
    onSuccess: (_, v) => {
      refresh();
      setNewName('');
      ModalManager.toast('success', t('storageClassSaved', { name: v.name }));
    },
    onError: (err: unknown) => {
      ModalManager.apiError(err);
    },
  });

  const deleteClass = useMutation({
    mutationFn: (name: string) => APIClient.deleteStorageClass(name),
    onSuccess: (_, name) => {
      refresh();
      ModalManager.toast('success', t('storageClassDeleted', { name }));
    },
    onError: (err: unknown) => {
      ModalManager.apiError(err);
    },
  });

  const moveNode = useMutation({
    mutationFn: (v: { nodeId: string; node: string; to: string }) => APIClient.setNodeStorageClass(v.nodeId, v.to),
    onSuccess: (_, v) => {
      refresh();
      ModalManager.toast('success', t('moveNodeSuccess', { node: v.node, to: v.to }));
    },
    onError: (err: unknown) => {
      ModalManager.apiError(err);
    },
  });

  const busy = setClass.isPending || deleteClass.isPending || moveNode.isPending;

  const handleProtection = (c: StorageClassState, p: Protection) => {
    if (p === protectionOf(c)) return;
    ModalManager.confirm(
      t('storageClassChangeConfirmTitle', { name: c.name }),
      t('storageClassChangeConfirmBody', { name: c.name, desc: t(PROTECTION_KEYS[String(p)].descKey) }),
      () => setClass.mutate({ name: c.name, protection: p }),
    );
  };

  const handleDelete = (name: string) => {
    ModalManager.confirm(
      t('storageClassDeleteConfirmTitle'),
      t('storageClassDeleteConfirmBody', { name }),
      () => deleteClass.mutate(name),
    );
  };

  const handleMove = (nodeId: string, node: string, from: string, to: string) => {
    if (from === to) return;
    ModalManager.confirm(
      t('moveNodeConfirmTitle'),
      t('moveNodeConfirmBody', { node, from, to }),
      () => moveNode.mutate({ nodeId, node, to }),
    );
  };

  const header = (
    <div className="flex items-center gap-4">
      <Button variant="ghost" size="sm" onClick={() => navigate('/cluster')}>
        <ArrowLeft className="h-4 w-4" />
      </Button>
      <div>
        <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
          <Layers className="h-6 w-6" />
          {t('storageClasses')}
        </h1>
        <p className="text-sm text-muted-foreground mt-0.5">{t('storageClassesDesc')}</p>
      </div>
    </div>
  );

  if (isConfigLoading || (isClusterEnabled && isLoading)) return <Loading />;

  if (configError || error) {
    return (
      <div className="p-6">
        <div className="flex items-center gap-3 text-red-600 dark:text-red-400">
          <XCircle className="h-5 w-5" />
          <span>{t('storageClassesLoadError')}</span>
        </div>
      </div>
    );
  }

  if (!isClusterEnabled) {
    return (
      <div className="p-6 space-y-6">
        {header}
        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-8 text-center">
          <Network className="w-16 h-16 text-blue-600 dark:text-blue-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-foreground mb-2">{t('clusterNotInitialized')}</h2>
          <p className="text-muted-foreground mb-6">{t('clusterNotInitializedDesc')}</p>
          <Button onClick={() => navigate('/cluster')}>{t('clusterManagement')}</Button>
        </div>
      </div>
    );
  }

  if (!data) return null;
  const classNames = data.classes.map((c) => c.name);

  return (
    <div className="p-6 space-y-6">
      {header}

      <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
        <p className="text-sm text-blue-800 dark:text-blue-200">{t('storageClassesInfo')}</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        {data.classes.map((c) => (
          <ClassCard
            key={c.name}
            c={c}
            busy={busy}
            onProtection={(p) => handleProtection(c, p)}
            onDelete={() => handleDelete(c.name)}
          />
        ))}
      </div>

      <div className="bg-card border border-border rounded-lg p-6 shadow-sm space-y-4">
        <div>
          <h2 className="text-base font-semibold text-foreground">{t('addStorageClass')}</h2>
          <p className="text-xs text-muted-foreground mt-0.5">{t('addStorageClassDesc')}</p>
        </div>
        {data.definable.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('storageClassAllAdded')}</p>
        ) : (
          <div className="flex flex-wrap items-end gap-3">
            <div>
              <label htmlFor="new-storage-class" className="block text-xs text-muted-foreground mb-1">{t('storageClassName')}</label>
              <select
                id="new-storage-class"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                className="px-3 py-2 border border-border bg-card text-foreground rounded-md text-sm min-w-48"
              >
                <option value="">{t('storageClassPick')}</option>
                {data.definable.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
            </div>
            <div>
              <p className="block text-xs text-muted-foreground mb-1">{t('storageClassProtection')}</p>
              <ProtectionSelect label={t('storageClassNewProtection')} value={newProtection} onChange={setNewProtection} disabled={busy} />
            </div>
            <Button
              onClick={() => setClass.mutate({ name: newName, protection: newProtection })}
              disabled={!newName || busy}
              loading={setClass.isPending}
            >
              <Plus className="h-4 w-4" />
              {t('storageClassAdd')}
            </Button>
          </div>
        )}
      </div>

      <div className="bg-card border border-border rounded-lg shadow-sm">
        <div className="px-6 py-4 border-b border-border">
          <h2 className="text-base font-semibold text-foreground">{t('storageClassNodesTitle')}</h2>
          <p className="text-xs text-muted-foreground mt-0.5">{t('storageClassNodesDesc')}</p>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/30">
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colNode')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colStatus')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colUsed')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colTotal')}</th>
                <th className="text-left px-6 py-3 font-medium text-muted-foreground">{t('colStorageClass')}</th>
              </tr>
            </thead>
            <tbody>
              {data.nodes.map((n) => {
                const gone = n.health_status === 'dead';
                return (
                  <tr key={n.id} className="border-b border-border last:border-0 hover:bg-muted/20">
                    <td className="px-6 py-4 font-medium text-foreground">
                      <div className="flex items-center gap-2">
                        <Database className="h-4 w-4 text-muted-foreground" />
                        {n.name}
                      </div>
                    </td>
                    <td className="px-6 py-4 text-muted-foreground">{t(n.drained ? 'statusDraining' : healthKey(n.health_status))}</td>
                    <td className="px-6 py-4 text-muted-foreground">{formatBytes(n.capacity_used)}</td>
                    <td className="px-6 py-4 text-muted-foreground">{formatBytes(n.capacity_total)}</td>
                    <td className="px-6 py-4">
                      <select
                        aria-label={t('storageClassOfNode', { node: n.name })}
                        value={n.storage_class}
                        disabled={busy || gone || n.drained}
                        onChange={(e) => handleMove(n.id, n.name, n.storage_class, e.target.value)}
                        className="px-3 py-1.5 border border-border bg-card text-foreground rounded-md text-sm"
                      >
                        {classNames.map((name) => (
                          <option key={name} value={name}>{name}</option>
                        ))}
                      </select>
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

function healthKey(status: string): string {
  switch (status) {
    case 'healthy':
      return 'statusHealthy';
    case 'degraded':
      return 'statusDegraded';
    case 'unavailable':
      return 'statusUnavailable';
    case 'dead':
      return 'statusDead';
    case 'storage_pressure':
      return 'statusStoragePressure';
    default:
      return 'statusUnknown';
  }
}
