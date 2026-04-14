import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
  X
} from 'lucide-react';
import APIClient from '@/lib/api';
import { getErrorMessage } from '@/lib/utils';
import type { ClusterNode, AddNodeRequest, UpdateNodeRequest } from '@/types';

type HealthStatus = 'healthy' | 'degraded' | 'unavailable' | 'unknown';

export default function ClusterNodes() {
  const { t } = useTranslation('cluster');
  const navigate = useNavigate();
  const [nodes, setNodes] = useState<ClusterNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAddNodeDialog, setShowAddNodeDialog] = useState(false);
  const [editingNode, setEditingNode] = useState<ClusterNode | null>(null);
const [localNodeId, setLocalNodeId] = useState<string | null>(null);
  const [availableNodes, setAvailableNodes] = useState<ClusterNode[]>([]);

  useEffect(() => {
    loadNodes();
  }, []);

  const loadNodes = async () => {
    try {
      setLoading(true);
      setError(null);
      const clusterConfig = await APIClient.getClusterConfig();
      setLocalNodeId(clusterConfig.node_id);
      const data = await APIClient.listClusterNodes();
      setNodes(data);
      const remoteNodes = data.filter(node => node.id !== clusterConfig.node_id);
      setAvailableNodes(remoteNodes);
    } catch (err: unknown) {
      setError(getErrorMessage(err, t('failedToLoadNodes')));
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
      alert(getErrorMessage(err, t('failedToAddNode')));
    }
  };

  const handleUpdateNode = async (nodeId: string, request: UpdateNodeRequest) => {
    try {
      await APIClient.updateClusterNode(nodeId, request);
      setEditingNode(null);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToUpdateNode')));
    }
  };

  const handleRemoveNode = async (nodeId: string) => {
    if (!confirm(t('confirmRemoveNode'))) return;
    try {
      await APIClient.removeClusterNode(nodeId);
      await loadNodes();
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToRemoveNode')));
    }
  };

  const handleCheckHealth = async (nodeId: string) => {
    try {
      const health = await APIClient.checkNodeHealth(nodeId);
      alert(`${t('healthStatus')} ${health.status}\n${t('latency')} ${health.latency_ms}ms`);
    } catch (err: unknown) {
      alert(getErrorMessage(err, t('failedToCheckHealth')));
    }
  };

