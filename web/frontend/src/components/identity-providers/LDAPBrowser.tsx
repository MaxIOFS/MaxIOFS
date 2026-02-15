import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { ArrowLeft, Search, Download, Check, Users } from 'lucide-react';
import type { IdentityProvider, ExternalUser } from '@/types';
import ModalManager from '@/lib/modals';

interface LDAPBrowserProps {
  provider: IdentityProvider;
  onBack: () => void;
}

export function LDAPBrowser({ provider, onBack }: LDAPBrowserProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<ExternalUser[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [importRole, setImportRole] = useState('user');
  const [hasSearched, setHasSearched] = useState(false);

  const searchMutation = useMutation({
    mutationFn: () => APIClient.idpSearchUsers(provider.id, query),
    onSuccess: (data) => {
      setResults(data);
      setHasSearched(true);
    },
    onError: (err: any) => {
      ModalManager.error('Search Failed', err.message || 'Failed to search users');
    },
  });

  const importMutation = useMutation({
    mutationFn: () => {
      const users = results
        .filter((u) => selected.has(u.external_id))
        .map((u) => ({ external_id: u.external_id, username: u.username || u.email }));
      return APIClient.idpImportUsers(provider.id, users, importRole);
    },
    onSuccess: (data) => {
      const msg = `Imported: ${data.imported}, Skipped: ${data.skipped}${
        data.errors?.length ? `, Errors: ${data.errors.length}` : ''
      }`;
      ModalManager.success('Import Complete', msg);
      setSelected(new Set());
    },
    onError: (err: any) => {
      ModalManager.error('Import Failed', err.message || 'Failed to import users');
    },
  });

  const toggleSelect = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  const toggleAll = () => {
    if (selected.size === results.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(results.map((u) => u.external_id)));
    }
  };

  return (
    <div>
      {/* Header */}
      <div className="flex items-center gap-3 mb-6">
        <button onClick={onBack} className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800">
          <ArrowLeft className="h-5 w-5 text-gray-500" />
        </button>
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Browse Users</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">{provider.name} - Search and import users from LDAP directory</p>
        </div>
      </div>

      {/* Search */}
      <div className="flex gap-3 mb-6">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && searchMutation.mutate()}
            placeholder="Search by name, email, or username..."
            className="w-full pl-10 pr-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <Button onClick={() => searchMutation.mutate()} disabled={searchMutation.isPending || !query}>
          {searchMutation.isPending ? 'Searching...' : 'Search'}
        </Button>
      </div>

      {/* Import bar */}
      {selected.size > 0 && (
        <div className="flex items-center gap-4 mb-4 p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
          <span className="text-sm font-medium text-blue-700 dark:text-blue-300">
            {selected.size} user{selected.size > 1 ? 's' : ''} selected
          </span>
          <select
            value={importRole}
            onChange={(e) => setImportRole(e.target.value)}
            className="px-2 py-1 border border-blue-300 dark:border-blue-700 rounded bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white"
          >
            <option value="user">Role: User</option>
            <option value="admin">Role: Admin</option>
            <option value="readonly">Role: Read-only</option>
          </select>
          <Button size="sm" onClick={() => importMutation.mutate()} disabled={importMutation.isPending}>
            <Download className="h-3.5 w-3.5 mr-1.5" />
            {importMutation.isPending ? 'Importing...' : 'Import Selected'}
          </Button>
        </div>
      )}

      {/* Results */}
      {results.length > 0 ? (
        <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <input
                    type="checkbox"
                    checked={selected.size === results.length && results.length > 0}
                    onChange={toggleAll}
                    className="rounded border-gray-300"
                  />
                </TableHead>
                <TableHead>Username</TableHead>
                <TableHead>Email</TableHead>
                <TableHead>Display Name</TableHead>
                <TableHead>Groups</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {results.map((user) => (
                <TableRow key={user.external_id} className={selected.has(user.external_id) ? 'bg-blue-50/50 dark:bg-blue-900/10' : ''}>
                  <TableCell>
                    <input
                      type="checkbox"
                      checked={selected.has(user.external_id)}
                      onChange={() => toggleSelect(user.external_id)}
                      className="rounded border-gray-300"
                    />
                  </TableCell>
                  <TableCell className="font-medium text-gray-900 dark:text-white">{user.username}</TableCell>
                  <TableCell className="text-sm text-gray-500 dark:text-gray-400">{user.email}</TableCell>
                  <TableCell className="text-sm text-gray-500 dark:text-gray-400">{user.display_name}</TableCell>
                  <TableCell className="text-sm text-gray-500 dark:text-gray-400">
                    {(user.groups || []).length > 0 ? (
                      <span className="inline-flex items-center gap-1 text-xs">
                        <Users className="h-3 w-3" /> {user.groups.length}
                      </span>
                    ) : '-'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : hasSearched ? (
        <div className="text-center py-12 text-gray-500 dark:text-gray-400">
          <Users className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">No users found matching your search.</p>
        </div>
      ) : (
        <div className="text-center py-12 text-gray-500 dark:text-gray-400">
          <Search className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">Enter a search query to find users in the LDAP directory.</p>
        </div>
      )}
    </div>
  );
}
