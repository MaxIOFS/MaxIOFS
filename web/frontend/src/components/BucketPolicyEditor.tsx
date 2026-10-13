import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, ChevronRight, Plus, Trash2, X } from 'lucide-react';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';
import ModalManager from '@/lib/modals';
import type { PermissionGroup } from '@/types';
import {
  type BucketPolicyDocument,
  type PolicyRule,
  POLICY_MANAGEMENT_ACTIONS,
  isAddressRange,
  isObjectAction,
  newRule,
  policyFromRules,
  principalName,
  ruleProblem,
  rulesFromPolicy,
  userARN,
} from '@/lib/bucketPolicyRules';

interface BucketPolicyEditorProps {
  bucketName: string;
  // Tenant of the bucket: a policy names only users of it.
  bucketTenantId: string;
  policy: BucketPolicyDocument | null;
  readOnly: boolean;
  publicPolicyBlocked: boolean;
  saving: boolean;
  onSave: (policy: string) => void;
  onCancel: () => void;
}

// Each rule keeps its key while others are added or removed.
let ruleKeys = 0;
const keyed = (rule: PolicyRule) => ({ key: ruleKeys++, rule });

// A bucket policy edited as rules: who, what, where and when.
export function BucketPolicyEditor({
  bucketName,
  bucketTenantId,
  policy,
  readOnly,
  publicPolicyBlocked,
  saving,
  onSave,
  onCancel,
}: BucketPolicyEditorProps) {
  const { t } = useTranslation('bucketSettings');
  const parsed = useMemo(() => rulesFromPolicy(policy, bucketName), [policy, bucketName]);
  const [entries, setEntries] = useState(() => (parsed ?? []).map(keyed));
  const [replacing, setReplacing] = useState(false);
  const rules = entries.map((e) => e.rule);

  const { data: catalog } = useQuery({
    queryKey: ['permissionCatalog'],
    queryFn: APIClient.getPermissionCatalog,
  });
  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    retry: false,
  });

  // The actions a bucket policy decides: S3 actions on a bucket or its objects.
  const groups: PermissionGroup[] = useMemo(
    () =>
      (catalog || [])
        .map((group) => ({
          ...group,
          permissions: group.permissions.filter(
            (p) => p.action.startsWith('s3:') && p.resourceScoped && !POLICY_MANAGEMENT_ACTIONS.has(p.action),
          ),
        }))
        .filter((group) => group.permissions.length > 0),
    [catalog],
  );
  const tenantUsers = useMemo(
    () => (users || []).filter((u) => (u.tenantId || '') === bucketTenantId).map((u) => u.username).sort(),
    [users, bucketTenantId],
  );

  const update = (key: number, change: Partial<PolicyRule>) =>
    setEntries((prev) => prev.map((e) => (e.key === key ? { ...e, rule: { ...e.rule, ...change } } : e)));
  const add = (rule: PolicyRule) => setEntries((prev) => [...prev, keyed(rule)]);

  const presets: { key: string; public: boolean; rule: () => PolicyRule }[] = [
    { key: 'presetPublicRead', public: true, rule: () => ({ ...newRule(), everyone: true, actions: ['s3:GetObject'] }) },
    { key: 'presetPublicList', public: true, rule: () => ({ ...newRule(), everyone: true, actions: ['s3:GetObject', 's3:ListBucket'] }) },
    { key: 'presetRequireTLS', public: false, rule: () => ({ ...newRule(), effect: 'Deny', everyone: true, allActions: true, transport: 'insecure' }) },
    { key: 'presetOnlyFrom', public: false, rule: () => ({ ...newRule(), effect: 'Deny', everyone: true, allActions: true, addresses: 'notFrom' }) },
  ];

  const problems = rules.map(ruleProblem);
  const canSave = !readOnly && rules.length > 0 && problems.every((p) => p === null);

  if (parsed === null && !replacing) {
    return (
      <div className="space-y-4">
        <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
          <p className="text-sm font-medium text-yellow-800 dark:text-yellow-200">{t('policyEditor.unsupportedTitle')}</p>
          <p className="text-sm text-yellow-800 dark:text-yellow-200">{t('policyEditor.unsupportedDesc')}</p>
        </div>
        <pre className="text-xs font-mono bg-secondary text-foreground rounded-lg p-3 overflow-auto max-h-80">
          {JSON.stringify(policy, null, 2)}
        </pre>
        <div className="flex justify-end gap-2 pt-4 border-t border-border">
          <Button variant="outline" onClick={onCancel}>{t('policy.cancel')}</Button>
          {!readOnly && (
            <Button
              onClick={() =>
                ModalManager.confirm(t('policyEditor.confirmReplaceTitle'), t('policyEditor.confirmReplaceMsg'), () => {
                  setEntries([]);
                  setReplacing(true);
                })
              }
            >
              {t('policyEditor.replace')}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t('policyEditor.intro')}</p>

      {rules.length === 0 && (
        <div className="text-sm text-muted-foreground border border-dashed border-border rounded-lg p-4 text-center">
          {t('policyEditor.empty')}
        </div>
      )}

      {entries.map((entry, index) => (
        <RuleCard
          key={entry.key}
          index={index}
          rule={entry.rule}
          problem={problems[index]}
          groups={groups}
          tenantUsers={tenantUsers}
          readOnly={readOnly}
          onChange={(change) => update(entry.key, change)}
          onRemove={() => setEntries((prev) => prev.filter((e) => e.key !== entry.key))}
        />
      ))}

      {!readOnly && (
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => add(newRule())}>
            <Plus className="h-4 w-4 mr-1" />
            {t('policyEditor.addRule')}
          </Button>
          <span className="text-xs text-muted-foreground ml-2">{t('policyEditor.presetsLabel')}</span>
          {presets.map((preset) => (
            <Button
              key={preset.key}
              variant="ghost"
              size="sm"
              disabled={preset.public && publicPolicyBlocked}
              title={preset.public && publicPolicyBlocked ? t('policy.blockedByPublicAccess') : undefined}
              onClick={() => add(preset.rule())}
            >
              {t('policyEditor.' + preset.key)}
            </Button>
          ))}
        </div>
      )}

      <div className="flex items-center justify-end gap-2 pt-4 border-t border-border">
        {!readOnly && rules.length === 0 && (
          <span className="text-xs text-muted-foreground mr-auto">{t('policyEditor.noRulesToSave')}</span>
        )}
        <Button variant="outline" onClick={onCancel}>{t('policy.cancel')}</Button>
        {!readOnly && (
          <Button
            disabled={!canSave || saving}
            onClick={() => onSave(JSON.stringify(policyFromRules(rules, bucketName)))}
          >
            {saving ? t('policy.saving') : t('policy.savePolicy')}
          </Button>
        )}
      </div>
    </div>
  );
}

