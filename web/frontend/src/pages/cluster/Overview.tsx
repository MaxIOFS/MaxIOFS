import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import {
  Server,
  Package,
  Box,
  Network,
  CheckCircle,
  AlertTriangle,
  XCircle,
  ArrowRightLeft,
  Copy,
  Check,
  Link,
  KeyRound
} from 'lucide-react';
import APIClient from '@/lib/api';
import { getErrorMessage } from '@/lib/utils';
import type { ClusterStatus, ClusterConfig } from '@/types';

export default function ClusterOverview() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<ClusterStatus | null>(null);
  const [config, setConfig] = useState<ClusterConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showInitDialog, setShowInitDialog] = useState(false);
  const [showJoinDialog, setShowJoinDialog] = useState(false);
  const [showTokenModal, setShowTokenModal] = useState(false);
  const [clusterToken, setClusterToken] = useState('');
  const [tokenCopied, setTokenCopied] = useState(false);
  const [joinLoading, setJoinLoading] = useState(false);

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      setLoading(true);
      setError(null);

      const configData = await APIClient.getClusterConfig();
      setConfig(configData);

      if (configData.is_cluster_enabled) {
        const statusData = await APIClient.getClusterStatus();
        setStatus(statusData);
      } else {
        setStatus(null);
      }
    } catch (err: unknown) {
      setError(getErrorMessage(err, 'Failed to load cluster data'));
    } finally {
      setLoading(false);
    }
  };

  const handleInitializeCluster = async (nodeName: string, region: string) => {
    try {
      const response = await APIClient.initializeCluster({ node_name: nodeName, region: region || undefined });
      setClusterToken(response.cluster_token);
      setShowInitDialog(false);
      setShowTokenModal(true);
      await loadData();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to initialize cluster'));
    }
  };

  const handleJoinCluster = async (clusterTokenValue: string, nodeEndpoint: string) => {
    try {
      setJoinLoading(true);
      await APIClient.joinCluster({ cluster_token: clusterTokenValue, node_endpoint: nodeEndpoint });
      setShowJoinDialog(false);
      await loadData();
    } catch (err: unknown) {
      alert(getErrorMessage(err, 'Failed to join cluster'));
    } finally {
      setJoinLoading(false);
    }
  };

  const copyToken = async () => {
    try {
      await navigator.clipboard.writeText(clusterToken);
      setTokenCopied(true);
      setTimeout(() => setTokenCopied(false), 2000);
    } catch {
      // Fallback for non-HTTPS contexts
      const textarea = document.createElement('textarea');
      textarea.value = clusterToken;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      setTokenCopied(true);
      setTimeout(() => setTokenCopied(false), 2000);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text="Loading cluster data..." />
      </div>
    );
  }

  // Cluster not initialized
  if (!config || !config.is_cluster_enabled) {
    return (
      <div className="space-y-6">
        {/* Page Header */}
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Cluster Management</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Initialize and manage your MaxIOFS cluster</p>
          </div>
        </div>

        <Card className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 p-8 text-center">
          <Network className="w-16 h-16 text-blue-600 dark:text-blue-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-2">Cluster Not Initialized</h2>
          <p className="text-gray-600 dark:text-gray-400 mb-6">
            Initialize a new cluster or join an existing one to enable multi-node replication and high availability
          </p>
          <div className="flex items-center justify-center gap-3">
            <Button
              onClick={() => setShowInitDialog(true)}
              className="bg-brand-600 hover:bg-brand-700 text-white"
            >
              <Server className="h-4 w-4 mr-2" />
              Initialize Cluster
            </Button>
            <Button
              variant="outline"
              onClick={() => setShowJoinDialog(true)}
              className="bg-white dark:bg-gray-800"
            >
              <Link className="h-4 w-4 mr-2" />
              Join Existing Cluster
            </Button>
          </div>
        </Card>

        {/* Initialize Dialog */}
        {showInitDialog && (
          <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
            <Card className="w-full max-w-md p-6">
              <h2 className="text-xl font-bold mb-4 text-gray-900 dark:text-white">Initialize Cluster</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
                This will create a new cluster with this node as the first member. You will receive a cluster token to add other nodes.
              </p>
              <form onSubmit={(e) => {
                e.preventDefault();
                const formData = new FormData(e.currentTarget);
                handleInitializeCluster(
                  formData.get('node_name') as string,
                  formData.get('region') as string
                );
              }}>
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Node Name</label>
                    <input
                      name="node_name"
                      type="text"
                      required
                      className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                      placeholder="node-1"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Region (optional)</label>
                    <input
                      name="region"
                      type="text"
                      className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                      placeholder="us-east-1"
                    />
                  </div>
                </div>
                <div className="flex gap-2 mt-6">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setShowInitDialog(false)}
                    className="flex-1"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                  >
                    Initialize
                  </Button>
                </div>
              </form>
            </Card>
          </div>
        )}

        {/* Join Cluster Dialog */}
        {showJoinDialog && (
          <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
            <Card className="w-full max-w-md p-6">
              <h2 className="text-xl font-bold mb-4 text-gray-900 dark:text-white">Join Existing Cluster</h2>
              <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
                Enter the endpoint of an existing cluster node and the cluster token to join.
              </p>
              <form onSubmit={(e) => {
                e.preventDefault();
                const formData = new FormData(e.currentTarget);
                handleJoinCluster(
                  formData.get('cluster_token') as string,
                  formData.get('node_endpoint') as string
                );
              }}>
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Cluster Node Endpoint</label>
                    <input
                      name="node_endpoint"
                      type="url"
                      required
                      className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500"
                      placeholder="https://node-1.example.com:8081"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      Console URL of an existing cluster node (e.g., https://node-1.example.com:8081)
                    </p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Cluster Token</label>
                    <textarea
                      name="cluster_token"
                      required
                      rows={3}
                      className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500 font-mono text-sm"
                      placeholder="Paste the cluster token here..."
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      The token displayed when the cluster was initialized
                    </p>
                  </div>
                </div>
                <div className="flex gap-2 mt-6">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setShowJoinDialog(false)}
                    className="flex-1"
                    disabled={joinLoading}
                  >
                    Cancel
                  </Button>
                  <Button
                    type="submit"
                    className="flex-1 bg-brand-600 hover:bg-brand-700 text-white"
                    disabled={joinLoading}
                  >
                    {joinLoading ? 'Joining...' : 'Join Cluster'}
                  </Button>
                </div>
              </form>
            </Card>
          </div>
        )}

        {/* Token Display Modal */}
        {showTokenModal && (
          <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
            <Card className="w-full max-w-lg p-6">
              <div className="flex items-center gap-3 mb-4">
                <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-green-100 dark:bg-green-900/30">
                  <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
                </div>
                <h2 className="text-xl font-bold text-gray-900 dark:text-white">Cluster Initialized</h2>
              </div>
              <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3 mb-4">
                <p className="text-sm text-amber-800 dark:text-amber-300 font-medium">
                  Save this token now. You will need it to join other nodes to this cluster.
                </p>
              </div>
              <div className="relative">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Cluster Token</label>
                <div className="flex gap-2">
                  <code className="flex-1 bg-gray-100 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 text-sm font-mono text-gray-900 dark:text-white break-all select-all">
                    {clusterToken}
                  </code>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={copyToken}
                    className="shrink-0"
                  >
                    {tokenCopied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
                  </Button>
                </div>
              </div>
              <div className="mt-6">
                <Button
                  onClick={() => { setShowTokenModal(false); setClusterToken(''); }}
                  className="w-full bg-brand-600 hover:bg-brand-700 text-white"
                >
                  I have saved the token
                </Button>
              </div>
            </Card>
          </div>
        )}
      </div>
    );
  }

  // Cluster initialized
  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Cluster Overview</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Monitor and manage your MaxIOFS cluster
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Button
            variant="outline"
            onClick={async () => {
              try {
                const data = await APIClient.getClusterToken();
                setClusterToken(data.cluster_token);
                setShowTokenModal(true);
              } catch (err: unknown) {
                alert(getErrorMessage(err, 'Failed to retrieve cluster token'));
              }
            }}
            className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            <KeyRound className="h-4 w-4 mr-2" />
            Cluster Token
          </Button>
          <Button
            variant="outline"
            onClick={() => navigate('/cluster/buckets')}
            className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            <Package className="h-4 w-4 mr-2" />
            Manage Replication
          </Button>
          <Button
            variant="outline"
            onClick={() => navigate('/cluster/nodes')}
            className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            <Server className="h-4 w-4 mr-2" />
            Manage Nodes
          </Button>
          <Button
            onClick={() => navigate('/cluster/migrations')}
            className="bg-brand-600 hover:bg-brand-700 text-white"
          >
            <ArrowRightLeft className="h-4 w-4 mr-2" />
            Manage Migrations
          </Button>
        </div>
      </div>

      {error && (
        <Card className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 p-4">
          <p className="text-red-600 dark:text-red-400">{error}</p>
        </Card>
      )}

      {/* Stats Grid - 4 Cards as requested */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 md:gap-6">
        <MetricCard
          title="Total Nodes"
          value={status?.total_nodes || 0}
          icon={Server}
          description="Nodes in cluster"
          color="brand"
        />

        <MetricCard
          title="Healthy Nodes"
          value={status?.healthy_nodes || 0}
          icon={CheckCircle}
          description="Fully operational"
          color="success"
        />

        <MetricCard
          title="Degraded Nodes"
          value={status?.degraded_nodes || 0}
          icon={AlertTriangle}
          description="Performance issues"
          color="warning"
        />

        <MetricCard
          title="Unavailable Nodes"
          value={status?.unavailable_nodes || 0}
          icon={XCircle}
          description="Offline or unreachable"
          color="error"
        />
      </div>

      {/* Bucket Stats */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 md:gap-6">
        <MetricCard
          title="Total Buckets"
          value={status?.total_buckets || 0}
          icon={Package}
          description="Across all nodes"
          color="blue-light"
        />

        <MetricCard
          title="Replicated Buckets"
          value={status?.replicated_buckets || 0}
          icon={Box}
          description="With replication configured"
          color="success"
        />

        <MetricCard
          title="Local Buckets"
          value={status?.local_buckets || 0}
          icon={Box}
          description="Not replicated"
          color="warning"
        />
      </div>

      {/* This Node Info */}
      {config && (
        <Card className="p-6">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">This Node Information</h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div>
              <p className="text-sm text-gray-500 dark:text-gray-400">Node Name</p>
              <p className="font-medium text-gray-900 dark:text-white mt-1">{config.node_name}</p>
            </div>
            <div>
              <p className="text-sm text-gray-500 dark:text-gray-400">Node ID</p>
              <p className="font-mono text-sm text-gray-900 dark:text-white mt-1">{config.node_id}</p>
            </div>
            {config.region && (
              <div>
                <p className="text-sm text-gray-500 dark:text-gray-400">Region</p>
                <p className="font-medium text-gray-900 dark:text-white mt-1">{config.region}</p>
              </div>
            )}
          </div>
        </Card>
      )}

      {/* Token Display Modal */}
      {showTokenModal && (
        <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
          <Card className="w-full max-w-lg p-6">
            <div className="flex items-center gap-3 mb-4">
              <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-brand-100 dark:bg-brand-900/30">
                <KeyRound className="h-5 w-5 text-brand-600 dark:text-brand-400" />
              </div>
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">Cluster Token</h2>
            </div>
            <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">
              Use this token to join other nodes to this cluster. You can provide it in the "Join Existing Cluster" form on a standalone node, or use "Add Node" from this cluster to add nodes automatically.
            </p>
            <div className="relative">
              <div className="flex gap-2">
                <code className="flex-1 bg-gray-100 dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 text-sm font-mono text-gray-900 dark:text-white break-all select-all">
                  {clusterToken}
                </code>
                <Button
                  type="button"
                  variant="outline"
                  onClick={copyToken}
                  className="shrink-0"
                >
                  {tokenCopied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </div>
            <div className="mt-6">
              <Button
                onClick={() => { setShowTokenModal(false); setClusterToken(''); }}
                className="w-full bg-brand-600 hover:bg-brand-700 text-white"
              >
                Close
              </Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
