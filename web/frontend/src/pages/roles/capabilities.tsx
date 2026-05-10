import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { ShieldCheck, ArrowLeft, Save } from 'lucide-react';
import ModalManager from '@/lib/modals';

const ROLE_ORDER = ['admin', 'user', 'read', 'readonly', 'guest'];

export default function RoleCapabilitiesPage() {
  const { t } = useTranslation('users');
  const navigate = useNavigate();
  const { isGlobalAdmin, isLoading: isCurrentUserLoading } = useCurrentUser();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!isCurrentUserLoading && isGlobalAdmin === false) navigate('/');
  }, [isCurrentUserLoading, isGlobalAdmin, navigate]);

  const { data, isLoading } = useQuery({
    queryKey: ['roleCapabilities'],
    queryFn: APIClient.getAllRoleCapabilities,
    enabled: !isCurrentUserLoading && isGlobalAdmin === true,
  });

  const [edits, setEdits] = useState<Record<string, Set<string>>>({});
  const [dirty, setDirty] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!data) return;
    const init: Record<string, Set<string>> = {};
    for (const role of Object.keys(data.role_capabilities)) {
      init[role] = new Set(data.role_capabilities[role]);
    }
    setEdits(init);
    setDirty(new Set());
  }, [data]);

  const saveMutation = useMutation({
    mutationFn: ({ role, capabilities }: { role: string; capabilities: string[] }) =>
      APIClient.setRoleCapabilities(role, capabilities),
    onSuccess: (_data, { role }) => {
      queryClient.invalidateQueries({ queryKey: ['roleCapabilities'] });
      setDirty((prev) => { const s = new Set(prev); s.delete(role); return s; });
      ModalManager.toast('success', t('roleCapabilitiesRoleSaved', { role }));
    },
    onError: (error) => ModalManager.apiError(error),
  });

  const toggleCap = (role: string, cap: string) => {
    setEdits((prev) => {
      const roleSet = new Set(prev[role] || []);
      if (roleSet.has(cap)) { roleSet.delete(cap); } else { roleSet.add(cap); }
      return { ...prev, [role]: roleSet };
    });
    setDirty((prev) => new Set(prev).add(role));
  };

  const saveRole = (role: string) => {
    saveMutation.mutate({ role, capabilities: Array.from(edits[role] || []) });
  };

  const allCaps: string[] = data?.all_capabilities || [];
  const roles = ROLE_ORDER.filter((r) => data?.role_capabilities[r] !== undefined);

  if (isCurrentUserLoading || isLoading) {
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
            <ShieldCheck className="h-6 w-6 text-muted-foreground" />
            {t('roleCapabilitiesTitle')}
          </h1>
          <p className="text-sm text-muted-foreground mt-0.5">{t('roleCapabilitiesDesc')}</p>
        </div>
      </div>

      <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3">
        <p className="text-sm text-amber-800 dark:text-amber-300">{t('roleCapabilitiesGlobalWarning')}</p>
      </div>

      <div className="overflow-x-auto rounded-xl border border-border shadow-md">
        <table className="min-w-full text-sm">
          <thead>
            <tr className="bg-muted">
              <th className="px-4 py-3 text-left font-semibold text-foreground sticky left-0 bg-muted z-10 min-w-[180px]">
                {t('roleCapabilitiesCapabilityCol')}
              </th>
              {roles.map((role) => (
                <th key={role} className="px-4 py-3 text-center font-semibold text-foreground min-w-[110px]">
                  <div className="flex flex-col items-center gap-1">
                    <span className="capitalize">{role}</span>
                    {dirty.has(role) && (
                      <Button
                        size="sm"
                        variant="default"
                        className="h-6 text-xs px-2 py-0"
                        disabled={saveMutation.isPending}
                        onClick={() => saveRole(role)}
                      >
                        <Save className="h-3 w-3 mr-1" />
                        {t('roleCapabilitiesSave')}
                      </Button>
                    )}
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {allCaps.map((cap, idx) => (
              <tr key={cap} className={idx % 2 === 0 ? 'bg-card' : 'bg-muted/30'}>
                <td className="px-4 py-2.5 sticky left-0 bg-inherit z-10">
                  <code className="text-xs font-mono text-foreground">{cap}</code>
                </td>
                {roles.map((role) => {
                  const granted = (edits[role] || new Set()).has(cap);
                  const isAdmin = role === 'admin';
                  return (
                    <td key={role} className="px-4 py-2.5 text-center">
                      <button
                        type="button"
                        disabled={isAdmin || saveMutation.isPending}
                        onClick={() => toggleCap(role, cap)}
                        className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-1 disabled:opacity-50 disabled:cursor-not-allowed ${
                          granted ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'
                        }`}
                        aria-checked={granted}
                        role="switch"
                      >
                        <span
                          className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow-sm transition-transform ${
                            granted ? 'translate-x-4' : 'translate-x-0.5'
                          }`}
                        />
                      </button>
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="text-xs text-muted-foreground">{t('roleCapabilitiesAdminNote')}</p>
    </div>
  );
}
