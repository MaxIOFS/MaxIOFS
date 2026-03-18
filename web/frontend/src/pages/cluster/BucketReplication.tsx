import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import {
  Package,
  RefreshCw,
  CheckCircle,
  XCircle,
  Settings,
  ArrowLeft,
  X,
  AlertTriangle
} from 'lucide-react';
import APIClient from '@/lib/api';
import { getErrorMessage } from '@/lib/utils';
import type { BucketWithReplication, ClusterNode, CreateClusterReplicationRequest } from '@/types';

type FilterType = 'all' | 'replicated' | 'local';

export default function BucketReplication() {
  const { t } = useTranslation('cluster');
  const navigate = useNavigate();
  const [buckets, setBuckets] = useState<BucketWithReplication[]>([]);
  const [filteredBuckets, setFilteredBuckets] = useState<BucketWithReplication[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<FilterType>('all');
  const [selectedBucket, setSelectedBucket] = useState<string | null>(null);
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loadingNodes, setLoadingNodes] = useState(false);
  const [configuring, setConfiguring] = useState(false);
  const [localNodeId, setLocalNodeId] = useState<string | null>(null);

  useEffect(() => {
    loadBuckets();
  }, []);

  useEffect(() => {
    applyFilter();
  }, [buckets, filter]);

  const loadBuckets = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await APIClient.getClusterBuckets();
      if (data && data.buckets) {
        setBuckets(data.buckets);
      } else {
        setBuckets([]);
      }
    } catch (err: unknown) {
      setError(getErrorMessage(err, t('failedToLoadBuckets')));
      setBuckets([]);
    } finally {
      setLoading(false);
    }
  };

  const applyFilter = () => {
    let filtered = buckets;
    if (filter === 'replicated') {
      filtered = buckets.filter(b => b.has_replication);
    } else if (filter === 'local') {
      filtered = buckets.filter(b => !b.has_replication);
    }
    setFilteredBuckets(filtered);
  };

  const loadNodes = async () => {
    try {
      setLoadingNodes(true);
      const clusterConfig = await APIClient.getClusterConfig();
      setLocalNodeId(clusterConfig.node_id);
      const data = await APIClient.listClusterNodes();
      const remoteNodes = data.filter(node => node.id !== clusterConfig.node_id);
      setNodes(remoteNodes);
    } catch (err: unknown) {
      console.error('Failed to load nodes:', err);
    } finally {
      setLoadingNodes(false);
    }
  };

  const handleConfigureReplication = async (bucket: string, targetNodeId: string, formData: any) => {
    try {
      setConfiguring(true);

      const targetNode = nodes.find(n => n.id === targetNodeId);
      if (!targetNode) {
        throw new Error(t('targetNodeNotFound'));
      }

      const syncInterval = parseInt(formData.syncInterval) || 60;
      if (syncInterval < 10) {
        throw new Error(t('syncIntervalMin10'));
      }

      const request: CreateClusterReplicationRequest = {
        source_bucket: bucket,
        destination_node_id: targetNodeId,
        destination_bucket: bucket,
        sync_interval_seconds: syncInterval,
        enabled: true,
        replicate_deletes: formData.replicateDeletes !== false,
        replicate_metadata: formData.replicateMetadata !== false,
        prefix: formData.prefix || undefined,
        priority: 0,
      };

      await APIClient.createClusterReplication(request);

      alert(t('replicationConfiguredSuccess'));
      setSelectedBucket(null);
      loadBuckets();
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToConfigureReplication')));
    } finally {
      setConfiguring(false);
    }
  };

  const handleOpenConfig = (bucket: string) => {
    setSelectedBucket(bucket);
    loadNodes();
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text={t('loadingBuckets')} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex items-center gap-2 mb-2">
        <Button variant="outline" size="sm" onClick={() => navigate('/cluster')}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-foreground">{t('clusterBucketReplication')}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t('clusterBucketReplicationDesc')}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={loadBuckets}
          className="bg-card hover:bg-secondary"
        >
          <RefreshCw className="h-4 w-4" />
          {t('refresh')}
        </Button>
      </div>

      {error && (
        <Card className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 p-4">
          <p className="text-red-600 dark:text-red-400">{error}</p>
        </Card>
      )}

      {/* Filters */}
      <div className="flex gap-2">
        <Button
          variant={filter === 'all' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setFilter('all')}
          className={filter === 'all' ? 'bg-brand-600 text-white' : ''}
        >
          {t('allBuckets', { count: buckets.length })}
        </Button>
        <Button
          variant={filter === 'replicated' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setFilter('replicated')}
          className={filter === 'replicated' ? 'bg-brand-600 text-white' : ''}
        >
          {t('replicated', { count: buckets.filter(b => b.has_replication).length })}
        </Button>
        <Button
          variant={filter === 'local' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setFilter('local')}
          className={filter === 'local' ? 'bg-brand-600 text-white' : ''}
        >
          {t('localOnly', { count: buckets.filter(b => !b.has_replication).length })}
        </Button>
      </div>

      {/* Buckets Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {filteredBuckets.map((bucket) => (
          <Card key={bucket.name} className="p-4 hover:shadow-md transition-shadow">
            <div className="flex items-start justify-between mb-3">
              <div className="flex items-center gap-2">
                <Package className="h-5 w-5 text-brand-600 dark:text-brand-400" />
                <span className="font-semibold text-foreground">{bucket.name}</span>
              </div>
              {bucket.has_replication ? (
                <CheckCircle className="h-5 w-5 text-green-500" />
              ) : (
                <XCircle className="h-5 w-5 text-gray-400" />
              )}
            </div>

            <div className="space-y-2 text-sm text-muted-foreground mb-4">
              <div>{t('objectsCount', { count: bucket.object_count })}</div>
              <div>{t('sizeLabel', { size: formatBytes(bucket.total_size) })}</div>
              <div>{bucket.has_replication ? t('statusReplicated') : t('statusLocalOnly')}</div>
            </div>

            <Button
              variant={bucket.has_replication ? 'outline' : 'default'}
              size="sm"
              onClick={() => handleOpenConfig(bucket.name)}
              className={bucket.has_replication ? '' : 'bg-brand-600 hover:bg-brand-700 text-white w-full'}
            >
              <Settings className="h-4 w-4" />
              {bucket.has_replication ? t('manageReplicationBtn') : t('configureReplicationBtn')}
            </Button>
          </Card>
        ))}
      </div>

      {filteredBuckets.length === 0 && (
        <Card className="p-8 text-center">
          <Package className="h-12 w-12 text-gray-400 mx-auto mb-4" />
          <p className="text-sm text-muted-foreground mt-1">{t('noBucketsMatchingFilter')}</p>
        </Card>
      )}

      {/* Configuration Modal */}
      {selectedBucket && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-2xl p-6 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold text-foreground">
                {t('configureClusterReplicationTitle')}
              </h2>
              <button
                onClick={() => setSelectedBucket(null)}
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            {/* Info Banner */}
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4 mb-6">
              <div className="flex gap-3">
                <AlertTriangle className="h-5 w-5 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
                <div className="text-sm text-blue-800 dark:text-blue-200">
                  <p className="font-semibold mb-1">{t('clusterReplicationInfoTitle')}</p>
                  <p>{t('clusterReplicationInfoDesc')}</p>
                </div>
              </div>
            </div>

            <form onSubmit={(e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              handleConfigureReplication(
                selectedBucket,
                formData.get('targetNode') as string,
                {
                  syncInterval: formData.get('syncInterval'),
                  prefix: formData.get('prefix'),
                  replicateDeletes: formData.get('replicateDeletes') === 'on',
                  replicateMetadata: formData.get('replicateMetadata') === 'on',
                }
              );
            }}>
              <div className="space-y-4">
                {/* Source Bucket */}
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">
                    {t('sourceBucket')}
                  </label>
                  <input
                    type="text"
                    value={selectedBucket}
                    disabled
                    className="w-full border border-border rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-700 text-foreground"
                  />
                </div>

                {/* Destination Node */}
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">
                    {t('destinationNode')}
                  </label>
                  {loadingNodes ? (
                    <div className="flex items-center justify-center py-8">
                      <Loading size="sm" text={t('loadingNodes2')} />
                    </div>
                  ) : (
                    <>
                      <select
                        name="targetNode"
                        required
                        className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                      >
                        <option value="">{t('selectDestinationNode')}</option>
                        {nodes.map((node) => (
                          <option key={node.id} value={node.id}>
                            {node.name} ({node.endpoint}) - {node.health_status}
                          </option>
                        ))}
                      </select>
                      <p className="text-xs text-muted-foreground mt-1">
                        {t('localNodeNotShownHint')}
                      </p>
                    </>
                  )}
                  {nodes.some(n => n.health_status !== 'healthy') && (
                    <p className="text-sm text-yellow-600 dark:text-yellow-400 mt-1">
                      {t('someNodesUnhealthy')}
                    </p>
                  )}
                </div>

                {/* Sync Interval */}
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">
                    {t('syncInterval')}
                  </label>
                  <input
                    name="syncInterval"
                    type="number"
                    min="10"
                    defaultValue="60"
                    required
                    className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                    placeholder="60"
                  />
                  <p className="text-xs text-muted-foreground mt-1">
                    {t('syncIntervalHintLong')}
                  </p>
                </div>

                {/* Prefix Filter */}
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">
                    {t('prefixFilter')}
                  </label>
                  <input
                    name="prefix"
                    type="text"
                    className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                    placeholder="folder/"
                  />
                  <p className="text-xs text-muted-foreground mt-1">
                    {t('prefixFilterHint')}
                  </p>
                </div>

                {/* Options */}
                <div className="space-y-2">
                  <label className="flex items-center gap-2">
                    <input
                      name="replicateDeletes"
                      type="checkbox"
                      defaultChecked
                      className="rounded border-border text-brand-600 focus:ring-brand-500"
                    />
                    <span className="text-sm text-foreground">{t('replicateDeletions')}</span>
                  </label>

                  <label className="flex items-center gap-2">
                    <input
                      name="replicateMetadata"
                      type="checkbox"
                      defaultChecked
                      className="rounded border-border text-brand-600 focus:ring-brand-500"
                    />
                    <span className="text-sm text-foreground">{t('replicateMetadata')}</span>
                  </label>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 mt-6">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setSelectedBucket(null)}
                  className="flex-1"
                  disabled={configuring}
                >
                  {t('cancel')}
                </Button>
                <Button
                  type="submit"
                  className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                  disabled={configuring || loadingNodes}
                >
                  {configuring ? t('configuring') : t('configureReplication')}
                </Button>
              </div>
            </form>
          </Card>
        </div>
      )}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}
