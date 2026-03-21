import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { Users, Plus, Search, Trash2, Settings } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Group, CreateGroupRequest } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { EmptyState } from '@/components/ui/EmptyState';
import { formatRelativeTime } from '@/lib/utils';

export default function GroupsPage() {
  const { t } = useTranslation('groups');
  const navigate = useNavigate();
  const { isGlobalAdmin, isTenantAdmin } = useCurrentUser();
  const [searchTerm, setSearchTerm] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [newGroup, setNewGroup] = useState<Partial<CreateGroupRequest>>({});
  const queryClient = useQueryClient();

  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  const { data: groups = [], isLoading } = useQuery({
    queryKey: ['groups'],
    queryFn: () => APIClient.listGroups(),
    enabled: isAnyAdmin,
  });

  const { data: tenants = [] } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
    enabled: isGlobalAdmin,
  });

  const createMutation = useMutation({
    mutationFn: (data: CreateGroupRequest) => APIClient.createGroup(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups'] });
      setIsCreateModalOpen(false);
      setNewGroup({});
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (groupId: string) => APIClient.deleteGroup(groupId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });

  const handleDelete = (group: Group) => {
    ModalManager.confirmDelete(group.name, 'group', async () => {
      await deleteMutation.mutateAsync(group.id);
    });
  };

  const handleCreate = () => {
    if (!newGroup.name?.trim()) return;
    createMutation.mutate(newGroup as CreateGroupRequest);
  };

  const filtered = groups.filter(
    (g) =>
      g.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      (g.displayName || '').toLowerCase().includes(searchTerm.toLowerCase())
  );

  if (!isAnyAdmin) {
    return (
      <EmptyState
        icon={Users}
        title={t('accessDenied')}
        description={t('accessDeniedDesc')}
        showAction={false}
      />
    );
  }

  if (isLoading) return <Loading />;

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
            <Users className="w-6 h-6" />
            {t('title')}
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {t('subtitle')}
          </p>
        </div>
        <Button onClick={() => setIsCreateModalOpen(true)} className="flex items-center gap-2">
          <Plus className="w-4 h-4" />
          {t('newGroup')}
        </Button>
      </div>

      {/* Search */}
      <div className="relative w-full max-w-sm">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
        <Input
          placeholder={t('searchPlaceholder')}
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          className="pl-9"
        />
      </div>

      {/* Table */}
      {filtered.length === 0 ? (
        <EmptyState
          icon={Users}
          title={t('noGroupsFound')}
          description={searchTerm ? t('noGroupsSearch') : t('noGroupsEmpty')}
          actionLabel={t('newGroup')}
          onAction={() => setIsCreateModalOpen(true)}
          showAction={!searchTerm}
        />
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('colName')}</TableHead>
                <TableHead>{t('colDisplayName')}</TableHead>
                {isGlobalAdmin && <TableHead>{t('colScope')}</TableHead>}
                <TableHead>{t('colMembers')}</TableHead>
                <TableHead>{t('colCreated')}</TableHead>
                <TableHead className="text-right">{t('colActions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((group) => (
                <TableRow key={group.id} className="cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/50"
                  onClick={() => navigate(`/groups/${group.id}`)}>
                  <TableCell className="font-medium">{group.name}</TableCell>
                  <TableCell className="text-gray-500 dark:text-gray-400">
                    {group.displayName || '—'}
                  </TableCell>
                  {isGlobalAdmin && (
                    <TableCell>
                      {group.tenantId ? (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300">
                          {tenants.find(t => t.id === group.tenantId)?.displayName || group.tenantId}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                          {t('scopeGlobal')}
                        </span>
                      )}
                    </TableCell>
                  )}
                  <TableCell>
                    <span className="inline-flex items-center gap-1 text-sm">
                      <Users className="w-3.5 h-3.5 text-gray-400" />
                      {group.memberCount ?? 0}
                    </span>
                  </TableCell>
                  <TableCell className="text-gray-500 dark:text-gray-400 text-sm">
                    {formatRelativeTime(group.createdAt * 1000)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-2" onClick={(e) => e.stopPropagation()}>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => navigate(`/groups/${group.id}`)}
                      >
                        <Settings className="w-4 h-4" />
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => handleDelete(group)}
                        className="text-red-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20"
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Create Group Modal */}
      <Modal
        isOpen={isCreateModalOpen}
        onClose={() => { setIsCreateModalOpen(false); setNewGroup({}); }}
        title={t('createModalTitle')}
      >
        <div className="space-y-4">
          {/* Tenant selector — global admins only */}
          {isGlobalAdmin && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                {t('scopeLabel')}
              </label>
              <select
                className="w-full rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white px-3 py-2 text-sm"
                value={newGroup.tenantId || ''}
                onChange={(e) => setNewGroup({ ...newGroup, tenantId: e.target.value || undefined })}
              >
                <option value="">{t('scopeGlobalOption')}</option>
                {tenants.map((tenant) => (
                  <option key={tenant.id} value={tenant.id}>
                    {tenant.displayName || tenant.name}
                  </option>
                ))}
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {t('scopeHint')}
              </p>
            </div>
          )}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              {t('nameLabel')} <span className="text-red-500">*</span>
            </label>
            <Input
              placeholder={t('namePlaceholder')}
              value={newGroup.name || ''}
              onChange={(e) => setNewGroup({ ...newGroup, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              {t('displayNameLabel')}
            </label>
            <Input
              placeholder={t('displayNamePlaceholder')}
              value={newGroup.displayName || ''}
              onChange={(e) => setNewGroup({ ...newGroup, displayName: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              {t('descriptionLabel')}
            </label>
            <Input
              placeholder={t('descriptionPlaceholder')}
              value={newGroup.description || ''}
              onChange={(e) => setNewGroup({ ...newGroup, description: e.target.value })}
            />
          </div>
          <div className="flex justify-end gap-3 pt-2">
            <Button variant="secondary" onClick={() => { setIsCreateModalOpen(false); setNewGroup({}); }}>
              {t('cancel')}
            </Button>
            <Button
              onClick={handleCreate}
              disabled={!newGroup.name?.trim() || createMutation.isPending}
            >
              {createMutation.isPending ? t('creating') : t('createGroup')}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
