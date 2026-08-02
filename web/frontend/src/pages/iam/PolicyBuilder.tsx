import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Loading } from '@/components/ui/Loading';
import { ChevronDown, ChevronRight, Trash2 } from 'lucide-react';
import type { PermissionGroup } from '@/types';

// Builds a policy document from a selection of permissions. The document is an
// output, never something a person types: the same catalogue that governs a
// user's permissions is what a policy is written from.
interface Props {
  document: string;
  onChange: (document: string) => void;
}

interface Statement {
  Effect: string;
  Action: string[];
  Resource: string[];
}

const ALL_RESOURCES = '*';

// bucketOf reduces a resource ARN to the bucket it names, or "*" when it names
// everything. A policy statement carries both the bucket and its keys, so the
// two ARNs collapse back into one scope in the editor.
function bucketOf(resource: string): string {
  if (resource === '*') return ALL_RESOURCES;
  const trimmed = resource.replace(/^arn:aws:s3:::/, '').replace(/\/\*$/, '');
  return trimmed === '' || trimmed === '*' ? ALL_RESOURCES : trimmed;
}

// parseSelection reads an existing document back into scopes, so editing shows
// what the policy actually says instead of a blank form.
function parseSelection(document: string): Record<string, Set<string>> {
  const scopes: Record<string, Set<string>> = {};
  if (!document.trim()) return scopes;

  try {
    const parsed = JSON.parse(document);
    for (const statement of parsed.Statement || []) {
      if (statement.Effect !== 'Allow') continue;
      const actions = Array.isArray(statement.Action) ? statement.Action : [statement.Action];
      const resources = Array.isArray(statement.Resource) ? statement.Resource : [statement.Resource];
      for (const resource of resources) {
        const scope = bucketOf(String(resource));
        scopes[scope] = scopes[scope] || new Set();
        for (const action of actions) scopes[scope].add(String(action));
      }
    }
  } catch {
    // A document that does not parse is shown as an empty selection rather than
    // silently discarded: saving then rewrites it from what the operator picks.
  }
  return scopes;
}

function buildDocument(scopes: Record<string, Set<string>>): string {
  const statements: Statement[] = [];
  for (const [scope, actions] of Object.entries(scopes)) {
    if (actions.size === 0) continue;
    statements.push({
      Effect: 'Allow',
      Action: Array.from(actions).sort(),
      Resource:
        scope === ALL_RESOURCES
          ? ['*']
          : [`arn:aws:s3:::${scope}`, `arn:aws:s3:::${scope}/*`],
    });
  }
  return JSON.stringify({ Version: '2012-10-17', Statement: statements }, null, 2);
}

export default function PolicyBuilder({ document, onChange }: Props) {
  const { t } = useTranslation('users');

  const { data: catalog, isLoading } = useQuery({
    queryKey: ['permissionCatalog'],
    queryFn: APIClient.getPermissionCatalog,
  });

  const { data: buckets } = useQuery({
    queryKey: ['buckets'],
    queryFn: () => APIClient.getBuckets(),
  });

  const [scopes, setScopes] = useState<Record<string, Set<string>>>({});
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [loaded, setLoaded] = useState(false);

  // Load the document once. Reloading on every change would fight the edits, as
  // the parent's document is rewritten from this selection on each click.
  useEffect(() => {
    if (loaded) return;
    const parsed = parseSelection(document);
    setScopes(Object.keys(parsed).length > 0 ? parsed : { [ALL_RESOURCES]: new Set() });
    setLoaded(true);
  }, [document, loaded]);

  const update = (next: Record<string, Set<string>>) => {
    setScopes(next);
    onChange(buildDocument(next));
  };

  const groups: PermissionGroup[] = catalog || [];
  const scopeNames = useMemo(
    () => [ALL_RESOURCES, ...Object.keys(scopes).filter((s) => s !== ALL_RESOURCES).sort()],
    [scopes],
  );

  const availableBuckets = useMemo(
    () => (buckets || []).map((b) => b.name).filter((name) => !(name in scopes)),
    [buckets, scopes],
  );

  const toggle = (scope: string, action: string) => {
    const next = { ...scopes };
    const current = new Set(next[scope] || []);
    if (current.has(action)) current.delete(action);
    else current.add(action);
    next[scope] = current;
    update(next);
  };

  const toggleGroup = (scope: string, group: PermissionGroup) => {
    const visible = group.permissions.filter((p) => scope === ALL_RESOURCES || p.resourceScoped);
    const next = { ...scopes };
    const current = new Set(next[scope] || []);
    const allOn = visible.every((p) => current.has(p.action));
    for (const p of visible) {
      if (allOn) current.delete(p.action);
      else current.add(p.action);
    }
    next[scope] = current;
    update(next);
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-32">
        <Loading size="md" />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-xs text-muted-foreground">{t('policyBuilderHint')}</p>
        <select
          className="rounded-md border border-border bg-background px-2 py-1.5 text-sm"
          value=""
          disabled={availableBuckets.length === 0}
          onChange={(e) => {
            if (!e.target.value) return;
            update({ ...scopes, [e.target.value]: new Set() });
            setOpen((prev) => ({ ...prev, [e.target.value]: true }));
          }}
        >
          <option value="">
            {availableBuckets.length === 0 ? t('permissionsNoBucketsLeft') : t('permissionsAddBucket')}
          </option>
          {availableBuckets.map((name) => (
            <option key={name} value={name}>{name}</option>
          ))}
        </select>
      </div>

      {scopeNames.map((scope) => {
        const selected = scopes[scope] || new Set<string>();
        return (
          <div key={scope} className="rounded-lg border border-border">
            <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-muted/40">
              <span className="text-sm font-medium text-foreground">
                {scope === ALL_RESOURCES ? t('permissionsEverywhere') : scope}
              </span>
              <div className="flex items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {t('permissionsSelectedCount', { count: selected.size })}
                </span>
                {scope !== ALL_RESOURCES && (
                  <button
                    type="button"
                    onClick={() => {
                      const next = { ...scopes };
                      delete next[scope];
                      update(next);
                    }}
                  >
                    <Trash2 className="h-4 w-4 text-red-500" />
                  </button>
                )}
              </div>
            </div>

            <div className="divide-y divide-border">
              {groups.map((group) => {
                const visible = group.permissions.filter(
                  (p) => scope === ALL_RESOURCES || p.resourceScoped,
                );
                if (visible.length === 0) return null;

                const key = scope + ':' + group.name;
                const expanded = open[key];
                const onCount = visible.filter((p) => selected.has(p.action)).length;

                return (
                  <div key={key}>
                    <div className="flex items-center gap-2 px-3 py-1.5">
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
                        <span className="text-sm text-foreground">
                          {t('permissionGroup.' + group.name, { defaultValue: group.name })}
                        </span>
                      </label>
                      <span className="text-xs text-muted-foreground">
                        {onCount}/{visible.length}
                      </span>
                    </div>

                    {expanded && (
                      <div className="px-3 pb-2 pl-10 grid gap-1 sm:grid-cols-2">
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
