'use client';

import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { Database, FolderOpen, Users, Activity, HardDrive, TrendingUp } from 'lucide-react';
import { formatBytes } from '@/lib/utils';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useRouter } from 'next/navigation';

export default function Dashboard() {
  const router = useRouter();

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
          <Button variant="outline" onClick={() => router.push('/buckets')}>
            <Database className="h-4 w-4 mr-2" />
            Create Bucket
          </Button>
          <Button onClick={() => router.push('/buckets')}>
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
            <CardTitle className="text-sm font-medium">System Status</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">Online</div>
            <p className="text-xs text-muted-foreground">
              All services operational
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">API Version</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">S3 Compatible</div>
            <p className="text-xs text-muted-foreground">
              Full S3 API support
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
              <Button className="w-full justify-start" variant="outline" onClick={() => router.push('/buckets')}>
                <Database className="h-4 w-4 mr-2" />
                Create New Bucket
              </Button>
              <Button className="w-full justify-start" variant="outline" onClick={() => router.push('/buckets')}>
                <FolderOpen className="h-4 w-4 mr-2" />
                Browse Objects
              </Button>
              <Button className="w-full justify-start" variant="outline" onClick={() => router.push('/users')}>
                <Users className="h-4 w-4 mr-2" />
                Manage Users
              </Button>
              <Button className="w-full justify-start" variant="outline" onClick={() => router.push('/metrics')}>
                <Activity className="h-4 w-4 mr-2" />
                View Metrics
              </Button>
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
                  onClick={() => router.push('/buckets')}
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
                    onClick={() => router.push(`/buckets/${bucket.name}`)}
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
                    onClick={() => router.push('/buckets')}
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