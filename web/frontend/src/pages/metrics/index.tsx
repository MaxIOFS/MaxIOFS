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
import type { StorageMetrics, SystemMetrics, S3Metrics } from '@/types';

export default function MetricsPage() {
  const [activeTab, setActiveTab] = React.useState<'system' | 'storage' | 'requests' | 'performance'>('system');

  // Fetch storage metrics from backend
  const { data: storageMetricsData, isLoading: storageLoading } = useQuery<StorageMetrics>({
    queryKey: ['storageMetrics'],
    queryFn: APIClient.getStorageMetrics,
  });

  // Fetch system metrics from backend
  const { data: systemMetricsData, isLoading: systemLoading } = useQuery<SystemMetrics>({
    queryKey: ['systemMetrics'],
    queryFn: APIClient.getSystemMetrics,
  });

  // Fetch S3 metrics from backend
  const { data: s3MetricsData, isLoading: s3Loading } = useQuery<S3Metrics>({
    queryKey: ['s3Metrics'],
    queryFn: APIClient.getS3Metrics,
  });

  const isLoading = storageLoading || systemLoading || s3Loading;

  // Parse backend metrics
  const storageMetrics = storageMetricsData || {} as StorageMetrics;
  const systemMetrics = systemMetricsData || {} as SystemMetrics;
  const s3Metrics = s3MetricsData || {} as S3Metrics;

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
      uptime: formatUptime(systemMetrics.uptime || 0),
      cpu: systemMetrics.cpuUsagePercent || 0,
      memory: systemMetrics.memoryUsagePercent || 0,
      disk: systemMetrics.diskUsagePercent || 0,
      network: 0
    },
    storage: {
      totalSize: (storageMetrics.totalBuckets || 0) * 100 * 1024 * 1024, // Estimate
      usedSize: storageMetrics.totalSize || 0,
      objects: storageMetrics.totalObjects || 0,
      buckets: storageMetrics.totalBuckets || 0,
      averageObjectSize: storageMetrics.averageObjectSize || 0
    },
    requests: {
      totalRequests: s3Metrics.totalRequests || 0,
      totalErrors: s3Metrics.totalErrors || 0,
      avgLatency: s3Metrics.avgLatency || 0,
      requestsPerSec: s3Metrics.requestsPerSec || 0
    },
    performance: {
      uptime: systemMetrics.uptime || 0,
      goRoutines: systemMetrics.goroutines || 0,
      heapAllocMB: systemMetrics.heapAllocBytes ? (systemMetrics.heapAllocBytes / (1024 * 1024)) : 0,
      gcRuns: systemMetrics.gcRuns || 0
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

  const tabs = [
    { id: 'system', label: 'System Health', icon: Activity },
    { id: 'storage', label: 'Storage', icon: HardDrive },
    { id: 'requests', label: 'Requests', icon: Globe },
    { id: 'performance', label: 'Performance', icon: Zap },
  ];

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Metrics Overview</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Monitor system performance and usage statistics
          </p>
        </div>
        <div className="flex items-center space-x-2 text-sm text-gray-500 dark:text-gray-400">
          <div className="w-2 h-2 bg-green-500 rounded-full"></div>
          <span>Live Data</span>
        </div>
      </div>

      {/* Tabs */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
        <div className="p-6">
          <div className="flex space-x-1 border-b border-gray-200 dark:border-gray-700 mb-6">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              return (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`flex items-center space-x-2 px-4 py-2 font-medium text-sm border-b-2 transition-colors ${
                    activeTab === tab.id
                      ? 'border-brand-600 text-brand-600 dark:text-brand-400'
                      : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'
                  }`}
                >
                  <Icon className="h-4 w-4" />
                  <span>{tab.label}</span>
                </button>
              );
            })}
          </div>

          {/* System Health Tab */}
          {activeTab === 'system' && (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
              {/* CPU Usage */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">CPU Usage</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.system.cpu.toFixed(2)}%</h3>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
                    <Activity className="h-7 w-7 text-blue-600 dark:text-blue-400" />
                  </div>
                </div>
                <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                  <div
                    className="bg-blue-600 dark:bg-blue-400 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${displayMetrics.system.cpu}%` }}
                  ></div>
                </div>
              </div>

              {/* Memory Usage */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Memory Usage</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.system.memory.toFixed(2)}%</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      {formatBytes(systemMetrics.memoryUsedBytes || 0)} / {formatBytes(systemMetrics.memoryTotalBytes || 0)}
                    </p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
                    <BarChart3 className="h-7 w-7 text-green-600 dark:text-green-400" />
                  </div>
                </div>
                <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                  <div
                    className="bg-green-600 dark:bg-green-400 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${displayMetrics.system.memory}%` }}
                  ></div>
                </div>
              </div>

              {/* Disk Usage */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Disk Usage</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.system.disk.toFixed(2)}%</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      {formatBytes(systemMetrics.diskUsedBytes || 0)} / {formatBytes(systemMetrics.diskTotalBytes || 0)}
                    </p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-yellow-50 dark:bg-yellow-900/30">
                    <HardDrive className="h-7 w-7 text-yellow-600 dark:text-yellow-400" />
                  </div>
                </div>
                <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                  <div
                    className="bg-yellow-600 dark:bg-yellow-400 h-2 rounded-full transition-all duration-300"
                    style={{ width: `${displayMetrics.system.disk}%` }}
                  ></div>
                </div>
              </div>

              {/* Uptime */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Uptime</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.system.uptime}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">System uptime</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
                    <Clock className="h-7 w-7 text-purple-600 dark:text-purple-400" />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Storage Tab */}
          {activeTab === 'storage' && (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
              {/* Total Storage */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Storage</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatBytes(displayMetrics.storage.usedSize)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Storage in use</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
                    <HardDrive className="h-7 w-7 text-brand-600 dark:text-brand-400" />
                  </div>
                </div>
              </div>

              {/* Total Objects */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Objects</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatNumber(displayMetrics.storage.objects)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Stored objects</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
                    <Database className="h-7 w-7 text-blue-600 dark:text-blue-400" />
                  </div>
                </div>
              </div>

              {/* Total Buckets */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Buckets</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.storage.buckets}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Total buckets</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
                    <Database className="h-7 w-7 text-green-600 dark:text-green-400" />
                  </div>
                </div>
              </div>

              {/* Avg Object Size */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Avg Object Size</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatBytes(displayMetrics.storage.averageObjectSize)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Per object</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
                    <BarChart3 className="h-7 w-7 text-purple-600 dark:text-purple-400" />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Requests Tab */}
          {activeTab === 'requests' && (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
              {/* Total Requests */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Requests</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatNumber(displayMetrics.requests.totalRequests)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Since startup</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
                    <Globe className="h-7 w-7 text-blue-600 dark:text-blue-400" />
                  </div>
                </div>
              </div>

              {/* Total Errors */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Errors</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatNumber(displayMetrics.requests.totalErrors)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Failed requests</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-red-50 dark:bg-red-900/30">
                    <BarChart3 className="h-7 w-7 text-red-600 dark:text-red-400" />
                  </div>
                </div>
              </div>

              {/* Avg Latency */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Avg Latency</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.requests.avgLatency.toFixed(1)}ms</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Average response time</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-yellow-50 dark:bg-yellow-900/30">
                    <Zap className="h-7 w-7 text-yellow-600 dark:text-yellow-400" />
                  </div>
                </div>
              </div>

              {/* Requests/sec */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Requests/sec</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.requests.requestsPerSec.toFixed(2)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Current throughput</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
                    <Activity className="h-7 w-7 text-green-600 dark:text-green-400" />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Performance Tab */}
          {activeTab === 'performance' && (
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
              {/* Goroutines */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Goroutines</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatNumber(displayMetrics.performance.goRoutines)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Active goroutines</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
                    <Activity className="h-7 w-7 text-brand-600 dark:text-brand-400" />
                  </div>
                </div>
              </div>

              {/* Heap Memory */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Heap Memory</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{displayMetrics.performance.heapAllocMB.toFixed(1)} MB</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Allocated heap</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
                    <BarChart3 className="h-7 w-7 text-purple-600 dark:text-purple-400" />
                  </div>
                </div>
              </div>

              {/* GC Runs */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">GC Runs</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{formatNumber(displayMetrics.performance.gcRuns)}</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Garbage collections</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-orange-50 dark:bg-orange-900/30">
                    <TrendingUp className="h-7 w-7 text-orange-600 dark:text-orange-400" />
                  </div>
                </div>
              </div>

              {/* Success Rate */}
              <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Success Rate</p>
                    <h3 className="text-3xl font-bold text-gray-900 dark:text-white">
                      {displayMetrics.requests.totalRequests > 0
                        ? (((displayMetrics.requests.totalRequests - displayMetrics.requests.totalErrors) / displayMetrics.requests.totalRequests) * 100).toFixed(2)
                        : (100).toFixed(2)}%
                    </h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Request success rate</p>
                  </div>
                  <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
                    <TrendingDown className="h-7 w-7 text-green-600 dark:text-green-400" />
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
