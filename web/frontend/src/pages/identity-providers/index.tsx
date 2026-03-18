import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { EmptyState } from '@/components/ui/EmptyState';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import {
  Plus,
  Trash2,
  Settings,
  Search,
  Shield,
  Globe,
  Plug,
  Users as UsersIcon,
  FolderTree,
} from 'lucide-react';
import type { IdentityProvider, Tenant } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { CreateIDPModal } from '@/components/identity-providers/CreateIDPModal';
import { LDAPBrowser } from '@/components/identity-providers/LDAPBrowser';
import { GroupMappingTable } from '@/components/identity-providers/GroupMappingTable';

export default function IdentityProvidersPage() {
  const { t } = useTranslation('idp');
  const { isGlobalAdmin, isTenantAdmin } = useCurrentUser();
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;
  const queryClient = useQueryClient();

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingIDP, setEditingIDP] = useState<IdentityProvider | null>(null);
  const [browsingIDP, setBrowsingIDP] = useState<IdentityProvider | null>(null);
  const [mappingIDP, setMappingIDP] = useState<IdentityProvider | null>(null);
  const [searchTerm, setSearchTerm] = useState('');

  const { data: idps, isLoading } = useQuery({
    queryKey: ['identity-providers'],
    queryFn: APIClient.listIDPs,
    enabled: isAnyAdmin,
  });

  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
    enabled: isGlobalAdmin,
  });

  const tenantMap = new Map((tenants || []).map((t: Tenant) => [t.id, t.displayName || t.name]));
  const getTenantName = (tenantId?: string) => {
    if (!tenantId) return t('global');
    return tenantMap.get(tenantId) || tenantId;
  };

  const deleteMutation = useMutation({
    mutationFn: (id: string) => APIClient.deleteIDP(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity-providers'] });
      ModalManager.success(t('deleted'), t('providerDeletedSuccess'));
    },
    onError: (err: any) => {
      ModalManager.error(t('errorTitle'), err.message || t('failedToDelete'));
    },
  });

  const testMutation = useMutation({
    mutationFn: (id: string) => APIClient.testIDPConnection(id),
    onSuccess: (data) => {
      if (data.success) {
        ModalManager.success(t('connectionOK'), data.message || t('connectionPassed'));
      } else {
        ModalManager.error(t('connectionFailed'), data.message || t('connectionTestFailed'));
      }
    },
    onError: (err: any) => {
      ModalManager.error(t('connectionFailed'), err.message || t('connectionTestFailed'));
    },
  });

  const handleDelete = async (idp: IdentityProvider) => {
    const result = await ModalManager.confirmDelete(idp.name, 'identity provider');
    if (result.isConfirmed) {
      deleteMutation.mutate(idp.id);
    }
  };

  const filtered = (idps || []).filter(
    (idp) =>
      idp.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      idp.type.toLowerCase().includes(searchTerm.toLowerCase())
  );

  if (!isAnyAdmin) {
    return (
      <div className="p-6">
        <p className="text-sm text-muted-foreground mt-1">{t('noPermission')}</p>
      </div>
    );
  }

  // Show LDAP browser if browsing
  if (browsingIDP) {
    return (
      <LDAPBrowser
        provider={browsingIDP}
        onBack={() => setBrowsingIDP(null)}
      />
    );
  }

  // Show group mappings if mapping
  if (mappingIDP) {
    return (
      <GroupMappingTable
        provider={mappingIDP}
        onBack={() => setMappingIDP(null)}
      />
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-foreground">{t('pageTitle')}</h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t('pageSubtitle')}
        </p>
      </div>

      {/* Actions */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-6">
        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder={t('searchProviders')}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="w-full pl-10 pr-4 py-2 border border-border rounded-lg bg-card text-sm text-foreground focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Plus className="h-4 w-4" />
          {t('addProvider')}
        </Button>
      </div>

      {/* Content */}
      {isLoading ? (
        <Loading />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Shield}
          title={t('noProviders')}
          description={t('noProvidersDesc')}
          actionLabel={t('addProvider')}
          onAction={() => setIsCreateOpen(true)}
          showAction
        />
      ) : (
        <div className="bg-card rounded-xl border border-border shadow-sm overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('name')}</TableHead>
                <TableHead>{t('type')}</TableHead>
                <TableHead>{t('status')}</TableHead>
                <TableHead>{t('tenant')}</TableHead>
                <TableHead className="text-right">{t('actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((idp) => (
                <TableRow key={idp.id}>
                  <TableCell className="font-medium text-foreground">
                    {idp.name}
                  </TableCell>
                  <TableCell>
                    <span className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium ${
                      idp.type === 'ldap'
                        ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                        : 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
                    }`}>
                      {idp.type === 'ldap' ? (
                        <><Globe className="h-3 w-3" /> LDAP/AD</>
                      ) : (
                        <><Shield className="h-3 w-3" /> OAuth2</>
                      )}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                      idp.status === 'active'
                        ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                        : idp.status === 'testing'
                        ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300'
                        : 'bg-secondary text-muted-foreground'
                    }`}>
                      {idp.status === 'active' ? t('statusActive') : idp.status === 'testing' ? t('statusTesting') : t('statusInactive')}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {getTenantName(idp.tenantId)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => testMutation.mutate(idp.id)}
                        disabled={testMutation.isPending}
                        className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground"
                        title={t('testConnection')}
                      >
                        <Plug className="h-4 w-4" />
                      </button>
                      {idp.type === 'ldap' && (
                        <button
                          onClick={() => setBrowsingIDP(idp)}
                          className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground"
                          title={t('browseUsers')}
                        >
                          <UsersIcon className="h-4 w-4" />
                        </button>
                      )}
                      <button
                        onClick={() => setMappingIDP(idp)}
                        className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground"
                        title={t('groupMappings')}
                      >
                        <FolderTree className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => setEditingIDP(idp)}
                        className="p-1.5 rounded-lg hover:bg-secondary text-muted-foreground"
                        title={t('edit')}
                      >
                        <Settings className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(idp)}
                        className="p-1.5 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/30 text-muted-foreground hover:text-red-600"
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
      )}

      {/* Create/Edit Modal */}
      {(isCreateOpen || editingIDP) && (
        <CreateIDPModal
          idp={editingIDP}
          onClose={() => {
            setIsCreateOpen(false);
            setEditingIDP(null);
          }}
          onSuccess={() => {
            setIsCreateOpen(false);
            setEditingIDP(null);
            queryClient.invalidateQueries({ queryKey: ['identity-providers'] });
          }}
        />
      )}
    </div>
  );
}
