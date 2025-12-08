import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
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
  ArrowLeft
} from 'lucide-react';
import APIClient from '@/lib/api';
import type { ClusterNode, AddNodeRequest, UpdateNodeRequest } from '@/types';

type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown';

export default function ClusterNodes() {
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAddNodeDialog, setShowAddNodeDialog] = useState(false);
  const [editingNode, setEditingNode] = useState<ClusterNode | null>(null);

  useEffect(() => {
    loadNodes();
  }, []);

  const loadNodes = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await APIClient.listClusterNodes();
      setNodes(data);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to load nodes');
    } finally {
      setLoading(false);
    }
  };

  const handleAddNode = async (request: AddNodeRequest) => {
    try {
      await APIClient.addClusterNode(request);
      setShowAddNodeDialog(false);
      await loadNodes();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to add node');
    }
  };

  const handleUpdateNode = async (nodeId: string, request: UpdateNodeRequest) => {
    try {
      await APIClient.updateClusterNode(nodeId, request);
      setEditingNode(null);
      await loadNodes();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to update node');
    }
  };

  const handleRemoveNode = async (nodeId: string) => {
    if (!confirm('Are you sure you want to remove this node from the cluster?')) {
      return;
    }

    try {
      await APIClient.removeClusterNode(nodeId);
      await loadNodes();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to remove node');
    }
  };

  const handleCheckHealth = async (nodeId: string) => {
    try {
      const health = await APIClient.checkNodeHealth(nodeId);
      alert(`Health Status: ${health.status}\nLatency: ${health.latency_ms}ms`);
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to check node health');
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
            onClick={() => setShowAddNodeDialog(true)}
            className="bg-brand-600 hover:bg-brand-700 text-white"
          >
            <Plus className="h-4 w-4 mr-2" />
            Add Node
          </Button>
        </div>
      </div>

      {/* Nodes Table */}
      <Card className="overflow-hidden">
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
                          title="Edit Node"
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => handleRemoveNode(node.id)}
                          className="text-red-600 hover:text-red-900 dark:text-red-400 dark:hover:text-red-300"
                          title="Remove Node"
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
      </Card>

      {/* TODO: Add AddNodeDialog and EditNodeDialog components */}
      {showAddNodeDialog && (
        <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-md p-6">
            <h2 className="text-xl font-bold mb-4 text-gray-900 dark:text-white">Add Node</h2>
            <p className="text-gray-600 dark:text-gray-400 mb-4">Add node dialog coming soon...</p>
            <Button
              variant="outline"
              onClick={() => setShowAddNodeDialog(false)}
            >
              Close
            </Button>
          </Card>
        </div>
      )}

      {editingNode && (
        <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-md p-6">
            <h2 className="text-xl font-bold mb-4 text-gray-900 dark:text-white">Edit Node</h2>
            <p className="text-gray-600 dark:text-gray-400 mb-4">Edit node dialog coming soon...</p>
            <Button
              variant="outline"
              onClick={() => setEditingNode(null)}
            >
              Close
            </Button>
          </Card>
        </div>
      )}
    </div>
  );
}
