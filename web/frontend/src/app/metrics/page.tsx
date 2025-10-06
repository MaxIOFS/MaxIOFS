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

  // Helper to format uptime from seconds
  const formatUptime = (seconds: number) => {
    if (!seconds) return 'N/A';

    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    if (days > 0) return `${days}d ${hours}h ${minutes}m`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  };

  // Build display metrics from backend data
  const displayMetrics = {
    system: {
      uptime: formatUptime(systemMetrics.uptime_seconds || 0),
      cpu: systemMetrics.cpu_percent || 0,
      memory: systemMetrics.memory?.used_percent || 0,
      disk: systemMetrics.disk?.used_percent || 0,
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
      totalRequests: systemMetrics.requests?.total_requests || 0,
      totalErrors: systemMetrics.requests?.total_errors || 0,
      avgLatency: systemMetrics.requests?.average_latency_ms || 0,
      requestsPerSec: systemMetrics.requests?.requests_per_sec || 0
    },
    performance: {
      uptime: systemMetrics.performance?.uptime_seconds || 0,
      goRoutines: systemMetrics.performance?.goroutines || 0,
      heapAllocMB: systemMetrics.performance?.heap_alloc_mb || 0,
      gcRuns: systemMetrics.performance?.gc_runs || 0
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
              <div className="text-2xl font-bold">{displayMetrics.system.cpu.toFixed(2)}%</div>
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
              <div className="text-2xl font-bold">{displayMetrics.system.memory.toFixed(2)}%</div>
              <p className="text-xs text-muted-foreground mt-1">
                {formatBytes(systemMetrics.memory?.used_bytes || 0)} / {formatBytes(systemMetrics.memory?.total_bytes || 0)}
              </p>
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
              <div className="text-2xl font-bold">{displayMetrics.system.disk.toFixed(2)}%</div>
              <p className="text-xs text-muted-foreground mt-1">
                {formatBytes(systemMetrics.disk?.used_bytes || 0)} / {formatBytes(systemMetrics.disk?.total_bytes || 0)}
              </p>
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
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Requests</CardTitle>
              <Globe className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.requests.totalRequests)}</div>
              <p className="text-xs text-muted-foreground">
                Since startup
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Total Errors</CardTitle>
              <BarChart3 className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.requests.totalErrors)}</div>
              <p className="text-xs text-muted-foreground">
                Failed requests
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Avg Latency</CardTitle>
              <Zap className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.requests.avgLatency.toFixed(1)}ms</div>
              <p className="text-xs text-muted-foreground">
                Average response time
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Requests/sec</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.requests.requestsPerSec.toFixed(2)}</div>
              <p className="text-xs text-muted-foreground">
                Current throughput
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Performance Metrics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Runtime Performance</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Goroutines</CardTitle>
              <Activity className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.performance.goRoutines)}</div>
              <p className="text-xs text-muted-foreground">
                Active goroutines
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Heap Memory</CardTitle>
              <BarChart3 className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{displayMetrics.performance.heapAllocMB.toFixed(1)} MB</div>
              <p className="text-xs text-muted-foreground">
                Allocated heap
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">GC Runs</CardTitle>
              <TrendingUp className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{formatNumber(displayMetrics.performance.gcRuns)}</div>
              <p className="text-xs text-muted-foreground">Garbage collections</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Success Rate</CardTitle>
              <TrendingDown className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {displayMetrics.requests.totalRequests > 0
                  ? (((displayMetrics.requests.totalRequests - displayMetrics.requests.totalErrors) / displayMetrics.requests.totalRequests) * 100).toFixed(2)
                  : (100).toFixed(2)}%
              </div>
              <p className="text-xs text-muted-foreground">
                Request success rate
              </p>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}