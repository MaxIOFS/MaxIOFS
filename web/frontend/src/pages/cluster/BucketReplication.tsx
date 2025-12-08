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
  ArrowLeft
} from 'lucide-react';
import APIClient from '@/lib/api';
import type { BucketWithReplication } from '@/types';

type FilterType = 'all' | 'replicated' | 'local';

export default function BucketReplication() {
  const navigate = useNavigate();
  const [buckets, setBuckets] = useState<BucketWithReplication[]>([]);
  const [filteredBuckets, setFilteredBuckets] = useState<BucketWithReplication[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<FilterType>('all');
  const [selectedBucket, setSelectedBucket] = useState<string | null>(null);

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
      setBuckets(data.buckets || []);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to load buckets');
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
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Bucket Replication Manager</h1>
          </div>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Configure replication for your buckets across cluster nodes
          </p>
        </div>
        <Button
          variant="outline"
          onClick={loadBuckets}
          className="bg-white dark:bg-gray-800"
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
      <Card className="p-4">
        <div className="flex items-center gap-4">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Filter:</span>
          <div className="flex gap-2">
            <Button
              variant={filter === 'all' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilter('all')}
              className={filter === 'all' ? 'bg-brand-600 hover:bg-brand-700 text-white' : ''}
            >
              All ({buckets.length})
            </Button>
            <Button
              variant={filter === 'replicated' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilter('replicated')}
              className={filter === 'replicated' ? 'bg-brand-600 hover:bg-brand-700 text-white' : ''}
            >
              Replicated ({buckets.filter(b => b.has_replication).length})
            </Button>
            <Button
              variant={filter === 'local' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setFilter('local')}
              className={filter === 'local' ? 'bg-brand-600 hover:bg-brand-700 text-white' : ''}
            >
              Local Only ({buckets.filter(b => !b.has_replication).length})
            </Button>
          </div>
        </div>
      </Card>

      {/* Buckets Table */}
      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
            <thead className="bg-gray-50 dark:bg-gray-700">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                  Bucket Name
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                  Primary Node
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                  Replicas
                </th>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
              {filteredBuckets.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-6 py-12 text-center">
                    <Package className="w-12 h-12 text-gray-400 mx-auto mb-3" />
                    <p className="text-gray-500 dark:text-gray-400">
                      {filter === 'replicated' && 'No replicated buckets found'}
                      {filter === 'local' && 'No local-only buckets found'}
                      {filter === 'all' && 'No buckets found'}
                    </p>
                  </td>
                </tr>
              ) : (
                filteredBuckets.map((bucket) => (
                  <tr key={bucket.name} className="hover:bg-gray-50 dark:hover:bg-gray-700/50 transition-colors">
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2">
                        <Package className="w-5 h-5 text-brand-600 dark:text-brand-400" />
                        <span className="font-medium text-gray-900 dark:text-white">{bucket.name}</span>
                      </div>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-400">
                      {bucket.primary_node}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      {bucket.replica_count > 0 ? (
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 dark:bg-blue-900/50 text-blue-800 dark:text-blue-200">
                          {bucket.replica_count} {bucket.replica_count === 1 ? 'replica' : 'replicas'}
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
                          No replicas
                        </span>
                      )}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      {bucket.has_replication ? (
                        <div className="flex items-center gap-1.5 text-sm text-green-600 dark:text-green-400">
                          <CheckCircle className="w-4 h-4" />
                          <span>Replicating</span>
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5 text-sm text-gray-500 dark:text-gray-400">
                          <XCircle className="w-4 h-4" />
                          <span>Local only</span>
                        </div>
                      )}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setSelectedBucket(bucket.name)}
                        className="inline-flex items-center"
                      >
                        <Settings className="w-4 h-4 mr-1" />
                        Configure
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </Card>

      {/* TODO: ConfigureReplicationModal */}
      {selectedBucket && (
        <div className="fixed inset-0 bg-black bg-opacity-50 dark:bg-opacity-70 flex items-center justify-center z-50 p-4">
          <Card className="w-full max-w-2xl p-6">
            <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-4">
              Configure Replication: {selectedBucket}
            </h2>
            <p className="text-gray-600 dark:text-gray-400 mb-6">
              Replication configuration modal will be implemented here. You'll be able to:
            </p>
            <ul className="list-disc list-inside text-gray-600 dark:text-gray-400 mb-6 space-y-1">
              <li>Add replication targets (destination nodes)</li>
              <li>Configure replication mode (realtime, scheduled, batch)</li>
              <li>Set replication schedule</li>
              <li>Enable/disable replication for deletes and metadata</li>
            </ul>
            <div className="flex justify-end">
              <Button
                variant="outline"
                onClick={() => setSelectedBucket(null)}
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