const getHealthIcon = (status: HealthStatus) => {
    switch (status) {
      case 'healthy':     return <CheckCircle className="w-5 h-5 text-green-600 dark:text-green-400" />;
      case 'degraded':    return <AlertTriangle className="w-5 h-5 text-yellow-600 dark:text-yellow-400" />;
      case 'unavailable': return <XCircle className="w-5 h-5 text-red-600 dark:text-red-400" />;
      default:            return <HelpCircle className="w-5 h-5 text-muted-foreground" />;
    }
  };

  const getHealthLabel = (status: HealthStatus): string => {
    const map: Record<HealthStatus, string> = {
      healthy: t('statusHealthy'),
      degraded: t('statusDegraded'),
      unavailable: t('statusUnavailable'),
      unknown: t('statusUnknown'),
    };
    return map[status] ?? status;
  };

  const getHealthBadge = (status: HealthStatus) => {
    const colors: Record<HealthStatus, string> = {
      healthy:     'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200',
      degraded:    'bg-yellow-100 dark:bg-yellow-900 text-yellow-800 dark:text-yellow-200',
      unavailable: 'bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200',
      unknown:     'bg-gray-100 dark:bg-gray-700 text-foreground',
    };
    return (
      <span className={`px-2 py-1 text-xs font-medium rounded-full ${colors[status]}`}>
        {getHealthLabel(status)}
      </span>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text={t('loadingNodes')} />
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
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-2">
            <Button variant="outline" size="sm" onClick={() => navigate('/cluster')} className="bg-card">
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <h1 className="text-2xl font-bold text-foreground">{t('clusterNodesTitle')}</h1>
          </div>
          <p className="text-sm text-muted-foreground">{t('manageNodesDesc')}</p>
        </div>
        <div className="flex gap-3">
          <Button variant="outline" onClick={loadNodes} className="bg-card">
            <RefreshCw className="h-4 w-4" />
            {t('refresh')}
          </Button>
<Button onClick={() => setShowAddNodeDialog(true)} variant="default">
            <Plus className="h-4 w-4" />
            {t('addNode')}
          </Button>
        </div>
      </div>

      <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-border">
          <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
            <Server className="h-5 w-5 text-brand-600 dark:text-brand-400" />
            {t('clusterNodesCount', { count: nodes.length })}
          </h3>
        </div>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('colName')}</TableHead>
                <TableHead>{t('colEndpoint')}</TableHead>
                <TableHead>{t('colStatus')}</TableHead>
                <TableHead>{t('colLatency')}</TableHead>
                <TableHead>{t('colBuckets')}</TableHead>
                <TableHead>{t('colPriority')}</TableHead>
                <TableHead className="text-right">{t('colActions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center py-8 text-muted-foreground">
                    {t('noNodesYet')}
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
                              <span className="ml-2 text-xs font-normal text-muted-foreground">{t('thisNode')}</span>
                            )}
                          </div>
                          {getHealthIcon(node.health_status)}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap"><span className="text-sm">{node.endpoint}</span></TableCell>
                    <TableCell className="whitespace-nowrap">{getHealthBadge(node.health_status)}</TableCell>
                    <TableCell className="whitespace-nowrap"><span className="text-sm">{node.latency_ms}ms</span></TableCell>
                    <TableCell className="whitespace-nowrap"><span className="text-sm">{node.bucket_count}</span></TableCell>
                    <TableCell className="whitespace-nowrap"><span className="text-sm">{node.priority}</span></TableCell>
                    <TableCell className="whitespace-nowrap text-right">
                      <div className="flex items-center justify-end gap-2">
                        <button onClick={() => handleCheckHealth(node.id)} className="p-2 text-muted-foreground hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow" title={t('checkHealth')}>
                          <Activity className="h-4 w-4" />
                        </button>
                        <button onClick={() => setEditingNode(node)} className="p-2 text-muted-foreground hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow" title={t('editNode')}>
                          <Edit className="h-4 w-4" />
                        </button>
                        {node.id !== localNodeId && (
                          <button onClick={() => handleRemoveNode(node.id)} className="p-2 text-muted-foreground hover:text-error-600 dark:hover:text-error-400 hover:bg-gradient-to-br hover:from-error-50 hover:to-red-50 dark:hover:from-error-900/30 dark:hover:to-red-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow" title={t('removeNode')}>
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
              <h2 className="text-xl font-bold text-foreground">{t('addNodeTitle')}</h2>
              <button onClick={() => setShowAddNodeDialog(false)} className="text-muted-foreground hover:text-foreground"><X className="h-5 w-5" /></button>
            </div>
            <p className="text-sm text-muted-foreground mb-4">{t('addNodeDesc')}</p>
            <form onSubmit={(e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              handleAddNode({ endpoint: formData.get('endpoint') as string, username: formData.get('username') as string, password: formData.get('password') as string, node_name: formData.get('node_name') as string || undefined } as AddNodeRequest);
            }}>
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('consoleUrl')}</label>
                  <input name="endpoint" type="text" required placeholder="192.168.1.10" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                  <p className="text-xs text-muted-foreground mt-1">{t('consoleUrlHint')}</p>
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('nodeName')}</label>
                  <input name="node_name" type="text" placeholder="node-2" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                  <p className="text-xs text-muted-foreground mt-1">{t('nodeNameHint')}</p>
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('adminUsername')}</label>
                  <input name="username" type="text" required placeholder="admin" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('adminPassword')}</label>
                  <input name="password" type="password" required placeholder="********" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                </div>
              </div>
              <div className="flex gap-2 mt-6">
                <Button type="button" variant="outline" onClick={() => setShowAddNodeDialog(false)} className="flex-1">{t('cancel')}</Button>
                <Button type="submit" className="flex-1 bg-brand-600 hover:bg-brand-700 text-white">{t('addNode')}</Button>
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
              <h2 className="text-xl font-bold text-foreground">{t('editNodeTitle')}</h2>
              <button onClick={() => setEditingNode(null)} className="text-muted-foreground hover:text-foreground"><X className="h-5 w-5" /></button>
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
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('nodeNameFieldLabel')}</label>
                  <input name="name" type="text" defaultValue={editingNode.name} placeholder="node-us-west-1" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('endpointUrl')}</label>
                  <input type="text" value={editingNode.endpoint} disabled className="w-full border border-border rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-800 text-muted-foreground" />
                  <p className="text-xs text-muted-foreground mt-1">{t('endpointCannotChange')}</p>
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('nodeId')}</label>
                  <input type="text" value={editingNode.id} disabled className="w-full border border-border rounded-lg px-3 py-2 bg-gray-100 dark:bg-gray-800 text-muted-foreground font-mono text-sm" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('region')}</label>
                  <input name="region" type="text" defaultValue={editingNode.region || ''} placeholder="us-west-1" className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('priority')}</label>
                  <input name="priority" type="number" defaultValue={editingNode.priority} min={1} max={1000} className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500" />
                  <p className="text-xs text-muted-foreground mt-1">{t('priorityHint')}</p>
                </div>
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('metadataJson')}</label>
                  <textarea name="metadata" rows={3} defaultValue={editingNode.metadata || ''} placeholder='{"location": "datacenter-1", "environment": "production"}' className="w-full border border-border rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-brand-500 font-mono text-sm" />
                </div>
                <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-4 space-y-2">
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">{t('healthStatus')}</span>
                    <span className={`font-medium ${editingNode.health_status === 'healthy' ? 'text-green-600' : editingNode.health_status === 'degraded' ? 'text-yellow-600' : 'text-red-600'}`}>
                      {getHealthLabel(editingNode.health_status as HealthStatus)}
                    </span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">{t('latency')}</span>
                    <span className="font-medium text-foreground">{editingNode.latency_ms}ms</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">{t('bucketCount')}</span>
                    <span className="font-medium text-foreground">{editingNode.bucket_count}</span>
                  </div>
                </div>
              </div>
              <div className="flex gap-2 mt-6">
                <Button type="button" variant="outline" onClick={() => setEditingNode(null)} className="flex-1">{t('cancel')}</Button>
                <Button type="submit" className="flex-1 bg-brand-600 hover:bg-brand-700 text-white">{t('saveChanges')}</Button>
              </div>
            </form>
          </Card>
        </div>
      )}

    </div>
  );
}
