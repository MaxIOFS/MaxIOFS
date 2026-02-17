import React, { useState, useMemo } from 'react';
import { useNavigate, Link } from 'react-router-dom';
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
import { Box, Plus, Search, Settings, Trash2, Calendar, HardDrive, Lock, Shield, Building2, Users, ChevronLeft, ChevronRight, ArrowUpDown, ArrowUp, ArrowDown } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Bucket } from '@/types';
import ModalManager from '@/lib/modals';
import { EmptyState } from '@/components/ui/EmptyState';

type SortField = 'name' | 'creationDate' | 'objectCount' | 'size';
type SortOrder = 'asc' | 'desc';

export default function BucketsPage() {
  const navigate = useNavigate();
  const [searchTerm, setSearchTerm] = useState('');
  const [currentPage, setCurrentPage] = useState(1);
  const [itemsPerPage] = useState(10);
  const [sortField, setSortField] = useState<SortField>('creationDate');
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc');
  const queryClient = useQueryClient();

  const { data: buckets, isLoading, error } = useQuery({
    queryKey: ['buckets'],
    queryFn: APIClient.getBuckets,
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  const deleteBucketMutation = useMutation({
    mutationFn: ({ bucketName, tenantId, force }: { bucketName: string; tenantId?: string; force?: boolean }) =>
      APIClient.deleteBucket(bucketName, tenantId, force),
    onSuccess: (response, { bucketName }) => {
      // Refetch to update immediately (buckets list and tenant counters)
      queryClient.refetchQueries({ queryKey: ['buckets'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.successBucketDeleted(bucketName);
    },
    onError: async (error: any, variables) => {
      ModalManager.close();

      // Check if error is "bucket not empty" and offer force delete option
      const errorMessage = error?.response?.data?.error || error?.message || String(error);

      if (errorMessage.includes('not empty') || errorMessage.includes('not empty')) {
        const result = await ModalManager.fire({
          title: 'Bucket is not empty',
          text: `The bucket "${variables.bucketName}" contains objects. Do you want to force delete it and all its contents? This action cannot be undone.`,
          icon: 'warning',
          showCancelButton: true,
          confirmButtonText: 'Yes, force delete',
          cancelButtonText: 'Cancel',
          confirmButtonColor: '#dc2626',
        });

        if (result.isConfirmed) {
          ModalManager.loading('Force deleting bucket...', `Deleting "${variables.bucketName}" and all its objects`);
          // Explicitly construct the mutation parameters with force=true
          deleteBucketMutation.mutate({
            bucketName: variables.bucketName,
            tenantId: variables.tenantId,
            force: true
          });
        }
      } else {
        ModalManager.apiError(error);
      }
    },
  });

  // Filtrar buckets por término de búsqueda
  const filteredBuckets = useMemo(() => {
    return buckets?.filter(bucket =>
      bucket.name.toLowerCase().includes(searchTerm.toLowerCase())
    ) || [];
  }, [buckets, searchTerm]);

  // Ordenar buckets
  const sortedBuckets = useMemo(() => {
    const sorted = [...filteredBuckets];
    sorted.sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case 'name':
          comparison = a.name.localeCompare(b.name);
          break;
        case 'creationDate':
          comparison = new Date(a.creation_date || a.creationDate || '').getTime() - 
                      new Date(b.creation_date || b.creationDate || '').getTime();
          break;
        case 'objectCount':
          comparison = (a.object_count || a.objectCount || 0) - (b.object_count || b.objectCount || 0);
          break;
        case 'size':
          comparison = (a.size || a.totalSize || 0) - (b.size || b.totalSize || 0);
          break;
      }
      return sortOrder === 'asc' ? comparison : -comparison;
    });
    return sorted;
  }, [filteredBuckets, sortField, sortOrder]);

  // Calcular paginación
  const totalPages = Math.ceil(sortedBuckets.length / itemsPerPage);
  const startIndex = (currentPage - 1) * itemsPerPage;
  const endIndex = startIndex + itemsPerPage;
  const paginatedBuckets = sortedBuckets.slice(startIndex, endIndex);

  // Reset a página 1 cuando cambia la búsqueda
  React.useEffect(() => {
    setCurrentPage(1);
  }, [searchTerm]);

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortOrder('asc');
    }
  };

  const getSortIcon = (field: SortField) => {
    if (sortField !== field) {
      return <ArrowUpDown className="h-3 w-3 text-gray-400" />;
    }
    return sortOrder === 'asc' ? 
      <ArrowUp className="h-3 w-3 text-brand-600 dark:text-brand-400" /> : 
      <ArrowDown className="h-3 w-3 text-brand-600 dark:text-brand-400" />;
  };

  const handleDeleteBucket = async (bucketName: string) => {
    try {
      const result = await ModalManager.confirmDeleteBucket(bucketName);
      if (result.isConfirmed) {
        ModalManager.loading('Deleting bucket...', `Deleting "${bucketName}" and all its data`);

        // Find the bucket to get its tenant_id
        const bucket = buckets?.find(b => b.name === bucketName);
        const tenantId = bucket?.tenant_id || bucket?.tenantId;

        deleteBucketMutation.mutate({ bucketName, tenantId });
      }
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const formatSize = (bytes: number) => {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let size = bytes;
    let unitIndex = 0;
    while (size >= 1024 && unitIndex < units.length - 1) {
      size /= 1024;
      unitIndex++;
    }
    return `${size.toFixed(1)} ${units[unitIndex]}`;
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getOwnerDisplay = (bucket: Bucket) => {
    const ownerId = bucket.owner_id || bucket.ownerId;
    const ownerType = bucket.owner_type || bucket.ownerType;
    if (!ownerId || !ownerType) {
      return { type: 'global', name: 'Global', icon: Shield };
    }
    if (ownerType === 'user') {
      const user = users?.find(u => u.id === ownerId);
      return { type: 'user', name: user?.username || ownerId, icon: Users };
    }
    if (ownerType === 'tenant') {
      const tenant = tenants?.find(t => t.id === ownerId);
      return { type: 'tenant', name: tenant?.displayName || ownerId, icon: Building2 };
    }
    return { type: 'unknown', name: 'Unknown', icon: Shield };
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg bg-error-50 dark:bg-error-900/30 border border-error-200 dark:border-error-800 p-4">
        <div className="text-sm text-error-700 dark:text-error-400 font-medium">
          Error loading buckets: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">
            Buckets
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            Manage your S3 buckets and their configurations
          </p>
        </div>
        <Button
          onClick={() => navigate('/buckets/create')}
          className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md hover:shadow-lg transition-all duration-200 inline-flex items-center gap-2"
        >
          <Plus className="h-4 w-4" />
          Create Bucket
        </Button>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 md:gap-6">
        <MetricCard
          title="Total Buckets"
          value={sortedBuckets.length}
          icon={Box}
          description="Active storage containers"
          color="brand"
        />

        <MetricCard
          title="Total Objects"
          value={sortedBuckets.reduce((sum, bucket) => sum + (bucket.object_count || bucket.objectCount || 0), 0).toLocaleString()}
          icon={HardDrive}
          description="Stored across all buckets"
          color="blue-light"
        />

        <MetricCard
          title="Total Size"
          value={formatSize(sortedBuckets.reduce((sum, bucket) => sum + (bucket.size || bucket.totalSize || 0), 0))}
          icon={HardDrive}
          description="Storage consumption"
          color="warning"
        />
      </div>

      {/* Enhanced Search Bar */}
      <div className="bg-gradient-to-r from-white to-gray-50 dark:from-gray-800 dark:to-gray-800/50 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm p-4">
        <div className="relative max-w-md">
          <div className="absolute left-4 top-1/2 transform -translate-y-1/2">
            <Search className="text-gray-400 dark:text-gray-500 h-5 w-5" />
          </div>
          <Input
            placeholder="Search buckets..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-12 bg-white dark:bg-gray-900 border-gray-300 dark:border-gray-600 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500 focus:border-brand-500 rounded-lg shadow-sm"
          />
        </div>
      </div>

      {/* Enhanced Buckets Table */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Box className="h-5 w-5 text-brand-600 dark:text-brand-400" />
            All Buckets ({sortedBuckets.length})
          </h3>
        </div>

        <div className="overflow-x-auto">
          {paginatedBuckets.length === 0 ? (
            <EmptyState
              icon={Box}
              title="No buckets found"
              description={searchTerm ? "No buckets match your search criteria. Try adjusting your search terms." : "Get started by creating your first bucket to store objects."}
              actionLabel={!searchTerm ? "Create Bucket" : undefined}
              onAction={!searchTerm ? () => navigate('/buckets/create') : undefined}
              showAction={!searchTerm}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    <button
                      onClick={() => handleSort('name')}
                      className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                    >
                      Name
                      {getSortIcon('name')}
                    </button>
                  </TableHead>
                  <TableHead>Region</TableHead>
                  <TableHead>Node</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>
                    <button
                      onClick={() => handleSort('objectCount')}
                      className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                    >
                      Objects
                      {getSortIcon('objectCount')}
                    </button>
                  </TableHead>
                  <TableHead>
                    <button
                      onClick={() => handleSort('size')}
                      className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                    >
                      Size
                      {getSortIcon('size')}
                    </button>
                  </TableHead>
                  <TableHead>
                    <button
                      onClick={() => handleSort('creationDate')}
                      className="flex items-center gap-2 text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                    >
                      Created
                      {getSortIcon('creationDate')}
                    </button>
                  </TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paginatedBuckets.map((bucket) => {
                  const tenantId = bucket.tenant_id || bucket.tenantId;
                  const bucketPath = tenantId ? `/buckets/${tenantId}/${bucket.name}` : `/buckets/${bucket.name}`;
                  const owner = getOwnerDisplay(bucket);
                  const OwnerIcon = owner.icon;

                  return (
                    <TableRow key={`${tenantId || 'global'}-${bucket.name}`}>
                      <TableCell className="whitespace-nowrap">
                        <div className="flex items-center gap-3">
                          <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gradient-to-br from-brand-50 to-blue-50 dark:from-brand-900/30 dark:to-blue-900/30 shadow-sm">
                            <Box className="h-4 w-4 text-brand-600 dark:text-brand-400" />
                          </div>
                          <div>
                            <Link
                              to={bucketPath}
                              className="text-sm font-semibold text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 transition-colors"
                            >
                              {bucket.name}
                            </Link>
                            {bucket.objectLock?.objectLockEnabled && (
                              <div className="flex items-center gap-1 mt-1">
                                <span className="inline-flex items-center gap-1 bg-gradient-to-r from-blue-100 to-cyan-100 dark:from-blue-900/40 dark:to-cyan-900/40 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded-md text-xs font-medium shadow-sm">
                                  <Lock className="h-3 w-3" />
                                  WORM
                                </span>
                              </div>
                            )}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <span className="text-sm">{bucket.region || 'us-east-1'}</span>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        {bucket.node_name || bucket.nodeName ? (
                          <div className="flex items-center gap-2">
                            <div className={`w-2 h-2 rounded-full ${
                              (bucket.node_status || bucket.nodeStatus) === 'healthy'
                                ? 'bg-green-500 dark:bg-green-400'
                                : 'bg-yellow-500 dark:bg-yellow-400'
                            }`} />
                            <span className="text-sm">
                              {bucket.node_name || bucket.nodeName}
                            </span>
                          </div>
                        ) : (
                          <span className="text-xs text-gray-400 dark:text-gray-500 italic">Local</span>
                        )}
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <div className="flex items-center gap-2">
                          <OwnerIcon className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                          <span className={owner.type === 'global' ? 'text-sm text-gray-500 dark:text-gray-400 italic' : 'text-sm'}>
                            {owner.name}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <span className="text-sm">
                          {(bucket.object_count || bucket.objectCount || 0).toLocaleString()}
                        </span>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <span className="text-sm">
                          {formatSize(bucket.size || bucket.totalSize || 0)}
                        </span>
                      </TableCell>
                      <TableCell className="whitespace-nowrap">
                        <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                          <Calendar className="h-3 w-3" />
                          {formatDate(bucket.creation_date || bucket.creationDate || '')}
                        </div>
                      </TableCell>
                      <TableCell className="whitespace-nowrap text-right">
                        <div className="flex items-center justify-end gap-2">
                          <button
                            onClick={() => navigate(`${bucketPath}/settings`)}
                            className="p-2 text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 hover:bg-gradient-to-br hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 rounded-lg transition-all duration-200 shadow-sm hover:shadow"
                            title="Settings"
                          >
                            <Settings className="h-4 w-4" />
                          </button>
                          <button
                            onClick={() => handleDeleteBucket(bucket.name)}
                            disabled={deleteBucketMutation.isPending}
                            className="p-2 text-gray-600 dark:text-gray-400 hover:text-error-600 dark:hover:text-error-400 hover:bg-gradient-to-br hover:from-error-50 hover:to-red-50 dark:hover:from-error-900/30 dark:hover:to-red-900/30 rounded-lg transition-all duration-200 disabled:opacity-50 shadow-sm hover:shadow"
                            title="Delete"
                          >
                            <Trash2 className="h-4 w-4" />
                          </button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </div>

        {/* Enhanced Pagination */}
        {sortedBuckets.length > 0 && (
          <div className="px-6 py-4 border-t border-gray-200 dark:border-gray-700 bg-gradient-to-r from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 flex items-center justify-between">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Showing <span className="text-brand-600 dark:text-brand-400 font-semibold">{startIndex + 1}</span> to <span className="text-brand-600 dark:text-brand-400 font-semibold">{Math.min(endIndex, sortedBuckets.length)}</span> of <span className="text-brand-600 dark:text-brand-400 font-semibold">{sortedBuckets.length}</span> buckets
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                onClick={() => setCurrentPage(currentPage - 1)}
                disabled={currentPage === 1}
                className="inline-flex items-center gap-1 bg-white dark:bg-gray-800 hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 border-gray-300 dark:border-gray-600 shadow-sm"
              >
                <ChevronLeft className="h-4 w-4" />
                Previous
              </Button>

              <div className="flex items-center gap-1">
                {Array.from({ length: totalPages }, (_, i) => i + 1)
                  .filter(page => {
                    // Mostrar primera, última, actual y 2 páginas adyacentes
                    return page === 1 ||
                           page === totalPages ||
                           (page >= currentPage - 1 && page <= currentPage + 1);
                  })
                  .map((page, index, array) => {
                    // Agregar "..." si hay salto
                    const showEllipsis = index > 0 && page - array[index - 1] > 1;
                    return (
                      <React.Fragment key={page}>
                        {showEllipsis && (
                          <span className="px-2 text-gray-500 dark:text-gray-400">...</span>
                        )}
                        <button
                          onClick={() => setCurrentPage(page)}
                          className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-all duration-200 ${
                            currentPage === page
                              ? 'bg-gradient-to-r from-brand-600 to-brand-700 text-white shadow-md hover:shadow-lg'
                              : 'bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 border border-gray-300 dark:border-gray-600 shadow-sm'
                          }`}
                        >
                          {page}
                        </button>
                      </React.Fragment>
                    );
                  })}
              </div>

              <Button
                variant="outline"
                onClick={() => setCurrentPage(currentPage + 1)}
                disabled={currentPage === totalPages}
                className="inline-flex items-center gap-1 bg-white dark:bg-gray-800 hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 border-gray-300 dark:border-gray-600 shadow-sm"
              >
                Next
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
