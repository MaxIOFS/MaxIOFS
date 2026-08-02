import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import { Loading } from '@/components/ui/Loading';
import { ChevronDown, ChevronRight, Trash2, Save } from 'lucide-react';
import ModalManager from '@/lib/modals';
import type { PermissionGroup, UserPermissions } from '@/types';

// Permissions are picked from a list, never written as a document. A group can
// be granted in one click; individual actions stay available underneath it for
// the cases a group is too coarse.
interface Props {
  userId: string;
}

export default function PermissionPicker({ userId }: Props) {
  const { t } = useTranslation('users');
  const queryClient = useQueryClient();

  const { data: catalog, isLoading: catalogLoading } = useQuery({
    queryKey: ['permissionCatalog'],
    queryFn: APIClient.getPermissionCatalog,
  });

  const { data: stored, isLoading: storedLoading } = useQuery({
    queryKey: ['userPermissions', userId],
    queryFn: () => APIClient.getUserIAMPermissions(userId),
    enabled: !!userId,
  });

  const { data: buckets } = useQuery({
    queryKey: ['buckets'],
    queryFn: () => APIClient.getBuckets(),
  });

  // Working copy. Global actions apply everywhere; each bucket carries its own
  // set, which is what makes permissions fragmentable per bucket.
  const [global, setGlobal] = useState<Set<string>>(new Set());
  const [perBucket, setPerBucket] = useState<Record<string, Set<string>>>({});
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    if (!stored) return;
    setGlobal(new Set(stored.global));
    const next: Record<string, Set<string>> = {};
    for (const grant of stored.buckets) next[grant.bucket] = new Set(grant.actions);
    setPerBucket(next);
    setDirty(false);
  }, [stored]);

  const save = useMutation({
    mutationFn: () => {
      const payload: UserPermissions = {
        global: Array.from(global),
        buckets: Object.entries(perBucket)
          .filter(([, actions]) => actions.size > 0)
          .map(([bucket, actions]) => ({ bucket, actions: Array.from(actions) })),
      };
      return APIClient.setUserIAMPermissions(userId, payload);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userPermissions', userId] });
      ModalManager.toast('success', t('permissionsSaved'));
      setDirty(false);
    },
    onError: (error) => ModalManager.apiError(error),
  });

  const groups: PermissionGroup[] = catalog || [];

  // A scope is either "everywhere" or one bucket; the same catalogue is shown
  // for both, with the actions that name no bucket hidden in a bucket scope.
  const scopes = useMemo(
    () => ['*', ...Object.keys(perBucket).sort()],
    [perBucket],
  );

  const actionsFor = (scope: string): Set<string> =>
    scope === '*' ? global : perBucket[scope] || new Set();

  const setActionsFor = (scope: string, next: Set<string>) => {
    if (scope === '*') setGlobal(next);
    else setPerBucket((prev) => ({ ...prev, [scope]: next }));
    setDirty(true);
  };

  const toggle = (scope: string, action: string) => {
    const current = new Set(actionsFor(scope));
    if (current.has(action)) current.delete(action);
    else current.add(action);
    setActionsFor(scope, current);
  };

  const toggleGroup = (scope: string, group: PermissionGroup) => {
    const visible = group.permissions.filter((p) => scope === '*' || p.resourceScoped);
    const current = new Set(actionsFor(scope));
    const allOn = visible.every((p) => current.has(p.action));
    for (const p of visible) {
      if (allOn) current.delete(p.action);
      else current.add(p.action);
    }
    setActionsFor(scope, current);
  };

  const availableBuckets = useMemo(
    () => (buckets || []).map((b) => b.name).filter((name) => !(name in perBucket)),
    [buckets, perBucket],
  );

  const addBucket = (bucket: string) => {
    if (!bucket) return;
    setPerBucket((prev) => ({ ...prev, [bucket]: new Set() }));
    setOpen((prev) => ({ ...prev, [bucket]: true }));
    setDirty(true);
  };

  const removeBucket = (bucket: string) => {
    setPerBucket((prev) => {
      const next = { ...prev };
      delete next[bucket];
      return next;
    });
    setDirty(true);
  };

  if (catalogLoading || storedLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loading size="md" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{t('permissionsDesc')}</p>
        <div className="flex gap-2">
          <select
            className="rounded-md border border-border bg-background px-2 py-1.5 text-sm"
            value=""
            disabled={availableBuckets.length === 0}
            onChange={(e) => addBucket(e.target.value)}
          >
            <option value="">
              {availableBuckets.length === 0 ? t('permissionsNoBucketsLeft') : t('permissionsAddBucket')}
            </option>
            {availableBuckets.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
          <Button size="sm" disabled={!dirty || save.isPending} onClick={() => save.mutate()}>
            <Save className="h-4 w-4 mr-1" />
            {t('permissionsSave')}
          </Button>
        </div>
      </div>

      {scopes.map((scope) => {
        const selected = actionsFor(scope);
        return (
          <div key={scope} className="rounded-xl border border-border bg-card shadow-sm">
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <div>
                <h3 className="text-sm font-semibold text-foreground">
                  {scope === '*' ? t('permissionsEverywhere') : scope}
                </h3>
                <p className="text-xs text-muted-foreground">
                  {scope === '*' ? t('permissionsEverywhereHint') : t('permissionsBucketHint')}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {t('permissionsSelectedCount', { count: selected.size })}
                </span>
                {scope !== '*' && (
                  <Button size="sm" variant="ghost" onClick={() => removeBucket(scope)}>
                    <Trash2 className="h-4 w-4 text-red-500" />
                  </Button>
                )}
              </div>
            </div>

            <div className="divide-y divide-border">
              {groups.map((group) => {
                const visible = group.permissions.filter((p) => scope === '*' || p.resourceScoped);
                if (visible.length === 0) return null;

                const key = scope + ':' + group.name;
                const expanded = open[key];
                const onCount = visible.filter((p) => selected.has(p.action)).length;

                return (
                  <div key={key}>
                    <div className="flex items-center gap-2 px-4 py-2">
                      <button
                        type="button"
                        className="text-muted-foreground hover:text-foreground"
                        onClick={() => setOpen((prev) => ({ ...prev, [key]: !expanded }))}
                        aria-label={group.name}
                      >
                        {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                      </button>

                      <label className="flex items-center gap-2 cursor-pointer flex-1">
                        <input
                          type="checkbox"
                          className="h-4 w-4 rounded border-border"
                          checked={onCount === visible.length}
                          ref={(el) => {
                            if (el) el.indeterminate = onCount > 0 && onCount < visible.length;
                          }}
                          onChange={() => toggleGroup(scope, group)}
                        />
                        <span className="text-sm font-medium text-foreground">
                          {t('permissionGroup.' + group.name, { defaultValue: group.name })}
                        </span>
                      </label>

                      <span className="text-xs text-muted-foreground">
                        {onCount}/{visible.length}
                      </span>
                    </div>

                    {expanded && (
                      <div className="px-4 pb-3 pl-12 grid gap-1.5 sm:grid-cols-2">
                        {visible.map((permission) => (
                          <label
                            key={permission.action}
                            className="flex items-start gap-2 cursor-pointer"
                            title={permission.description || permission.action}
                          >
                            <input
                              type="checkbox"
                              className="h-4 w-4 mt-0.5 rounded border-border"
                              checked={selected.has(permission.action)}
                              onChange={() => toggle(scope, permission.action)}
                            />
                            <span>
                              <span className="text-sm text-foreground">{permission.label}</span>
                              <code className="block text-[11px] font-mono text-muted-foreground">
                                {permission.action}
                              </code>
                            </span>
                          </label>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}
