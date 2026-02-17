import React, { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import {
  BarChart3,
  Activity,
  HardDrive,
  Clock,
  TrendingUp,
  Zap,
  Box,
  Globe,
  AlertCircle,
  Cpu,
  MemoryStick,
  Server,
  FolderOpen,
  CheckCircle,
  XCircle,
  ArrowUp,
  ArrowDown,
  Gauge,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { StorageMetrics, SystemMetrics, S3Metrics, LatenciesResponse, ThroughputResponse } from '@/types';
import { MetricLineChart, TimeRangeSelector, TIME_RANGES, type TimeRange } from '@/components/charts';

export default function MetricsPage() {
  const navigate = useNavigate();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();
  const [activeTab, setActiveTab] = React.useState<'overview' | 'system' | 'storage' | 'api' | 'performance'>('overview');
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
    refetchOnWindowFocus: false,
  });

  // Fetch current system metrics
  const { data: systemMetricsData, isLoading: systemLoading } = useQuery<SystemMetrics>({
    queryKey: ['systemMetrics'],
    queryFn: APIClient.getSystemMetrics,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false,
  });

  // Fetch current S3 metrics
  const { data: s3MetricsData, isLoading: s3Loading } = useQuery<S3Metrics>({
    queryKey: ['s3Metrics'],
    queryFn: APIClient.getS3Metrics,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false,
  });

  // Fetch performance latency metrics
  const { data: performanceLatencies, isLoading: latenciesLoading } = useQuery<LatenciesResponse>({
    queryKey: ['performanceLatencies'],
    queryFn: APIClient.getPerformanceLatencies,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false,
  });

  // Fetch performance throughput metrics
  const { data: performanceThroughput, isLoading: throughputLoading } = useQuery<ThroughputResponse>({
    queryKey: ['performanceThroughput'],
    queryFn: APIClient.getPerformanceThroughput,
    refetchInterval: 30000,
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false,
  });

  // Fetch historical metrics based on active tab and time range
  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['historicalMetrics', activeTab, timeRange.label],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000);
      const start = end - (timeRange.hours * 3600);

      const metricTypeMap: Record<string, string> = {
        overview: 'system',
        system: 'system',
        storage: 'storage',
        api: 's3',
        performance: 'system',
      };

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
    { id: 'overview', label: 'Overview', icon: Gauge },
    { id: 'system', label: 'System', icon: Server },
    { id: 'storage', label: 'Storage', icon: Box },
    { id: 'api', label: 'API & Requests', icon: Globe },
    { id: 'performance', label: 'Performance', icon: Activity },
  ];

  const chartData = processHistoricalData();

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

          {/* OVERVIEW TAB */}
          {activeTab === 'overview' && (
            <div className="space-y-6">
              {/* Quick Stats Grid - General Summary */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard
                  title="System Health"
                  value={`${(systemMetrics.cpuUsagePercent || 0).toFixed(0)}% CPU`}
                  icon={Server}
                  description={`${(systemMetrics.memoryUsagePercent || 0).toFixed(0)}% Memory`}
                  color="blue-light"
                />
                <MetricCard
                  title="Total Storage"
                  value={formatBytes(storageMetrics.totalSize || 0)}
                  icon={Box}
                  description={`${formatNumber(storageMetrics.totalBuckets || 0)} buckets`}
                  color="brand"
                />
                <MetricCard
                  title="Total Requests"
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Globe}
                  description={`${formatNumber(s3Metrics.totalErrors || 0)} errors`}
                  color="warning"
                />
                <MetricCard
                  title="Uptime"
                  value={formatUptime(systemMetrics.uptime || 0)}
                  icon={Clock}
                  description="System running"
                  color="success"
                />
              </div>

              {/* Charts - Combined overview */}
              {historyLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : chartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title="System Resources Over Time"
                    dataKeys={[
                      { key: 'cpuUsagePercent', name: 'CPU %', color: '#3b82f6' },
                      { key: 'memoryUsagePercent', name: 'Memory %', color: '#10b981' },
                      { key: 'diskUsagePercent', name: 'Disk %', color: '#f59e0b' },
                    ]}
                    height={300}
                    formatYAxis={(value) => `${value.toFixed(0)}%`}
                    formatTooltip={(value) => `${value.toFixed(2)}%`}
                    timeRange={historyData?.requestedRange}
                  />
                  <MetricLineChart
                    data={chartData}
                    title="Storage Growth Over Time"
                    dataKeys={[
                      { key: 'totalObjects', name: 'Objects', color: '#8b5cf6' },
                    ]}
                    height={300}
                    formatYAxis={(value) => formatNumber(value)}
                    formatTooltip={(value) => `${formatNumber(value)} objects`}
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

          {/* SYSTEM TAB */}
          {activeTab === 'system' && (
            <div className="space-y-6">
              {/* System Resources */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard
                  title="CPU Usage"
                  value={`${(systemMetrics.cpuUsagePercent || 0).toFixed(1)}%`}
                  icon={Cpu}
                  description={
                    systemMetrics.cpuCores
                      ? `${systemMetrics.cpuCores} cores @ ${(systemMetrics.cpuFrequencyMhz || 0) > 1000 ? ((systemMetrics.cpuFrequencyMhz || 0) / 1000).toFixed(2) + ' GHz' : (systemMetrics.cpuFrequencyMhz || 0).toFixed(0) + ' MHz'}`
                      : 'CPU info unavailable'
                  }
                  color="blue-light"
                />
                <MetricCard
                  title="Memory"
                  value={`${(systemMetrics.memoryUsagePercent || 0).toFixed(1)}%`}
                  icon={MemoryStick}
                  description={`${formatBytes(systemMetrics.memoryUsedBytes || 0)} / ${formatBytes(systemMetrics.memoryTotalBytes || 0)}`}
                  color="success"
                />
                <MetricCard
                  title="Disk Usage"
                  value={`${(systemMetrics.diskUsagePercent || 0).toFixed(1)}%`}
                  icon={HardDrive}
                  description={`${formatBytes(systemMetrics.diskUsedBytes || 0)} / ${formatBytes(systemMetrics.diskTotalBytes || 0)}`}
                  color="warning"
                />
                <MetricCard
                  title="Uptime"
                  value={formatUptime(systemMetrics.uptime || 0)}
                  icon={Clock}
                  description="System running"
                  color="brand"
                />
              </div>

              {/* Runtime Metrics */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard
                  title="Goroutines"
                  value={formatNumber(systemMetrics.goroutines || 0)}
                  icon={Activity}
                  description="Active"
                  color="brand"
                />
                <MetricCard
                  title="Heap Memory"
                  value={`${((systemMetrics.heapAllocBytes || 0) / (1024 * 1024)).toFixed(1)} MB`}
                  icon={BarChart3}
                  description="Allocated"
                  color="blue-light"
                />
                <MetricCard
                  title="GC Runs"
                  value={formatNumber(systemMetrics.gcRuns || 0)}
                  icon={TrendingUp}
                  description="Collections"
                  color="warning"
                />
                <MetricCard
                  title="Network I/O"
                  value={formatBytes(systemMetrics.networkBytesOut || 0)}
                  icon={ArrowUp}
                  description={`↓ ${formatBytes(systemMetrics.networkBytesIn || 0)}`}
                  color="success"
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
                <MetricCard
                  title="Total Storage"
                  value={formatBytes(storageMetrics.totalSize || 0)}
                  icon={HardDrive}
                  description="Used space"
                  color="brand"
                />
                <MetricCard
                  title="Total Objects"
                  value={formatNumber(storageMetrics.totalObjects || 0)}
                  icon={Box}
                  description="Stored files"
                  color="blue-light"
                />
                <MetricCard
                  title="Buckets"
                  value={formatNumber(storageMetrics.totalBuckets || 0)}
                  icon={Box}
                  description="Total buckets"
                  color="success"
                />
                <MetricCard
                  title="Avg Object Size"
                  value={formatBytes(storageMetrics.averageObjectSize || 0)}
                  icon={BarChart3}
                  description="Per object"
                  color="warning"
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

          {/* API & REQUESTS TAB */}
          {activeTab === 'api' && (
            <div className="space-y-6">
              {/* API Metrics */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard
                  title="Total Requests"
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Globe}
                  description="Since startup"
                  color="blue-light"
                />
                <MetricCard
                  title="Total Errors"
                  value={formatNumber(s3Metrics.totalErrors || 0)}
                  icon={XCircle}
                  description="Failed requests"
                  color="error"
                />
                <MetricCard
                  title="Success Rate"
                  value={`${
                    s3Metrics.totalRequests > 0
                      ? (((s3Metrics.totalRequests - s3Metrics.totalErrors) / s3Metrics.totalRequests) * 100).toFixed(2)
                      : 100
                  }%`}
                  icon={CheckCircle}
                  description="Request success"
                  color="success"
                />
                <MetricCard
                  title="Avg Latency"
                  value={`${(s3Metrics.avgLatency || 0).toFixed(1)}ms`}
                  icon={Clock}
                  description="Average response time"
                  color="warning"
                />
              </div>

              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard
                  title="Requests/Sec"
                  value={(s3Metrics.requestsPerSec || 0).toFixed(2)}
                  icon={TrendingUp}
                  description="Current rate"
                  color="brand"
                />
                <MetricCard
                  title="Error Rate"
                  value={`${
                    s3Metrics.totalRequests > 0
                      ? ((s3Metrics.totalErrors / s3Metrics.totalRequests) * 100).toFixed(2)
                      : 0
                  }%`}
                  icon={AlertCircle}
                  description="Percentage of errors"
                  color="error"
                />
                <MetricCard
                  title="Total Operations"
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Activity}
                  description="All API calls"
                  color="blue-light"
                />
                <MetricCard
                  title="Response Time"
                  value={`${(s3Metrics.avgLatency || 0).toFixed(0)}ms`}
                  icon={Zap}
                  description="Avg latency"
                  color="success"
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
                    height={300}
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
                    height={300}
                    formatYAxis={(value) => `${value.toFixed(0)}ms`}
                    formatTooltip={(value) => `${value.toFixed(2)}ms`}
                    timeRange={historyData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">No API history available yet</p>
                  <p className="text-sm">Request metrics will appear after collecting data</p>
                </div>
              )}
            </div>
          )}

          {/* PERFORMANCE TAB */}
          {activeTab === 'performance' && (
            <div className="space-y-6">
              {latenciesLoading || throughputLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : performanceLatencies && performanceThroughput ? (
                <>
                  {/* Section 1: General Overview */}
                  <div>
                    <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 uppercase tracking-wide">
                      Overview
                    </h3>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                      <MetricCard
                        title="Total Requests"
                        value={formatNumber(s3Metrics.totalRequests || 0)}
                        icon={Globe}
                        description="Since startup"
                        color="blue-light"
                      />
                      <MetricCard
                        title="Total Errors"
                        value={formatNumber(s3Metrics.totalErrors || 0)}
                        icon={AlertCircle}
                        description="Failed requests"
                        color="error"
                      />
                      <MetricCard
                        title="Success Rate"
                        value={`${
                          s3Metrics.totalRequests > 0
                            ? (((s3Metrics.totalRequests - s3Metrics.totalErrors) / s3Metrics.totalRequests) * 100).toFixed(2)
                            : 100
                        }%`}
                        icon={Zap}
                        description="Request success"
                        color="success"
                      />
                      <MetricCard
                        title="Avg Latency"
                        value={`${(s3Metrics.avgLatency || 0).toFixed(1)}ms`}
                        icon={Activity}
                        description="Overall average"
                        color="warning"
                      />
                    </div>
                  </div>

                  {/* Section 2: Real-time Throughput */}
                  <div>
                    <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 uppercase tracking-wide">
                      Real-time Throughput
                    </h3>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                      <MetricCard
                        title="Requests/Sec"
                        value={performanceThroughput.current.requests_per_second.toFixed(2)}
                        icon={Zap}
                        description="Current rate"
                        color="brand"
                      />
                      <MetricCard
                        title="Bytes/Sec"
                        value={formatBytes(performanceThroughput.current.bytes_per_second)}
                        icon={HardDrive}
                        description="Data transfer"
                        color="blue-light"
                      />
                      <MetricCard
                        title="Objects/Sec"
                        value={performanceThroughput.current.objects_per_second.toFixed(2)}
                        icon={Box}
                        description="Object ops"
                        color="success"
                      />
                      <MetricCard
                        title="Total Operations"
                        value={formatNumber(
                          Object.values(performanceLatencies.latencies).reduce(
                            (sum, stat) => sum + stat.count,
                            0
                          )
                        )}
                        icon={Activity}
                        description="Since last reset"
                        color="warning"
                      />
                    </div>
                  </div>

                  {/* Section 3: Operation Latencies */}
                  <div>
                    <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 uppercase tracking-wide">
                      Operation Latencies (p50 / p95 / p99)
                    </h3>
                    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
                      {/* Always show these 4 main operations even if no data yet */}
                      {['PutObject', 'GetObject', 'DeleteObject', 'ListObjects'].map((operation) => {
                        const stats = performanceLatencies.latencies[operation] || {
                          operation,
                          count: 0,
                          p50_ms: 0,
                          p95_ms: 0,
                          p99_ms: 0,
                          mean_ms: 0,
                          min_ms: 0,
                          max_ms: 0,
                          success_rate: 100.0,
                          error_count: 0
                        };

                        return (
                          <div
                            key={operation}
                            className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4"
                          >
                            <div className="flex items-center justify-between mb-4">
                              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                                {operation}
                              </h3>
                              {stats.count > 0 ? (
                                <span
                                  className={`px-2 py-1 text-xs font-medium rounded ${
                                    stats.success_rate >= 99
                                      ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'
                                      : stats.success_rate >= 95
                                      ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-400'
                                      : 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400'
                                  }`}
                                >
                                  {stats.success_rate.toFixed(2)}% success
                                </span>
                              ) : (
                                <span className="px-2 py-1 text-xs font-medium rounded bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400">
                                  No data
                                </span>
                              )}
                            </div>

                            <div className="space-y-2">
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">Count</span>
                                <span className="text-sm font-medium text-gray-900 dark:text-white">
                                  {formatNumber(stats.count)}
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">p50</span>
                                <span className="text-sm font-medium text-gray-900 dark:text-white">
                                  {stats.p50_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">p95</span>
                                <span className="text-sm font-bold text-brand-600 dark:text-brand-400">
                                  {stats.p95_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">p99</span>
                                <span className="text-sm font-medium text-orange-600 dark:text-orange-400">
                                  {stats.p99_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">Mean</span>
                                <span className="text-sm font-medium text-gray-900 dark:text-white">
                                  {stats.mean_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                <span className="text-sm text-gray-600 dark:text-gray-400">Min/Max</span>
                                <span className="text-sm font-medium text-gray-900 dark:text-white">
                                  {stats.min_ms.toFixed(2)} / {stats.max_ms.toFixed(2)} ms
                                </span>
                              </div>
                              {stats.error_count > 0 && (
                                <div className="flex justify-between items-center py-1 border-b border-gray-100 dark:border-gray-700">
                                  <span className="text-sm text-red-600 dark:text-red-400">Errors</span>
                                  <span className="text-sm font-medium text-red-600 dark:text-red-400">
                                    {formatNumber(stats.error_count)}
                                  </span>
                                </div>
                              )}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  {/* Section 4: Historical Trends */}
                  {historyLoading ? (
                    <div className="flex items-center justify-center h-48">
                      <Loading size="md" />
                    </div>
                  ) : chartData.length > 0 ? (
                    <div>
                      <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 uppercase tracking-wide">
                        Historical Trends
                      </h3>
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
                    </div>
                  ) : null}

                  {Object.keys(performanceLatencies.latencies).length === 0 && (
                    <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                      <AlertCircle className="h-12 w-12 mb-4" />
                      <p className="text-lg font-medium">No performance data collected yet</p>
                      <p className="text-sm">S3 operation metrics will appear after requests are made</p>
                    </div>
                  )}
                </>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-gray-500 dark:text-gray-400">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">Performance collector not available</p>
                  <p className="text-sm">Ensure the server is running with performance metrics enabled</p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
