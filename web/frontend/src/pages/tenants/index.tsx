import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
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
  Building2,
  Plus,
  Search,
  Settings,
  Trash2,
  Users,
  Database,
  HardDrive,
  Key,
  CheckCircle,
  XCircle,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Tenant, CreateTenantRequest, UpdateTenantRequest } from '@/types';
import SweetAlert from '@/lib/sweetalert';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function TenantsPage() {
  const { isGlobalAdmin } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [selectedTenant, setSelectedTenant] = useState<Tenant | null>(null);
  const [newTenant, setNewTenant] = useState<Partial<CreateTenantRequest>>({
    maxAccessKeys: 10,
    maxStorageBytes: 107374182400, // 100GB
    maxBuckets: 100,
  });
  const queryClient = useQueryClient();

  const { data: tenants, isLoading, error } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  const createTenantMutation = useMutation({
    mutationFn: (data: CreateTenantRequest) => APIClient.createTenant(data),
    onSuccess: (response, variables) => {
      queryClient.invalidateQueries({ queryKey: ['tenants'] });
      setIsCreateModalOpen(false);
      setNewTenant({ maxAccessKeys: 10, maxStorageBytes: 107374182400, maxBuckets: 100 });
      SweetAlert.toast('success', `Tenant "${variables.displayName}" created successfully`);
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const updateTenantMutation = useMutation({
    mutationFn: ({ tenantId, data }: { tenantId: string; data: UpdateTenantRequest }) =>
      APIClient.updateTenant(tenantId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenants'] });
      setIsEditModalOpen(false);
      setSelectedTenant(null);
      SweetAlert.toast('success', 'Tenant updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteTenantMutation = useMutation({
    mutationFn: (tenantId: string) => APIClient.deleteTenant(tenantId),
    onSuccess: () => {
      SweetAlert.close();
      queryClient.invalidateQueries({ queryKey: ['tenants'] });
      SweetAlert.toast('success', 'Tenant deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.close();
      SweetAlert.apiError(error);
    },
  });

  const filteredTenants = Array.isArray(tenants)
    ? tenants.filter((tenant: Tenant) =>
        tenant.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
        tenant.displayName?.toLowerCase().includes(searchTerm.toLowerCase())
      )
    : [];

  const handleCreateTenant = (e: React.FormEvent) => {
    e.preventDefault();
    if (newTenant.name && newTenant.displayName) {
      createTenantMutation.mutate(newTenant as CreateTenantRequest);
    }
  };

  const handleUpdateTenant = (e: React.FormEvent) => {
    e.preventDefault();
    if (selectedTenant) {
      const { id, name, createdAt, ...updateData } = selectedTenant;
      updateTenantMutation.mutate({
        tenantId: id,
        data: updateData as UpdateTenantRequest
      });
    }
  };

  const handleDelete = (tenant: Tenant) => {
    SweetAlert.confirm(
      `Are you sure you want to delete "${tenant.displayName}"?`,
      'This will remove the tenant and unassign all users. This action cannot be undone.',
      () => deleteTenantMutation.mutate(tenant.id)
    );
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
  };

  const getUsagePercentage = (current: number, max: number) => {
    if (max === 0) return 0;
    return Math.round((current / max) * 100);
  };

  if (isLoading) return <Loading />;
  if (error) return <div className="p-4 text-red-500">Error loading tenants</div>;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Tenants</h1>
          <p className="text-gray-500 dark:text-gray-400 mt-1">Manage organizational tenants and quotas</p>
        </div>
        {isGlobalAdmin && (
          <Button onClick={() => setIsCreateModalOpen(true)} className="bg-brand-600 hover:bg-brand-700 text-white inline-flex items-center gap-2" variant="outline">
            <Plus className="w-4 h-4 mr-2" />
            Create Tenant
          </Button>
        )}
      </div>

      {/* Statistics Cards */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {/* Total Tenants */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Tenants</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{tenants?.length || 0}</h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">All registered tenants</p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
              <Building2 className="h-7 w-7 text-blue-600 dark:text-blue-400" />
            </div>
          </div>
        </div>

        {/* Active Tenants */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Active Tenants</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">
                {tenants?.filter((t: Tenant) => t.status === 'active').length || 0}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Currently active</p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
              <CheckCircle className="h-7 w-7 text-green-600 dark:text-green-400" />
            </div>
          </div>
        </div>

        {/* Total Storage Used */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Storage</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">
                {formatBytes(tenants?.reduce((acc: number, t: Tenant) => acc + (t.currentStorageBytes || 0), 0) || 0)}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Across all tenants</p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
              <HardDrive className="h-7 w-7 text-purple-600 dark:text-purple-400" />
            </div>
          </div>
        </div>

        {/* Total Buckets */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Total Buckets</p>
              <h3 className="text-3xl font-bold text-gray-900 dark:text-white">
                {tenants?.reduce((acc: number, t: Tenant) => acc + (t.currentBuckets || 0), 0) || 0}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Across all tenants</p>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-orange-50 dark:bg-orange-900/30">
              <Database className="h-7 w-7 text-orange-600 dark:text-orange-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Search */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6">
        <div className="relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 w-5 h-5" />
          <Input
            placeholder="Search tenants..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="pl-10 bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
          />
        </div>
      </div>

      {/* Tenants Table */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">All Tenants ({filteredTenants.length})</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Manage tenant quotas and configurations</p>
        </div>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Storage Usage</TableHead>
                <TableHead>Buckets</TableHead>
                <TableHead>Access Keys</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredTenants.map((tenant: Tenant) => (
                <TableRow key={tenant.id}>
                  <TableCell>
                    <div>
                      <div className="font-medium text-gray-900 dark:text-white">{tenant.displayName}</div>
                      <div className="text-sm text-gray-500 dark:text-gray-400">{tenant.name}</div>
                      {tenant.description && (
                        <div className="text-xs text-gray-400 dark:text-gray-500 mt-1">{tenant.description}</div>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      {tenant.status === 'active' ? (
                        <>
                          <CheckCircle className="w-4 h-4 text-green-500" />
                          <span className="text-sm text-green-600">Active</span>
                        </>
                      ) : (
                        <>
                          <XCircle className="w-4 h-4 text-gray-400" />
                          <span className="text-sm text-gray-500">Inactive</span>
                        </>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="space-y-1">
                      <div className="text-sm text-gray-900 dark:text-white">
                        {formatBytes(tenant.currentStorageBytes || 0)} / {formatBytes(tenant.maxStorageBytes)}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {formatBytes(tenant.maxStorageBytes - (tenant.currentStorageBytes || 0))} free
                      </div>
                      <div className="w-32 bg-gray-200 dark:bg-gray-700 rounded-full h-2">
                        <div
                          className={`h-2 rounded-full ${
                            getUsagePercentage(tenant.currentStorageBytes || 0, tenant.maxStorageBytes) > 90
                              ? 'bg-red-500 dark:bg-red-400'
                              : getUsagePercentage(tenant.currentStorageBytes || 0, tenant.maxStorageBytes) > 75
                              ? 'bg-yellow-500 dark:bg-yellow-400'
                              : 'bg-blue-500 dark:bg-blue-400'
                          }`}
                          style={{
                            width: `${Math.min(getUsagePercentage(tenant.currentStorageBytes || 0, tenant.maxStorageBytes), 100)}%`,
                          }}
                        />
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div>
                      <div className="text-sm font-medium text-gray-900 dark:text-white">
                        {tenant.currentBuckets || 0} / {tenant.maxBuckets}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {tenant.maxBuckets - (tenant.currentBuckets || 0)} available
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div>
                      <div className="text-sm font-medium text-gray-900 dark:text-white">
                        {tenant.currentAccessKeys || 0} / {tenant.maxAccessKeys}
                      </div>
                      <div className="text-xs text-gray-500 dark:text-gray-400">
                        {tenant.maxAccessKeys - (tenant.currentAccessKeys || 0)} available
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="text-sm text-gray-500 dark:text-gray-400">
                      {new Date(tenant.createdAt * 1000).toLocaleDateString()}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    {isGlobalAdmin && (
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setSelectedTenant(tenant);
                            setIsEditModalOpen(true);
                          }}
                        >
                          <Settings className="w-4 h-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(tenant)}
                        >
                          <Trash2 className="w-4 h-4" />
                        </Button>
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      {/* Create Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create New Tenant"
      >
        <form onSubmit={handleCreateTenant} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Tenant Name (ID)</label>
            <Input
              value={newTenant.name || ''}
              onChange={(e) => setNewTenant({ ...newTenant, name: e.target.value })}
              placeholder="acme-corp"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              required
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Lowercase, no spaces (used as identifier)</p>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Display Name</label>
            <Input
              value={newTenant.displayName || ''}
              onChange={(e) => setNewTenant({ ...newTenant, displayName: e.target.value })}
              placeholder="ACME Corporation"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Description</label>
            <Input
              value={newTenant.description || ''}
              onChange={(e) => setNewTenant({ ...newTenant, description: e.target.value })}
              placeholder="Main tenant for ACME Corp"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Access Keys</label>
              <Input
                type="number"
                value={newTenant.maxAccessKeys || 10}
                onChange={(e) => setNewTenant({ ...newTenant, maxAccessKeys: parseInt(e.target.value) })}
                min="1"
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Buckets</label>
              <Input
                type="number"
                value={newTenant.maxBuckets || 100}
                onChange={(e) => setNewTenant({ ...newTenant, maxBuckets: parseInt(e.target.value) })}
                min="1"
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Storage (GB)</label>
            <Input
              type="number"
              value={Math.round((newTenant.maxStorageBytes || 107374182400) / (1024 * 1024 * 1024))}
              onChange={(e) => setNewTenant({ ...newTenant, maxStorageBytes: parseInt(e.target.value) * 1024 * 1024 * 1024 })}
              min="1"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>

          <div className="flex justify-end gap-2 pt-4">
            <Button type="button" variant="outline" onClick={() => setIsCreateModalOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={createTenantMutation.isPending}>
              {createTenantMutation.isPending ? 'Creating...' : 'Create Tenant'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Edit Modal */}
      {selectedTenant && (
        <Modal
          isOpen={isEditModalOpen}
          onClose={() => {
            setIsEditModalOpen(false);
            setSelectedTenant(null);
          }}
          title={`Edit Tenant: ${selectedTenant.displayName}`}
        >
          <form onSubmit={handleUpdateTenant} className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Display Name</label>
              <Input
                value={selectedTenant.displayName || ''}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, displayName: e.target.value })}
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                required
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Description</label>
              <Input
                value={selectedTenant.description || ''}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, description: e.target.value })}
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Status</label>
              <select
                value={selectedTenant.status}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, status: e.target.value as 'active' | 'inactive' })}
                className="w-full border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md px-3 py-2"
              >
                <option value="active">Active</option>
                <option value="inactive">Inactive</option>
              </select>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Access Keys</label>
                <Input
                  type="number"
                  value={selectedTenant.maxAccessKeys}
                  onChange={(e) => setSelectedTenant({ ...selectedTenant, maxAccessKeys: parseInt(e.target.value) })}
                  min="1"
                  className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Buckets</label>
                <Input
                  type="number"
                  value={selectedTenant.maxBuckets}
                  onChange={(e) => setSelectedTenant({ ...selectedTenant, maxBuckets: parseInt(e.target.value) })}
                  min="1"
                  className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Max Storage (GB)</label>
              <Input
                type="number"
                value={Math.round(selectedTenant.maxStorageBytes / (1024 * 1024 * 1024))}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, maxStorageBytes: parseInt(e.target.value) * 1024 * 1024 * 1024 })}
                min="1"
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              />
            </div>

            <div className="flex justify-end gap-2 pt-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setIsEditModalOpen(false);
                  setSelectedTenant(null);
                }}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={updateTenantMutation.isPending}>
                {updateTenantMutation.isPending ? 'Updating...' : 'Update Tenant'}
              </Button>
            </div>
          </form>
        </Modal>
      )}
    </div>
  );
}
