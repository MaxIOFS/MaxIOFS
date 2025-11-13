import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Loading } from '@/components/ui/Loading';
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
} from 'lucide-react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AuditLogFilters, AuditLogsResponse } from '@/types';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { formatDistanceToNow } from 'date-fns';

// Event type badges color mapping
const getEventTypeColor = (eventType: string | undefined): string => {
  if (!eventType) return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-300';
  if (eventType.includes('login') || eventType.includes('logout')) return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300';
  if (eventType.includes('blocked') || eventType.includes('unblocked')) return 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-300';
  if (eventType.includes('user_created') || eventType.includes('user_deleted')) return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-300';
  if (eventType.includes('bucket')) return 'bg-cyan-100 text-cyan-800 dark:bg-cyan-900 dark:text-cyan-300';
  if (eventType.includes('access_key')) return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300';
  if (eventType.includes('tenant')) return 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-300';
  if (eventType.includes('2fa')) return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-300';
  return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-300';
};

// Format event type for display
const formatEventType = (eventType: string | undefined): string => {
  if (!eventType) return 'Unknown';
  return eventType
    .split('_')
    .map(word => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
};

// Format timestamp
const formatTimestamp = (timestamp: number): string => {
  const date = new Date(timestamp * 1000);
  return date.toLocaleString();
};

export default function AuditLogsPage() {
  const { isGlobalAdmin, isTenantAdmin } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [showFilters, setShowFilters] = useState(false);
  const [expandedRow, setExpandedRow] = useState<number | null>(null);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(50);
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

  // Export to CSV
  const exportToCSV = () => {
    const csvHeaders = ['Timestamp', 'User', 'Event Type', 'Resource', 'Action', 'Status', 'IP Address'];
    const csvRows = filteredLogs.map((log) => [
      formatTimestamp(log.timestamp),
      log.username,
      formatEventType(log.event_type),
      log.resource_name || log.resource_id || '-',
      log.action,
      log.status,
      log.ip_address || '-',
    ]);

    const csv = [
      csvHeaders.join(','),
      ...csvRows.map((row) => row.map((cell) => `"${cell}"`).join(',')),
    ].join('\n');

    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `audit-logs-${new Date().toISOString().split('T')[0]}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  };

  // Toggle row expansion
  const toggleRowExpansion = (logId: number) => {
    setExpandedRow(expandedRow === logId ? null : logId);
  };

  if (!canViewAuditLogs) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[60vh]">
        <AlertCircle className="w-16 h-16 text-red-500 mb-4" />
        <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">Access Denied</h2>
        <p className="text-gray-600 dark:text-gray-400">
          You don't have permission to view audit logs.
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
        <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">Error Loading Logs</h2>
        <p className="text-gray-600 dark:text-gray-400">
          {error instanceof Error ? error.message : 'An error occurred while loading audit logs.'}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
            <FileText className="w-8 h-8" />
            Audit Logs
          </h1>
          <p className="text-gray-600 dark:text-gray-400 mt-1">
            {isGlobalAdmin
              ? 'View all system audit logs across all tenants'
              : 'View audit logs for your tenant'}
          </p>
        </div>
        <Button onClick={exportToCSV} variant="outline">
          <Download className="w-4 h-4 mr-2" />
          Export CSV
        </Button>
      </div>

      {/* Search and Filters */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4 space-y-4">
        <div className="flex gap-4">
          <div className="flex-1">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-5 h-5 text-gray-400" />
              <Input
                type="text"
                placeholder="Search by user, event, action, resource, or IP..."
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
            <Filter className="w-4 h-4 mr-2" />
            Filters
            {showFilters ? (
              <ChevronUp className="w-4 h-4 ml-2" />
            ) : (
              <ChevronDown className="w-4 h-4 ml-2" />
            )}
          </Button>
        </div>

        {/* Advanced Filters */}
        {showFilters && (
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 pt-4 border-t border-gray-200 dark:border-gray-700">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Event Type
              </label>
              <select
                value={filters.eventType || ''}
                onChange={(e) => handleFilterChange('eventType', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">All Events</option>
                <option value="login_success">Login Success</option>
                <option value="login_failed">Login Failed</option>
                <option value="logout">Logout</option>
                <option value="user_created">User Created</option>
                <option value="user_deleted">User Deleted</option>
                <option value="user_updated">User Updated</option>
                <option value="bucket_created">Bucket Created</option>
                <option value="bucket_deleted">Bucket Deleted</option>
                <option value="access_key_created">Access Key Created</option>
                <option value="access_key_deleted">Access Key Deleted</option>
                <option value="tenant_created">Tenant Created</option>
                <option value="tenant_deleted">Tenant Deleted</option>
                <option value="tenant_updated">Tenant Updated</option>
                <option value="password_changed">Password Changed</option>
                <option value="2fa_enabled">2FA Enabled</option>
                <option value="2fa_disabled">2FA Disabled</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Status
              </label>
              <select
                value={filters.status || ''}
                onChange={(e) => handleFilterChange('status', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">All Status</option>
                <option value="success">Success</option>
                <option value="failed">Failed</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Resource Type
              </label>
              <select
                value={filters.resourceType || ''}
                onChange={(e) => handleFilterChange('resourceType', e.target.value || undefined)}
                className="w-full px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              >
                <option value="">All Resources</option>
                <option value="user">User</option>
                <option value="bucket">Bucket</option>
                <option value="access_key">Access Key</option>
                <option value="tenant">Tenant</option>
                <option value="system">System</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Start Date
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
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                End Date
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
                Clear Filters
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* Stats Summary */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Total Logs</p>
              <p className="text-2xl font-bold text-gray-900 dark:text-white">{totalLogs}</p>
            </div>
            <FileText className="w-8 h-8 text-blue-500" />
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Success</p>
              <p className="text-2xl font-bold text-green-600">
                {logs.filter((l) => l.status === 'success').length}
              </p>
            </div>
            <CheckCircle className="w-8 h-8 text-green-500" />
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Failed</p>
              <p className="text-2xl font-bold text-red-600">
                {logs.filter((l) => l.status === 'failed').length}
              </p>
            </div>
            <XCircle className="w-8 h-8 text-red-500" />
          </div>
        </div>

        <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-gray-600 dark:text-gray-400">Current Page</p>
              <p className="text-2xl font-bold text-gray-900 dark:text-white">
                {currentPage} / {totalPages}
              </p>
            </div>
            <Activity className="w-8 h-8 text-purple-500" />
          </div>
        </div>
      </div>

      {/* Audit Logs Table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow relative">
        {/* Loading overlay */}
        {isFetching && !isLoading && (
          <div className="absolute inset-0 bg-white/50 dark:bg-gray-800/50 backdrop-blur-sm flex items-center justify-center z-10 rounded-lg">
            <div className="flex items-center gap-2 bg-white dark:bg-gray-800 px-4 py-2 rounded-lg shadow-lg">
              <svg className="animate-spin h-5 w-5 text-blue-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <span className="text-sm text-gray-600 dark:text-gray-400">Updating...</span>
            </div>
          </div>
        )}
        
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Timestamp</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Event Type</TableHead>
              <TableHead>Resource</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>IP Address</TableHead>
              <TableHead>Details</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredLogs.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="text-center py-8 text-gray-500">
                  No audit logs found
                </TableCell>
              </TableRow>
            ) : (
              filteredLogs.map((log) => (
                <React.Fragment key={log.id}>
                  <TableRow className="hover:bg-gray-50 dark:hover:bg-gray-700">
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="text-sm font-medium text-gray-900 dark:text-white">
                          {formatTimestamp(log.timestamp)}
                        </span>
                        <span className="text-xs text-gray-500 dark:text-gray-400">
                          {formatDistanceToNow(new Date(log.timestamp * 1000), { addSuffix: true })}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <User className="w-4 h-4 text-gray-400" />
                        <span className="text-sm font-medium text-gray-900 dark:text-white">
                          {log.username}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span
                        className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getEventTypeColor(
                          log.event_type
                        )}`}
                      >
                        {formatEventType(log.event_type)}
                      </span>
                    </TableCell>
                    <TableCell>
                      {log.resource_name || log.resource_id ? (
                        <div className="flex flex-col">
                          <span className="text-sm text-gray-900 dark:text-white">
                            {log.resource_name || log.resource_id}
                          </span>
                          {log.resource_type && (
                            <span className="text-xs text-gray-500 dark:text-gray-400">
                              {log.resource_type}
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-gray-400">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-900 dark:text-white capitalize">
                        {log.action}
                      </span>
                    </TableCell>
                    <TableCell>
                      {log.status === 'success' ? (
                        <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
                          <CheckCircle className="w-4 h-4" />
                          <span className="text-sm font-medium">Success</span>
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
                          <XCircle className="w-4 h-4" />
                          <span className="text-sm font-medium">Failed</span>
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-gray-600 dark:text-gray-400">
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
                          <h4 className="text-sm font-semibold text-gray-900 dark:text-white mb-2">
                            Event Details
                          </h4>
                          <div className="grid grid-cols-2 gap-4 text-sm">
                            <div>
                              <span className="text-gray-600 dark:text-gray-400">User ID:</span>
                              <span className="ml-2 text-gray-900 dark:text-white font-mono">
                                {log.user_id}
                              </span>
                            </div>
                            {log.tenant_id && (
                              <div>
                                <span className="text-gray-600 dark:text-gray-400">Tenant ID:</span>
                                <span className="ml-2 text-gray-900 dark:text-white font-mono">
                                  {log.tenant_id}
                                </span>
                              </div>
                            )}
                            {log.user_agent && (
                              <div className="col-span-2">
                                <span className="text-gray-600 dark:text-gray-400">User Agent:</span>
                                <span className="ml-2 text-gray-900 dark:text-white">
                                  {log.user_agent}
                                </span>
                              </div>
                            )}
                            {log.details && (
                              <div className="col-span-2">
                                <span className="text-gray-600 dark:text-gray-400">Additional Details:</span>
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
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow p-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-4">
            <span className="text-sm text-gray-600 dark:text-gray-400">
              Showing {(currentPage - 1) * pageSize + 1} to{' '}
              {Math.min(currentPage * pageSize, totalLogs)} of {totalLogs} logs
            </span>
            <select
              value={pageSize}
              onChange={(e) => handlePageSizeChange(Number(e.target.value))}
              className="px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
            >
              <option value={10}>10 per page</option>
              <option value={25}>25 per page</option>
              <option value={50}>50 per page</option>
              <option value={100}>100 per page</option>
            </select>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(currentPage - 1)}
              disabled={currentPage === 1}
            >
              Previous
            </Button>
            <span className="text-sm text-gray-600 dark:text-gray-400">
              Page {currentPage} of {totalPages}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handlePageChange(currentPage + 1)}
              disabled={currentPage === totalPages}
            >
              Next
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
