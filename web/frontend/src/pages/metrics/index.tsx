import React, { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import {
  BarChart3,
  Activity,
  HardDrive,
  Clock,
  TrendingUp,
  Zap,
  Database,
  Globe,
  AlertCircle
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { StorageMetrics, SystemMetrics, S3Metrics } from '@/types';
import { MetricLineChart, MetricPieChart, TimeRangeSelector, TIME_RANGES, type TimeRange } from '@/components/charts';

export default function MetricsPage() {
  const navigate = useNavigate();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();
  const [activeTab, setActiveTab] = React.useState<'system' | 'storage' | 'requests' | 'performance'>('system');
  const [timeRange, setTimeRange] = React.useState<TimeRange>(TIME_RANGES[0]); // Default: Real-time (5 min)

  // Only global admins can access metrics
  useEffect(() => {
    if (currentUser && !isGlobalAdmin) {
      navigate('/');
    }
  }, [currentUser, isGlobalAdmin, navigate]);

  // Fetch current storage metrics
  const { data: storageMetricsData, isLoading: storageLoading } = useQuery<StorageMetrics>({
    queryKey: ['storageMetrics'],
    queryFn: APIClient.getStorageMetrics,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Fetch current system metrics
  const { data: systemMetricsData, isLoading: systemLoading } = useQuery<SystemMetrics>({
    queryKey: ['systemMetrics'],
    queryFn: APIClient.getSystemMetrics,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Fetch current S3 metrics
  const { data: s3MetricsData, isLoading: s3Loading } = useQuery<S3Metrics>({
    queryKey: ['s3Metrics'],
    queryFn: APIClient.getS3Metrics,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    retry: false,
    refetchOnWindowFocus: false,
  });

  // Fetch historical metrics based on active tab and time range
  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['historicalMetrics', activeTab, timeRange.label],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000);
      const start = end - (timeRange.hours * 3600);

      const metricTypeMap: Record<string, string> = {
        system: 'system',
        storage: 'storage',
        requests: 's3',
        performance: 'system',
      };

      console.log(`Fetching metrics: type=${metricTypeMap[activeTab]}, start=${new Date(start * 1000).toISOString()}, end=${new Date(end * 1000).toISOString()}, range=${timeRange.label}`);

      const result = await APIClient.getHistoricalMetrics({
        type: metricTypeMap[activeTab],
        start,
        end,
      });

      // Store time range for gap filling
      return { ...result, requestedRange: { start, end } };
    },
    // Adaptive refetch based on time range - longer periods need less frequent updates
    refetchInterval:
      timeRange.hours <= 1 ? 10000 :      // ≤1h: every 10s (real-time)
      timeRange.hours <= 6 ? 30000 :      // ≤6h: every 30s
      timeRange.hours <= 24 ? 60000 :     // ≤24h: every 1min
      timeRange.hours <= 168 ? 300000 :   // ≤7d: every 5min
      timeRange.hours <= 720 ? 600000 :   // ≤30d: every 10min
      1800000,                             // >30d (year): every 30min
    staleTime: 5000, // Consider data fresh for 5 seconds
    enabled: isGlobalAdmin,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const isLoading = storageLoading || systemLoading || s3Loading;

  const storageMetrics = storageMetricsData || {} as StorageMetrics;
  const systemMetrics = systemMetricsData || {} as SystemMetrics;
  const s3Metrics = s3MetricsData || {} as S3Metrics;

  const formatUptime = (seconds: number) => {
    if (!seconds) return 'N/A';
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  };

  const processHistoricalData = () => {
    if (!historyData || !historyData.snapshots) return [];
    const processed = historyData.snapshots.map((snapshot: any) => {
      // Convert timestamp to Unix timestamp (seconds)
      let timestamp: number;
      if (typeof snapshot.timestamp === 'string') {
        // Parse ISO string or datetime string
        timestamp = Math.floor(new Date(snapshot.timestamp).getTime() / 1000);
      } else if (typeof snapshot.timestamp === 'number') {
        // Already a timestamp
        timestamp = snapshot.timestamp;
      } else {
        timestamp = 0;
      }

      return {
        timestamp,
        cpuUsagePercent: snapshot.data.cpuUsagePercent || 0,
        memoryUsagePercent: snapshot.data.memoryUsagePercent || 0,
        diskUsagePercent: snapshot.data.diskUsagePercent || 0,
        totalRequests: snapshot.data.totalRequests || 0,
        totalErrors: snapshot.data.totalErrors || 0,
        avgLatency: snapshot.data.avgLatency || 0,
        requestsPerSec: snapshot.data.requestsPerSec || 0,
        goroutines: snapshot.data.goroutines || 0,
        heapAllocMB: snapshot.data.heapAllocBytes ? (snapshot.data.heapAllocBytes / (1024 * 1024)) : 0,
        // Storage metrics
        totalBuckets: snapshot.data.totalBuckets || 0,
        totalObjects: snapshot.data.totalObjects || 0,
        totalSize: snapshot.data.totalSize || 0,
        totalSizeMB: snapshot.data.totalSize ? (snapshot.data.totalSize / (1024 * 1024)) : 0,
      };
    });

    // Add current metrics as the most recent data point (only if we have historical data)
    if (processed.length > 0 && (systemMetrics || storageMetrics || s3Metrics)) {
      const currentTimestamp = Math.floor(Date.now() / 1000);
      const lastTimestamp = processed[processed.length - 1].timestamp;
      
      // Only add if current time is newer than last snapshot (avoid duplicates)
      if (currentTimestamp > lastTimestamp + 30) { // 30 seconds threshold
        processed.push({
          timestamp: currentTimestamp,
          cpuUsagePercent: systemMetrics?.cpuUsagePercent || 0,
          memoryUsagePercent: systemMetrics?.memoryUsagePercent || 0,
          diskUsagePercent: systemMetrics?.diskUsagePercent || 0,
          totalRequests: s3Metrics?.totalRequests || 0,
          totalErrors: s3Metrics?.totalErrors || 0,
          avgLatency: s3Metrics?.avgLatency || 0,
          requestsPerSec: s3Metrics?.requestsPerSec || 0,
          goroutines: systemMetrics?.goroutines || 0,
          heapAllocMB: systemMetrics?.heapAllocBytes ? (systemMetrics.heapAllocBytes / (1024 * 1024)) : 0,
          totalBuckets: storageMetrics?.totalBuckets || 0,
          totalObjects: storageMetrics?.totalObjects || 0,
          totalSize: storageMetrics?.totalSize || 0,
          totalSizeMB: storageMetrics?.totalSize ? (storageMetrics.totalSize / (1024 * 1024)) : 0,
        });
      }
    }

    return processed;
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

  const chartData = processHistoricalData();

  // Stats Card Component
  const StatCard = ({ icon: Icon, label, value, subtext, color }: any) => (
    <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
      <div className="flex items-start justify-between">
        <div className="flex-1">
          <div className="flex items-center space-x-2 mb-1">
            <Icon className={`h-4 w-4 ${color}`} />
            <p className="text-xs font-medium text-gray-600 dark:text-gray-400">{label}</p>
          </div>
          <p className="text-2xl font-bold text-gray-900 dark:text-white">{value}</p>
          {subtext && <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">{subtext}</p>}
        </div>
      </div>
    </div>
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Metrics Dashboard</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Real-time system monitoring and historical trends
          </p>
        </div>
        <div className="flex items-center space-x-4">
          <TimeRangeSelector selected={timeRange} onChange={setTimeRange} />
          <div className="flex items-center space-x-2 text-sm text-gray-500 dark:text-gray-400">
            <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
            <span>Live</span>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
        <div className="p-6">
          {/* Tabs Navigation */}
          <div className="flex space-x-1 bg-gray-100 dark:bg-gray-900 rounded-lg p-1 mb-6">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              return (
                <button
                  key={tab.id}
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`flex-1 flex items-center justify-center space-x-2 px-4 py-3 font-medium text-sm rounded-md transition-all duration-200 ${
                    activeTab === tab.id
                      ? 'bg-white dark:bg-gray-800 text-brand-600 dark:text-brand-400 shadow-sm'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200'
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
            <div className="space-y-6">
              {/* Quick Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <StatCard
                  icon={Activity}
                  label="CPU Usage"
                  value={`${(systemMetrics.cpuUsagePercent || 0).toFixed(1)}%`}
                  subtext={
                    systemMetrics.cpuCores
                      ? `${systemMetrics.cpuCores} cores @ ${(systemMetrics.cpuFrequencyMhz || 0) > 1000 ? ((systemMetrics.cpuFrequencyMhz || 0) / 1000).toFixed(2) + ' GHz' : (systemMetrics.cpuFrequencyMhz || 0).toFixed(0) + ' MHz'}`
                      : 'CPU info unavailable'
                  }
                  color="text-blue-600 dark:text-blue-400"
                />
                <StatCard
                  icon={BarChart3}
                  label="Memory"
                  value={`${(systemMetrics.memoryUsagePercent || 0).toFixed(1)}%`}
                  subtext={`${formatBytes(systemMetrics.memoryUsedBytes || 0)} / ${formatBytes(systemMetrics.memoryTotalBytes || 0)}`}
                  color="text-green-600 dark:text-green-400"
                />
                <StatCard
                  icon={HardDrive}
                  label="Disk Usage"
                  value={`${(systemMetrics.diskUsagePercent || 0).toFixed(1)}%`}
                  subtext={`${formatBytes(systemMetrics.diskUsedBytes || 0)} / ${formatBytes(systemMetrics.diskTotalBytes || 0)}`}
                  color="text-yellow-600 dark:text-yellow-400"
                />
                <StatCard
                  icon={Clock}
                  label="Uptime"
                  value={formatUptime(systemMetrics.uptime || 0)}
                  subtext="System running"
                  color="text-purple-600 dark:text-purple-400"
                />
              </div>

              {/* Charts */}
              {historyLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : chartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title="CPU & Memory Usage Over Time"
                    dataKeys={[
                      { key: 'cpuUsagePercent', name: 'CPU %', color: '#3b82f6' },
                      { key: 'memoryUsagePercent', name: 'Memory %', color: '#10b981' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)}%`}
                    formatTooltip={(value) => `${value.toFixed(2)}%`}
                    timeRange={historyData?.requestedRange}
                  />
                  <MetricLineChart
                    data={chartData}
                    title="Disk Usage Over Time"
                    dataKeys={[
                      { key: 'diskUsagePercent', name: 'Disk %', color: '#f59e0b' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)}%`}
                    formatTooltip={(value) => `${value.toFixed(2)}%`}
                    timeRange={historyData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">No historical data available yet</p>
                  <p className="text-sm">Metrics will appear after the system collects data</p>
                </div>
              )}
            </div>
          )}

          {/* Storage Tab */}
          {activeTab === 'storage' && (
            <div className="space-y-6">
              {/* Quick Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <StatCard
                  icon={HardDrive}
                  label="Total Storage"
                  value={formatBytes(storageMetrics.totalSize || 0)}
                  subtext="Used space"
                  color="text-brand-600 dark:text-brand-400"
                />
                <StatCard
                  icon={Database}
                  label="Total Objects"
                  value={formatNumber(storageMetrics.totalObjects || 0)}
                  subtext="Stored files"
                  color="text-blue-600 dark:text-blue-400"
                />
                <StatCard
                  icon={Database}
                  label="Buckets"
                  value={formatNumber(storageMetrics.totalBuckets || 0)}
                  subtext="Total buckets"
                  color="text-green-600 dark:text-green-400"
                />
                <StatCard
                  icon={BarChart3}
                  label="Avg Object Size"
                  value={formatBytes(storageMetrics.averageObjectSize || 0)}
                  subtext="Per object"
                  color="text-purple-600 dark:text-purple-400"
                />
              </div>

              {/* Charts */}
              {historyLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : chartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title="Objects Count Over Time"
                    dataKeys={[
                      { key: 'totalObjects', name: 'Objects', color: '#3b82f6' },
                    ]}
                    height={350}
                    formatYAxis={(value) => formatNumber(value)}
                    formatTooltip={(value) => formatNumber(value)}
                    timeRange={historyData?.requestedRange}
                  />
                  <MetricLineChart
                    data={chartData}
                    title="Storage Size Over Time"
                    dataKeys={[
                      { key: 'totalSizeMB', name: 'Size (MB)', color: '#10b981' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)} MB`}
                    formatTooltip={(value) => formatBytes(value * 1024 * 1024)}
                    timeRange={historyData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">No storage history available yet</p>
                  <p className="text-sm">Historical storage metrics will appear here</p>
                </div>
              )}
            </div>
          )}

          {/* Requests Tab */}
          {activeTab === 'requests' && (
            <div className="space-y-6">
              {/* Quick Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <StatCard
                  icon={Globe}
                  label="Total Requests"
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  subtext="Since startup"
                  color="text-blue-600 dark:text-blue-400"
                />
                <StatCard
                  icon={AlertCircle}
                  label="Total Errors"
                  value={formatNumber(s3Metrics.totalErrors || 0)}
                  subtext="Failed requests"
                  color="text-red-600 dark:text-red-400"
                />
                <StatCard
                  icon={Zap}
                  label="Avg Latency"
                  value={`${(s3Metrics.avgLatency || 0).toFixed(1)}ms`}
                  subtext="Response time"
                  color="text-yellow-600 dark:text-yellow-400"
                />
                <StatCard
                  icon={Activity}
                  label="Throughput"
                  value={`${(s3Metrics.requestsPerSec || 0).toFixed(2)}/s`}
                  subtext="Requests per second"
                  color="text-green-600 dark:text-green-400"
                />
              </div>

              {/* Charts */}
              {historyLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : chartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title="Request Throughput Over Time"
                    dataKeys={[
                      { key: 'requestsPerSec', name: 'Requests/sec', color: '#3b82f6' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(1)}/s`}
                    formatTooltip={(value) => `${value.toFixed(2)}/s`}
                    timeRange={historyData?.requestedRange}
                  />
                  <MetricLineChart
                    data={chartData}
                    title="Average Latency Over Time"
                    dataKeys={[
                      { key: 'avgLatency', name: 'Latency (ms)', color: '#f59e0b' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)}ms`}
                    formatTooltip={(value) => `${value.toFixed(2)}ms`}
                    timeRange={historyData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">No request data available yet</p>
                  <p className="text-sm">Historical request metrics will appear here</p>
                </div>
              )}
            </div>
          )}

          {/* Performance Tab */}
          {activeTab === 'performance' && (
            <div className="space-y-6">
              {/* Quick Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <StatCard
                  icon={Activity}
                  label="Goroutines"
                  value={formatNumber(systemMetrics.goroutines || 0)}
                  subtext="Active"
                  color="text-brand-600 dark:text-brand-400"
                />
                <StatCard
                  icon={BarChart3}
                  label="Heap Memory"
                  value={`${((systemMetrics.heapAllocBytes || 0) / (1024 * 1024)).toFixed(1)} MB`}
                  subtext="Allocated"
                  color="text-purple-600 dark:text-purple-400"
                />
                <StatCard
                  icon={TrendingUp}
                  label="GC Runs"
                  value={formatNumber(systemMetrics.gcRuns || 0)}
                  subtext="Collections"
                  color="text-orange-600 dark:text-orange-400"
                />
                <StatCard
                  icon={Zap}
                  label="Success Rate"
                  value={`${
                    s3Metrics.totalRequests > 0
                      ? (((s3Metrics.totalRequests - s3Metrics.totalErrors) / s3Metrics.totalRequests) * 100).toFixed(2)
                      : 100
                  }%`}
                  subtext="Request success"
                  color="text-green-600 dark:text-green-400"
                />
              </div>

              {/* Charts */}
              {historyLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : chartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title="Goroutines Over Time"
                    dataKeys={[
                      { key: 'goroutines', name: 'Goroutines', color: '#8b5cf6' },
                    ]}
                    height={350}
                    formatYAxis={(value) => formatNumber(value)}
                    formatTooltip={(value) => formatNumber(value)}
                    timeRange={historyData?.requestedRange}
                  />
                  <MetricLineChart
                    data={chartData}
                    title="Heap Memory Over Time"
                    dataKeys={[
                      { key: 'heapAllocMB', name: 'Heap (MB)', color: '#ec4899' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)} MB`}
                    formatTooltip={(value) => `${value.toFixed(2)} MB`}
                    timeRange={historyData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">No performance data available yet</p>
                  <p className="text-sm">Historical performance metrics will appear here</p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
