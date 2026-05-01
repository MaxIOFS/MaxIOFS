import React, { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
  CheckCircle,
  XCircle,
  ArrowUp,
  Gauge,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { StorageMetrics, SystemMetrics, S3Metrics, LatenciesResponse, ThroughputResponse } from '@/types';
import { MetricLineChart, TimeRangeSelector, TIME_RANGES, type TimeRange } from '@/components/charts';

export default function MetricsPage() {
  const { t } = useTranslation('metrics');
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

  const historyRefetchInterval =
    timeRange.hours <= 1 ? 10000 :
    timeRange.hours <= 6 ? 30000 :
    timeRange.hours <= 24 ? 60000 :
    timeRange.hours <= 168 ? 300000 :
    timeRange.hours <= 720 ? 600000 :
    1800000;

  // Fetch historical system/api metrics (for overview, system, api, performance tabs)
  const { data: historyData, isLoading: historyLoading } = useQuery({
    queryKey: ['historicalMetrics', activeTab, timeRange.label],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000);
      const start = end - (timeRange.hours * 3600);

      const metricTypeMap: Record<string, string> = {
        overview: 'system',
        system: 'system',
        api: 's3',
        performance: 'system',
      };

      const result = await APIClient.getHistoricalMetrics({
        type: metricTypeMap[activeTab] || 'system',
        start,
        end,
      });

      return { ...result, requestedRange: { start, end } };
    },
    refetchInterval: historyRefetchInterval,
    staleTime: 5000,
    enabled: isGlobalAdmin && activeTab !== 'storage',
    refetchOnWindowFocus: false,
  });

  // Fetch historical storage metrics separately (for overview and storage tabs)
  const { data: storageHistoryData, isLoading: storageHistoryLoading } = useQuery({
    queryKey: ['storageHistoricalMetrics', timeRange.label],
    queryFn: async () => {
      const end = Math.floor(Date.now() / 1000);
      const start = end - (timeRange.hours * 3600);
      const result = await APIClient.getHistoricalMetrics({ type: 'storage', start, end });
      return { ...result, requestedRange: { start, end } };
    },
    refetchInterval: historyRefetchInterval,
    staleTime: 5000,
    enabled: isGlobalAdmin && (activeTab === 'overview' || activeTab === 'storage'),
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

  const processHistoricalData = (source?: typeof historyData) => {
    if (!source || !source.snapshots) return [];
    const processed = source.snapshots.map((snapshot: any) => {
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

    if (processed.length > 0) {
      const currentTimestamp = Math.floor(Date.now() / 1000);
      const lastTimestamp = processed[processed.length - 1].timestamp;
      const secondsSinceLastSnapshot = currentTimestamp - lastTimestamp;
      // Only append a "current" point if the last snapshot is older than 30s.
      // If there's a recent snapshot it already represents the current state —
      // appending here with potentially stale query data causes false positives.
      if (secondsSinceLastSnapshot > 30) {
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
    { id: 'overview',    label: t('tabOverview'),    icon: Gauge },
    { id: 'system',      label: t('tabSystem'),      icon: Server },
    { id: 'storage',     label: t('tabStorage'),     icon: Box },
    { id: 'api',         label: t('tabApi'),         icon: Globe },
    { id: 'performance', label: t('tabPerformance'), icon: Activity },
  ];

  const chartData = processHistoricalData(historyData);
  const storageChartData = processHistoricalData(storageHistoryData);

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground">{t('metricsDashboard')}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t('realTimeMonitoring')}
          </p>
        </div>
        <div className="flex items-center space-x-4">
          <TimeRangeSelector selected={timeRange} onChange={setTimeRange} />
          <div className="flex items-center space-x-2 text-sm text-muted-foreground">
            <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
            <span>{t('live')}</span>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="bg-card rounded-lg border border-border shadow-sm">
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
                      ? 'bg-card text-brand-600 dark:text-brand-400 shadow-sm'
                      : 'text-muted-foreground hover:text-foreground'
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
                <MetricCard compact
                  title={t('systemHealth')}
                  value={`${(systemMetrics.cpuUsagePercent || 0).toFixed(0)}% CPU`}
                  icon={Server}
                  description={`${(systemMetrics.memoryUsagePercent || 0).toFixed(0)}% Memory`}
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('totalStorage')}
                  value={formatBytes(storageMetrics.totalSize || 0)}
                  icon={Box}
                  description={`${formatNumber(storageMetrics.totalBuckets || 0)} ${t('bucketsCount')}`}
                  color="brand"
                />
                <MetricCard compact
                  title={t('totalRequests')}
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Globe}
                  description={`${formatNumber(s3Metrics.totalErrors || 0)} ${t('errors')}`}
                  color="warning"
                />
                <MetricCard compact
                  title={t('uptime')}
                  value={formatUptime(systemMetrics.uptime || 0)}
                  icon={Clock}
                  description={t('systemRunning')}
                  color="success"
                />
              </div>

              {/* Charts - Combined overview */}
              {(historyLoading || storageHistoryLoading) ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : (chartData.length > 0 || storageChartData.length > 0) ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={chartData}
                    title={t('systemResourcesOverTime')}
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
                    data={storageChartData}
                    title={t('storageGrowthOverTime')}
                    dataKeys={[
                      { key: 'totalObjects', name: 'Objects', color: '#8b5cf6' },
                    ]}
                    height={300}
                    formatYAxis={(value) => formatNumber(value)}
                    formatTooltip={(value) => `${formatNumber(value)} objects`}
                    timeRange={storageHistoryData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">{t('noHistoricalData')}</p>
                  <p className="text-sm">{t('metricsWillAppear')}</p>
                </div>
              )}
            </div>
          )}

          {/* SYSTEM TAB */}
          {activeTab === 'system' && (
            <div className="space-y-6">
              {/* System Resources */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard compact
                  title={t('cpuUsage')}
                  value={`${(systemMetrics.cpuUsagePercent || 0).toFixed(1)}%`}
                  icon={Cpu}
                  description={
                    systemMetrics.cpuCores
                      ? `${systemMetrics.cpuCores} cores @ ${(systemMetrics.cpuFrequencyMhz || 0) > 1000 ? ((systemMetrics.cpuFrequencyMhz || 0) / 1000).toFixed(2) + ' GHz' : (systemMetrics.cpuFrequencyMhz || 0).toFixed(0) + ' MHz'}`
                      : t('cpuInfoUnavailable')
                  }
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('memoryUsage')}
                  value={`${(systemMetrics.memoryUsagePercent || 0).toFixed(1)}%`}
                  icon={MemoryStick}
                  description={`${formatBytes(systemMetrics.memoryUsedBytes || 0)} / ${formatBytes(systemMetrics.memoryTotalBytes || 0)}`}
                  color="success"
                />
                <MetricCard compact
                  title={t('diskUsage')}
                  value={`${(systemMetrics.diskUsagePercent || 0).toFixed(1)}%`}
                  icon={HardDrive}
                  description={`${formatBytes(systemMetrics.diskUsedBytes || 0)} / ${formatBytes(systemMetrics.diskTotalBytes || 0)}`}
                  color="warning"
                />
                <MetricCard compact
                  title={t('uptime')}
                  value={formatUptime(systemMetrics.uptime || 0)}
                  icon={Clock}
                  description={t('systemRunning')}
                  color="brand"
                />
              </div>

              {/* Runtime Metrics */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard compact
                  title={t('goroutines')}
                  value={formatNumber(systemMetrics.goroutines || 0)}
                  icon={Activity}
                  description={t('active')}
                  color="brand"
                />
                <MetricCard compact
                  title={t('heapMemory')}
                  value={`${((systemMetrics.heapAllocBytes || 0) / (1024 * 1024)).toFixed(1)} MB`}
                  icon={BarChart3}
                  description={t('allocated')}
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('gcRuns')}
                  value={formatNumber(systemMetrics.gcRuns || 0)}
                  icon={TrendingUp}
                  description={t('collections')}
                  color="warning"
                />
                <MetricCard compact
                  title={t('networkIO')}
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
                    title={t('cpuMemoryUsageOverTime')}
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
                    title={t('diskUsageOverTime')}
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
                <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">{t('noHistoricalData')}</p>
                  <p className="text-sm">{t('metricsWillAppear')}</p>
                </div>
              )}
            </div>
          )}

          {/* Storage Tab */}
          {activeTab === 'storage' && (
            <div className="space-y-6">
              {/* Quick Stats */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard compact
                  title={t('totalStorage')}
                  value={formatBytes(storageMetrics.totalSize || 0)}
                  icon={HardDrive}
                  description={t('usedSpace')}
                  color="brand"
                />
                <MetricCard compact
                  title={t('totalObjectsCount')}
                  value={formatNumber(storageMetrics.totalObjects || 0)}
                  icon={Box}
                  description={t('storedFiles')}
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('buckets')}
                  value={formatNumber(storageMetrics.totalBuckets || 0)}
                  icon={Box}
                  description={t('totalBucketsCount')}
                  color="success"
                />
                <MetricCard compact
                  title={t('avgObjectSize')}
                  value={formatBytes(storageMetrics.averageObjectSize || 0)}
                  icon={BarChart3}
                  description={t('perObject')}
                  color="warning"
                />
              </div>

              {/* Charts */}
              {storageHistoryLoading ? (
                <div className="flex items-center justify-center h-64">
                  <Loading size="lg" />
                </div>
              ) : storageChartData.length > 0 ? (
                <div className="grid gap-6 md:grid-cols-2">
                  <MetricLineChart
                    data={storageChartData}
                    title={t('objectsCountOverTime')}
                    dataKeys={[
                      { key: 'totalObjects', name: 'Objects', color: '#3b82f6' },
                    ]}
                    height={350}
                    formatYAxis={(value) => formatNumber(value)}
                    formatTooltip={(value) => formatNumber(value)}
                    timeRange={storageHistoryData?.requestedRange}
                  />
                  <MetricLineChart
                    data={storageChartData}
                    title={t('storageSizeOverTime')}
                    dataKeys={[
                      { key: 'totalSizeMB', name: 'Size (MB)', color: '#10b981' },
                    ]}
                    height={350}
                    formatYAxis={(value) => `${value.toFixed(0)} MB`}
                    formatTooltip={(value) => formatBytes(value * 1024 * 1024)}
                    timeRange={storageHistoryData?.requestedRange}
                  />
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">{t('noStorageHistory')}</p>
                  <p className="text-sm">{t('historicalStorageMetrics')}</p>
                </div>
              )}
            </div>
          )}

          {/* API & REQUESTS TAB */}
          {activeTab === 'api' && (
            <div className="space-y-6">
              {/* API Metrics */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard compact
                  title={t('totalRequests')}
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Globe}
                  description={t('sinceStartup')}
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('totalErrors')}
                  value={formatNumber(s3Metrics.totalErrors || 0)}
                  icon={XCircle}
                  description={t('failedRequests')}
                  color="error"
                />
                <MetricCard compact
                  title={t('successRate')}
                  value={`${
                    s3Metrics.totalRequests > 0
                      ? (((s3Metrics.totalRequests - s3Metrics.totalErrors) / s3Metrics.totalRequests) * 100).toFixed(2)
                      : 100
                  }%`}
                  icon={CheckCircle}
                  description={t('requestSuccess')}
                  color="success"
                />
                <MetricCard compact
                  title={t('avgLatency')}
                  value={`${(s3Metrics.avgLatency || 0).toFixed(1)}ms`}
                  icon={Clock}
                  description={t('averageResponseTime')}
                  color="warning"
                />
              </div>

              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <MetricCard compact
                  title={t('requestsPerSec')}
                  value={(s3Metrics.requestsPerSec || 0).toFixed(2)}
                  icon={TrendingUp}
                  description={t('currentRate')}
                  color="brand"
                />
                <MetricCard compact
                  title={t('errorRate')}
                  value={`${
                    s3Metrics.totalRequests > 0
                      ? ((s3Metrics.totalErrors / s3Metrics.totalRequests) * 100).toFixed(2)
                      : 0
                  }%`}
                  icon={AlertCircle}
                  description={t('percentageErrors')}
                  color="error"
                />
                <MetricCard compact
                  title={t('totalOperations')}
                  value={formatNumber(s3Metrics.totalRequests || 0)}
                  icon={Activity}
                  description={t('allApiCalls')}
                  color="blue-light"
                />
                <MetricCard compact
                  title={t('responseTime')}
                  value={`${(s3Metrics.avgLatency || 0).toFixed(0)}ms`}
                  icon={Zap}
                  description={t('avgLatencyShort')}
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
                    title={t('requestThroughputOverTime')}
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
                    title={t('averageLatencyOverTime')}
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
                <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">{t('noApiHistory')}</p>
                  <p className="text-sm">{t('requestMetricsWillAppear')}</p>
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
                    <h3 className="text-sm font-semibold text-foreground mb-3 uppercase tracking-wide">
                      {t('tabOverview')}
                    </h3>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                      <MetricCard compact
                        title={t('totalRequests')}
                        value={formatNumber(s3Metrics.totalRequests || 0)}
                        icon={Globe}
                        description={t('sinceStartup')}
                        color="blue-light"
                      />
                      <MetricCard compact
                        title={t('totalErrors')}
                        value={formatNumber(s3Metrics.totalErrors || 0)}
                        icon={AlertCircle}
                        description={t('failedRequests')}
                        color="error"
                      />
                      <MetricCard compact
                        title={t('successRate')}
                        value={`${
                          s3Metrics.totalRequests > 0
                            ? (((s3Metrics.totalRequests - s3Metrics.totalErrors) / s3Metrics.totalRequests) * 100).toFixed(2)
                            : 100
                        }%`}
                        icon={Zap}
                        description={t('requestSuccess')}
                        color="success"
                      />
                      <MetricCard compact
                        title={t('avgLatency')}
                        value={`${(s3Metrics.avgLatency || 0).toFixed(1)}ms`}
                        icon={Activity}
                        description={t('overallAverage')}
                        color="warning"
                      />
                    </div>
                  </div>

                  {/* Section 2: Real-time Throughput */}
                  <div>
                    <h3 className="text-sm font-semibold text-foreground mb-3 uppercase tracking-wide">
                      {t('realtimeThroughput')}
                    </h3>
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                      <MetricCard compact
                        title={t('requestsPerSec')}
                        value={performanceThroughput.current.requests_per_second.toFixed(2)}
                        icon={Zap}
                        description={t('currentRate')}
                        color="brand"
                      />
                      <MetricCard compact
                        title={t('bytesPerSec')}
                        value={formatBytes(performanceThroughput.current.bytes_per_second)}
                        icon={HardDrive}
                        description={t('dataTransfer')}
                        color="blue-light"
                      />
                      <MetricCard compact
                        title={t('objectsPerSec')}
                        value={performanceThroughput.current.objects_per_second.toFixed(2)}
                        icon={Box}
                        description={t('objectOps')}
                        color="success"
                      />
                      <MetricCard compact
                        title={t('totalOperations')}
                        value={formatNumber(
                          Object.values(performanceLatencies.latencies).reduce(
                            (sum, stat) => sum + stat.count,
                            0
                          )
                        )}
                        icon={Activity}
                        description={t('sinceLastReset')}
                        color="warning"
                      />
                    </div>
                  </div>

                  {/* Section 3: Operation Latencies */}
                  <div>
                    <h3 className="text-sm font-semibold text-foreground mb-3 uppercase tracking-wide">
                      {t('operationLatencies')}
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
                            className="bg-card rounded-lg border border-border p-4"
                          >
                            <div className="flex items-center justify-between mb-4">
                              <h3 className="text-lg font-semibold text-foreground">
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
                                  {stats.success_rate.toFixed(2)}% {t('requestSuccess')}
                                </span>
                              ) : (
                                <span className="px-2 py-1 text-xs font-medium rounded bg-secondary text-muted-foreground">
                                  {t('noData')}
                                </span>
                              )}
                            </div>

                            <div className="space-y-2">
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">{t('count')}</span>
                                <span className="text-sm font-medium text-foreground">
                                  {formatNumber(stats.count)}
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">p50</span>
                                <span className="text-sm font-medium text-foreground">
                                  {stats.p50_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">p95</span>
                                <span className="text-sm font-bold text-brand-600 dark:text-brand-400">
                                  {stats.p95_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">p99</span>
                                <span className="text-sm font-medium text-orange-600 dark:text-orange-400">
                                  {stats.p99_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">{t('mean')}</span>
                                <span className="text-sm font-medium text-foreground">
                                  {stats.mean_ms.toFixed(2)} ms
                                </span>
                              </div>
                              <div className="flex justify-between items-center py-1 border-b border-border/50">
                                <span className="text-sm text-muted-foreground">{t('minMax')}</span>
                                <span className="text-sm font-medium text-foreground">
                                  {stats.min_ms.toFixed(2)} / {stats.max_ms.toFixed(2)} ms
                                </span>
                              </div>
                              {stats.error_count > 0 && (
                                <div className="flex justify-between items-center py-1 border-b border-border/50">
                                  <span className="text-sm text-red-600 dark:text-red-400">{t('errors')}</span>
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
                      <h3 className="text-sm font-semibold text-foreground mb-3 uppercase tracking-wide">
                        {t('historicalTrends')}
                      </h3>
                      <div className="grid gap-6 md:grid-cols-2">
                        <MetricLineChart
                          data={chartData}
                          title={t('requestThroughputOverTime')}
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
                          title={t('averageLatencyOverTime')}
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
                    <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                      <AlertCircle className="h-12 w-12 mb-4" />
                      <p className="text-lg font-medium">{t('noPerformanceData')}</p>
                      <p className="text-sm">{t('s3MetricsWillAppear')}</p>
                    </div>
                  )}
                </>
              ) : (
                <div className="flex flex-col items-center justify-center h-64 text-muted-foreground">
                  <AlertCircle className="h-12 w-12 mb-4" />
                  <p className="text-lg font-medium">{t('performanceCollectorNA')}</p>
                  <p className="text-sm">{t('performanceMetricsEnabled')}</p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
