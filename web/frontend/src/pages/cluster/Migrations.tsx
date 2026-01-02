import { useState } from 'react';
import {
  ArrowRightLeft,
  Clock,
  TrendingUp,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Eye,
  Server,
  Database
} from 'lucide-react';
import type { MigrationJob, MigrateBucketRequest, ClusterNode, BucketWithReplication } from '@/types';

interface MigrationsTabProps {
  migrations: MigrationJob[];
  buckets: BucketWithReplication[];
  nodes: ClusterNode[];
  onMigrate: (request: MigrateBucketRequest) => void;
  onViewDetails: (id: number) => void;
  onRefresh: () => void;
}

export function MigrationsTab({ migrations, buckets, nodes, onMigrate, onViewDetails, onRefresh }: MigrationsTabProps) {
  const [showMigrateDialog, setShowMigrateDialog] = useState(false);
  const [selectedBucket, setSelectedBucket] = useState<string | null>(null);

  const getMigrationStatusBadge = (status: string) => {
    const colors = {
      pending: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300',
      in_progress: 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300',
      completed: 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-300',
      failed: 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-300',
      cancelled: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-300'
    };
    return (
      <span className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status as keyof typeof colors] || colors.pending}`}>
        {status.replace('_', ' ')}
      </span>
    );
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`;
  };

  const getProgressPercentage = (migrated: number, total: number) => {
    if (total === 0) return 0;
    return Math.round((migrated / total) * 100);
  };

  const handleStartMigration = (bucket: string) => {
    setSelectedBucket(bucket);
    setShowMigrateDialog(true);
  };

  return (
    <div className="space-y-4">
      {/* Header with actions */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
        <div>
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">Bucket Migrations</h3>
          <p className="text-sm text-gray-600 dark:text-gray-400">Move buckets between cluster nodes</p>
        </div>
        <button
          onClick={() => setShowMigrateDialog(true)}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 dark:bg-blue-600 dark:hover:bg-blue-700"
        >
          <ArrowRightLeft className="w-4 h-4" />
          Migrate Bucket
        </button>
      </div>

      {/* Migrations Table */}
      <div className="overflow-x-auto">
        <table className="w-full">
          <thead className="bg-gray-50 dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">ID</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Bucket</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Source → Target</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Status</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Progress</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Data Size</th>
              <th className="px-6 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Started</th>
              <th className="px-6 py-3 text-right text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {migrations.length === 0 ? (
              <tr>
                <td colSpan={8} className="px-6 py-12 text-center">
                  <div className="flex flex-col items-center gap-3">
                    <ArrowRightLeft className="w-12 h-12 text-gray-400" />
                    <div>
                      <p className="text-gray-900 dark:text-white font-medium">No migrations yet</p>
                      <p className="text-sm text-gray-600 dark:text-gray-400">Start migrating buckets between nodes</p>
                    </div>
                  </div>
                </td>
              </tr>
            ) : (
              migrations.map((migration) => {
                const progress = getProgressPercentage(migration.objects_migrated, migration.objects_total);
                return (
                  <tr key={migration.id} className="hover:bg-gradient-to-r hover:from-brand-50/30 hover:to-blue-50/30 dark:hover:from-brand-900/10 dark:hover:to-blue-900/10 transition-all duration-200">
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className="text-sm font-mono text-gray-900 dark:text-gray-300">#{migration.id}</span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center gap-2">
                        <Database className="w-4 h-4 text-brand-600 dark:text-brand-400" />
                        <span className="text-sm font-medium text-gray-900 dark:text-gray-300">{migration.bucket_name}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center gap-2 text-sm text-gray-900 dark:text-gray-300">
                        <span className="font-mono">{migration.source_node_id}</span>
                        <ArrowRightLeft className="w-3 h-3 text-gray-400" />
                        <span className="font-mono">{migration.target_node_id}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      {getMigrationStatusBadge(migration.status)}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="space-y-1">
                        <div className="flex items-center gap-2">
                          <div className="flex-1 h-2 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                            <div
                              className={`h-full transition-all duration-300 ${
                                migration.status === 'completed' ? 'bg-green-500' :
                                migration.status === 'failed' ? 'bg-red-500' :
                                'bg-blue-500'
                              }`}
                              style={{ width: `${progress}%` }}
                            />
                          </div>
                          <span className="text-xs font-medium text-gray-900 dark:text-gray-300 w-12 text-right">{progress}%</span>
                        </div>
                        <div className="text-xs text-gray-600 dark:text-gray-400">
                          {migration.objects_migrated.toLocaleString()} / {migration.objects_total.toLocaleString()} objects
                        </div>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="text-sm text-gray-900 dark:text-gray-300">
                        {formatBytes(migration.bytes_migrated)} / {formatBytes(migration.bytes_total)}
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <div className="flex items-center gap-1 text-xs text-gray-600 dark:text-gray-400">
                        <Clock className="w-3 h-3" />
                        {migration.started_at ? new Date(migration.started_at).toLocaleString() : '-'}
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-right">
                      <button
                        onClick={() => onViewDetails(migration.id)}
                        className="inline-flex items-center gap-1 px-3 py-1.5 text-sm font-medium text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200"
                      >
                        <Eye className="w-3.5 h-3.5" />
                        Details
                      </button>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Migrate Bucket Dialog */}
      {showMigrateDialog && (
        <MigrateBucketDialog
          buckets={buckets}
          nodes={nodes}
          selectedBucket={selectedBucket}
          onClose={() => {
            setShowMigrateDialog(false);
            setSelectedBucket(null);
          }}
          onSubmit={(bucket, request) => {
            setSelectedBucket(bucket);
            onMigrate(request);
            setShowMigrateDialog(false);
          }}
        />
      )}
    </div>
  );
}

// Migrate Bucket Dialog
function MigrateBucketDialog({
  buckets,
  nodes,
  selectedBucket,
  onClose,
  onSubmit
}: {
  buckets: BucketWithReplication[];
  nodes: ClusterNode[];
  selectedBucket: string | null;
  onClose: () => void;
  onSubmit: (bucket: string, request: MigrateBucketRequest) => void;
}) {
  const [bucket, setBucket] = useState(selectedBucket || '');
  const [targetNodeId, setTargetNodeId] = useState('');
  const [deleteSource, setDeleteSource] = useState(false);
  const [verifyData, setVerifyData] = useState(true);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!bucket || !targetNodeId) {
      alert('Please select a bucket and target node');
      return;
    }

    const request: MigrateBucketRequest = {
      target_node_id: targetNodeId,
      delete_source: deleteSource,
      verify_data: verifyData
    };

    onSubmit(bucket, request);
  };

  return (
    <div className="fixed inset-0 bg-black/50 dark:bg-black/70 flex items-center justify-center z-50">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-md p-6">
        <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-4">Migrate Bucket</h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Bucket *</label>
            <select
              value={bucket}
              onChange={(e) => setBucket(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              required
              disabled={!!selectedBucket}
            >
              <option value="">Select a bucket</option>
              {buckets.map((b) => (
                <option key={b.name} value={b.name}>
                  {b.name} ({b.primary_node})
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Target Node *</label>
            <select
              value={targetNodeId}
              onChange={(e) => setTargetNodeId(e.target.value)}
              className="w-full border border-gray-300 dark:border-gray-600 rounded-lg px-3 py-2 bg-white dark:bg-gray-700 text-gray-900 dark:text-white"
              required
            >
              <option value="">Select target node</option>
              {nodes.filter(n => n.health_status === 'healthy').map((node) => (
                <option key={node.id} value={node.id}>
                  {node.name} ({node.endpoint}) - {node.health_status}
                </option>
              ))}
            </select>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Only healthy nodes are shown</p>
          </div>

          <div className="space-y-2">
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={verifyData}
                onChange={(e) => setVerifyData(e.target.checked)}
                className="rounded border-gray-300 dark:border-gray-600"
              />
              <span className="text-sm text-gray-700 dark:text-gray-300">Verify data integrity after migration</span>
            </label>

            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={deleteSource}
                onChange={(e) => setDeleteSource(e.target.checked)}
                className="rounded border-gray-300 dark:border-gray-600"
              />
              <span className="text-sm text-gray-700 dark:text-gray-300">Delete source data after successful migration</span>
            </label>
          </div>

          <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
            <p className="text-xs text-yellow-800 dark:text-yellow-200">
              <strong>Warning:</strong> Migration will move all bucket data to the target node. Ensure the target node has sufficient storage space.
            </p>
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
              Start Migration
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
