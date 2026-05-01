import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import {
  FileText,
  Search,
  Filter,
  Download,
  ChevronDown,
  ChevronUp,
  User,
  Activity,
  CheckCircle,
  XCircle,
  AlertCircle,
  Calendar,
  Clock,
} from 'lucide-react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AuditLogFilters, AuditLogsResponse } from '@/types';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { formatDistanceToNow } from 'date-fns';
import { cn } from '@/lib/utils';
import { EmptyState } from '@/components/ui/EmptyState';
import ModalManager from '@/lib/modals';

// Event type badges color mapping - gray for all events
const getEventTypeColor = (_eventType: string | undefined): string => {
  // All events use gray color for professional appearance
  return 'bg-secondary text-foreground';
};

// Format event type for display
const formatEventType = (eventType: string | undefined): string => {
  if (!eventType) return 'Unknown';
  return eventType
    .split('_')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
};

// Check if event is critical (security-related)
const isCriticalEvent = (eventType: string | undefined, status: string): boolean => {
  if (!eventType) return false;
  const criticalEvents = [
    'login_failed',
    'user_blocked',
    '2fa_disabled',
    '2fa_verify_failed',
    'user_deleted',
    'tenant_deleted',
    'access_key_deleted',
  ];
  return criticalEvents.includes(eventType) || status === 'failed';
};

// Format timestamp — ISO-like format with no commas (comma-safe for CSV)
const formatTimestamp = (timestamp: number): string => {
  const d = new Date(timestamp * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  );
};

