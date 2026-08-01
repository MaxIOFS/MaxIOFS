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
  Clock,
  Timer,
  Shield,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AccessKey, STSSession } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';

export default function AccessKeysPage() {
  const { t } = useTranslation('users');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchTerm, setSearchTerm] = useState('');
  const { isGlobalAdmin, isTenantAdmin, user: currentUser, isLoading: currentUserLoading } = useCurrentUser();
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  const { data: accessKeys, isLoading } = useQuery({
    queryKey: ['accessKeys'],
    queryFn: () => APIClient.getAccessKeys(),
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: isAnyAdmin,
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
    const username = user?.username || (currentUser?.id === key.userId ? currentUser.username : t('unknownUser'));

    try {
      const result = await ModalManager.fire({
        icon: 'warning',
        title: t('deleteAccessKeyTitle'),
        html: `<p>${t('deleteAccessKeyMessage', { keyId: key.id, username })}</p>
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

  // --- STS temporary credentials ---

  const [stsDuration, setStsDuration] = useState(3600);
  const [showAllSessions, setShowAllSessions] = useState(false);
  const [stsPolicyOpen, setStsPolicyOpen] = useState(false);
  const [stsPolicy, setStsPolicy] = useState('');

  // Policy documents are user-written and rendered inside modal HTML.
  const escapeHtml = (value: string) =>
    value.replace(/[&<>"']/g, (c) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string
    );

  const stsPolicyTemplate = JSON.stringify(
    {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:GetObject', 's3:ListBucket'],
          Resource: ['arn:aws:s3:::my-bucket', 'arn:aws:s3:::my-bucket/*'],
        },
      ],
    },
    null,
    2
  );

  const { data: stsSessions } = useQuery({
    queryKey: ['stsSessions', showAllSessions],
    queryFn: () => APIClient.getSTSSessions(showAllSessions),
    // Sessions expire on their own; refresh so the list doesn't show stale ones.
    refetchInterval: 60000,
  });

  const issueSTSMutation = useMutation({
    mutationFn: () => APIClient.issueSTSSession(stsDuration, stsPolicyOpen ? stsPolicy : ''),
    onSuccess: (creds) => {
      queryClient.invalidateQueries({ queryKey: ['stsSessions'] });
      const row = (label: string, value: string) =>
        `<div class="mt-3 text-left">
           <div class="text-xs uppercase tracking-wide opacity-70">${label}</div>
           <code class="block mt-1 p-2 rounded bg-gray-100 dark:bg-gray-800 break-all text-sm">${value}</code>
         </div>`;
      ModalManager.fire({
        icon: 'success',
        title: t('stsIssuedTitle'),
        html: `<p class="text-amber-600">${t('stsIssuedWarning')}</p>
               ${row(t('stsAccessKeyId'), creds.accessKeyId)}
               ${row(t('stsSecretKey'), creds.secretAccessKey)}
               ${row(t('stsSessionToken'), creds.sessionToken)}`,
        width: 640,
      });
    },
    onError: (error: Error) => ModalManager.apiError(error),
  });

  const revokeSTSMutation = useMutation({
    mutationFn: (keyId: string) => APIClient.revokeSTSSession(keyId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['stsSessions'] });
      ModalManager.toast('success', t('stsRevoked'));
    },
    onError: (error: Error) => ModalManager.apiError(error),
  });

  const handleViewPolicy = (session: STSSession) => {
    let pretty = session.sessionPolicy || '';
    try {
      pretty = JSON.stringify(JSON.parse(pretty), null, 2);
    } catch {
      // Show it verbatim if it is not parseable — that is worth seeing.
    }
    ModalManager.fire({
      title: t('stsPolicyTitle'),
      html: `<code class="text-sm">${session.accessKeyId}</code>
             <pre class="mt-3 p-3 rounded bg-gray-100 dark:bg-gray-800 text-left text-xs overflow-x-auto">${escapeHtml(pretty)}</pre>`,
      width: 640,
    });
  };

  const handleRevokeSession = async (session: STSSession) => {
    const result = await ModalManager.fire({
      icon: 'warning',
      title: t('stsRevokeTitle'),
      html: `<code class="text-sm">${session.accessKeyId}</code>
             <p class="mt-2">${t('stsRevokeMessage')}</p>`,
      showCancelButton: true,
      confirmButtonText: t('stsRevoke'),
      cancelButtonText: t('cancel'),
      confirmButtonColor: '#dc2626',
    });
    if (result.isConfirmed) {
      revokeSTSMutation.mutate(session.accessKeyId);
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
    if (user?.username) return user.username;
    if (currentUser?.id === userId) return currentUser.username;
    return t('unknownUser');
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

  if (currentUserLoading || isLoading) {
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
          <h1 className="text-2xl font-bold text-foreground">{t('accessKeysTitle')}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t('manageAccessKeys')}
          </p>
        </div>
      </div>

      {/* Access Keys Table */}
      <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-border">
          <h3 className="text-lg font-semibold text-foreground">{t('accessKeysCount', { count: filteredKeys.length })}</h3>
          <p className="text-sm text-muted-foreground mt-1">{t('allAccessKeysDesc')}</p>

          {/* Search */}
          <div className="mt-4 relative">
            <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground h-5 w-5" />
            <Input
              placeholder={t('searchByKeyOrUser')}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-10 bg-card text-foreground border-border"
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
                        <Key className="h-4 w-4 text-muted-foreground" />
                        <code className="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">{key.id}</code>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <User className="h-4 w-4 text-muted-foreground" />
                        <span>{getUserName(key.userId)}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(key.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        {key.lastUsed ? (
                          <>
                            <Calendar className="h-3 w-3" />
                            {formatDate(key.lastUsed)}
                          </>
                        ) : (
                          <span className="text-muted-foreground">{t('never')}</span>
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

      {/* Temporary Credentials (STS) */}
      <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-border">
          <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
            <div>
              <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                <Timer className="h-5 w-5 text-muted-foreground" />
                {t('stsTitle')}
              </h3>
              <p className="text-sm text-muted-foreground mt-1 max-w-2xl">{t('stsDesc')}</p>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <select
                value={stsDuration}
                onChange={(e) => setStsDuration(Number(e.target.value))}
                aria-label={t('stsDuration')}
                className="h-9 rounded-md border border-border bg-card text-foreground text-sm px-2"
              >
                <option value={3600}>{t('stsDuration1h')}</option>
                <option value={14400}>{t('stsDuration4h')}</option>
                <option value={43200}>{t('stsDuration12h')}</option>
              </select>
              <Button
                onClick={() => issueSTSMutation.mutate()}
                disabled={issueSTSMutation.isPending}
              >
                {issueSTSMutation.isPending ? t('stsIssuing') : t('stsIssue')}
              </Button>
            </div>
          </div>

          <label className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
            <input
              type="checkbox"
              checked={stsPolicyOpen}
              onChange={(e) => setStsPolicyOpen(e.target.checked)}
              className="rounded border-border"
            />
            {t('stsPolicyToggle')}
          </label>

          {stsPolicyOpen && (
            <div className="mt-3 space-y-2">
              <p className="text-sm text-muted-foreground max-w-2xl">{t('stsPolicyHelp')}</p>
              <textarea
                value={stsPolicy}
                onChange={(e) => setStsPolicy(e.target.value)}
                rows={10}
                spellCheck={false}
                aria-label={t('stsPolicyLabel')}
                placeholder={t('stsPolicyPlaceholder')}
                className="w-full rounded-md border border-border bg-card text-foreground font-mono text-xs p-3"
              />
              <Button variant="ghost" size="sm" onClick={() => setStsPolicy(stsPolicyTemplate)}>
                {t('stsPolicyExample')}
              </Button>
            </div>
          )}

          {isGlobalAdmin && (
            <label className="mt-4 flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={showAllSessions}
                onChange={(e) => setShowAllSessions(e.target.checked)}
                className="rounded border-border"
              />
              {t('stsShowAll')}
            </label>
          )}
        </div>

        <div className="overflow-x-auto">
          {!stsSessions || stsSessions.length === 0 ? (
            <EmptyState
              icon={Timer}
              title={t('stsNoSessions')}
              description={t('stsNoSessionsDesc')}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('stsAccessKeyId')}</TableHead>
                  {showAllSessions && <TableHead>{t('user')}</TableHead>}
                  <TableHead>{t('stsScope')}</TableHead>
                  <TableHead>{t('created')}</TableHead>
                  <TableHead>{t('stsExpires')}</TableHead>
                  <TableHead className="text-right">{t('actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {stsSessions.map((session: STSSession) => {
                  const expired = session.expiresAt * 1000 <= Date.now();
                  return (
                    <TableRow key={session.accessKeyId}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Timer className="h-4 w-4 text-muted-foreground" />
                          <code className="text-sm bg-gray-100 dark:bg-gray-800 px-2 py-1 rounded">
                            {session.accessKeyId}
                          </code>
                        </div>
                      </TableCell>
                      {showAllSessions && (
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <User className="h-4 w-4 text-muted-foreground" />
                            <span>{session.username || getUserName(session.userId)}</span>
                          </div>
                        </TableCell>
                      )}
                      <TableCell>
                        {session.sessionPolicy ? (
                          <button
                            type="button"
                            onClick={() => handleViewPolicy(session)}
                            className="inline-flex items-center gap-1 text-sm text-blue-600 hover:underline"
                          >
                            <Shield className="h-3 w-3" />
                            {t('stsScopeRestricted')}
                          </button>
                        ) : (
                          <span className="text-sm text-muted-foreground">{t('stsScopeFull')}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1 text-sm text-muted-foreground">
                          <Calendar className="h-3 w-3" />
                          {formatDate(session.createdAt)}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div
                          className={`flex items-center gap-1 text-sm ${
                            expired ? 'text-amber-600' : 'text-muted-foreground'
                          }`}
                        >
                          <Clock className="h-3 w-3" />
                          {expired ? t('stsExpired') : formatDate(session.expiresAt)}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRevokeSession(session)}
                          disabled={revokeSTSMutation.isPending}
                          title={t('stsRevoke')}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </div>
      </div>
    </div>
  );
}
