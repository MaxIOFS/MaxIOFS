'use client';

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

export default function TenantsPage() {
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

  const filteredTenants = tenants?.filter((tenant: Tenant) =>
    tenant.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
    tenant.displayName?.toLowerCase().includes(searchTerm.toLowerCase())
  ) || [];

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

  if (isLoading) return <Loading fullScreen />;
  if (error) return <div className="p-4 text-red-500">Error loading tenants</div>;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Tenants</h1>
          <p className="text-gray-500 mt-1">Manage organizational tenants and quotas</p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)}>
          <Plus className="w-4 h-4 mr-2" />
          Create Tenant
        </Button>
      </div>

      {/* Search */}
      <Card>
        <CardContent className="pt-6">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400 w-5 h-5" />
            <Input
              placeholder="Search tenants..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-10"
            />
          </div>
        </CardContent>
      </Card>

      {/* Tenants Table */}
      <Card>
        <CardHeader>
          <CardTitle>All Tenants ({filteredTenants.length})</CardTitle>
        </CardHeader>
        <CardContent>
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
                      <div className="font-medium">{tenant.displayName}</div>
                      <div className="text-sm text-gray-500">{tenant.name}</div>
                      {tenant.description && (
                        <div className="text-xs text-gray-400 mt-1">{tenant.description}</div>
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
                      <div className="text-sm">
                        {formatBytes(tenant.currentStorageBytes || 0)} / {formatBytes(tenant.maxStorageBytes)}
                      </div>
                      <div className="text-xs text-gray-500">
                        {formatBytes(tenant.maxStorageBytes - (tenant.currentStorageBytes || 0))} free
                      </div>
                      <div className="w-32 bg-gray-200 rounded-full h-2">
                        <div
                          className={`h-2 rounded-full ${
                            getUsagePercentage(tenant.currentStorageBytes || 0, tenant.maxStorageBytes) > 90
                              ? 'bg-red-500'
                              : getUsagePercentage(tenant.currentStorageBytes || 0, tenant.maxStorageBytes) > 75
                              ? 'bg-yellow-500'
                              : 'bg-blue-500'
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
                      <div className="text-sm font-medium">
                        {tenant.currentBuckets || 0} / {tenant.maxBuckets}
                      </div>
                      <div className="text-xs text-gray-500">
                        {tenant.maxBuckets - (tenant.currentBuckets || 0)} available
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div>
                      <div className="text-sm font-medium">
                        Limit: {tenant.maxAccessKeys}
                      </div>
                      <div className="text-xs text-gray-500">
                        per tenant
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="text-sm text-gray-500">
                      {new Date(tenant.createdAt * 1000).toLocaleDateString()}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
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
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* Create Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => setIsCreateModalOpen(false)}
        title="Create New Tenant"
      >
        <form onSubmit={handleCreateTenant} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-1">Tenant Name (ID)</label>
            <Input
              value={newTenant.name || ''}
              onChange={(e) => setNewTenant({ ...newTenant, name: e.target.value })}
              placeholder="acme-corp"
              required
            />
            <p className="text-xs text-gray-500 mt-1">Lowercase, no spaces (used as identifier)</p>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Display Name</label>
            <Input
              value={newTenant.displayName || ''}
              onChange={(e) => setNewTenant({ ...newTenant, displayName: e.target.value })}
              placeholder="ACME Corporation"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Description</label>
            <Input
              value={newTenant.description || ''}
              onChange={(e) => setNewTenant({ ...newTenant, description: e.target.value })}
              placeholder="Main tenant for ACME Corp"
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Max Access Keys</label>
              <Input
                type="number"
                value={newTenant.maxAccessKeys || 10}
                onChange={(e) => setNewTenant({ ...newTenant, maxAccessKeys: parseInt(e.target.value) })}
                min="1"
              />
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Max Buckets</label>
              <Input
                type="number"
                value={newTenant.maxBuckets || 100}
                onChange={(e) => setNewTenant({ ...newTenant, maxBuckets: parseInt(e.target.value) })}
                min="1"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Max Storage (GB)</label>
            <Input
              type="number"
              value={Math.round((newTenant.maxStorageBytes || 107374182400) / (1024 * 1024 * 1024))}
              onChange={(e) => setNewTenant({ ...newTenant, maxStorageBytes: parseInt(e.target.value) * 1024 * 1024 * 1024 })}
              min="1"
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
              <label className="block text-sm font-medium mb-1">Display Name</label>
              <Input
                value={selectedTenant.displayName || ''}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, displayName: e.target.value })}
                required
              />
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Description</label>
              <Input
                value={selectedTenant.description || ''}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, description: e.target.value })}
              />
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Status</label>
              <select
                value={selectedTenant.status}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, status: e.target.value as 'active' | 'inactive' })}
                className="w-full border border-gray-300 rounded-md px-3 py-2"
              >
                <option value="active">Active</option>
                <option value="inactive">Inactive</option>
              </select>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-1">Max Access Keys</label>
                <Input
                  type="number"
                  value={selectedTenant.maxAccessKeys}
                  onChange={(e) => setSelectedTenant({ ...selectedTenant, maxAccessKeys: parseInt(e.target.value) })}
                  min="1"
                />
              </div>

              <div>
                <label className="block text-sm font-medium mb-1">Max Buckets</label>
                <Input
                  type="number"
                  value={selectedTenant.maxBuckets}
                  onChange={(e) => setSelectedTenant({ ...selectedTenant, maxBuckets: parseInt(e.target.value) })}
                  min="1"
                />
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium mb-1">Max Storage (GB)</label>
              <Input
                type="number"
                value={Math.round(selectedTenant.maxStorageBytes / (1024 * 1024 * 1024))}
                onChange={(e) => setSelectedTenant({ ...selectedTenant, maxStorageBytes: parseInt(e.target.value) * 1024 * 1024 * 1024 })}
                min="1"
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
