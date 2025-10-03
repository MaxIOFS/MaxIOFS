'use client';

import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  BarChart3,
  Activity,
  HardDrive,
  Users,
  Clock,
  TrendingUp,
  TrendingDown,
  Zap,
  Database,
  Globe
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

export default function MetricsPage() {
  // Fetch storage metrics from backend
  const { data: storageMetricsData, isLoading: storageLoading } = useQuery({
    queryKey: ['storageMetrics'],
    queryFn: APIClient.getStorageMetrics,
  });

  // Fetch system metrics from backend
  const { data: systemMetricsData, isLoading: systemLoading } = useQuery({
    queryKey: ['systemMetrics'],
    queryFn: APIClient.getSystemMetrics,
  });

  const isLoading = storageLoading || systemLoading;

  // Parse backend metrics
  const storageMetrics = storageMetricsData || {};
  const systemMetrics = systemMetricsData || {};

  // Build display metrics from backend data
  const displayMetrics = {
    system: {
      uptime: systemMetrics.uptime || 'N/A',
      cpu: (systemMetrics.cpu_usage || 0) * 100,
      memory: (systemMetrics.memory_usage || 0) * 100,
      disk: (systemMetrics.disk_usage || 0) * 100,
      network: 0
    },
    storage: {
      totalSize: (storageMetrics.total_buckets || 0) * 100 * 1024 * 1024, // Estimate
      usedSize: storageMetrics.total_size || 0,
      objects: storageMetrics.total_objects || 0,
      buckets: storageMetrics.total_buckets || 0,
      averageObjectSize: storageMetrics.total_objects > 0
        ? (storageMetrics.total_size || 0) / storageMetrics.total_objects
        : 0
    },
    requests: {
      today: 0, // TODO: Implement request metrics
      thisWeek: 0,
      thisMonth: 0,
      getRequests: 0,
      putRequests: 0,
      deleteRequests: 0
    },
    performance: {
      avgResponseTime: 0, // TODO: Implement performance metrics
      p95ResponseTime: 0,
      p99ResponseTime: 0,
      requestsPerSecond: 0,
      errorRate: 0,
      successRate: 100
    }
  };

  const formatBytes = (bytes: number) => {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = bytes;
    let unitIndex = 0;

    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }

    return `${size.toFixed(1)} ${units[unitIndex]}`;
  };

  const formatNumber = (num: number) => {
    return new Intl.NumberFormat().format(num);
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Metrics Overview</h1>
          <p className="text-muted-foreground">
            Monitor system performance and usage statistics
          </p>
        </div>
        <div className="flex items-center space-x-2 text-sm text-muted-foreground">
          <div className="w-2 h-2 bg-green-500 rounded-full"></div>
          <span>Live Data</span>
        </div>
      </div>

      {/* System Metrics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">System Health</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">CPU Usage</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.system.cpu}%</div>
              <div className="w-full bg-gray-200 rounded-full h-2 mt-2">
                <div
                  className="bg-blue-600 h-2 rounded-full transition-all duration-300"
                  style={{ width: `${displayMetrics.system.cpu}%` }}
                ></div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Memory Usage</CardTitle>
              <BarChart3 className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.system.memory}%</div>
              <div className="w-full bg-gray-200 rounded-full h-2 mt-2">
                <div
                  className="bg-green-600 h-2 rounded-full transition-all duration-300"
                  style={{ width: `${displayMetrics.system.memory}%` }}
                ></div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Disk Usage</CardTitle>
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.system.disk}%</div>
              <div className="w-full bg-gray-200 rounded-full h-2 mt-2">
                <div
                  className="bg-yellow-600 h-2 rounded-full transition-all duration-300"
                  style={{ width: `${displayMetrics.system.disk}%` }}
                ></div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Uptime</CardTitle>
              <Clock className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-lg font-bold">{displayMetrics.system.uptime}</div>
              <p className="text-xs text-muted-foreground">
                System uptime
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Storage Metrics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Storage Statistics</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Storage</CardTitle>
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(displayMetrics.storage.usedSize)}</div>
              <p className="text-xs text-muted-foreground">
                Storage in use
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
              <Database className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.storage.objects)}</div>
              <p className="text-xs text-muted-foreground">
                Stored objects
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Buckets</CardTitle>
              <Database className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.storage.buckets}</div>
              <p className="text-xs text-muted-foreground">
                Total buckets
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Avg Object Size</CardTitle>
              <BarChart3 className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatBytes(displayMetrics.storage.averageObjectSize)}</div>
              <p className="text-xs text-muted-foreground">
                Per object
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Request Metrics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Request Statistics</h2>
        <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4 mb-4">
          <p className="text-sm text-yellow-800">
            <strong>⚠️ Note:</strong> Request metrics are not yet implemented in the backend.
            These counters will show data once the metrics collection system is activated.
          </p>
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Requests Today</CardTitle>
              <Globe className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.requests.today)}</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>

          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">This Week</CardTitle>
              <BarChart3 className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.requests.thisWeek)}</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>

          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">This Month</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.requests.thisMonth)}</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Performance Metrics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Performance</h2>
        <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4 mb-4">
          <p className="text-sm text-yellow-800">
            <strong>⚠️ Note:</strong> Performance metrics are not yet implemented in the backend.
            Response time and throughput data will be available once monitoring is enabled.
          </p>
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Avg Response Time</CardTitle>
              <Zap className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.performance.avgResponseTime}ms</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>

          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Requests/sec</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.performance.requestsPerSecond}</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>

          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
              <TrendingUp className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.performance.successRate}%</div>
              <p className="text-xs text-muted-foreground">Not available</p>
            </CardContent>
          </Card>

          <Card className="opacity-60">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Error Rate</CardTitle>
              <TrendingDown className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.performance.errorRate}%</div>
              <p className="text-xs text-muted-foreground">
                Not available
              </p>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}