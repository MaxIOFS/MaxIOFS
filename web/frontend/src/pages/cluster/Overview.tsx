import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
  KeyRound,
  Shield,
} from 'lucide-react';
import APIClient from '@/lib/api';
import { getErrorMessage } from '@/lib/utils';
import type { ClusterStatus, ClusterConfig } from '@/types';

export default function ClusterOverview() {
  const { t } = useTranslation('cluster');
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
      setError(getErrorMessage(err, t('loadingClusterData')));
    } finally {
      setLoading(false);
    }
  };

  const handleInitializeCluster = async (nodeName: string, region: string) => {
    try {
      const response = await APIClient.initializeCluster({
        node_name: nodeName,
        region: region || undefined,
      });
      setClusterToken(response.cluster_token);
      setShowInitDialog(false);
      setShowTokenModal(true);
      await loadData();
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToInitCluster')));
    }
  };

  const handleJoinCluster = async (clusterTokenValue: string, nodeEndpoint: string) => {
    try {
      setJoinLoading(true);
      await APIClient.joinCluster({ cluster_token: clusterTokenValue, node_endpoint: nodeEndpoint });
      setShowJoinDialog(false);
      await loadData();
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToJoinCluster')));
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
        <Loading size="lg" text={t('loadingClusterData')} />
      </div>
    );
  }

  // Cluster not initialized
  if (!config || !config.is_cluster_enabled) {
    return (
      <div className="space-y-6">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-foreground">{t('clusterManagement')}</h1>
            <p className="text-sm text-muted-foreground mt-1">{t('manageClusterDesc')}</p>
          </div>
        </div>

        <Card className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 p-8 text-center">
          <Network className="w-16 h-16 text-blue-600 dark:text-blue-400 mx-auto mb-4" />
          <h2 className="text-xl font-semibold text-foreground mb-2">{t('clusterNotInitialized')}</h2>
          <p className="text-muted-foreground mb-6">{t('clusterNotInitializedDesc')}</p>
          <div className="flex items-center justify-center gap-3">
            <Button
              onClick={() => setShowInitDialog(true)}
              variant="default"
            >
              <Server className="h-4 w-4" />
              {t('initializeCluster')}
            </Button>
            <Button
              variant="outline"
              onClick={() => setShowJoinDialog(true)}
              className="bg-card"
            >
              <Link className="h-4 w-4" />
              {t('joinExistingCluster')}
            </Button>
          </div>
        </Card>

        {/* Initialize Dialog */}
        {showInitDialog && (
          <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
            <Card className="w-full max-w-md p-6">
              <h2 className="text-xl font-bold mb-4 text-foreground">{t('initClusterTitle')}</h2>
              <p className="text-sm text-muted-foreground mb-4">{t('initClusterDesc')}</p>
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
                    <label className="block text-sm font-medium text-foreground mb-1">{t('nodeName')}</label>
                    <input
                      name="node_name"
                      type="text"
                      required
                      className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                      placeholder="node-1"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-foreground mb-1">{t('regionOptional')}</label>
                    <input
                      name="region"
                      type="text"
                      className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                      placeholder="us-east-1"
                    />
                  </div>

                </div>
                <div className="flex gap-2 mt-6">
                  <Button type="button" variant="outline" onClick={() => setShowInitDialog(false)} className="flex-1">
                    {t('cancel')}
                  </Button>
                  <Button type="submit" className="flex-1 bg-brand-600 hover:bg-brand-700 text-white">
                    {t('initialize')}
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
              <h2 className="text-xl font-bold mb-4 text-foreground">{t('joinClusterTitle')}</h2>
              <p className="text-sm text-muted-foreground mb-4">{t('joinClusterDesc')}</p>
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
                    <label className="block text-sm font-medium text-foreground mb-1">{t('clusterNodeEndpoint')}</label>
                    <input
                      name="node_endpoint"
                      type="text"
                      required
                      className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500"
                      placeholder="192.168.1.10"
                    />
                    <p className="text-xs text-muted-foreground mt-1">{t('clusterNodeEndpointHint')}</p>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-foreground mb-1">{t('clusterToken')}</label>
                    <textarea
                      name="cluster_token"
                      required
                      rows={3}
                      className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500 font-mono text-sm"
                      placeholder={t('clusterTokenPlaceholder')}
                    />
                    <p className="text-xs text-muted-foreground mt-1">{t('clusterTokenHint')}</p>
                  </div>
                </div>
                <div className="flex gap-2 mt-6">
                  <Button type="button" variant="outline" onClick={() => setShowJoinDialog(false)} className="flex-1" disabled={joinLoading}>
                    {t('cancel')}
                  </Button>
                  <Button type="submit" className="flex-1 bg-brand-600 hover:bg-brand-700 text-white" disabled={joinLoading}>
                    {joinLoading ? t('joining') : t('joinCluster')}
                  </Button>
                </div>
              </form>
            </Card>
          </div>
        )}

        {/* Token Display Modal — after init */}
        {showTokenModal && (
          <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
            <Card className="w-full max-w-lg p-6">
              <div className="flex items-center gap-3 mb-4">
                <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-green-100 dark:bg-green-900/30">
                  <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
                </div>
                <h2 className="text-xl font-bold text-foreground">{t('clusterInitialized')}</h2>
              </div>
              <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3 mb-4">
                <p className="text-sm text-amber-800 dark:text-amber-300 font-medium">{t('saveTokenWarning')}</p>
              </div>
              <div className="relative">
                <label className="block text-sm font-medium text-foreground mb-1">{t('clusterToken')}</label>
                <div className="flex gap-2">
                  <code className="flex-1 bg-gray-100 dark:bg-gray-700 border border-border rounded-lg px-3 py-2 text-sm font-mono text-foreground break-all select-all">
                    {clusterToken}
                  </code>
                  <Button type="button" variant="outline" onClick={copyToken} className="shrink-0">
                    {tokenCopied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
                  </Button>
                </div>
              </div>
              <div className="mt-6">
                <Button
                  onClick={() => { setShowTokenModal(false); setClusterToken(''); }}
                  className="w-full bg-brand-600 hover:bg-brand-700 text-white"
                >
                  {t('iHaveSavedToken')}
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
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground">{t('clusterOverview')}</h1>
          <p className="text-sm text-muted-foreground mt-1">{t('monitorClusterDesc')}</p>
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
                alert(getErrorMessage(err, t('failedToRetrieveToken')));
              }
            }}
            className="bg-card hover:bg-secondary"
          >
            <KeyRound className="h-4 w-4" />
            {t('clusterTokenTitle')}
          </Button>
<Button
            variant="outline"
            onClick={() => navigate('/cluster/nodes')}
            className="bg-card hover:bg-secondary"
          >
            <Server className="h-4 w-4" />
            {t('manageNodes')}
          </Button>
          <Button
            onClick={() => navigate('/cluster/migrations')}
            variant="default"
          >
            <ArrowRightLeft className="h-4 w-4" />
            {t('manageMigrations')}
          </Button>
          <Button
            onClick={() => navigate('/cluster/ha')}
            variant="outline"
            className="bg-card hover:bg-secondary"
          >
            <Shield className="h-4 w-4" />
            HA Replication
          </Button>
        </div>
      </div>

      {error && (
        <Card className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 p-4">
          <p className="text-red-600 dark:text-red-400">{error}</p>
        </Card>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 md:gap-6">
        <MetricCard compact title={t('totalNodes')} value={status?.total_nodes || 0} icon={Server} description={t('nodesInCluster')} color="brand" />
        <MetricCard compact title={t('healthyNodes')} value={status?.healthy_nodes || 0} icon={CheckCircle} description={t('fullyOperational')} color="success" />
        <MetricCard compact title={t('degradedNodes')} value={status?.degraded_nodes || 0} icon={AlertTriangle} description={t('performanceIssues')} color="warning" />
        <MetricCard compact title={t('unavailableNodes')} value={status?.unavailable_nodes || 0} icon={XCircle} description={t('offlineOrUnreachable')} color="error" />
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 md:gap-6">
        <MetricCard compact title={t('totalBuckets')} value={status?.total_buckets || 0} icon={Package} description={t('acrossAllNodes')} color="blue-light" />
        <MetricCard compact title={t('replicatedBuckets')} value={status?.replicated_buckets || 0} icon={Box} description={t('withReplicationConfigured')} color="success" />
        <MetricCard compact title={t('localBuckets')} value={status?.local_buckets || 0} icon={Box} description={t('notReplicated')} color="warning" />
      </div>

      {config && (
        <Card className="p-6">
          <h2 className="text-lg font-semibold text-foreground mb-4">{t('thisNodeInfo')}</h2>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <div>
              <p className="text-sm text-muted-foreground">{t('nodeNameLabel')}</p>
              <p className="font-medium text-foreground mt-1">{config.node_name}</p>
            </div>
            <div>
              <p className="text-sm text-muted-foreground">{t('nodeIdLabel')}</p>
              <p className="font-mono text-sm text-foreground mt-1">{config.node_id}</p>
            </div>
            {config.region && (
              <div>
                <p className="text-sm text-muted-foreground">{t('regionLabel')}</p>
                <p className="font-medium text-foreground mt-1">{config.region}</p>
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
              <h2 className="text-xl font-bold text-foreground">{t('clusterTokenTitle')}</h2>
            </div>
            <p className="text-sm text-muted-foreground mb-4">{t('clusterTokenDesc')}</p>
            <div className="relative">
              <div className="flex gap-2">
                <code className="flex-1 bg-gray-100 dark:bg-gray-700 border border-border rounded-lg px-3 py-2 text-sm font-mono text-foreground break-all select-all">
                  {clusterToken}
                </code>
                <Button type="button" variant="outline" onClick={copyToken} className="shrink-0">
                  {tokenCopied ? <Check className="h-4 w-4 text-green-600" /> : <Copy className="h-4 w-4" />}
                </Button>
              </div>
            </div>
            <div className="mt-6">
              <Button
                onClick={() => { setShowTokenModal(false); setClusterToken(''); }}
                className="w-full bg-brand-600 hover:bg-brand-700 text-white"
              >
                {t('close')}
              </Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
