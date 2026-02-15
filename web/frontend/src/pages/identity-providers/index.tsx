import { useState } from 'react';
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
import type { IdentityProvider } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { CreateIDPModal } from '@/components/identity-providers/CreateIDPModal';
import { LDAPBrowser } from '@/components/identity-providers/LDAPBrowser';
import { GroupMappingTable } from '@/components/identity-providers/GroupMappingTable';

export default function IdentityProvidersPage() {
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

  const deleteMutation = useMutation({
    mutationFn: (id: string) => APIClient.deleteIDP(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identity-providers'] });
      ModalManager.success('Deleted', 'Identity provider deleted successfully');
    },
    onError: (err: any) => {
      ModalManager.error('Error', err.message || 'Failed to delete provider');
    },
  });

  const testMutation = useMutation({
    mutationFn: (id: string) => APIClient.testIDPConnection(id),
    onSuccess: (data) => {
      if (data.success) {
        ModalManager.success('Connection OK', data.message || 'Connection test passed');
      } else {
        ModalManager.error('Connection Failed', data.message || 'Connection test failed');
      }
    },
    onError: (err: any) => {
      ModalManager.error('Connection Failed', err.message || 'Connection test failed');
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
        <p className="text-gray-500 dark:text-gray-400">You do not have permission to view this page.</p>
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
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Identity Providers</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Manage LDAP/AD and OAuth2/SSO integrations for user authentication
        </p>
      </div>

      {/* Actions */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-6">
        <div className="relative w-full sm:w-72">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search providers..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            className="w-full pl-10 pr-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <Button onClick={() => setIsCreateOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Add Provider
        </Button>
      </div>

      {/* Content */}
      {isLoading ? (
        <Loading />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Shield}
          title="No Identity Providers"
          description="Add an LDAP/AD directory or OAuth2/SSO provider to enable external authentication."
          actionLabel="Add Provider"
          onAction={() => setIsCreateOpen(true)}
          showAction
        />
      ) : (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Tenant</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((idp) => (
                <TableRow key={idp.id}>
                  <TableCell className="font-medium text-gray-900 dark:text-white">
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
                        : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'
                    }`}>
                      {idp.status}
                    </span>
                  </TableCell>
                  <TableCell className="text-sm text-gray-500 dark:text-gray-400">
                    {idp.tenant_id || 'Global'}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => testMutation.mutate(idp.id)}
                        disabled={testMutation.isPending}
                        className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400"
                        title="Test Connection"
                      >
                        <Plug className="h-4 w-4" />
                      </button>
                      {idp.type === 'ldap' && (
                        <button
                          onClick={() => setBrowsingIDP(idp)}
                          className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400"
                          title="Browse Users"
                        >
                          <UsersIcon className="h-4 w-4" />
                        </button>
                      )}
                      <button
                        onClick={() => setMappingIDP(idp)}
                        className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400"
                        title="Group Mappings"
                      >
                        <FolderTree className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => setEditingIDP(idp)}
                        className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400"
                        title="Edit"
                      >
                        <Settings className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => handleDelete(idp)}
                        className="p-1.5 rounded-lg hover:bg-red-100 dark:hover:bg-red-900/30 text-gray-500 dark:text-gray-400 hover:text-red-600"
                        title="Delete"
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
