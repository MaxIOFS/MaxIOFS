import React, { useMemo, useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import { Box, Boxes, FolderOpen, Users, Activity, HardDrive, ArrowUpRight, Shield, BarChart3 } from 'lucide-react';
import { formatBytes } from '@/lib/utils';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { Bucket } from '@/types';
import { useNavigate } from 'react-router-dom';
import { useBasePath } from '@/hooks/useBasePath';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip as RechartsTooltip } from 'recharts';

export default function Dashboard() {
  const { t } = useTranslation('dashboard');
  const navigate = useNavigate();
  const { isGlobalAdmin, isTenantAdmin, isTenantUser } = useCurrentUser();
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  // Safe dark mode detection
  const [isDarkMode, setIsDarkMode] = useState(false);

  useEffect(() => {
    const checkDarkMode = () => {
      if (typeof document !== 'undefined') {
        setIsDarkMode(document.documentElement.classList.contains('dark'));
      }
    };

    checkDarkMode();

    // Watch for theme changes
    const observer = new MutationObserver(checkDarkMode);
    if (typeof document !== 'undefined') {
      observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['class'],
      });
    }

    return () => observer.disconnect();
  }, []);

  const basePath = useBasePath();

  // Queries already filtered by tenant on backend
  const { data: metrics, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics'],
    queryFn: APIClient.getStorageMetrics,
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  });

  const { data: bucketsResponse, isLoading: bucketsLoading } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  });

  const { data: usersResponse } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: isAnyAdmin,
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

  const { data: systemMetrics } = useQuery({
    queryKey: ['systemMetrics'],
    queryFn: APIClient.getSystemMetrics,
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
    enabled: !isTenantUser, // global users (admin or not) can see system metrics
  });

  const isLoading = metricsLoading || bucketsLoading;

  // Data is already tenant-filtered by backend
  const buckets: Bucket[] = bucketsResponse || [];
  const users = usersResponse || [];
  const totalBuckets = buckets.length;
  const totalObjects = buckets.reduce((sum, bucket) => sum + (bucket.object_count || 0), 0);
  const totalSize = buckets.reduce((sum, bucket) => sum + (bucket.size || 0), 0);
  const activeUsers = users.filter((u: any) => u.status === 'active').length;

  // Top 5 buckets by size (shared base for both charts)
  const top5Buckets = useMemo(() => {
    return [...buckets]
      .filter((b) => (b.size || 0) > 0)
      .sort((a, b) => (b.size || 0) - (a.size || 0))
      .slice(0, 5);
  }, [buckets]);

  // Pie chart data — storage distribution
  const storageDistribution = useMemo(() => {
    if (top5Buckets.length === 0 || totalSize === 0) return [];
    return top5Buckets.map((bucket) => ({
      name: bucket.name,
      value: bucket.size || 0,
      percentage: Number(((bucket.size || 0) / totalSize * 100).toFixed(1)),
    }));
  }, [top5Buckets, totalSize]);

  // Bar chart data — top buckets
  const topBuckets = useMemo(() => {
    return top5Buckets.map((bucket) => ({
      name: bucket.name.length > 15 ? bucket.name.substring(0, 15) + '...' : bucket.name,
      size: bucket.size || 0,
      objects: bucket.object_count || 0,
    }));
  }, [top5Buckets]);

  const COLORS = ['#4F46E5', '#06B6D4', '#8B5CF6', '#F59E0B', '#EF4444'];

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text={t('loadingDashboard')} />
      </div>
    );
  }

  // Calculate metrics
  const encryptedBucketsCount = buckets.filter((b: any) => b.encryption).length;
  const encryptedPercentage = totalBuckets > 0 ? ((encryptedBucketsCount / totalBuckets) * 100).toFixed(0) : 0;

  // Calculate real storage percentage based on disk size
  const diskTotal = systemMetrics?.diskTotalBytes || 0;
  const storagePercentage = diskTotal > 0 ? ((totalSize / diskTotal) * 100) : 0;

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold bg-gradient-to-r from-gray-900 via-gray-800 to-gray-900 dark:from-white dark:via-gray-100 dark:to-white bg-clip-text text-transparent">
            {t('title')}
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {isGlobalAdmin ? 'System-wide overview' : 'Your storage overview'}
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Button
            variant="outline"
            onClick={() => navigate('/buckets')}
            className="bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white shadow-sm"
          >
            <Box className="h-4 w-4 mr-2" />
            {t('viewBuckets')}
          </Button>
          <Button
            onClick={() => navigate('/buckets/create')}
            className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md hover:shadow-lg transition-all duration-200"
          >
            <FolderOpen className="h-4 w-4 mr-2" />
            {t('createBucket')}
          </Button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 4xl:grid-cols-4 5xl:grid-cols-5 6xl:grid-cols-6 gap-4 md:gap-6 4xl:gap-8">
        <MetricCard
          title={t('totalBuckets')}
          value={totalBuckets}
          icon={Box}
          description={t('activeStorageContainers')}
          color="brand"
        />

        <MetricCard
          title={t('totalObjects')}
          value={totalObjects.toLocaleString()}
          icon={Boxes}
          description={t('storedAcrossAllBuckets')}
          color="blue-light"
        />

        <MetricCard
          title={t('storageUsed')}
          value={formatBytes(totalSize)}
          icon={HardDrive}
          description={diskTotal > 0 ? `of ${formatBytes(diskTotal)}` : t('totalStorageConsumption')}
          color="warning"
        />

        <MetricCard
          title={t('activeUsers')}
          value={activeUsers}
          icon={Users}
          description={t('totalUsers', { count: users.length })}
          color="success"
        />

        <MetricCard
          title={t('systemHealth')}
          value={healthStatus?.status === 'healthy' ? t('healthy') : t('offline')}
          icon={Activity}
          description={healthStatus?.status === 'healthy' ? t('s3ApiOperational') : t('serviceUnavailable')}
          color={healthStatus?.status === 'healthy' ? 'success' : 'error'}
        />

        <MetricCard
          title={t('encryptedBuckets')}
          value={encryptedBucketsCount}
          icon={Shield}
          description={t('outOfTotalBuckets', { total: totalBuckets })}
          color="warning"
        />
      </div>

      {/* Charts and Analytics */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-stretch">
        {/* Storage Distribution Pie Chart */}
        <Card className="flex flex-col">
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2">
              <div className="p-2 bg-blue-100 dark:bg-blue-900/30 rounded-lg">
                <BarChart3 className="h-5 w-5 text-blue-600 dark:text-blue-400" />
              </div>
              Storage Distribution
            </CardTitle>
            <p className="text-sm text-gray-500 dark:text-gray-400">Top 5 buckets by size</p>
          </CardHeader>
          <CardContent className="flex-1 flex flex-col justify-center">
            {storageDistribution.length > 0 ? (
              <div className="flex flex-col md:flex-row items-center justify-between gap-4">
                <div className="w-full md:w-1/2">
                  <ResponsiveContainer width="100%" height={180} minWidth={180}>
                    <PieChart>
                      <Pie
                        data={storageDistribution}
                        cx="50%"
                        cy="50%"
                        innerRadius={60}
                        outerRadius={80}
                        paddingAngle={5}
                        dataKey="value"
                        label={false}
                      >
                        {storageDistribution.map((_entry, index) => (
                          <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                        ))}
                      </Pie>
                      <RechartsTooltip
                        formatter={(value: any) => formatBytes(value)}
                        contentStyle={{
                          backgroundColor: isDarkMode
                            ? 'rgba(31, 41, 55, 0.95)'
                            : 'rgba(255, 255, 255, 0.95)',
                          border: isDarkMode
                            ? '1px solid rgba(75, 85, 99, 0.5)'
                            : '1px solid #e5e7eb',
                          borderRadius: '8px',
                          padding: '8px 12px',
                          color: isDarkMode ? '#f9fafb' : '#1f2937'
                        }}
                        itemStyle={{
                          color: isDarkMode ? '#f9fafb' : '#1f2937'
                        }}
                        labelStyle={{
                          color: isDarkMode ? '#f9fafb' : '#1f2937'
                        }}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                </div>
                <div className="w-full md:w-1/2 space-y-1.5">
                  {storageDistribution.map((item, index) => (
                    <div key={index} className="flex items-center justify-between p-1.5 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
                      <div className="flex items-center gap-2 flex-1 min-w-0">
                        <div
                          className="w-3 h-3 rounded-full flex-shrink-0"
                          style={{ backgroundColor: COLORS[index % COLORS.length] }}
                        />
                        <span className="text-sm font-medium text-gray-700 dark:text-gray-300 truncate">{item.name}</span>
                      </div>
                      <div className="text-right">
                        <p className="text-sm font-semibold text-gray-900 dark:text-white">{formatBytes(item.value)}</p>
                        <p className="text-xs text-gray-500 dark:text-gray-400">{item.percentage}%</p>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <div className="text-center py-12">
                <div className="flex items-center justify-center w-20 h-20 mx-auto rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 mb-4 shadow-inner">
                  <BarChart3 className="h-10 w-10 text-gray-400 dark:text-gray-500" />
                </div>
                <p className="text-sm font-semibold text-gray-900 dark:text-white mb-1">No storage data yet</p>
                <p className="text-xs text-gray-500 dark:text-gray-400">Upload files to buckets to see distribution</p>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Top Buckets Bar Chart */}
        <Card className="flex flex-col">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <div className="p-2 bg-cyan-100 dark:bg-cyan-900/30 rounded-lg">
                <Box className="h-5 w-5 text-cyan-600 dark:text-cyan-400" />
              </div>
              Top Buckets
            </CardTitle>
            <p className="text-sm text-gray-500 dark:text-gray-400">Largest buckets by storage</p>
          </CardHeader>
          <CardContent className="flex-1">
            {topBuckets.length > 0 ? (
              <ResponsiveContainer width="100%" height={280}>
                <BarChart data={topBuckets} margin={{ top: 10, right: 10, left: 0, bottom: 20 }}>
                  <CartesianGrid
                    strokeDasharray="3 3"
                    stroke={isDarkMode ? '#4b5563' : '#e5e7eb'}
                    opacity={0.3}
                  />
                  <XAxis
                    dataKey="name"
                    tick={{
                      fontSize: 12,
                      fill: isDarkMode ? '#d1d5db' : '#6b7280'
                    }}
                    angle={-15}
                    textAnchor="end"
                    height={60}
                  />
                  <YAxis
                    width={70}
                    tick={{
                      fontSize: 11,
                      fill: isDarkMode ? '#d1d5db' : '#6b7280'
                    }}
                    tickFormatter={(value) => {
                      if (value === 0) return '0';
                      const k = 1024;
                      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
                      const i = Math.floor(Math.log(value) / Math.log(k));
                      const num = value / Math.pow(k, i);
                      return `${num % 1 === 0 ? num.toFixed(0) : num.toFixed(1)} ${sizes[i]}`;
                    }}
                  />
                  <RechartsTooltip
                    cursor={{ fill: isDarkMode ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.04)', radius: 4 }}
                    formatter={(value: any, name: string) => {
                      if (name === 'size') return formatBytes(value);
                      return value.toLocaleString();
                    }}
                    labelFormatter={(label) => `Bucket: ${label}`}
                    contentStyle={{
                      backgroundColor: isDarkMode
                        ? 'rgba(31, 41, 55, 0.95)'
                        : 'rgba(255, 255, 255, 0.95)',
                      border: isDarkMode
                        ? '1px solid rgba(75, 85, 99, 0.5)'
                        : '1px solid #e5e7eb',
                      borderRadius: '8px',
                      padding: '8px 12px',
                      color: isDarkMode ? '#f9fafb' : '#1f2937'
                    }}
                    itemStyle={{
                      color: isDarkMode ? '#f9fafb' : '#1f2937'
                    }}
                    labelStyle={{
                      color: isDarkMode ? '#f9fafb' : '#1f2937'
                    }}
                  />
                  <Bar dataKey="size" fill="url(#colorGradient)" radius={[8, 8, 0, 0]} activeBar={{ fill: 'url(#colorGradientHover)', stroke: isDarkMode ? 'rgba(6, 182, 212, 0.4)' : 'rgba(6, 182, 212, 0.3)', strokeWidth: 1 }} />
                  <defs>
                    <linearGradient id="colorGradient" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#06B6D4" stopOpacity={0.9}/>
                      <stop offset="100%" stopColor="#0891B2" stopOpacity={0.7}/>
                    </linearGradient>
                    <linearGradient id="colorGradientHover" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#22D3EE" stopOpacity={1}/>
                      <stop offset="100%" stopColor="#06B6D4" stopOpacity={0.85}/>
                    </linearGradient>
                  </defs>
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="text-center py-12">
                <div className="flex items-center justify-center w-20 h-20 mx-auto rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 mb-4 shadow-inner">
                  <Box className="h-10 w-10 text-gray-400 dark:text-gray-500" />
                </div>
                <p className="text-sm font-semibold text-gray-900 dark:text-white mb-1">No buckets yet</p>
                <p className="text-xs text-gray-500 dark:text-gray-400">Create your first bucket to get started</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Quick Actions and Recent Buckets */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Quick Actions Card - Enhanced */}
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-all duration-300 overflow-hidden">
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700 bg-gradient-to-r from-gray-50 to-white dark:from-gray-800 dark:to-gray-800">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <div className="p-2 bg-brand-100 dark:bg-brand-900/30 rounded-lg">
                <Activity className="h-5 w-5 text-brand-600 dark:text-brand-400" />
              </div>
              {t('quickActions')}
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{t('commonTasksShortcuts')}</p>
          </div>
          <div className="p-6">
            <div className="space-y-3">
              <button
                onClick={() => navigate('/buckets')}
                className="w-full flex items-center justify-between px-4 py-4 bg-gradient-to-r from-blue-50 to-indigo-50 dark:from-blue-950/30 dark:to-indigo-950/30 hover:from-blue-100 hover:to-indigo-100 dark:hover:from-blue-900/40 dark:hover:to-indigo-900/40 rounded-xl transition-all duration-200 text-left group border border-blue-100 dark:border-blue-900/50 shadow-sm hover:shadow-md"
              >
                <div className="flex items-center space-x-3">
                  <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500 to-indigo-600 shadow-lg">
                    <Box className="h-6 w-6 text-white" />
                  </div>
                  <div>
                    <p className="text-sm font-semibold text-gray-900 dark:text-white">{t('createNewBucket')}</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">{t('setupNewStorageContainer')}</p>
                  </div>
                </div>
                <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-blue-600 dark:group-hover:text-blue-400 group-hover:translate-x-1 group-hover:-translate-y-1 transition-all duration-200" />
              </button>

              <button
                onClick={() => navigate('/users')}
                className="w-full flex items-center justify-between px-4 py-4 bg-gradient-to-r from-green-50 to-emerald-50 dark:from-green-950/30 dark:to-emerald-950/30 hover:from-green-100 hover:to-emerald-100 dark:hover:from-green-900/40 dark:hover:to-emerald-900/40 rounded-xl transition-all duration-200 text-left group border border-green-100 dark:border-green-900/50 shadow-sm hover:shadow-md"
              >
                <div className="flex items-center space-x-3">
                  <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-gradient-to-br from-green-500 to-emerald-600 shadow-lg">
                    <Users className="h-6 w-6 text-white" />
                  </div>
                  <div>
                    <p className="text-sm font-semibold text-gray-900 dark:text-white">{t('manageUsers')}</p>
                    <p className="text-xs text-gray-500 dark:text-gray-400">{t('addEditUserAccounts')}</p>
                  </div>
                </div>
                <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-green-600 dark:group-hover:text-green-400 group-hover:translate-x-1 group-hover:-translate-y-1 transition-all duration-200" />
              </button>

              {isGlobalAdmin && (
                <button
                  onClick={() => navigate('/metrics')}
                  className="w-full flex items-center justify-between px-4 py-4 bg-gradient-to-r from-amber-50 to-orange-50 dark:from-amber-950/30 dark:to-orange-950/30 hover:from-amber-100 hover:to-orange-100 dark:hover:from-amber-900/40 dark:hover:to-orange-900/40 rounded-xl transition-all duration-200 text-left group border border-amber-100 dark:border-amber-900/50 shadow-sm hover:shadow-md"
                >
                  <div className="flex items-center space-x-3">
                    <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-gradient-to-br from-amber-500 to-orange-600 shadow-lg">
                      <Activity className="h-6 w-6 text-white" />
                    </div>
                    <div>
                      <p className="text-sm font-semibold text-gray-900 dark:text-white">{t('viewMetrics')}</p>
                      <p className="text-xs text-gray-500 dark:text-gray-400">{t('checkSystemStatistics')}</p>
                    </div>
                  </div>
                  <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-amber-600 dark:group-hover:text-amber-400 group-hover:translate-x-1 group-hover:-translate-y-1 transition-all duration-200" />
                </button>
              )}
            </div>
          </div>
        </div>

        {/* Recent Buckets Card - Enhanced */}
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-all duration-300 overflow-hidden">
          <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700 bg-gradient-to-r from-gray-50 to-white dark:from-gray-800 dark:to-gray-800">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <div className="p-2 bg-cyan-100 dark:bg-cyan-900/30 rounded-lg">
                <Box className="h-5 w-5 text-cyan-600 dark:text-cyan-400" />
              </div>
              {t('recentBuckets')}
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{t('latestStorageContainers')}</p>
          </div>
          <div className="p-6">
            {buckets.length === 0 ? (
              <div className="text-center py-8">
                <div className="flex items-center justify-center w-20 h-20 mx-auto rounded-2xl bg-gradient-to-br from-gray-100 to-gray-200 dark:from-gray-700 dark:to-gray-800 mb-4 shadow-inner">
                  <Box className="h-10 w-10 text-gray-400 dark:text-gray-500" />
                </div>
                <p className="text-sm font-semibold text-gray-900 dark:text-white mb-1">{t('noBucketsYet')}</p>
                <p className="text-xs text-gray-500 dark:text-gray-400 mb-5">{t('createFirstBucket')}</p>
                <Button
                  size="sm"
                  onClick={() => navigate('/buckets')}
                  className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md"
                >
                  <FolderOpen className="h-4 w-4 mr-2" />
                  {t('createBucket')}
                </Button>
              </div>
            ) : (
              <div className="space-y-2">
                {buckets
                  .sort((a: any, b: any) => new Date(b.creation_date).getTime() - new Date(a.creation_date).getTime())
                  .slice(0, 3)
                  .map((bucket: any) => {
                    const tenantId = bucket.tenant_id || bucket.tenantId;
                    const bucketPath = tenantId
                      ? `/buckets/${tenantId}/${bucket.name}`
                      : `/buckets/${bucket.name}`;

                    return (
                      <div
                        key={bucket.name}
                        className="flex items-center justify-between p-4 hover:bg-gradient-to-r hover:from-gray-50 hover:to-blue-50 dark:hover:from-gray-700 dark:hover:to-blue-950/30 rounded-xl cursor-pointer transition-all duration-200 group border border-transparent hover:border-blue-100 dark:hover:border-blue-900/50 hover:shadow-sm"
                        onClick={() => navigate(bucketPath)}
                      >
                        <div className="flex items-center gap-3 flex-1 min-w-0">
                          <div className="flex items-center justify-center w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500 to-indigo-600 shadow-md group-hover:shadow-lg transition-shadow duration-200 flex-shrink-0">
                            <Box className="h-6 w-6 text-white" />
                          </div>
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-semibold text-gray-900 dark:text-white truncate">{bucket.name}</p>
                            <div className="flex items-center gap-3 mt-1">
                              <span className="text-xs text-gray-500 dark:text-gray-400">
                                {bucket.object_count || 0} {t('objects')}
                              </span>
                              <span className="text-xs text-gray-400 dark:text-gray-500">•</span>
                              <span className="text-xs font-medium text-blue-600 dark:text-blue-400">
                                {formatBytes(bucket.size || 0)}
                              </span>
                            </div>
                          </div>
                        </div>
                        <ArrowUpRight className="h-5 w-5 text-gray-400 dark:text-gray-500 group-hover:text-blue-600 dark:group-hover:text-blue-400 group-hover:translate-x-1 group-hover:-translate-y-1 transition-all duration-200 flex-shrink-0" />
                      </div>
                    );
                  })}
                {buckets.length > 3 && (
                  <button
                    onClick={() => navigate('/buckets')}
                    className="w-full mt-4 text-center text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-semibold py-3 rounded-lg hover:bg-brand-50 dark:hover:bg-brand-950/30 transition-all duration-200"
                  >
                    {t('viewAllBuckets', { count: buckets.length })}
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
