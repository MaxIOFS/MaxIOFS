import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
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
import type { BucketWithReplication, ClusterNode, CreateClusterReplicationRequest } from '@/types';

type FilterType = 'all' | 'replicated' | 'local';

export default function BucketReplication() {
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
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to load buckets');
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

      // Get local node ID
      const clusterConfig = await APIClient.getClusterConfig();
      setLocalNodeId(clusterConfig.node_id);

      // Get all cluster nodes
      const data = await APIClient.listClusterNodes();

      // Filter out local node (cannot replicate to itself)
      const remoteNodes = data.filter(node => node.id !== clusterConfig.node_id);
      setNodes(remoteNodes);
    } catch (err: any) {
      console.error('Failed to load nodes:', err);
    } finally {
      setLoadingNodes(false);
    }
  };

  const handleConfigureReplication = async (bucket: string, targetNodeId: string, formData: any) => {
    try {
      setConfiguring(true);

      // Find the target node
      const targetNode = nodes.find(n => n.id === targetNodeId);
      if (!targetNode) {
        throw new Error('Target node not found');
      }

      // Validate sync interval (minimum 10 seconds)
      const syncInterval = parseInt(formData.syncInterval) || 60;
      if (syncInterval < 10) {
        throw new Error('Sync interval must be at least 10 seconds');
      }

      // Create cluster replication rule (NO CREDENTIALS needed)
      const request: CreateClusterReplicationRequest = {
        source_bucket: bucket,
        destination_node_id: targetNodeId,
        destination_bucket: bucket, // Same bucket name on destination
        sync_interval_seconds: syncInterval,
        enabled: true,
        replicate_deletes: formData.replicateDeletes !== false,
        replicate_metadata: formData.replicateMetadata !== false,
        prefix: formData.prefix || undefined,
        priority: 0,
      };

      await APIClient.createClusterReplication(request);

      alert('Cluster replication configured successfully!');
      setSelectedBucket(null);
      loadBuckets();
    } catch (err: any) {
      alert(err.response?.data?.error || err.message || 'Failed to configure replication');
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
        <Loading size="lg" text="Loading buckets..." />
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
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Cluster Bucket Replication</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Configure high-availability replication between cluster nodes
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={loadBuckets}
          className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
        >
          <RefreshCw className="h-4 w-4 mr-2" />
          Refresh
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
          All Buckets ({buckets.length})
        </Button>
        <Button
          variant={filter === 'replicated' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setFilter('replicated')}
          className={filter === 'replicated' ? 'bg-brand-600 text-white' : ''}
        >
          Replicated ({buckets.filter(b => b.has_replication).length})
        </Button>
        <Button
          variant={filter === 'local' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setFilter('local')}
          className={filter === 'local' ? 'bg-brand-600 text-white' : ''}
        >
          Local Only ({buckets.filter(b => !b.has_replication).length})
        </Button>
      </div>

      {/* Buckets Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {filteredBuckets.map((bucket) => (
          <Card key={bucket.name} className="p-4 hover:shadow-md transition-shadow">
            <div className="flex items-start justify-between mb-3">
              <div className="flex items-center gap-2">
                <Package className="h-5 w-5 text-brand-600 dark:text-brand-400" />
                <span className="font-semibold text-gray-900 dark:text-white">{bucket.name}</span>
              </div>
              {bucket.has_replication ? (
                <CheckCircle className="h-5 w-5 text-green-500" />
              ) : (
                <XCircle className="h-5 w-5 text-gray-400" />
              )}
            </div>

            <div className="space-y-2 text-sm text-gray-600 dark:text-gray-400 mb-4">
              <div>Objects: {bucket.object_count}</div>
              <div>Size: {formatBytes(bucket.total_size)}</div>
              <div>Status: {bucket.has_replication ? 'Replicated' : 'Local only'}</div>
            </div>

            <Button
              variant={bucket.has_replication ? 'outline' : 'default'}
              size="sm"
              onClick={() => handleOpenConfig(bucket.name)}
              className={bucket.has_replication ? '' : 'bg-brand-600 hover:bg-brand-700 text-white w-full'}
            >
              <Settings className="h-4 w-4 mr-2" />
              {bucket.has_replication ? 'Manage Replication' : 'Configure Replication'}
            </Button>
          </Card>
        ))}
      </div>

      {filteredBuckets.length === 0 && (
        <Card className="p-8 text-center">
          <Package className="h-12 w-12 text-gray-400 mx-auto mb-4" />
          <p className="text-gray-600 dark:text-gray-400">No buckets found matching the filter</p>
        </Card>
      )}

      {/* Configuration Modal */}
      {selectedBucket && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-2xl p-6 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">
                Configure Cluster Replication
              </h2>
              <button
                onClick={() => setSelectedBucket(null)}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            {/* Info Banner */}
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4 mb-6">
              <div className="flex gap-3">
                <AlertTriangle className="h-5 w-5 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
                <div className="text-sm text-blue-800 dark:text-blue-200">
                  <p className="font-semibold mb-1">Cluster Replication</p>
                  <p>Nodes authenticate using cluster tokens - no credentials needed. Objects are automatically encrypted/decrypted during replication.</p>
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
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Source Bucket
                  </label>
                  <input
                    type="text"
                    value={selectedBucket}
                    disabled
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-white"
                  />
                </div>

                {/* Destination Node */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Destination Node *
                  </label>
                  {loadingNodes ? (
                    <div className="flex items-center justify-center py-8">
                      <Loading size="sm" text="Loading nodes..." />
                    </div>
                  ) : (
                    <>
                      <select
                        name="targetNode"
                        required
                        className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                      >
                        <option value="">Select destination node...</option>
                        {nodes.map((node) => (
                          <option key={node.id} value={node.id}>
                            {node.name} ({node.endpoint}) - {node.health_status}
                          </option>
                        ))}
                      </select>
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                        Note: Local node is not shown. Cluster replication is for HA between different MaxIOFS servers.
                      </p>
                    </>
                  )}
                  {nodes.some(n => n.health_status !== 'healthy') && (
                    <p className="text-sm text-yellow-600 dark:text-yellow-400 mt-1">
                      ⚠️ Some nodes are unhealthy. Replication may be affected.
                    </p>
                  )}
                </div>

                {/* Sync Interval (in seconds) */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Sync Interval (seconds) *
                  </label>
                  <input
                    name="syncInterval"
                    type="number"
                    min="10"
                    defaultValue="60"
                    required
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                    placeholder="60"
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Minimum 10 seconds. Use 60 for real-time HA, or higher values (e.g., 21600 = 6 hours) for backups.
                  </p>
                </div>

                {/* Prefix Filter (optional) */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Prefix Filter (optional)
                  </label>
                  <input
                    name="prefix"
                    type="text"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                    placeholder="folder/"
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Only replicate objects with keys starting with this prefix
                  </p>
                </div>

                {/* Options */}
                <div className="space-y-2">
                  <label className="flex items-center gap-2">
                    <input
                      name="replicateDeletes"
                      type="checkbox"
                      defaultChecked
                      className="rounded border-gray-300 dark:border-gray-600 text-brand-600 focus:ring-brand-500"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">Replicate deletions</span>
                  </label>

                  <label className="flex items-center gap-2">
                    <input
                      name="replicateMetadata"
                      type="checkbox"
                      defaultChecked
                      className="rounded border-gray-300 dark:border-gray-600 text-brand-600 focus:ring-brand-500"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">Replicate metadata</span>
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
                  Cancel
                </Button>
                <Button
                  type="submit"
                  className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                  disabled={configuring || loadingNodes}
                >
                  {configuring ? 'Configuring...' : 'Configure Replication'}
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