interface RuleCardProps {
  index: number;
  rule: PolicyRule;
  problem: string | null;
  groups: PermissionGroup[];
  tenantUsers: string[];
  readOnly: boolean;
  onChange: (change: Partial<PolicyRule>) => void;
  onRemove: () => void;
}

function Segmented<T extends string>({
  value,
  options,
  disabled,
  onChange,
}: {
  value: T;
  options: { value: T; label: string; tone?: 'deny' }[];
  disabled: boolean;
  onChange: (value: T) => void;
}) {
  return (
    <div className="inline-flex rounded-md border border-border overflow-hidden">
      {options.map((option) => {
        const active = option.value === value;
        const tone = option.tone === 'deny'
          ? 'bg-red-600 text-white'
          : 'bg-brand-600 text-white';
        return (
          <button
            key={option.value}
            type="button"
            disabled={disabled}
            onClick={() => onChange(option.value)}
            className={`px-3 py-1 text-sm transition-colors disabled:cursor-not-allowed ${
              active ? tone : 'bg-card text-foreground hover:bg-secondary'
            }`}
          >
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

// Values added one at a time and shown as removable chips.
function ChipList({
  values,
  show,
  placeholder,
  valid,
  disabled,
  options,
  optionsLabel,
  onChange,
}: {
  values: string[];
  show: (value: string) => string;
  placeholder: string;
  valid: (value: string) => boolean;
  disabled: boolean;
  options?: { value: string; label: string }[];
  optionsLabel?: string;
  onChange: (values: string[]) => void;
}) {
  const { t } = useTranslation('bucketSettings');
  const [draft, setDraft] = useState('');
  const addValue = (value: string) => {
    const v = value.trim();
    if (!v || !valid(v) || values.includes(v)) return;
    onChange([...values, v]);
    setDraft('');
  };
  const free = (options || []).filter((o) => !values.includes(o.value));
  return (
    <div className="space-y-2">
      {values.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {values.map((value) => (
            <span
              key={value}
              className={`inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs border ${
                valid(value)
                  ? 'bg-secondary text-foreground border-border'
                  : 'bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border-red-300 dark:border-red-800'
              }`}
            >
              {show(value)}
              {!disabled && (
                <button type="button" onClick={() => onChange(values.filter((v) => v !== value))} aria-label={t('policyEditor.removeValue')}>
                  <X className="h-3 w-3" />
                </button>
              )}
            </span>
          ))}
        </div>
      )}
      {!disabled && (
        <div className="flex flex-wrap gap-2">
          {free.length > 0 && (
            <select
              value=""
              onChange={(e) => addValue(e.target.value)}
              className="px-2 py-1 text-sm border border-border bg-card text-foreground rounded-md"
            >
              <option value="">{optionsLabel}</option>
              {free.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          )}
          <input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                addValue(draft);
              }
            }}
            placeholder={placeholder}
            className="px-2 py-1 text-sm border border-border bg-card text-foreground rounded-md w-48"
          />
          <Button variant="outline" size="sm" disabled={!draft.trim() || !valid(draft.trim())} onClick={() => addValue(draft)}>
            {t('policyEditor.add')}
          </Button>
        </div>
      )}
    </div>
  );
}

function RuleCard({ index, rule, problem, groups, tenantUsers, readOnly, onChange, onRemove }: RuleCardProps) {
  const { t } = useTranslation('bucketSettings');
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [byPrefix, setByPrefix] = useState(!!rule.prefix);
  const catalogActions = new Set(groups.flatMap((g) => g.permissions.map((p) => p.action)));
  const otherActions = rule.actions.filter((a) => !catalogActions.has(a));
  const selected = new Set(rule.actions);
  const offered = (action: string) => !rule.prefix || isObjectAction(action);

  const toggle = (action: string) => {
    const next = new Set(selected);
    if (next.has(action)) next.delete(action);
    else next.add(action);
    onChange({ actions: Array.from(next) });
  };
  const toggleGroup = (group: PermissionGroup) => {
    const actions = group.permissions.map((p) => p.action).filter(offered);
    const all = actions.every((a) => selected.has(a));
    const next = new Set(selected);
    for (const a of actions) {
      if (all) next.delete(a);
      else next.add(a);
    }
    onChange({ actions: Array.from(next) });
  };

  const label = 'block text-xs font-semibold uppercase tracking-wide text-muted-foreground mb-1.5';
  const radio = 'flex items-start gap-2 text-sm text-foreground';

  return (
    <div className={`rounded-lg border ${problem ? 'border-red-300 dark:border-red-800' : 'border-border'} bg-card`}>
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border">
        <div className="flex items-center gap-3">
          <span className="text-sm font-medium text-foreground">{t('policyEditor.rule', { n: index + 1 })}</span>
          <Segmented
            value={rule.effect}
            disabled={readOnly}
            onChange={(effect) => onChange({ effect })}
            options={[
              { value: 'Allow', label: t('policyEditor.allow') },
              { value: 'Deny', label: t('policyEditor.deny'), tone: 'deny' },
            ]}
          />
        </div>
        {!readOnly && (
          <Button variant="ghost" size="sm" onClick={onRemove} title={t('policyEditor.remove')}>
            <Trash2 className="h-4 w-4 text-red-500" />
          </Button>
        )}
      </div>

      <div className="p-4 grid gap-5 lg:grid-cols-2">
        <div>
          <span className={label}>{t('policyEditor.who')}</span>
          <div className="space-y-2">
            <label className={radio}>
              <input type="radio" className="mt-1" disabled={readOnly} checked={rule.everyone} onChange={() => onChange({ everyone: true })} />
              <span>
                {t('policyEditor.everyone')}
                <span className="block text-xs text-muted-foreground">{t('policyEditor.everyoneHint')}</span>
              </span>
            </label>
            <label className={radio}>
              <input type="radio" className="mt-1" disabled={readOnly} checked={!rule.everyone} onChange={() => onChange({ everyone: false })} />
              <span>{t('policyEditor.specificUsers')}</span>
            </label>
            {!rule.everyone && (
              <div className="pl-6">
                <ChipList
                  values={rule.users}
                  show={principalName}
                  placeholder={t('policyEditor.userPlaceholder')}
                  valid={(v) => v.length > 0}
                  disabled={readOnly}
                  options={tenantUsers.map((u) => ({ value: userARN(u), label: u }))}
                  optionsLabel={t('policyEditor.addUser')}
                  onChange={(users) =>
                    // A name typed in is a user name; IDs come only from policies set before.
                    onChange({ users: users.map((u) => (u.startsWith('arn:') || rule.users.includes(u) ? u : userARN(u))) })
                  }
                />
              </div>
            )}
          </div>

          <span className={label + ' mt-5'}>{t('policyEditor.where')}</span>
          <div className="space-y-2">
            <label className={radio}>
              <input type="radio" className="mt-1" disabled={readOnly} checked={!byPrefix}
                onChange={() => {
                  setByPrefix(false);
                  onChange({ prefix: '' });
                }} />
              <span>{t('policyEditor.wholeBucket')}</span>
            </label>
            <label className={radio}>
              <input type="radio" className="mt-1" disabled={readOnly} checked={byPrefix}
                onChange={() => setByPrefix(true)} />
              <span>
                {t('policyEditor.prefix')}
                <span className="block text-xs text-muted-foreground">{t('policyEditor.prefixHint')}</span>
              </span>
            </label>
            {byPrefix && (
              <input
                value={rule.prefix}
                disabled={readOnly}
                onChange={(e) => onChange({ prefix: e.target.value.replace(/\*/g, '') })}
                placeholder={t('policyEditor.prefixPlaceholder')}
                className="ml-6 px-2 py-1 text-sm border border-border bg-card text-foreground rounded-md w-56"
              />
            )}
          </div>

          <span className={label + ' mt-5'}>{t('policyEditor.when')}</span>
          <div className="space-y-3">
            <div className="space-y-2">
              <span className="text-sm text-foreground mr-2">{t('policyEditor.addresses')}</span>
              <Segmented
                value={rule.addresses}
                disabled={readOnly}
                onChange={(addresses) => onChange({ addresses })}
                options={[
                  { value: 'any', label: t('policyEditor.anyAddress') },
                  { value: 'from', label: t('policyEditor.fromAddresses') },
                  { value: 'notFrom', label: t('policyEditor.notFromAddresses') },
                ]}
              />
              {rule.addresses !== 'any' && (
                <ChipList
                  values={rule.sourceIps}
                  show={(v) => v}
                  placeholder={t('policyEditor.addressPlaceholder')}
                  valid={isAddressRange}
                  disabled={readOnly}
                  onChange={(sourceIps) => onChange({ sourceIps })}
                />
              )}
            </div>
            <div className="space-y-2">
              <span className="text-sm text-foreground mr-2">{t('policyEditor.transport')}</span>
              <Segmented
                value={rule.transport}
                disabled={readOnly}
                onChange={(transport) => onChange({ transport })}
                options={[
                  { value: 'any', label: t('policyEditor.anyTransport') },
                  { value: 'secure', label: t('policyEditor.secureTransport') },
                  { value: 'insecure', label: t('policyEditor.insecureTransport') },
                ]}
              />
            </div>
          </div>
        </div>

        <div>
          <span className={label}>{t('policyEditor.what')}</span>
          <label className={radio + ' mb-2'}>
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 rounded border-border"
              disabled={readOnly || !!rule.prefix}
              checked={rule.allActions}
              onChange={(e) => onChange({ allActions: e.target.checked })}
            />
            <span>{t('policyEditor.allActions')}</span>
          </label>
          {!rule.allActions && (
            <div className="rounded-md border border-border divide-y divide-border max-h-72 overflow-y-auto">
              {groups.map((group) => {
                const visible = group.permissions.filter((p) => offered(p.action));
                if (visible.length === 0) return null;
                const on = visible.filter((p) => selected.has(p.action)).length;
                const expanded = open[group.name];
                return (
                  <div key={group.name}>
                    <div className="flex items-center gap-2 px-3 py-1.5">
                      <button type="button" className="text-muted-foreground hover:text-foreground"
                        onClick={() => setOpen((prev) => ({ ...prev, [group.name]: !expanded }))} aria-label={group.name}>
                        {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                      </button>
                      <label className="flex items-center gap-2 cursor-pointer flex-1">
                        <input
                          type="checkbox"
                          className="h-4 w-4 rounded border-border"
                          disabled={readOnly}
                          checked={on === visible.length}
                          ref={(el) => {
                            if (el) el.indeterminate = on > 0 && on < visible.length;
                          }}
                          onChange={() => toggleGroup(group)}
                        />
                        <span className="text-sm font-medium text-foreground">
                          {t('permissionGroup.' + group.name, { ns: 'users', defaultValue: group.name })}
                        </span>
                      </label>
                      <span className="text-xs text-muted-foreground">{on}/{visible.length}</span>
                    </div>
                    {expanded && (
                      <div className="px-3 pb-2 pl-10 grid gap-1">
                        {visible.map((permission) => (
                          <label key={permission.action} className="flex items-start gap-2 cursor-pointer" title={permission.description || permission.action}>
                            <input
                              type="checkbox"
                              className="h-4 w-4 mt-0.5 rounded border-border"
                              disabled={readOnly}
                              checked={selected.has(permission.action)}
                              onChange={() => toggle(permission.action)}
                            />
                            <span>
                              <span className="text-sm text-foreground">{permission.label}</span>
                              <code className="block text-[11px] font-mono text-muted-foreground">{permission.action}</code>
                            </span>
                          </label>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
              {otherActions.length > 0 && (
                <div className="px-3 py-2">
                  <span className="text-sm font-medium text-foreground">{t('policyEditor.otherActions')}</span>
                  <div className="pl-7 grid gap-1 mt-1">
                    {otherActions.map((action) => (
                      <label key={action} className="flex items-center gap-2 cursor-pointer">
                        <input type="checkbox" className="h-4 w-4 rounded border-border" disabled={readOnly} checked onChange={() => toggle(action)} />
                        <code className="text-xs font-mono text-foreground">{action}</code>
                      </label>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {problem && !readOnly && (
        <div className="px-4 pb-3 text-xs text-red-600 dark:text-red-400">{t(problem)}</div>
      )}
    </div>
  );
}
