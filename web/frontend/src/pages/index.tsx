import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import { Database, FolderOpen, Users, Activity, HardDrive, TrendingUp, ArrowUpRight, Shield } from 'lucide-react';
import { formatBytes } from '@/lib/utils';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useNavigate } from 'react-router-dom';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function Dashboard() {
  const navigate = useNavigate();
  const { isGlobalAdmin } = useCurrentUser();

  // Get base path from window (injected by backend based on public_console_url)
  const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');

  const { data: metrics, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics'],
    queryFn: APIClient.getStorageMetrics,
    refetchInterval: 30000,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const { data: bucketsResponse, isLoading: bucketsLoading } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const { data: usersResponse, isLoading: usersLoading } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  const { data: healthStatus } = useQuery({
    queryKey: ['health'],
    queryFn: async () => {
      const response = await fetch(`${basePath}/api/v1/health`);
      if (!response.ok) {
        throw new Error('Health check failed');
      }
      const result = await response.json();
      return result.data || result;
    },
    refetchInterval: 30000,
    retry: 1,
    refetchOnWindowFocus: false,
  });

  const isLoading = metricsLoading || bucketsLoading || usersLoading;

  const buckets = bucketsResponse || [];
  const users = usersResponse || [];
  const totalBuckets = buckets.length;
  const totalObjects = buckets.reduce((sum, bucket) => sum + (bucket.object_count || 0), 0);
  const totalSize = buckets.reduce((sum, bucket) => sum + (bucket.size || 0), 0);
  const activeUsers = users.filter(u => u.status === 'active').length;
  
  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text="Loading dashboard..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Dashboard</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Welcome to MaxIOFS Object Storage Console</p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Button 
            variant="outline" 
            onClick={() => navigate('/buckets')}
            className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white"
          >
            <Database className="h-4 w-4 mr-2" />
            View Buckets
          </Button>
          <Button 
            onClick={() => navigate('/buckets/create')}
            className="bg-brand-600 hover:bg-brand-700 text-white"
          >
            <FolderOpen className="h-4 w-4 mr-2" />
            Create Bucket
          </Button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 4xl:grid-cols-4 5xl:grid-cols-5 gap-4 md:gap-6 4xl:gap-8">
        <MetricCard
          title="Total Buckets"
          value={totalBuckets}
          icon={Database}
          description="Active storage containers"
          color="brand"
        />

        <MetricCard
          title="Total Objects"
          value={totalObjects.toLocaleString()}
          icon={FolderOpen}
          description="Stored across all buckets"
          color="blue-light"
        />

        <MetricCard
          title="Storage Used"
          value={formatBytes(totalSize)}
          icon={HardDrive}
          description="Total storage consumption"
          color="warning"
        />

        <MetricCard
          title="Active Users"
          value={activeUsers}
          icon={Users}
          description={`Total: ${users.length} users`}
          color="success"
        />

        <MetricCard
          title="System Health"
          value={healthStatus?.status === 'healthy' ? 'Healthy' : 'Offline'}
          icon={Activity}
          description={healthStatus?.status === 'healthy' ? 'S3 API operational' : 'Service unavailable'}
          color={healthStatus?.status === 'healthy' ? 'success' : 'error'}
        />

        <MetricCard
          title="Encrypted Buckets"
          value={buckets.filter(b => b.encryption).length}
          icon={Shield}
          description={`Out of ${totalBuckets} total buckets`}
          color="warning"
        />
      </div>

      {/* Quick Actions and Recent Buckets */}
      <div className="grid grid-cols-1 lg:grid-cols-2 4xl:grid-cols-3 gap-6 4xl:gap-8">
        {/* Quick Actions Card */}
        <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card hover:shadow-card-hover transition-all duration-300 overflow-hidden">
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white">Quick Actions</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Common tasks and shortcuts</p>
          </div>
          <div className="p-6">
            <div className="space-y-3">
              <button
                onClick={() => navigate('/buckets')}
                className="w-full flex items-center justify-between px-4 py-3.5 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-button transition-all duration-200 text-left group shadow-soft hover:shadow-soft-md"
              >
                <div className="flex items-center space-x-3">
                  <div className="flex items-center justify-center w-10 h-10 rounded-button bg-brand-100 dark:bg-brand-900/30">
                    <Database className="h-5 w-5 text-brand-600 dark:text-brand-400" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">Create New Bucket</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">Set up a new storage container</p>
                  </div>
                </div>
                <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-brand-600 dark:group-hover:text-brand-400 transition-colors" />
              </button>

              <button
                onClick={() => navigate('/users')}
                className="w-full flex items-center justify-between px-4 py-3.5 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-button transition-all duration-200 text-left group shadow-soft hover:shadow-soft-md"
              >
                <div className="flex items-center space-x-3">
                  <div className="flex items-center justify-center w-10 h-10 rounded-button bg-success-100 dark:bg-success-900/30">
                    <Users className="h-5 w-5 text-success-600 dark:text-success-400" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-gray-900 dark:text-white">Manage Users</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">Add or edit user accounts</p>
                  </div>
                </div>
                <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-success-600 dark:group-hover:text-success-400 transition-colors" />
              </button>

              {isGlobalAdmin && (
                <button
                  onClick={() => navigate('/metrics')}
                  className="w-full flex items-center justify-between px-4 py-3.5 bg-gray-50 dark:bg-gray-700 hover:bg-gray-100 dark:hover:bg-gray-600 rounded-button transition-all duration-200 text-left group shadow-soft hover:shadow-soft-md"
                >
                  <div className="flex items-center space-x-3">
                    <div className="flex items-center justify-center w-10 h-10 rounded-button bg-orange-100 dark:bg-orange-900/30">
                      <Activity className="h-5 w-5 text-orange-600 dark:text-orange-400" />
                    </div>
                    <div>
                      <p className="text-sm font-medium text-gray-900 dark:text-white">View Metrics</p>
                      <p className="text-xs text-gray-500 dark:text-gray-400">Check system statistics</p>
                    </div>
                  </div>
                  <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-orange-600 dark:group-hover:text-orange-400 transition-colors" />
                </button>
              )}
            </div>
          </div>
        </div>

        {/* Recent Buckets Card */}
        <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card hover:shadow-card-hover transition-all duration-300 overflow-hidden">
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white">Recent Buckets</h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Your latest storage containers</p>
          </div>
          <div className="p-6">
            {buckets.length === 0 ? (
              <div className="text-center py-8">
                <div className="flex items-center justify-center w-16 h-16 mx-auto rounded-full bg-gray-100 dark:bg-gray-700 mb-4">
                  <Database className="h-8 w-8 text-gray-400 dark:text-gray-500" />
                </div>
                <p className="text-sm font-medium text-gray-900 dark:text-white mb-1">No buckets yet</p>
                <p className="text-xs text-gray-500 dark:text-gray-400 mb-4">Create your first bucket to get started</p>
                <Button
                  size="sm"
                  onClick={() => navigate('/buckets')}
                  className="bg-brand-600 hover:bg-brand-700 text-white"
                >
                  Create Bucket
                </Button>
              </div>
            ) : (
              <div className="space-y-2">
                {buckets
                  .sort((a, b) => new Date(b.creation_date).getTime() - new Date(a.creation_date).getTime())
                  .slice(0, 3)
                  .map((bucket) => {
                    // Construir la URL correcta basada en si el bucket tiene tenant_id
                    const tenantId = bucket.tenant_id || bucket.tenantId;
                    const bucketPath = tenantId
                      ? `/buckets/${tenantId}/${bucket.name}`
                      : `/buckets/${bucket.name}`;
                    
                    return (
                      <div
                        key={bucket.name}
                        className="flex items-center justify-between p-3 hover:bg-gray-50 dark:hover:bg-gray-700 rounded-button cursor-pointer transition-all duration-200 group shadow-soft hover:shadow-soft-md"
                        onClick={() => navigate(bucketPath)}
                      >
                        <div className="flex items-center gap-3 flex-1 min-w-0">
                          <div className="flex items-center justify-center w-10 h-10 rounded-button bg-brand-50 dark:bg-brand-900/30 flex-shrink-0">
                            <Database className="h-5 w-5 text-brand-600 dark:text-brand-400" />
                          </div>
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 dark:text-white truncate">{bucket.name}</p>
                            <p className="text-xs text-gray-500 dark:text-gray-400">
                              {bucket.object_count || 0} objects · {formatBytes(bucket.size || 0)}
                            </p>
                          </div>
                        </div>
                        <ArrowUpRight className="h-4 w-4 text-gray-400 dark:text-gray-500 group-hover:text-brand-600 dark:group-hover:text-brand-400 transition-colors flex-shrink-0" />
                      </div>
                    );
                  })}
                {buckets.length > 3 && (
                  <button
                    onClick={() => navigate('/buckets')}
                    className="w-full mt-4 text-center text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium py-2"
                  >
                    View all {buckets.length} buckets →
                  </button>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
