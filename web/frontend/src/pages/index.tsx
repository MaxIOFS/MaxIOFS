import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { Database, FolderOpen, Users, Activity, HardDrive, TrendingUp } from 'lucide-react';
import { formatBytes } from '@/lib/utils';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useNavigate } from 'react-router-dom';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function Dashboard() {
  const navigate = useNavigate();
  const { isGlobalAdmin } = useCurrentUser();

  // Fetch real metrics from backend
  const { data: metrics, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics'],
    queryFn: APIClient.getStorageMetrics,
    refetchInterval: 30000, // Refresh every 30 seconds
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
      // Use relative URL to the Console API health endpoint (same server)
      const response = await fetch('/api/v1/health');
      if (!response.ok) {
        throw new Error('Health check failed');
      }
      const result = await response.json();
      // Console API returns { success: true, data: { status: "healthy" } }
      return result.data || result;
    },
    refetchInterval: 30000, // Check every 30 seconds
    retry: 1, // Only retry once to avoid long waits
  });

  const isLoading = metricsLoading || bucketsLoading || usersLoading;

  // Calculate stats from real data
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
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          <p className="text-gray-600">Welcome to MaxIOFS Object Storage Console</p>
        </div>
        <div className="flex space-x-3">
          <Button variant="outline" onClick={() => navigate('/buckets')}>
            <Database className="h-4 w-4 mr-2" />
            Create Bucket
          </Button>
          <Button onClick={() => navigate('/buckets')}>
            <FolderOpen className="h-4 w-4 mr-2" />
            Browse Buckets
          </Button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Buckets</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalBuckets}</div>
            <p className="text-xs text-muted-foreground">
              Active storage containers
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <FolderOpen className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{totalObjects.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">
              Stored across all buckets
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Storage Used</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatBytes(totalSize)}</div>
            <p className="text-xs text-muted-foreground">
              Total storage consumption
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Users</CardTitle>
            <Users className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{activeUsers}</div>
            <p className="text-xs text-muted-foreground">
              Total: {users.length} users
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">System Health</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className={`text-2xl font-bold ${healthStatus?.status === 'healthy' ? 'text-green-600' : 'text-red-600'}`}>
              {healthStatus?.status === 'healthy' ? 'Healthy' : 'Offline'}
            </div>
            <p className="text-xs text-muted-foreground">
              {healthStatus?.status === 'healthy' ? 'S3 API operational' : 'Service unavailable'}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Encrypted Buckets</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {buckets.filter(b => b.encryption).length}
            </div>
            <p className="text-xs text-muted-foreground">
              Out of {totalBuckets} total buckets
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Quick Actions and Bucket List */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Quick Actions</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              <Button className="w-full justify-start" variant="outline" onClick={() => navigate('/buckets')}>
                <Database className="h-4 w-4 mr-2" />
                Create New Bucket
              </Button>
              {/* Browse Objects button removed - access objects through individual buckets */}
              <Button className="w-full justify-start" variant="outline" onClick={() => navigate('/users')}>
                <Users className="h-4 w-4 mr-2" />
                Manage Users
              </Button>
              {isGlobalAdmin && (
                <Button className="w-full justify-start" variant="outline" onClick={() => navigate('/metrics')}>
                  <Activity className="h-4 w-4 mr-2" />
                  View Metrics
                </Button>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Recent Buckets</CardTitle>
          </CardHeader>
          <CardContent>
            {buckets.length === 0 ? (
              <div className="text-center py-6">
                <Database className="mx-auto h-8 w-8 text-muted-foreground mb-2" />
                <p className="text-sm text-muted-foreground">No buckets yet</p>
                <Button
                  className="mt-3"
                  size="sm"
                  variant="outline"
                  onClick={() => navigate('/buckets')}
                >
                  Create your first bucket
                </Button>
              </div>
            ) : (
              <div className="space-y-3">
                {buckets.slice(0, 5).map((bucket) => (
                  <div
                    key={bucket.name}
                    className="flex items-center justify-between p-2 hover:bg-gray-50 rounded-md cursor-pointer"
                    onClick={() => navigate(`/buckets/${bucket.name}`)}
                  >
                    <div className="flex items-center gap-2">
                      <Database className="h-4 w-4 text-muted-foreground" />
                      <div>
                        <p className="text-sm font-medium">{bucket.name}</p>
                        <p className="text-xs text-muted-foreground">
                          {bucket.object_count || 0} objects · {formatBytes(bucket.size || 0)}
                        </p>
                      </div>
                    </div>
                  </div>
                ))}
                {buckets.length > 5 && (
                  <Button
                    variant="link"
                    className="w-full"
                    size="sm"
                    onClick={() => navigate('/buckets')}
                  >
                    View all {buckets.length} buckets
                  </Button>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
