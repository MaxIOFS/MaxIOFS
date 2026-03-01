/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
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
import { EmptyState } from '@/components/ui/EmptyState';
import {
  Key,
  Trash2,
  Calendar,
  User,
  Search,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AccessKey } from '@/types';
import ModalManager from '@/lib/modals';

export default function AccessKeysPage() {
  const { t } = useTranslation('users');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchTerm, setSearchTerm] = useState('');

  const { data: accessKeys, isLoading } = useQuery({
    queryKey: ['accessKeys'],
    queryFn: () => APIClient.getAccessKeys(),
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  const deleteAccessKeyMutation = useMutation({
    mutationFn: ({ userId, keyId }: { userId: string; keyId: string }) =>
      APIClient.deleteAccessKey(userId, keyId),
    onSuccess: async (_, variables) => {
      ModalManager.close();

      // Update cache immediately by removing the deleted key
      queryClient.setQueryData(['accessKeys'], (oldData: AccessKey[] | undefined) => {
        if (!oldData) return [];
        return oldData.filter(key => key.id !== variables.keyId);
      });

      // Also invalidate users query to update key counts
      queryClient.invalidateQueries({ queryKey: ['users'] });

      // Force refetch to ensure we have the latest data from server
      await queryClient.refetchQueries({ queryKey: ['accessKeys'] });

      ModalManager.toast('success', t('accessKeyDeletedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.close();
      ModalManager.apiError(error);
    },
  });

  const handleDeleteKey = async (key: AccessKey) => {
    const user = users?.find((u: any) => u.id === key.userId);

    try {
      const result = await ModalManager.fire({
        icon: 'warning',
        title: t('deleteAccessKeyTitle'),
        html: `<p>${t('deleteAccessKeyMessage', { keyId: key.id, username: user?.username || t('unknownUser') })}</p>
               <p class="text-red-600 mt-2">${t('actionCannotBeUndone')}</p>`,
        showCancelButton: true,
        confirmButtonText: t('yesDelete'),
        cancelButtonText: t('cancel'),
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        ModalManager.loading(t('deletingAccessKey'), t('deletingAccessKeyMessage', { keyId: key.id }));
        deleteAccessKeyMutation.mutate({ userId: key.userId, keyId: key.id });
      }
    } catch (error) {
      ModalManager.close();
      ModalManager.apiError(error);
    }
  };

  const formatDate = (timestamp: number | string) => {
    const date = typeof timestamp === 'string'
      ? new Date(timestamp)
      : new Date(timestamp * 1000);
    return date.toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const getUserName = (userId: string) => {
    const user = users?.find((u: any) => u.id === userId);
    return user?.username || t('unknownUser');
  };

  const allKeys = accessKeys || [];
  const filteredKeys = allKeys.filter((key: AccessKey) => {
    if (!searchTerm) return true;
    const term = searchTerm.toLowerCase();
    return (
      key.id.toLowerCase().includes(term) ||
      getUserName(key.userId).toLowerCase().includes(term)
    );
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">{t('accessKeysTitle')}</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            {t('manageAccessKeys')}
          </p>
        </div>
      </div>

      {/* Access Keys Table */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white">{t('accessKeysCount', { count: filteredKeys.length })}</h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">{t('allAccessKeysDesc')}</p>

          {/* Search */}
          <div className="mt-4 relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400 dark:text-gray-500 h-5 w-5" />
            <Input
              placeholder={t('searchByKeyOrUser')}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-10 bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>
        </div>
        <div className="overflow-x-auto">
          {filteredKeys.length === 0 ? (
            <EmptyState
              icon={Key}
              title={searchTerm ? t('noResultsFound') : t('noAccessKeysFound')}
              description={searchTerm ? t('noAccessKeysTrySearch') : t('accessKeysWillAppear')}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('accessKeyId')}</TableHead>
                  <TableHead>{t('user')}</TableHead>
                  <TableHead>{t('created')}</TableHead>
                  <TableHead>{t('lastUsed')}</TableHead>
                  <TableHead className="text-right">{t('actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredKeys.map((key: AccessKey) => (
                  <TableRow key={key.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Key className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                        <code className="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{key.id}</code>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <User className="h-4 w-4 text-gray-400 dark:text-gray-500" />
                        <span>{getUserName(key.userId)}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                        <Calendar className="h-3 w-3" />
                        {formatDate(key.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-gray-500 dark:text-gray-400">
                        {key.lastUsed ? (
                          <>
                            <Calendar className="h-3 w-3" />
                            {formatDate(key.lastUsed)}
                          </>
                        ) : (
                          <span className="text-gray-400 dark:text-gray-500">{t('never')}</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => navigate(`/users/${key.userId}`)}
                          title={t('viewUserDetails')}
                        >
                          <User className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteKey(key)}
                          disabled={deleteAccessKeyMutation.isPending}
                          title={t('deleteAccessKey')}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
    </div>
  );
}
