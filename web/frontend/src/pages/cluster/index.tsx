import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Server,
  Plus,
  RefreshCw,
  CheckCircle,
  XCircle,
  AlertTriangle,
  HelpCircle,
  Trash2,
  Edit,
  Activity
} from 'lucide-react';
import APIClient from '@/lib/api';
import type { ClusterStatus, ClusterConfig, ClusterNode, AddNodeRequest, UpdateNodeRequest, InitializeClusterRequest, BucketWithReplication } from '@/types';

export default function Cluster() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<ClusterStatus | null>(null);
  const [config, setConfig] = useState<ClusterConfig | null>(null);
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [buckets, setBuckets] = useState<BucketWithReplication[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showInitDialog, setShowInitDialog] = useState(false);
  const [showAddNodeDialog, setShowAddNodeDialog] = useState(false);
  const [editingNode, setEditingNode] = useState<ClusterNode | null>(null);
  const [clusterToken, setClusterToken] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'buckets' | 'nodes'>('buckets');

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      setLoading(true);
      setError(null);

      // Get cluster config (always returns 200, with is_cluster_enabled: false if standalone)
      const configData = await APIClient.getClusterConfig();
      setConfig(configData);

      // Fetch cluster status and nodes if cluster is enabled
      if (configData.is_cluster_enabled) {
        const [statusData, nodesData] = await Promise.all([
          APIClient.getClusterStatus(),
          APIClient.listClusterNodes()
        ]);
        setStatus(statusData);
        setNodes(nodesData);
      } else {
        // Standalone mode - clear cluster data
        setStatus(null);
        setNodes([]);
      }

      // Always fetch buckets (works in both standalone and cluster mode)
      try {
        const bucketsData = await APIClient.getClusterBuckets();
        setBuckets(bucketsData?.buckets || []);
      } catch (err) {
        console.error('Failed to load buckets:', err);
        setBuckets([]);
      }
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to load cluster data');
    } finally {
      setLoading(false);
    }
  };

  const handleInitializeCluster = async (nodeName: string, region: string) => {
    try {
      const request: InitializeClusterRequest = { node_name: nodeName, region: region || undefined };
      const response = await APIClient.initializeCluster(request);
      setClusterToken(response.cluster_token);
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to initialize cluster');
    }
  };

  const handleAddNode = async (request: AddNodeRequest) => {
    try {
      await APIClient.addClusterNode(request);
      setShowAddNodeDialog(false);
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to add node');
    }
  };

  const handleUpdateNode = async (nodeId: string, request: UpdateNodeRequest) => {
    try {
      await APIClient.updateClusterNode(nodeId, request);
      setEditingNode(null);
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to update node');
    }
  };

  const handleRemoveNode = async (nodeId: string) => {
    if (!confirm('Are you sure you want to remove this node from the cluster?')) return;

    try {
      await APIClient.removeClusterNode(nodeId);
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to remove node');
    }
  };

  const handleCheckHealth = async (nodeId: string) => {
    try {
      await APIClient.checkNodeHealth(nodeId);
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to check node health');
    }
  };

  const getHealthIcon = (status: string) => {
    switch (status) {
      case 'healthy': return <CheckCircle className="w-5 h-5 text-green-500" />;
      case 'degraded': return <AlertTriangle className="w-5 h-5 text-yellow-500" />;
      case 'unavailable': return <XCircle className="w-5 h-5 text-red-500" />;
      default: return <HelpCircle className="w-5 h-5 text-gray-400" />;
    }
  };

  const getHealthBadge = (status: string) => {
    const colors = {
      healthy: 'bg-green-100 text-green-800',
      degraded: 'bg-yellow-100 text-yellow-800',
      unavailable: 'bg-red-100 text-red-800',
      unknown: 'bg-gray-100 text-gray-800'
    };
    return (
      <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status as keyof typeof colors] || colors.unknown}`}>
        {status}
      </span>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg p-4">
        <p className="text-red-800 dark:text-red-200">{error}</p>
      </div>
    );
  }

  // Cluster not initialized
  if (!config?.is_cluster_enabled) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Cluster Management</h1>
        </div>

        <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-8 text-center">
          <Server className="w-16 h-16 text-blue-600 dark:text-blue-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">Cluster Not Initialized</h2>
          <p className="text-gray-600 dark:text-gray-400 mb-6">
            Initialize a cluster to enable multi-node deployment with automatic failover and bucket replication.
          </p>
          <button
            onClick={() => setShowInitDialog(true)}
            className="bg-blue-600 text-white px-6 py-2 rounded-lg hover:bg-blue-700 dark:bg-blue-600 dark:hover:bg-blue-700"
          >
            Initialize Cluster
          </button>
        </div>

        {showInitDialog && (
          <InitializeClusterDialog
            onClose={() => setShowInitDialog(false)}
            onSubmit={handleInitializeCluster}
            clusterToken={clusterToken}
          />
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Cluster Management</h1>
        <div className="flex gap-2">
          <button
            onClick={loadData}
            className="flex items-center gap-2 px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-300"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh
          </button>
          <button
            onClick={() => setShowAddNodeDialog(true)}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 dark:bg-blue-600 dark:hover:bg-blue-700"
          >
            <Plus className="w-4 h-4" />
            Add Node
          </button>
        </div>
      </div>

      {/* Cluster Status Card */}
      {status && (
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-6">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">Cluster Status</h2>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Total Nodes</p>
              <p className="text-2xl font-bold text-gray-900 dark:text-white">{status.total_nodes}</p>
            </div>
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Healthy</p>
              <p className="text-2xl font-bold text-green-600 dark:text-green-400">{status.healthy_nodes}</p>
            </div>
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Degraded</p>
              <p className="text-2xl font-bold text-yellow-600 dark:text-yellow-400">{status.degraded_nodes}</p>
            </div>
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Unavailable</p>
              <p className="text-2xl font-bold text-red-600 dark:text-red-400">{status.unavailable_nodes}</p>
            </div>
          </div>

          {config && (
            <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700">
              <p className="text-sm text-gray-600 dark:text-gray-400">This Node: <span className="font-medium text-gray-900 dark:text-white">{config.node_name}</span></p>
              {config.region && <p className="text-sm text-gray-600 dark:text-gray-400">Region: <span className="font-medium text-gray-900 dark:text-white">{config.region}</span></p>}
            </div>
          )}
        </div>
      )}

      {/* Tabs */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow">
        <div className="border-b border-gray-200 dark:border-gray-700">
          <nav className="flex -mb-px">
            <button
              onClick={() => setActiveTab('buckets')}
              className={`px-6 py-4 text-sm font-medium border-b-2 ${
                activeTab === 'buckets'
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'
              }`}
            >
              Buckets & Replication
            </button>
            <button
              onClick={() => setActiveTab('nodes')}
              className={`px-6 py-4 text-sm font-medium border-b-2 ${
                activeTab === 'nodes'
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'
              }`}
            >
              Cluster Nodes
            </button>
          </nav>
        </div>

        {/* Buckets Table */}
        {activeTab === 'buckets' && (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
              <thead className="bg-gray-50 dark:bg-gray-700">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Bucket Name</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Primary Node</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Replicas</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Replication Status</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                {buckets.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-6 py-8 text-center text-gray-500 dark:text-gray-400">
                      No buckets found
                    </td>
                  </tr>
                ) : (
                  buckets.map((bucket) => (
                    <tr key={bucket.name} className="hover:bg-gray-50 dark:hover:bg-gray-700">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="font-medium text-gray-900 dark:text-white">{bucket.name}</div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">
                        {bucket.primary_node}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className="px-2 py-1 text-xs font-medium rounded-full bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200">
                          {bucket.replica_count} {bucket.replica_count === 1 ? 'replica' : 'replicas'}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        {bucket.has_replication ? (
                          <span className="flex items-center gap-1 text-sm text-green-600 dark:text-green-400">
                            <CheckCircle className="w-4 h-4" />
                            Enabled
                          </span>
                        ) : (
                          <span className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                            <XCircle className="w-4 h-4" />
                            Not configured
                          </span>
                        )}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-right text-sm">
                        <button
                          className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 font-medium"
                        >
                          Configure Replication
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        )}

        {/* Nodes Table */}
        {activeTab === 'nodes' && (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
              <thead className="bg-gray-50 dark:bg-gray-700">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Endpoint</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Latency</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Buckets</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Priority</th>
                  <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                {nodes.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-8 text-center text-gray-500 dark:text-gray-400">
                    No nodes added yet. Add your first node to start building the cluster.
                  </td>
                </tr>
              ) : (
                nodes.map((node) => (
                  <tr key={node.id} className="hover:bg-gray-50 dark:hover:bg-gray-700">
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center gap-2">
                        {getHealthIcon(node.health_status)}
                        <span className="font-medium text-gray-900 dark:text-white">{node.name}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">{node.endpoint}</td>
                    <td className="px-6 py-4 whitespace-nowrap">{getHealthBadge(node.health_status)}</td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">{node.latency_ms}ms</td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">{node.bucket_count}</td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">{node.priority}</td>
                    <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => handleCheckHealth(node.id)}
                          className="text-blue-600 hover:text-blue-900 dark:text-blue-400 dark:hover:text-blue-300"
                          title="Check Health"
                        >
                          <Activity className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => setEditingNode(node)}
                          className="text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-200"
                          title="Edit"
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleRemoveNode(node.id)}
                          className="text-red-600 hover:text-red-900 dark:text-red-400 dark:hover:text-red-300"
                          title="Remove"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Dialogs */}
      {showAddNodeDialog && (
        <AddNodeDialog
          onClose={() => setShowAddNodeDialog(false)}
          onSubmit={handleAddNode}
        />
      )}

      {editingNode && (
        <EditNodeDialog
          node={editingNode}
          onClose={() => setEditingNode(null)}
          onSubmit={(request) => handleUpdateNode(editingNode.id, request)}
        />
      )}
    </div>
  );
}

// Initialize Cluster Dialog
function InitializeClusterDialog({
  onClose,
  onSubmit,
  clusterToken
}: {
  onClose: () => void;
  onSubmit: (nodeName: string, region: string) => void;
  clusterToken: string | null;
}) {
  const [nodeName, setNodeName] = useState('');
  const [region, setRegion] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!nodeName.trim()) {
      alert('Node name is required');
      return;
    }
    onSubmit(nodeName.trim(), region.trim());
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md p-6">
        <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-4">Initialize Cluster</h2>

        {clusterToken ? (
          <div className="space-y-4">
            <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg p-4">
              <p className="text-sm text-green-800 dark:text-green-200 mb-2">✓ Cluster initialized successfully!</p>
              <p className="text-xs text-green-700 dark:text-green-300 mb-2">Save this cluster token securely. You'll need it to add other nodes:</p>
              <div className="bg-white dark:bg-gray-900 border border-green-300 dark:border-green-700 rounded p-2 font-mono text-xs break-all text-gray-900 dark:text-gray-100">
                {clusterToken}
              </div>
            </div>
            <button
              onClick={onClose}
              className="w-full bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700"
            >
              Close
            </button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Node Name *</label>
              <input
                type="text"
                value={nodeName}
                onChange={(e) => setNodeName(e.target.value)}
                className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
                placeholder="node-1"
                required
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Region (optional)</label>
              <input
                type="text"
                value={region}
                onChange={(e) => setRegion(e.target.value)}
                className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
                placeholder="us-east-1"
              />
            </div>

            <div className="flex gap-2">
              <button
                type="button"
                onClick={onClose}
                className="flex-1 border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 px-4 py-2 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="flex-1 bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700"
              >
                Initialize
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

// Add Node Dialog
function AddNodeDialog({ onClose, onSubmit }: { onClose: () => void; onSubmit: (request: AddNodeRequest) => void }) {
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [nodeToken, setNodeToken] = useState('');
  const [region, setRegion] = useState('');
  const [priority, setPriority] = useState('100');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !endpoint.trim() || !nodeToken.trim()) {
      alert('Name, endpoint, and node token are required');
      return;
    }

    const request: AddNodeRequest = {
      name: name.trim(),
      endpoint: endpoint.trim(),
      node_token: nodeToken.trim(),
      region: region.trim() || undefined,
      priority: parseInt(priority) || 100,
    };

    onSubmit(request);
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md p-6">
        <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-4">Add Cluster Node</h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Node Name *</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              placeholder="node-2"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Endpoint *</label>
            <input
              type="url"
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              placeholder="http://node2.example.com:8080"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Node Token *</label>
            <input
              type="text"
              value={nodeToken}
              onChange={(e) => setNodeToken(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white font-mono text-sm"
              placeholder="JWT token for authentication"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Region</label>
            <input
              type="text"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              placeholder="us-west-2"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Priority</label>
            <input
              type="number"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              min="1"
              max="1000"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Lower = higher priority (1-1000)</p>
          </div>

          <div className="flex gap-2">
            <button
              type="button"
              onClick={onClose}
              className="flex-1 border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 px-4 py-2 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="flex-1 bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700"
            >
              Add Node
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// Edit Node Dialog
function EditNodeDialog({
  node,
  onClose,
  onSubmit
}: {
  node: ClusterNode;
  onClose: () => void;
  onSubmit: (request: UpdateNodeRequest) => void
}) {
  const [name, setName] = useState(node.name);
  const [endpoint, setEndpoint] = useState(node.endpoint);
  const [region, setRegion] = useState(node.region || '');
  const [priority, setPriority] = useState(node.priority.toString());

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    const request: UpdateNodeRequest = {
      name: name.trim() || undefined,
      endpoint: endpoint.trim() || undefined,
      region: region.trim() || undefined,
      priority: parseInt(priority) || undefined,
    };

    onSubmit(request);
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md p-6">
        <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-4">Edit Node</h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Node Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Endpoint</label>
            <input
              type="url"
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Region</label>
            <input
              type="text"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Priority</label>
            <input
              type="number"
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              min="1"
              max="1000"
            />
          </div>

          <div className="flex gap-2">
            <button
              type="button"
              onClick={onClose}
              className="flex-1 border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 px-4 py-2 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              Cancel
            </button>
            <button
              type="submit"
              className="flex-1 bg-blue-600 text-white px-4 py-2 rounded-lg hover:bg-blue-700"
            >
              Save Changes
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
