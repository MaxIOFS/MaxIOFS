import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
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
  Activity,
  ArrowLeft,
  Copy,
  X
} from 'lucide-react';
import APIClient from '@/lib/api';
import { getErrorMessage } from '@/lib/utils';
import type { ClusterNode, AddNodeRequest, UpdateNodeRequest, CreateReplicationRuleRequest, BucketWithReplication } from '@/types';

type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown';

export default function ClusterNodes() {
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAddNodeDialog, setShowAddNodeDialog] = useState(false);
  const [editingNode, setEditingNode] = useState<ClusterNode | null>(null);
  const [showNodeReplicationDialog, setShowNodeReplicationDialog] = useState(false);
  const [configuringBulk, setConfiguringBulk] = useState(false);
  const [localNodeId, setLocalNodeId] = useState<string | null>(null);
  const [availableNodes, setAvailableNodes] = useState<ClusterNode[]>([]);

  useEffect(() => {
    loadNodes();
  }, []);

  const loadNodes = async () => {
    try {
      setLoading(true);
      setError(null);

      // Get local node ID
      const clusterConfig = await APIClient.getClusterConfig();
      setLocalNodeId(clusterConfig.node_id);

      // Get all cluster nodes
      const data = await APIClient.listClusterNodes();
      setNodes(data);

      // Filter out local node for bulk replication (cannot replicate to itself)
      const remoteNodes = data.filter(node => node.id !== clusterConfig.node_id);
      setAvailableNodes(remoteNodes);
    } catch (err: unknown) {
      setError(getErrorMessage(err, 'Failed to load nodes'));
    } finally {
      setLoading(false);
    }
  };

  const handleAddNode = async (request: AddNodeRequest) => {
    try {
      await APIClient.addClusterNode(request);
      setShowAddNodeDialog(false);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to add node'));
    }
  };

  const handleUpdateNode = async (nodeId: string, request: UpdateNodeRequest) => {
    try {
      await APIClient.updateClusterNode(nodeId, request);
      setEditingNode(null);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to update node'));
    }
  };

  const handleRemoveNode = async (nodeId: string) => {
    if (!confirm('Are you sure you want to remove this node from the cluster?')) {
      return;
    }

    try {
      await APIClient.removeClusterNode(nodeId);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to remove node'));
    }
  };

  const handleCheckHealth = async (nodeId: string) => {
    try {
      const health = await APIClient.checkNodeHealth(nodeId);
      alert(`Health Status: ${health.status}\nLatency: ${health.latency_ms}ms`);
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to check node health'));
    }
  };

  const handleBulkReplication = async (targetNodeId: string, syncInterval: number) => {
    try {
      setConfiguringBulk(true);

      // Validate sync interval (minimum 10 seconds)
      if (syncInterval < 10) {
        throw new Error('Sync interval must be at least 10 seconds');
      }

      // Use bulk cluster replication API (NO CREDENTIALS needed)
      const result = await APIClient.createBulkClusterReplication({
        destination_node_id: targetNodeId,
        sync_interval_seconds: syncInterval,
        enabled: true,
      });

      // Show results
      let message = `Bulk cluster replication configured!\n\nSuccess: ${result.rules_created}\nFailed: ${result.rules_failed}`;
      if (result.failed_buckets && result.failed_buckets.length > 0 && result.failed_buckets.length <= 5) {
        message += '\n\nFailed buckets:\n' + result.failed_buckets.join('\n');
      } else if (result.failed_buckets && result.failed_buckets.length > 5) {
        message += '\n\nFailed buckets (first 5):\n' + result.failed_buckets.slice(0, 5).join('\n');
      }

      alert(message);
      setShowNodeReplicationDialog(false);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to configure bulk replication'));
    } finally {
      setConfiguringBulk(false);
    }
  };

  const getHealthIcon = (status: HealthStatus) => {
    switch (status) {
      case 'healthy':
        return <CheckCircle className="w-5 h-5 text-green-600 dark:text-green-400" />;
      case 'degraded':
        return <AlertTriangle className="w-5 h-5 text-yellow-600 dark:text-yellow-400" />;
      case 'unavailable':
        return <XCircle className="w-5 h-5 text-red-600 dark:text-red-400" />;
      default:
        return <HelpCircle className="w-5 h-5 text-gray-600 dark:text-gray-400" />;
    }
  };

  const getHealthBadge = (status: HealthStatus) => {
    const colors = {
      healthy: 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200',
      degraded: 'bg-yellow-100 dark:bg-yellow-900 text-yellow-800 dark:text-yellow-200',
      unavailable: 'bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200',
      unknown: 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200',
    };

    return (
      <span className={`px-2 py-1 text-xs font-medium rounded-full ${colors[status]}`}>
        {status.charAt(0).toUpperCase() + status.slice(1)}
      </span>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text="Loading cluster nodes..." />
      </div>
    );
  }

  if (error) {
    return (
      <Card className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 p-4">
        <p className="text-red-600 dark:text-red-400">{error}</p>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigate('/cluster')}
              className="bg-white dark:bg-gray-800"
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Cluster Nodes</h1>
          </div>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Manage nodes in your MaxIOFS cluster
          </p>
        </div>
        <div className="flex gap-3">
          <Button
            variant="outline"
            onClick={loadNodes}
            className="bg-white dark:bg-gray-800"
          >
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Button
            variant="outline"
            onClick={() => setShowNodeReplicationDialog(true)}
            className="bg-white dark:bg-gray-800"
          >
            <Copy className="h-4 w-4 mr-2" />
            Configure Node Replication
          </Button>
          <Button
            onClick={() => setShowAddNodeDialog(true)}
            className="bg-brand-600 hover:bg-brand-700 text-white"
          >
            <Plus className="h-4 w-4 mr-2" />
            Add Node
          </Button>
        </div>
      </div>

      {/* Nodes Table */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Server className="h-5 w-5 text-brand-600 dark:text-brand-400" />
            Cluster Nodes ({nodes.length})
          </h3>
        </div>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Endpoint</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Latency</TableHead>
                <TableHead>Buckets</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-8 text-gray-500 dark:text-gray-400">
                    No nodes added yet. Add your first node to start building the cluster.
                  </TableCell>
                </TableRow>
              ) : (
                nodes.map((node) => (
                  <TableRow key={node.id}>
                    <TableCell className="whitespace-nowrap">
                      <div className="flex items-center gap-3">
                        <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gradient-to-br from-brand-50 to-blue-50 dark:from-brand-900/30 dark:to-blue-900/30 shadow-sm">
                          <Server className="h-4 w-4 text-brand-600 dark:text-brand-400" />
                        </div>
                        <div>
                          <div className="font-semibold text-brand-600 dark:text-brand-400">
                            {node.name}
                            {node.id === localNodeId && (
                              <span className="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400">(This node)</span>
                            )}
                          </div>
                          {getHealthIcon(node.health_status)}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className="text-sm">{node.endpoint}</span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap">{getHealthBadge(node.health_status)}</TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className="text-sm">{node.latency_ms}ms</span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className="text-sm">{node.bucket_count}</span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className="text-sm">{node.priority}</span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button
                          onClick={() => handleCheckHealth(node.id)}
                          className="p-2 text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow"
                          title="Check Health"
                        >
                          <Activity className="h-4 w-4" />
                        </button>
                        <button
                          onClick={() => setEditingNode(node)}
                          className="p-2 text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow"
                          title="Edit Node"
                        >
                          <Edit className="h-4 w-4" />
                        </button>
                        {node.id !== localNodeId && (
                          <button
                            onClick={() => handleRemoveNode(node.id)}
                            className="p-2 text-gray-600 dark:text-gray-400 hover:text-error-600 dark:hover:text-error-400 hover:bg-gradient-to-br hover:from-error-50 hover:to-red-50 dark:hover:from-error-900/30 dark:hover:to-red-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow"
                            title="Remove Node"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      {/* Add Node Dialog */}
      {showAddNodeDialog && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-md p-6 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">Add Node to Cluster</h2>
              <button
                onClick={() => setShowAddNodeDialog(false)}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
              Enter the console URL and admin credentials of the remote node. The node must be in standalone mode (not already in a cluster).
            </p>

            <form onSubmit={(e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              handleAddNode({
                endpoint: formData.get('endpoint') as string,
                username: formData.get('username') as string,
                password: formData.get('password') as string,
              } as AddNodeRequest);
            }}>
              <div className="space-y-4">
                {/* Endpoint URL */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Console URL *
                  </label>
                  <input
                    name="endpoint"
                    type="url"
                    required
                    placeholder="https://node2.example.com:8081"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Console URL of the remote node (e.g., https://node2.example.com:8081)
                  </p>
                </div>

                {/* Username */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Admin Username *
                  </label>
                  <input
                    name="username"
                    type="text"
                    required
                    placeholder="admin"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                </div>

                {/* Password */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Admin Password *
                  </label>
                  <input
                    name="password"
                    type="password"
                    required
                    placeholder="********"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 mt-6">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setShowAddNodeDialog(false)}
                  className="flex-1"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                >
                  Add Node
                </Button>
              </div>
            </form>
          </Card>
        </div>
      )}

      {/* Edit Node Dialog */}
      {editingNode && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-2xl p-6 max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">Edit Cluster Node</h2>
              <button
                onClick={() => setEditingNode(null)}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                <X className="h-5 w-5" />
              </button>
            </div>

            <form onSubmit={(e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              handleUpdateNode(editingNode.id, {
                name: formData.get('name') as string || undefined,
                region: formData.get('region') as string || undefined,
                priority: parseInt(formData.get('priority') as string) || undefined,
                metadata: formData.get('metadata') as string || undefined,
              });
            }}>
              <div className="space-y-4">
                {/* Node Name */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Node Name
                  </label>
                  <input
                    name="name"
                    type="text"
                    defaultValue={editingNode.name}
                    placeholder="node-us-west-1"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                </div>

                {/* Endpoint (Read-only) */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Endpoint URL
                  </label>
                  <input
                    type="text"
                    value={editingNode.endpoint}
                    disabled
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400"
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Endpoint cannot be changed. Remove and re-add the node to change the endpoint.
                  </p>
                </div>

                {/* Node ID (Read-only) */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Node ID
                  </label>
                  <input
                    type="text"
                    value={editingNode.id}
                    disabled
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 font-mono text-sm"
                  />
                </div>

                {/* Region */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Region
                  </label>
                  <input
                    name="region"
                    type="text"
                    defaultValue={editingNode.region || ''}
                    placeholder="us-west-1"
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                </div>

                {/* Priority */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Priority
                  </label>
                  <input
                    name="priority"
                    type="number"
                    defaultValue={editingNode.priority}
                    min={1}
                    max={1000}
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Lower values = higher priority for routing (1-1000)
                  </p>
                </div>

                {/* Metadata */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Metadata (JSON)
                  </label>
                  <textarea
                    name="metadata"
                    rows={3}
                    defaultValue={editingNode.metadata || ''}
                    placeholder='{"location": "datacenter-1", "environment": "production"}'
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500 font-mono text-sm"
                  />
                </div>

                {/* Health Info (Read-only) */}
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4 space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600 dark:text-gray-400">Health Status:</span>
                    <span className={`font-medium ${
                      editingNode.health_status === 'healthy' ? 'text-green-600' :
                      editingNode.health_status === 'degraded' ? 'text-yellow-600' :
                      'text-red-600'
                    }`}>
                      {editingNode.health_status}
                    </span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600 dark:text-gray-400">Latency:</span>
                    <span className="font-medium text-gray-900 dark:text-white">{editingNode.latency_ms}ms</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-gray-600 dark:text-gray-400">Bucket Count:</span>
                    <span className="font-medium text-gray-900 dark:text-white">{editingNode.bucket_count}</span>
                  </div>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 mt-6">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setEditingNode(null)}
                  className="flex-1"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                >
                  Save Changes
                </Button>
              </div>
            </form>
          </Card>
        </div>
      )}

      {/* Node-to-Node Replication Modal */}
      {showNodeReplicationDialog && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-lg p-6">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">
                Configure Node-to-Node Replication
              </h2>
              <button
                onClick={() => setShowNodeReplicationDialog(false)}
                className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="mb-4 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              <p className="text-sm text-blue-800 dark:text-blue-200">
                This will configure cluster replication for <strong>all local buckets</strong> to the selected target node.
                Nodes authenticate using cluster tokens - no credentials needed.
              </p>
            </div>

            <form onSubmit={(e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              handleBulkReplication(
                formData.get('targetNode') as string,
                parseInt(formData.get('syncInterval') as string) || 60
              );
            }}>
              <div className="space-y-4">
                {/* Target Node */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                    Target Node *
                  </label>
                  <select
                    name="targetNode"
                    required
                    className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                  >
                    <option value="">Select target node...</option>
                    {availableNodes.map(node => (
                      <option key={node.id} value={node.id}>
                        {node.name} ({node.endpoint}) - {node.health_status}
                      </option>
                    ))}
                  </select>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    All local buckets will be replicated to this node. Note: Local node is not shown (cannot replicate to itself).
                  </p>
                </div>

                {/* Sync Interval */}
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
                    Minimum 10 seconds. Use 60 for real-time HA, or higher values for backups.
                  </p>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-3 mt-6">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setShowNodeReplicationDialog(false)}
                  disabled={configuringBulk}
                  className="flex-1"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={configuringBulk}
                  className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                >
                  {configuringBulk ? 'Configuring...' : 'Configure Replication'}
                </Button>
              </div>
            </form>
          </Card>
        </div>
      )}
    </div>
  );
}
