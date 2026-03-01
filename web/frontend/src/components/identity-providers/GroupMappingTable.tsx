import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { ArrowLeft, Plus, Trash2, RefreshCw, FolderTree } from 'lucide-react';
import type { IdentityProvider, GroupMapping } from '@/types';
import ModalManager from '@/lib/modals';

interface GroupMappingTableProps {
  provider: IdentityProvider;
  onBack: () => void;
}

export function GroupMappingTable({ provider, onBack }: GroupMappingTableProps) {
  const { t } = useTranslation('idp');
  const queryClient = useQueryClient();
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [newMapping, setNewMapping] = useState({
    external_group: '',
    external_group_name: '',
    role: 'user',
    auto_sync: false,
  });

  const { data: mappings, isLoading } = useQuery({
    queryKey: ['group-mappings', provider.id],
    queryFn: () => APIClient.listGroupMappings(provider.id),
  });

  const createMutation = useMutation({
    mutationFn: (data: any) => APIClient.createGroupMapping(provider.id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings', provider.id] });
      setIsCreateOpen(false);
      setNewMapping({ external_group: '', external_group_name: '', role: 'user', auto_sync: false });
      ModalManager.success(t('created'), t('mappingCreatedSuccess'));
    },
    onError: (err: any) => {
      ModalManager.error(t('errorTitle'), err.message || t('failedToCreateMapping'));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (mapId: string) => APIClient.deleteGroupMapping(provider.id, mapId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings', provider.id] });
      ModalManager.success(t('deleted'), t('mappingDeletedSuccess'));
    },
  });

  const syncMutation = useMutation({
    mutationFn: (mapId: string) => APIClient.syncGroupMapping(provider.id, mapId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings', provider.id] });
      ModalManager.success(t('syncComplete'), t('syncResult', { imported: data.imported, updated: data.updated, removed: data.removed }));
    },
    onError: (err: any) => {
      ModalManager.error(t('syncFailed'), err.message || t('failedToSync'));
    },
  });

  const syncAllMutation = useMutation({
    mutationFn: () => APIClient.syncAllMappings(provider.id),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings', provider.id] });
      ModalManager.success(t('syncComplete'), data.message);
    },
    onError: (err: any) => {
      ModalManager.error(t('syncFailed'), err.message || t('failedToSyncAll'));
    },
  });

  const handleDelete = async (mapping: GroupMapping) => {
    const result = await ModalManager.confirmDelete(mapping.external_group_name || mapping.external_group, 'group mapping');
    if (result.isConfirmed) {
      deleteMutation.mutate(mapping.id);
    }
  };

  return (
    <div>
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <button onClick={onBack} className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800">
          <ArrowLeft className="h-5 w-5 text-gray-500" />
        </button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">{t('groupMappings')}</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">{provider.name} - {t('groupMappingsSubtitle')}</p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => syncAllMutation.mutate()} disabled={syncAllMutation.isPending}>
            <RefreshCw className={`h-4 w-4 mr-2 ${syncAllMutation.isPending ? 'animate-spin' : ''}`} />
            {t('syncAll')}
          </Button>
          <Button onClick={() => setIsCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-2" />
            {t('addMapping')}
          </Button>
        </div>
      </div>

      {/* Table */}
      {(mappings || []).length > 0 ? (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('externalGroup')}</TableHead>
                <TableHead>{t('roleColumn')}</TableHead>
                <TableHead>{t('autoSync')}</TableHead>
                <TableHead>{t('lastSynced')}</TableHead>
                <TableHead className="text-right">{t('actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(mappings || []).map((m) => (
                <TableRow key={m.id}>
                  <TableCell>
                    <div>
                      <p className="font-medium text-gray-900 dark:text-white">{m.external_group_name || m.external_group}</p>
                      {m.external_group_name && (
                        <p className="text-xs text-gray-500 dark:text-gray-400 truncate max-w-xs">{m.external_group}</p>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                      m.role === 'admin' ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300' :
                      m.role === 'readonly' ? 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300' :
                      'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                    }`}>
                      {m.role}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className={`text-xs ${m.auto_sync ? 'text-green-600' : 'text-gray-400'}`}>
                      {m.auto_sync ? t('enabled') : t('disabled')}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm text-gray-500 dark:text-gray-400">
                    {m.last_synced_at ? new Date(m.last_synced_at * 1000).toLocaleString() : t('never')}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => syncMutation.mutate(m.id)}
                        disabled={syncMutation.isPending}
                        className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500"
                        title={t('syncNow')}
                      >
                        <RefreshCw className={`h-4 w-4 ${syncMutation.isPending ? 'animate-spin' : ''}`} />
                      </button>
                      <button
                        onClick={() => handleDelete(m)}
                        className="p-1.5 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/30 text-gray-500 hover:text-red-600"
                        title={t('delete')}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <div className="text-center py-12 text-gray-500 dark:text-gray-400">
          <FolderTree className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm mb-4">{t('noGroupMappings')}</p>
          <Button onClick={() => setIsCreateOpen(true)}>
            <Plus className="h-4 w-4 mr-2" />
            {t('addMapping')}
          </Button>
        </div>
      )}

      {/* Create Modal */}
      {isCreateOpen && (
        <Modal isOpen onClose={() => setIsCreateOpen(false)} title={t('addGroupMapping')}>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('externalGroupLabel')}</label>
              <Input
                value={newMapping.external_group}
                onChange={(e) => setNewMapping({ ...newMapping, external_group: e.target.value })}
                placeholder="CN=Domain Admins,OU=Groups,DC=company,DC=com"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('displayName')}</label>
              <Input
                value={newMapping.external_group_name}
                onChange={(e) => setNewMapping({ ...newMapping, external_group_name: e.target.value })}
                placeholder="Domain Admins"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">{t('maxIOFSRole')}</label>
              <select
                value={newMapping.role}
                onChange={(e) => setNewMapping({ ...newMapping, role: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white"
              >
                <option value="user">{t('roleUserOption')}</option>
                <option value="admin">{t('roleAdminOption')}</option>
                <option value="readonly">{t('roleReadonlyOption')}</option>
              </select>
            </div>
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="auto_sync"
                checked={newMapping.auto_sync}
                onChange={(e) => setNewMapping({ ...newMapping, auto_sync: e.target.checked })}
                className="rounded border-gray-300"
              />
              <label htmlFor="auto_sync" className="text-sm text-gray-700 dark:text-gray-300">{t('enableAutoSync')}</label>
            </div>
          </div>
          <div className="flex justify-end gap-3 mt-6 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="secondary" onClick={() => setIsCreateOpen(false)}>{t('cancel')}</Button>
            <Button
              onClick={() => createMutation.mutate(newMapping)}
              disabled={createMutation.isPending || !newMapping.external_group}
            >
              {createMutation.isPending ? t('creating') : t('create')}
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
