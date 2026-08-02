import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { EmptyState } from '@/components/ui/EmptyState';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { KeyRound, ArrowLeft, Plus, Trash2, Lock } from 'lucide-react';
import ModalManager from '@/lib/modals';
import PermissionPicker from './PermissionPicker';
import PolicyBuilder from './PolicyBuilder';

type Tab = 'permissions' | 'policies' | 'roles';

// Offered as a starting point when a role is meant to be assumable.
const TRUST_TEMPLATE = JSON.stringify(
  {
    Version: '2012-10-17',
    Statement: [
      {
        Effect: 'Allow',
        Principal: { AWS: 'arn:aws:iam:::user/backup-agent' },
        Action: 'sts:AssumeRole',
      },
    ],
  },
  null,
  2,
);

export default function IAMPage() {
  const { t } = useTranslation('users');
  const navigate = useNavigate();
  const { isGlobalAdmin, isLoading: isCurrentUserLoading } = useCurrentUser();
  const queryClient = useQueryClient();

  const [tab, setTab] = useState<Tab>('permissions');
  const [pickedUser, setPickedUser] = useState('');
  const [editing, setEditing] = useState<null | 'policy' | 'role'>(null);
  const [form, setForm] = useState({ name: '', description: '', document: '', trustPolicy: '', duration: 3600 });
  const [editorKey, setEditorKey] = useState(0);

  useEffect(() => {
    if (!isCurrentUserLoading && isGlobalAdmin === false) navigate('/');
  }, [isCurrentUserLoading, isGlobalAdmin, navigate]);

  const enabled = !isCurrentUserLoading && isGlobalAdmin === true;

  const { data: policies, isLoading: policiesLoading } = useQuery({
    queryKey: ['iamPolicies'],
    queryFn: APIClient.listIAMPolicies,
    enabled,
  });

  const { data: roles, isLoading: rolesLoading } = useQuery({
    queryKey: ['iamRoles'],
    queryFn: APIClient.listIAMRoles,
    enabled,
  });

  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: () => APIClient.getUsers(),
    enabled,
  });

  const closeEditor = () => {
    setEditing(null);
    setForm({ name: '', description: '', document: '', trustPolicy: '', duration: 3600 });
  };

  const savePolicy = useMutation({
    mutationFn: () =>
      APIClient.saveIAMPolicy({ name: form.name, description: form.description, document: form.document }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['iamPolicies'] });
      ModalManager.toast('success', t('iamPolicySaved'));
      closeEditor();
    },
    onError: (error) => ModalManager.apiError(error),
  });

  const saveRole = useMutation({
    mutationFn: () =>
      APIClient.saveIAMRole({
        name: form.name,
        description: form.description,
        trustPolicy: form.trustPolicy,
        permissions: form.document,
        maxSessionDuration: form.duration,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['iamRoles'] });
      ModalManager.toast('success', t('iamRoleSaved'));
      closeEditor();
    },
    onError: (error) => ModalManager.apiError(error),
  });

  const deletePolicy = useMutation({
    mutationFn: (name: string) => APIClient.deleteIAMPolicy(name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['iamPolicies'] }),
    onError: (error) => ModalManager.apiError(error),
  });

  const deleteRole = useMutation({
    mutationFn: (name: string) => APIClient.deleteIAMRole(name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['iamRoles'] }),
    onError: (error) => ModalManager.apiError(error),
  });

  const confirmDeletePolicy = async (name: string) => {
    const result = await ModalManager.confirmDelete(name, t('iamPolicyType'));
    if (result.isConfirmed) deletePolicy.mutate(name);
  };

  const confirmDeleteRole = async (name: string) => {
    const result = await ModalManager.confirmDelete(name, t('iamRoleType'));
    if (result.isConfirmed) deleteRole.mutate(name);
  };

  const startNewPolicy = () => {
    setForm({ name: '', description: '', document: '', trustPolicy: '', duration: 3600 });
    setEditorKey((k) => k + 1);
    setEditing('policy');
  };

  const startNewRole = () => {
    setForm({ name: '', description: '', document: '', trustPolicy: '', duration: 3600 });
    setEditorKey((k) => k + 1);
    setEditing('role');
  };

  const startEditPolicy = (name: string, description: string, document: string) => {
    setForm({ name, description, document, trustPolicy: '', duration: 3600 });
    setEditorKey((k) => k + 1);
    setEditing('policy');
  };

  if (isCurrentUserLoading || policiesLoading || rolesLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-2xl font-bold text-foreground flex items-center gap-2">
            <KeyRound className="h-6 w-6 text-muted-foreground" />
            {t('iamTitle')}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">{t('iamDesc')}</p>
        </div>
      </div>

      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
        <p className="text-sm text-blue-800 dark:text-blue-300">{t('iamScopeNote')}</p>
      </div>

      <div className="flex gap-1 border-b border-border">
        {(['permissions', 'policies', 'roles'] as Tab[]).map((name) => (
          <button
            key={name}
            type="button"
            onClick={() => { setTab(name); closeEditor(); }}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === name
                ? 'border-brand-500 text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground'
            }`}
          >
            {name === 'permissions'
              ? t('iamTabPermissions')
              : name === 'policies'
                ? t('iamTabPolicies')
                : t('iamTabRoles')}
          </button>
        ))}
      </div>

      {editing && (
        <div className="rounded-xl border border-border bg-card p-4 space-y-3 shadow-md">
          <h2 className="text-sm font-semibold text-foreground">
            {editing === 'policy' ? t('iamPolicyEditorTitle') : t('iamRoleEditorTitle')}
          </h2>

          <div className="grid gap-3 sm:grid-cols-2">
            <input
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              placeholder={t('iamNamePlaceholder')}
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
            <input
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              placeholder={t('iamDescriptionPlaceholder')}
              value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })}
            />
          </div>

          {editing === 'role' && (
            <div>
              <label className="block text-xs text-muted-foreground mb-1">{t('iamMaxSessionDuration')}</label>
              <input
                type="number"
                min={900}
                step={900}
                className="w-40 rounded-md border border-border bg-background px-3 py-2 text-sm"
                value={form.duration}
                onChange={(e) => setForm({ ...form, duration: Number(e.target.value) })}
              />
            </div>
          )}

          <div>
            <label className="block text-xs text-muted-foreground mb-1">
              {editing === 'policy' ? t('iamPolicyDocument') : t('iamRolePermissions')}
            </label>
            <PolicyBuilder
              key={editorKey}
              document={form.document}
              onChange={(document) => setForm((prev) => ({ ...prev, document }))}
            />
          </div>

          {editing === 'role' && (
            <details className="rounded-md border border-border px-3 py-2">
              <summary className="text-xs text-muted-foreground cursor-pointer">
                {t('iamTrustPolicy')}
              </summary>
              <textarea
                className="mt-2 w-full h-32 rounded-md border border-border bg-background px-3 py-2 font-mono text-xs"
                spellCheck={false}
                placeholder={TRUST_TEMPLATE}
                value={form.trustPolicy}
                onChange={(e) => setForm({ ...form, trustPolicy: e.target.value })}
              />
              <p className="text-xs text-muted-foreground mt-1">{t('iamTrustPolicyOptionalHint')}</p>
            </details>
          )}

          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={
                !form.name ||
                (editing === 'policy' && !form.document) ||
                savePolicy.isPending ||
                saveRole.isPending
              }
              onClick={() => (editing === 'policy' ? savePolicy.mutate() : saveRole.mutate())}
            >
              {t('iamSave')}
            </Button>
            <Button size="sm" variant="ghost" onClick={closeEditor}>
              {t('iamCancel')}
            </Button>
          </div>
        </div>
      )}

      {tab === 'permissions' && (
        <div className="space-y-4">
          <div>
            <label className="block text-xs text-muted-foreground mb-1">{t('permissionsPickUser')}</label>
            <select
              className="w-full sm:w-80 rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={pickedUser}
              onChange={(e) => setPickedUser(e.target.value)}
            >
              <option value="">{t('permissionsPickUser')}</option>
              {(users || []).map((u) => (
                <option key={u.id} value={u.id}>{u.username}</option>
              ))}
            </select>
          </div>

          {pickedUser
            ? <PermissionPicker userId={pickedUser} />
            : <p className="text-sm text-muted-foreground">{t('permissionsPickUserHint')}</p>}
        </div>
      )}

      {tab === 'policies' && (
        <div className="space-y-3">
          {!editing && (
            <Button size="sm" onClick={startNewPolicy}>
              <Plus className="h-4 w-4 mr-1" />
              {t('iamNewPolicy')}
            </Button>
          )}

          {(policies || []).length === 0 ? (
            <EmptyState
              icon={KeyRound}
              title={t('iamNoPolicies')}
              description={t('iamNoPoliciesDesc')}
              actionLabel={t('iamNewPolicy')}
              onAction={startNewPolicy}
              showAction
            />
          ) : (
            <div className="overflow-x-auto rounded-xl border border-border shadow-md">
              <table className="min-w-full text-sm">
                <thead>
                  <tr className="bg-muted">
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColName')}</th>
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColArn')}</th>
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColVersion')}</th>
                    <th className="px-4 py-3 text-right font-semibold text-foreground">{t('iamColActions')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {(policies || []).map((policy, idx) => (
                    <tr key={policy.name} className={idx % 2 === 0 ? 'bg-card' : 'bg-muted/30'}>
                      <td className="px-4 py-2.5">
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-foreground">{policy.name}</span>
                          {policy.isBuiltin && (
                            <span className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                              <Lock className="h-3 w-3" />
                              {t('iamBuiltin')}
                            </span>
                          )}
                        </div>
                        {policy.description && (
                          <p className="text-xs text-muted-foreground">{policy.description}</p>
                        )}
                      </td>
                      <td className="px-4 py-2.5">
                        <code className="text-xs font-mono text-muted-foreground">{policy.arn}</code>
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">{policy.versionId}</td>
                      <td className="px-4 py-2.5 text-right whitespace-nowrap">
                        {!policy.isBuiltin && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => startEditPolicy(policy.name, policy.description || '', policy.document)}
                          >
                            {t('iamEdit')}
                          </Button>
                        )}
                        {!policy.isBuiltin && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => confirmDeletePolicy(policy.name)}
                          >
                            <Trash2 className="h-4 w-4 text-red-500" />
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {tab === 'roles' && (
        <div className="space-y-3">
          {!editing && (
            <Button size="sm" onClick={startNewRole}>
              <Plus className="h-4 w-4 mr-1" />
              {t('iamNewRole')}
            </Button>
          )}

          {(roles || []).length === 0 ? (
            <EmptyState
              icon={KeyRound}
              title={t('iamNoRoles')}
              description={t('iamNoRolesDesc')}
              actionLabel={t('iamNewRole')}
              onAction={startNewRole}
              showAction
            />
          ) : (
            <div className="overflow-x-auto rounded-xl border border-border shadow-md">
              <table className="min-w-full text-sm">
                <thead>
                  <tr className="bg-muted">
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColName')}</th>
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColArn')}</th>
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColPolicies')}</th>
                    <th className="px-4 py-3 text-left font-semibold text-foreground">{t('iamColDuration')}</th>
                    <th className="px-4 py-3 text-right font-semibold text-foreground">{t('iamColActions')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {(roles || []).map((role, idx) => (
                    <tr key={role.name} className={idx % 2 === 0 ? 'bg-card' : 'bg-muted/30'}>
                      <td className="px-4 py-2.5">
                        <span className="font-medium text-foreground">{role.name}</span>
                        {role.description && (
                          <p className="text-xs text-muted-foreground">{role.description}</p>
                        )}
                      </td>
                      <td className="px-4 py-2.5">
                        <code className="text-xs font-mono text-muted-foreground">{role.arn}</code>
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">
                        {role.policies.length > 0 ? role.policies.join(', ') : t('iamRoleNoPolicies')}
                        {!role.trustPolicy && (
                          <span className="block text-xs opacity-70">{t('iamRoleAssignable')}</span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">{role.maxSessionDuration}s</td>
                      <td className="px-4 py-2.5 text-right">
                        <Button size="sm" variant="ghost" onClick={() => confirmDeleteRole(role.name)}>
                          <Trash2 className="h-4 w-4 text-red-500" />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