export default function AuditLogsPage() {
  const { t } = useTranslation('auditLogs');
  const { isGlobalAdmin, isTenantAdmin } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [showFilters, setShowFilters] = useState(false);
  const [expandedRow, setExpandedRow] = useState<number | null>(null);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
  const [isExporting, setIsExporting] = useState(false);
  const [activeTimeFilter, setActiveTimeFilter] = useState<'all' | 'today' | 'week' | 'month'>('all');
  const [filters, setFilters] = useState<AuditLogFilters>({
    page: 1,
    pageSize: 50,
  });

  // Check permissions
  const canViewAuditLogs = isGlobalAdmin || isTenantAdmin;

  // Fetch audit logs
  const { data: auditLogsData, isLoading, error, isFetching } = useQuery<AuditLogsResponse>({
    queryKey: ['auditLogs', filters],
    queryFn: () => APIClient.getAuditLogs(filters),
    enabled: canViewAuditLogs,
    placeholderData: keepPreviousData, // Keep previous data while fetching new data
  });

  const logs = auditLogsData?.logs || [];
  const totalLogs = auditLogsData?.total || 0;
  const totalPages = Math.ceil(totalLogs / pageSize);

  // Fetch overall stats: get the server-reported total for each status.
  // We send status filter + pageSize:1 — we only need the `total` field,
  // not the actual records, so this is a very cheap request.
  const { data: successStatsData } = useQuery<AuditLogsResponse>({
    queryKey: ['auditLogsStats', 'success', filters.eventType, filters.resourceType, filters.startDate, filters.endDate, filters.tenantId, filters.userId],
    queryFn: () => APIClient.getAuditLogs({
      ...filters,
      status: 'success',
      page: 1,
      pageSize: 1,
    }),
    enabled: canViewAuditLogs,
  });

  const { data: failedStatsData } = useQuery<AuditLogsResponse>({
    queryKey: ['auditLogsStats', 'failed', filters.eventType, filters.resourceType, filters.startDate, filters.endDate, filters.tenantId, filters.userId],
    queryFn: () => APIClient.getAuditLogs({
      ...filters,
      status: 'failed',
      page: 1,
      pageSize: 1,
    }),
    enabled: canViewAuditLogs,
  });

  const totalSuccessCount = successStatsData?.total ?? 0;
  const totalFailedCount = failedStatsData?.total ?? 0;

  // Filter logs by search term (client-side for current page)
  const filteredLogs = logs.filter((log) => {
    const searchLower = searchTerm.toLowerCase();
    return (
      (log.username && log.username.toLowerCase().includes(searchLower)) ||
      (log.event_type && log.event_type.toLowerCase().includes(searchLower)) ||
      (log.action && log.action.toLowerCase().includes(searchLower)) ||
      (log.resource_name && log.resource_name.toLowerCase().includes(searchLower)) ||
      (log.ip_address && log.ip_address.toLowerCase().includes(searchLower))
    );
  });

  // Handle filter changes
  const handleFilterChange = (key: keyof AuditLogFilters, value: string | number | undefined) => {
    setFilters((prev) => ({
      ...prev,
      [key]: value,
      page: 1, // Reset to first page on filter change
    }));
    setCurrentPage(1);
  };

  // Handle page change
  const handlePageChange = (page: number) => {
    setCurrentPage(page);
    setFilters((prev) => ({ ...prev, page }));
  };

  // Handle page size change
  const handlePageSizeChange = (size: number) => {
    setPageSize(size);
    setCurrentPage(1);
    setFilters((prev) => ({ ...prev, pageSize: size, page: 1 }));
  };

  // Quick date filter helpers
  const setQuickDateFilter = (range: 'today' | 'week' | 'month' | 'all') => {
    const now = Math.floor(Date.now() / 1000);
    let startDate: number | undefined;

    switch (range) {
      case 'today': {
        const todayStart = new Date();
        todayStart.setHours(0, 0, 0, 0);
        startDate = Math.floor(todayStart.getTime() / 1000);
        break;
      }
      case 'week':
        startDate = now - (7 * 24 * 60 * 60);
        break;
      case 'month':
        startDate = now - (30 * 24 * 60 * 60);
        break;
      case 'all':
        startDate = undefined;
        break;
    }

    setActiveTimeFilter(range);
    setFilters((prev) => ({
      ...prev,
      startDate,
      endDate: range === 'all' ? undefined : now,
      page: 1,
    }));
    setCurrentPage(1);
  };

  // Get active date range description
  const getDateRangeDescription = (): string => {
    if (!filters.startDate && !filters.endDate) return 'All time';

    const formatDate = (timestamp: number) => new Date(timestamp * 1000).toLocaleDateString();

    if (filters.startDate && filters.endDate) {
      return `${formatDate(filters.startDate)} - ${formatDate(filters.endDate)}`;
    } else if (filters.startDate) {
      return `From ${formatDate(filters.startDate)}`;
    } else if (filters.endDate) {
      return `Until ${formatDate(filters.endDate)}`;
    }

    return 'All time';
  };

  // Export to CSV — fetches ALL records matching the current filters by
  // walking through every page (100 records each) and concatenating them.
  // Pagination limits in the API are intentional and stay unchanged.
  const exportToCSV = async () => {
    if (isExporting) return;
    setIsExporting(true);
    try {
      const PAGE_SIZE = 100;
      let logsToExport: typeof logs = [];
      let page = 1;

      while (true) {
        const chunk = await APIClient.getAuditLogs({
          ...filters,
          page,
          pageSize: PAGE_SIZE,
        });
        const pageLogs = chunk.logs || [];
        logsToExport = [...logsToExport, ...pageLogs];
        // Stop when the server returns fewer records than requested — that's the last page
        if (pageLogs.length < PAGE_SIZE) break;
        page++;
      }

      // Re-apply the client-side search term across the full dataset
      if (searchTerm.trim()) {
        const lower = searchTerm.toLowerCase();
        logsToExport = logsToExport.filter(
          (log) =>
            (log.username && log.username.toLowerCase().includes(lower)) ||
            (log.event_type && log.event_type.toLowerCase().includes(lower)) ||
            (log.action && log.action.toLowerCase().includes(lower)) ||
            (log.resource_name && log.resource_name.toLowerCase().includes(lower)) ||
            (log.ip_address && log.ip_address.toLowerCase().includes(lower))
        );
      }

      const csvHeaders = ['Timestamp', 'User', 'Event Type', 'Resource', 'Action', 'Status', 'IP Address'];
      const csvRows = logsToExport.map((log) => [
        formatTimestamp(log.timestamp),
        log.username || '-',
        formatEventType(log.event_type),
        log.resource_name || log.resource_id || '-',
        log.action || '-',
        log.status,
        log.ip_address || '-',
      ]);

      const csv = [
        csvHeaders.join(','),
        ...csvRows.map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(',')),
      ].join('\n');

      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `audit-logs-${new Date().toISOString().split('T')[0]}.csv`;
      link.click();
      URL.revokeObjectURL(url);

      ModalManager.toast('success', `Exported ${logsToExport.length} records`);
    } catch (err) {
      ModalManager.apiError(err);
    } finally {
      setIsExporting(false);
    }
  };

  // Toggle row expansion
  const toggleRowExpansion = (logId: number) => {
    setExpandedRow(expandedRow === logId ? null : logId);
  };

  if (!canViewAuditLogs) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh]">
        <AlertCircle className="w-16 h-16 text-red-500 mb-4" />
        <h2 className="text-2xl font-bold text-foreground mb-2">{t('accessDenied')}</h2>
        <p className="text-sm text-muted-foreground mt-1">
          {t('noPermissionMessage')}
        </p>
      </div>
    );
  }

  if (isLoading) {
    return <Loading />;
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh]">
        <XCircle className="w-16 h-16 text-red-500 mb-4" />
        <h2 className="text-2xl font-bold text-foreground mb-2">{t('errorLoadingLogs')}</h2>
        <p className="text-sm text-muted-foreground mt-1">
          {error instanceof Error ? error.message : t('errorOccurred')}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <FileText className="w-8 h-8" />
            {t('title')}
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            {isGlobalAdmin
              ? t('viewAllSystemLogs')
              : t('viewTenantLogs')}
          </p>
        </div>
        <Button onClick={exportToCSV} variant="outline" disabled={isExporting}>
          <Download className={cn('w-4 h-4', isExporting && 'animate-spin')} />
          {isExporting ? t('exporting', 'Exporting…') : t('exportCsv')}
        </Button>
      </div>

      {/* Search and Filters */}
      <div className="bg-card rounded-lg shadow p-4 space-y-4">
        {/* Quick Date Filters */}
        <div className="flex items-center gap-3 pb-4 border-b border-border">
          <Clock className="w-5 h-5 text-gray-400" />
          <span className="text-sm font-medium text-foreground">{t('timeRange')}</span>
          <div className="flex gap-2">
            <Button
              variant={activeTimeFilter === 'all' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setQuickDateFilter('all')}
            >
              {t('allTime')}
            </Button>
            <Button
              variant={activeTimeFilter === 'today' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setQuickDateFilter('today')}
            >
              {t('today')}
            </Button>
            <Button
              variant={activeTimeFilter === 'week' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setQuickDateFilter('week')}
            >
              {t('last7Days')}
            </Button>
            <Button
              variant={activeTimeFilter === 'month' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setQuickDateFilter('month')}
            >
              {t('last30Days')}
            </Button>
          </div>
          <div className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
            <Calendar className="w-4 h-4" />
            <span>{getDateRangeDescription()}</span>
          </div>
        </div>

        <div className="flex gap-4">
          <div className="flex-1">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-5 h-5 text-gray-400" />
              <Input
                type="text"
                placeholder={t('searchPlaceholder')}
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="pl-10"
              />
            </div>
          </div>
          <Button
            variant="outline"
            onClick={() => setShowFilters(!showFilters)}
          >
            <Filter className="w-4 h-4" />
            {t('filters')}
            {showFilters ? (
              <ChevronUp className="w-4 h-4 ml-2" />
            ) : (
              <ChevronDown className="w-4 h-4 ml-2" />
            )}
          </Button>
        </div>

        {/* Advanced Filters */}
        {showFilters && (
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 pt-4 border-t border-border">
            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                {t('eventType')}
              </label>
              <select
                value={filters.eventType || ''}
                onChange={(e) => handleFilterChange('eventType', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-border bg-white dark:bg-gray-700 text-foreground focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">{t('allEvents')}</option>
                <option value="login_success">{t('loginSuccess')}</option>
                <option value="login_failed">{t('loginFailed')}</option>
                <option value="logout">{t('logout')}</option>
                <option value="user_created">{t('userCreated')}</option>
                <option value="user_deleted">{t('userDeleted')}</option>
                <option value="user_updated">{t('userUpdated')}</option>
                <option value="bucket_created">{t('bucketCreated')}</option>
                <option value="bucket_deleted">{t('bucketDeleted')}</option>
                <option value="access_key_created">{t('accessKeyCreated')}</option>
                <option value="access_key_deleted">{t('accessKeyDeleted')}</option>
                <option value="tenant_created">{t('tenantCreated')}</option>
                <option value="tenant_deleted">{t('tenantDeleted')}</option>
                <option value="tenant_updated">{t('tenantUpdated')}</option>
                <option value="password_changed">{t('passwordChanged')}</option>
                <option value="2fa_enabled">{t('2faEnabled')}</option>
                <option value="2fa_disabled">{t('2faDisabled')}</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                {t('status')}
              </label>
              <select
                value={filters.status || ''}
                onChange={(e) => handleFilterChange('status', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-border bg-white dark:bg-gray-700 text-foreground focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">{t('allStatus')}</option>
                <option value="success">{t('success')}</option>
                <option value="failed">{t('failed')}</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                {t('resourceType')}
              </label>
              <select
                value={filters.resourceType || ''}
                onChange={(e) => handleFilterChange('resourceType', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-border bg-white dark:bg-gray-700 text-foreground focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">{t('allResources')}</option>
                <option value="user">{t('user')}</option>
                <option value="bucket">{t('bucket')}</option>
                <option value="access_key">{t('accessKey')}</option>
                <option value="tenant">{t('tenant')}</option>
                <option value="system">{t('system')}</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                {t('startDate')}
              </label>
              <Input
                type="datetime-local"
                value={
                  filters.startDate
                    ? new Date(filters.startDate * 1000).toISOString().slice(0, 16)
                    : ''
                }
                onChange={(e) => {
                  const timestamp = e.target.value
                    ? Math.floor(new Date(e.target.value).getTime() / 1000)
                    : undefined;
                  handleFilterChange('startDate', timestamp);
                }}
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-foreground mb-1">
                {t('endDate')}
              </label>
              <Input
                type="datetime-local"
                value={
                  filters.endDate
                    ? new Date(filters.endDate * 1000).toISOString().slice(0, 16)
                    : ''
                }
                onChange={(e) => {
                  const timestamp = e.target.value
                    ? Math.floor(new Date(e.target.value).getTime() / 1000)
                    : undefined;
                  handleFilterChange('endDate', timestamp);
                }}
              />
            </div>

            <div className="flex items-end">
              <Button
                variant="outline"
                onClick={() => {
                  setFilters({ page: 1, pageSize });
                  setSearchTerm('');
                }}
                className="w-full"
              >
                {t('clearFilters')}
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* Stats Summary */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 md:gap-6">
        <MetricCard compact
          title={t('totalLogs')}
          value={totalLogs.toLocaleString()}
          icon={FileText}
          description={getDateRangeDescription()}
          color="brand"
        />

        <MetricCard compact
          title={t('successful')}
          value={totalSuccessCount.toLocaleString()}
          icon={CheckCircle}
          description={totalLogs > 0 ? t('successRate', { percent: Math.round((totalSuccessCount / totalLogs) * 100) }) : t('noSuccessRate')}
          color="success"
        />

        <MetricCard compact
          title={t('failed')}
          value={totalFailedCount.toLocaleString()}
          icon={XCircle}
          description={totalLogs > 0 ? t('failureRate', { percent: Math.round((totalFailedCount / totalLogs) * 100) }) : t('noFailureRate')}
          color="error"
        />

        <MetricCard compact
          title={t('viewing')}
          value={`${currentPage} / ${totalPages}`}
          icon={Activity}
          description={t('itemsPerPage', { count: pageSize })}
          color="blue-light"
        />
      </div>

      {/* Audit Logs Table */}
      <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden relative">
        {/* Loading overlay */}
        {isFetching && !isLoading && (
          <div className="absolute inset-0 bg-white/50 dark:bg-gray-800/50 backdrop-blur-sm flex items-center justify-center z-10 rounded-lg">
            <div className="flex items-center gap-2 bg-card px-4 py-2 rounded-lg shadow-lg">
              <svg className="animate-spin h-5 w-5 text-blue-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <span className="text-sm text-muted-foreground">{t('updating')}</span>
            </div>
          </div>
        )}

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('timestamp')}</TableHead>
              <TableHead>User</TableHead>
              <TableHead>{t('eventTypeHeader')}</TableHead>
              <TableHead>{t('resource')}</TableHead>
              <TableHead>{t('action')}</TableHead>
              <TableHead>{t('status')}</TableHead>
              <TableHead>{t('ipAddress')}</TableHead>
              <TableHead>{t('details')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredLogs.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8}>
                  <EmptyState
                    icon={FileText}
                    title={t('noAuditLogsFound')}
                    description={t('noAuditLogsDescription')}
                    showAction={false}
                  />
                </TableCell>
              </TableRow>
            ) : (
              filteredLogs.map((log) => {
                const isCritical = isCriticalEvent(log.event_type, log.status);
                return (
                <React.Fragment key={log.id}>
                  <TableRow className={cn(
                    "hover:bg-secondary transition-colors",
                    isCritical && "bg-red-50/50 dark:bg-red-900/10 border-l-4 border-red-500"
                  )}>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="text-sm font-medium text-foreground">
                          {formatTimestamp(log.timestamp)}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {formatDistanceToNow(new Date(log.timestamp * 1000), { addSuffix: true })}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <User className="w-4 h-4 text-gray-400" />
                        <span className="text-sm font-medium text-foreground">
                          {log.username}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        {isCritical && (
                          <AlertCircle className="w-4 h-4 text-red-500" />
                        )}
                        <span
                          className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getEventTypeColor(
                            log.event_type
                          )}`}
                        >
                          {formatEventType(log.event_type)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      {log.resource_name || log.resource_id ? (
                        <div className="flex flex-col">
                          <span className="text-sm text-foreground">
                            {log.resource_name || log.resource_id}
                          </span>
                          {log.resource_type && (
                            <span className="text-xs text-muted-foreground">
                              {log.resource_type}
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-gray-400">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-foreground capitalize">
                        {log.action}
                      </span>
                    </TableCell>
                    <TableCell>
                      {log.status === 'success' ? (
                        <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                          <CheckCircle className="w-4 h-4" />
                          <span className="text-sm font-medium">{t('success')}</span>
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
                          <XCircle className="w-4 h-4" />
                          <span className="text-sm font-medium">{t('failed')}</span>
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-muted-foreground">
                        {log.ip_address || '-'}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => toggleRowExpansion(log.id)}
                      >
                        {expandedRow === log.id ? (
                          <ChevronUp className="w-4 h-4" />
                        ) : (
                          <ChevronDown className="w-4 h-4" />
                        )}
                      </Button>
                    </TableCell>
                  </TableRow>

                  {/* Expanded Details Row */}
                  {expandedRow === log.id && (
                    <TableRow>
                      <TableCell colSpan={8} className="bg-gray-50 dark:bg-gray-900">
                        <div className="p-4 space-y-2">
                          <h4 className="text-sm font-semibold text-foreground mb-2">
                            {t('eventDetails')}
                          </h4>
                          <div className="grid grid-cols-2 gap-4 text-sm">
                            <div>
                              <span className="text-muted-foreground">{t('userId')}</span>
                              <span className="ml-2 text-foreground font-mono">
                                {log.user_id}
                              </span>
                            </div>
                            {log.tenant_id && (
                              <div>
                                <span className="text-muted-foreground">{t('tenantId')}</span>
                                <span className="ml-2 text-foreground font-mono">
                                  {log.tenant_id}
                                </span>
                              </div>
                            )}
                            {log.user_agent && (
                              <div className="col-span-2">
                                <span className="text-muted-foreground">{t('userAgent')}</span>
                                <span className="ml-2 text-foreground">
                                  {log.user_agent}
                                </span>
                              </div>
                            )}
                            {log.details && (
                              <div className="col-span-2">
                                <span className="text-muted-foreground">{t('additionalDetails')}</span>
                                <pre className="mt-2 p-3 bg-gray-100 dark:bg-gray-800 rounded-md overflow-x-auto text-xs">
                                  {typeof log.details === 'string'
                                    ? JSON.stringify(JSON.parse(log.details), null, 2)
                                    : JSON.stringify(log.details, null, 2)}
                                </pre>
                              </div>
                            )}
                          </div>
                        </div>
                      </TableCell>
                    </TableRow>
                  )}
                </React.Fragment>
              )})
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      <div className="bg-card rounded-lg shadow p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <span className="text-sm text-muted-foreground">
              {t('showingLogs', {
                from: (currentPage - 1) * pageSize + 1,
                to: Math.min(currentPage * pageSize, totalLogs),
                total: totalLogs,
              })}
            </span>
            <select
              value={pageSize}
              onChange={(e) => handlePageSizeChange(Number(e.target.value))}
              className="px-3 py-2 text-sm rounded-md border border-border bg-white dark:bg-gray-700 text-foreground focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              <option value={10}>{t('perPage_10')}</option>
              <option value={25}>{t('perPage_25')}</option>
              <option value={50}>{t('perPage_50')}</option>
              <option value={100}>{t('perPage_100')}</option>
            </select>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(currentPage - 1)}
              disabled={currentPage === 1}
            >
              {t('previous')}
            </Button>
            <span className="text-sm text-muted-foreground">
              {t('pageNumber', { current: currentPage, total: totalPages })}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(currentPage + 1)}
              disabled={currentPage === totalPages}
            >
              {t('next')}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
