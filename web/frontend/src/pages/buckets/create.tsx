import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import ModalManager from '@/lib/modals';
import {
  ArrowLeft,
  Box,
  Lock,
  Shield,
  Clock,
  Settings,
  AlertTriangle,
  Info,
  CheckCircle2,
  Server
} from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { escapeHtml, formatBytes } from '@/lib/utils';

interface BucketCreationConfig {
  // General
  name: string;
  region: string;

  // Cluster node selection (only used in cluster mode)
  nodeId: string;

  // Ownership
  ownerId: string;
  ownerType: 'tenant' | '';
  isPublic: boolean;

  // Versioning
  versioningEnabled: boolean;

  // Object Lock & WORM
  objectLockEnabled: boolean;
  retentionMode: 'GOVERNANCE' | 'COMPLIANCE' | '';
  retentionDays: number;
  retentionYears: number;

  // Encryption
  encryptionEnabled: boolean;
  encryptionType: 'AES256';

  // Access Control
  blockPublicAccess: boolean;
  blockPublicAcls: boolean;
  ignorePublicAcls: boolean;
  blockPublicPolicy: boolean;
  restrictPublicBuckets: boolean;

  // Lifecycle
  lifecycleEnabled: boolean;
  expirationDays: number;

  // Tags
  tags: Array<{ key: string; value: string }>;
}

export default function CreateBucketPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { isGlobalAdmin } = useCurrentUser();
  const { t } = useTranslation('createBucket');
  const [activeTab, setActiveTab] = useState<'general' | 'objectlock' | 'lifecycle' | 'encryption' | 'access'>('general');
  const [config, setConfig] = useState<BucketCreationConfig>({
    name: '',
    region: 'us-east-1',
    nodeId: '',
    ownerId: '',
    ownerType: '',
    isPublic: false,
    versioningEnabled: false,
    objectLockEnabled: false,
    retentionMode: '',
    retentionDays: 0,
    retentionYears: 0,
    encryptionEnabled: false,
    encryptionType: 'AES256',
    blockPublicAccess: true,
    blockPublicAcls: true,
    ignorePublicAcls: true,
    blockPublicPolicy: true,
    restrictPublicBuckets: true,
    lifecycleEnabled: false,
    expirationDays: 365,
    tags: [],
  });

  // Fetch server config to check if encryption is enabled
  const { data: serverConfig } = useQuery({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
  });

  // Check if server has encryption enabled
  const serverEncryptionEnabled = serverConfig?.storage?.enableEncryption ?? false;

  // Check if cluster mode is active
  const isClusterMode = serverConfig?.cluster?.enabled === true;

  // Fetch cluster nodes only in cluster mode (for node selector in bucket creation)
  const { data: clusterNodes } = useQuery({
    queryKey: ['clusterNodes'],
    queryFn: APIClient.listClusterNodes,
    enabled: isClusterMode,
  });

  // When cluster nodes load, auto-select the node with the most free space
  useEffect(() => {
    if (!isClusterMode || !clusterNodes || clusterNodes.length === 0) return;
    // Already selected — don't override user choice
    if (config.nodeId) return;
    const best = [...clusterNodes].sort((a, b) => {
      const freeA = (a.capacity_total || 0) - (a.capacity_used || 0);
      const freeB = (b.capacity_total || 0) - (b.capacity_used || 0);
      // Prefer local node on tie (or when capacities are 0)
      if (freeA === freeB) return a.is_local ? -1 : 1;
      return freeB - freeA;
    })[0];
    if (best) setConfig(prev => ({ ...prev, nodeId: best.id }));
  }, [isClusterMode, clusterNodes]); // intentionally omit config.nodeId to only run on first load

  // When server config loads and encryption is globally active, auto-enable it for the new bucket
  useEffect(() => {
    if (serverEncryptionEnabled) {
      setConfig(prev => ({ ...prev, encryptionEnabled: true }));
    }
  }, [serverEncryptionEnabled]);

  // Fetch tenants for ownership selection
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  const createBucketMutation = useMutation({
    mutationFn: async () => {
      // Construct the creation payload
      const payload: any = {
        name: config.name,
        region: config.region,
        // Only include node_id in cluster mode so standalone deployments are unaffected
        node_id: isClusterMode && config.nodeId ? config.nodeId : undefined,
        ownerId: config.ownerId || undefined,
        ownerType: config.ownerType || undefined,
        isPublic: config.isPublic,
        versioning: config.versioningEnabled ? { status: 'Enabled' } : undefined,
        encryption: config.encryptionEnabled ? {
          type: config.encryptionType,
        } : undefined,
        objectLock: config.objectLockEnabled ? {
          enabled: true,
          mode: config.retentionMode,
          days: config.retentionDays > 0 ? config.retentionDays : undefined,
          years: config.retentionYears > 0 ? config.retentionYears : undefined,
        } : undefined,
        publicAccessBlock: {
          blockPublicAcls: config.blockPublicAcls,
          ignorePublicAcls: config.ignorePublicAcls,
          blockPublicPolicy: config.blockPublicPolicy,
          restrictPublicBuckets: config.restrictPublicBuckets,
        },
        lifecycle: config.lifecycleEnabled && config.expirationDays > 0 ? {
          rules: [{
            id: 'expiration',
            status: 'Enabled',
            expiration: {
              days: config.expirationDays,
            },
          }],
        } : undefined,
        tags: config.tags.length > 0 ? config.tags : undefined,
      };

      return APIClient.createBucket(payload);
    },
    onSuccess: () => {
      ModalManager.close();
      // Refetch to update immediately (buckets list and tenant counters)
      queryClient.refetchQueries({ queryKey: ['buckets'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.toast('success', t('bucketCreatedSuccess', { name: config.name }));
      navigate('/buckets');
    },
    onError: (error) => {
      ModalManager.close();
      ModalManager.apiError(error);
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // Validations
    if (!config.name) {
      ModalManager.toast('error', t('validationNameRequired'));
      return;
    }

    if (!/^[a-z0-9][a-z0-9.-]*[a-z0-9]$/.test(config.name)) {
      ModalManager.toast('error', t('validationNameInvalid'));
      return;
    }

    if (config.objectLockEnabled && !config.versioningEnabled) {
      ModalManager.toast('error', t('validationVersioningRequired'));
      return;
    }

    // Retention mode and period are optional. If one is provided, both must be set.
    if (config.objectLockEnabled && config.retentionMode && config.retentionDays === 0 && config.retentionYears === 0) {
      ModalManager.toast('error', t('validationRetentionPeriodRequired'));
      return;
    }
    if (config.objectLockEnabled && !config.retentionMode && (config.retentionDays > 0 || config.retentionYears > 0)) {
      ModalManager.toast('error', t('validationRetentionModeRequired'));
      return;
    }

    const result = await ModalManager.fire({
      icon: 'question',
      title: t('confirmTitle'),
      html: `
        <div class="text-left space-y-2">
          <p><strong>${t('confirmName')}</strong> ${escapeHtml(config.name)}</p>
          <p><strong>${t('confirmRegion')}</strong> ${escapeHtml(config.region)}</p>
          ${config.objectLockEnabled ? `
            <p class="text-yellow-600"><strong>${t('confirmObjectLock')}</strong> ${escapeHtml(config.retentionMode)}</p>
            <p class="text-sm text-red-600">${t('confirmObjectLockWarning')}</p>
          ` : ''}
          ${config.versioningEnabled ? `<p>${t('confirmVersioning')}</p>` : ''}
          ${config.encryptionEnabled ? `<p>${t('confirmEncryption')}</p>` : ''}
        </div>
      `,
      showCancelButton: true,
      confirmButtonText: t('confirmButton'),
      cancelButtonText: t('cancel'),
    });

    if (result.isConfirmed) {
      ModalManager.loading(t('loadingTitle'), t('loadingMessage', { name: config.name }));
      createBucketMutation.mutate();
    }
  };

  const updateConfig = (key: keyof BucketCreationConfig, value: any) => {
    setConfig(prev => ({ ...prev, [key]: value }));
  };

  const addTag = () => {
    setConfig(prev => ({
      ...prev,
      tags: [...prev.tags, { key: '', value: '' }],
    }));
  };

  const removeTag = (index: number) => {
    setConfig(prev => ({
      ...prev,
      tags: prev.tags.filter((_, i) => i !== index),
    }));
  };

  const updateTag = (index: number, field: 'key' | 'value', value: string) => {
    setConfig(prev => ({
      ...prev,
      tags: prev.tags.map((tag, i) => i === index ? { ...tag, [field]: value } : tag),
    }));
  };

  const tabs = [
    { id: 'general', label: t('tabGeneral'), icon: Box },
    { id: 'objectlock', label: t('tabObjectLock'), icon: Lock },
    { id: 'lifecycle', label: t('tabLifecycle'), icon: Clock },
    { id: 'encryption', label: t('tabEncryption'), icon: Shield },
    { id: 'access', label: t('tabAccessControl'), icon: Settings },
  ];

  return (
    <div className="space-y-6 p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate('/buckets')}
            className="gap-2 hover:bg-secondary transition-all duration-200"
          >
            <ArrowLeft className="h-4 w-4" />
            {t('back')}
          </Button>
          <div>
            <h1 className="text-2xl font-bold text-foreground">
              {t('title')}
            </h1>
            <p className="text-sm text-muted-foreground mt-1">
              {t('subtitle')}
            </p>
          </div>
        </div>
      </div>

      <form onSubmit={handleSubmit}>
        {/* Tabs */}
        <div className="bg-card rounded-lg border border-border shadow-sm">
          <div className="p-6">
            {/* Tabs Navigation */}
            <div className="flex space-x-1 bg-gray-100 dark:bg-gray-900 rounded-lg p-1 mb-6">
              {tabs.map((tab) => {
                const Icon = tab.icon;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => setActiveTab(tab.id as any)}
                    className={`flex-1 flex items-center justify-center space-x-2 px-4 py-3 font-medium text-sm rounded-md transition-all duration-200 ${
                      activeTab === tab.id
                        ? 'bg-card text-brand-600 dark:text-brand-400 shadow-sm'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{tab.label}</span>
                  </button>
                );
              })}
            </div>

            {/* Tab Content */}
            <div className="space-y-6">
          {/* General Tab */}
          {activeTab === 'general' && (
            <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-foreground mb-2">
                    {t('bucketName')} <span className="text-red-500">*</span>
                  </label>
                  <Input
                    value={config.name}
                    onChange={(e) => updateConfig('name', e.target.value.toLowerCase())}
                    placeholder={t('bucketNamePlaceholder')}
                    className="bg-card text-foreground border-border"
                    required
                  />
                  <p className="text-xs text-muted-foreground mt-1">
                    {t('bucketNameHelp')}
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-medium text-foreground mb-2">{t('region')}</label>
                  <select
                    value={config.region}
                    onChange={(e) => updateConfig('region', e.target.value)}
                    className="w-full px-3 py-2 border border-border bg-card text-foreground rounded-md"
                  >
                    <option value="us-east-1">{t('regionUsEast')}</option>
                    <option value="us-west-2">{t('regionUsWest')}</option>
                    <option value="eu-west-1">{t('regionEuWest')}</option>
                    <option value="ap-southeast-1">{t('regionApSoutheast')}</option>
                  </select>
                </div>

                {/* Ownership Section - Only visible to global admins */}
                {isGlobalAdmin && (
                  <div className="border-t border-border pt-4 mt-4">
                    <h3 className="text-sm font-semibold text-foreground mb-3 flex items-center gap-2">
                      <Shield className="h-4 w-4" />
                      {t('ownershipTitle')}
                    </h3>

                    <div className="space-y-4">
                      <div>
                        <label className="block text-sm font-medium text-foreground mb-2">{t('ownerType')}</label>
                        <select
                          value={config.ownerType}
                          onChange={(e) => {
                            updateConfig('ownerType', e.target.value);
                            updateConfig('ownerId', '');
                          }}
                          className="w-full px-3 py-2 border border-border bg-card text-foreground rounded-md"
                        >
                          <option value="">{t('noOwner')}</option>
                          <option value="tenant">{t('ownerTenant')}</option>
                        </select>
                      </div>

                      {config.ownerType === 'tenant' && (
                        <div>
                          <label className="block text-sm font-medium text-foreground mb-2">{t('ownerTenantLabel')}</label>
                          <select
                            value={config.ownerId}
                            onChange={(e) => updateConfig('ownerId', e.target.value)}
                            className="w-full px-3 py-2 border border-border bg-card text-foreground rounded-md"
                          >
                            <option value="">{t('selectTenant')}</option>
                            {tenants?.map((tenant) => (
                              <option key={tenant.id} value={tenant.id}>
                                {tenant.displayName} ({tenant.name})
                              </option>
                            ))}
                          </select>
                        </div>
                      )}

                      <div className="flex items-center space-x-2">
                        <input
                          type="checkbox"
                          id="isPublic"
                          checked={config.isPublic}
                          onChange={(e) => updateConfig('isPublic', e.target.checked)}
                          className="rounded border-border"
                        />
                        <label htmlFor="isPublic" className="text-sm font-medium text-foreground">
                          {t('makePublic')}
                        </label>
                      </div>
                      <p className="text-xs text-muted-foreground ml-6">
                        {t('makePublicHelp')}
                      </p>
                    </div>
                  </div>
                )}

                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="versioning"
                    checked={config.versioningEnabled}
                    onChange={(e) => updateConfig('versioningEnabled', e.target.checked)}
                    className="rounded border-border"
                  />
                  <label htmlFor="versioning" className="text-sm font-medium text-foreground">
                    {t('enableVersioning')}
                  </label>
                </div>
                <p className="text-xs text-muted-foreground ml-6">
                  {t('versioningHelp')}
                </p>

                {/* Node selector — only shown in cluster mode */}
                {isClusterMode && clusterNodes && clusterNodes.length > 0 && (
                  <div className="border-t border-border pt-4 mt-4">
                    <h3 className="text-sm font-semibold text-foreground mb-3 flex items-center gap-2">
                      <Server className="h-4 w-4" />
                      {t('targetNode')}
                    </h3>
                    <div className="space-y-2">
                      {clusterNodes.map(node => {
                        const freeBytes = (node.capacity_total || 0) - (node.capacity_used || 0);
                        const hasDiskInfo = node.capacity_total > 0;
                        const freeLabel = hasDiskInfo
                          ? t('targetNodeFree', { free: formatBytes(freeBytes) })
                          : t('targetNodeUnknown');
                        const isSelected = config.nodeId === node.id;
                        return (
                          <label
                            key={node.id}
                            className={`flex items-center justify-between p-3 border rounded-md cursor-pointer transition-colors ${
                              isSelected
                                ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20'
                                : 'border-border hover:bg-secondary'
                            }`}
                          >
                            <div className="flex items-center gap-3">
                              <input
                                type="radio"
                                name="targetNode"
                                value={node.id}
                                checked={isSelected}
                                onChange={() => updateConfig('nodeId', node.id)}
                                className="accent-brand-600"
                              />
                              <div>
                                <span className="text-sm font-medium text-foreground">{node.name}</span>
                                {node.is_local && (
                                  <span className="ml-2 text-xs text-muted-foreground">{t('targetNodeLocal')}</span>
                                )}
                                <div className="text-xs text-muted-foreground mt-0.5">
                                  {node.health_status} · {freeLabel}
                                </div>
                              </div>
                            </div>
                            {hasDiskInfo && (
                              <div className="text-right">
                                <div className="w-24 bg-secondary rounded-full h-1.5 overflow-hidden">
                                  <div
                                    className="h-full bg-brand-500 rounded-full"
                                    style={{
                                      width: `${Math.min(100, ((node.capacity_used || 0) / node.capacity_total) * 100).toFixed(0)}%`
                                    }}
                                  />
                                </div>
                                <span className="text-xs text-muted-foreground">
                                  {Math.min(100, Math.round(((node.capacity_used || 0) / node.capacity_total) * 100))}% used
                                </span>
                              </div>
                            )}
                          </label>
                        );
                      })}
                    </div>
                    <p className="text-xs text-muted-foreground mt-2">{t('targetNodeHelp')}</p>
                  </div>
                )}

                <div>
                  <label className="block text-sm font-medium text-foreground mb-2">{t('tags')}</label>
                  <div className="space-y-2">
                    {config.tags.map((tag, index) => (
                      <div key={index} className="flex gap-2">
                        <Input
                          placeholder={t('keyPlaceholder')}
                          value={tag.key}
                          onChange={(e) => updateTag(index, 'key', e.target.value)}
                          className="flex-1"
                        />
                        <Input
                          placeholder={t('valuePlaceholder')}
                          value={tag.value}
                          onChange={(e) => updateTag(index, 'value', e.target.value)}
                          className="flex-1"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          onClick={() => removeTag(index)}
                        >
                          ✕
                        </Button>
                      </div>
                    ))}
                    <Button
                      type="button"
                      variant="outline"
                      onClick={addTag}
                      className="w-full"
                    >
                      {t('addTag')}
                    </Button>
                  </div>
                </div>
            </div>
          )}

          {/* Object Lock Tab */}
          {activeTab === 'objectlock' && (
            <div className="space-y-4">
                <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-md p-4">
                  <div className="flex gap-2">
                    <AlertTriangle className="h-5 w-5 text-yellow-600 dark:text-yellow-400 flex-shrink-0" />
                    <div className="text-sm text-yellow-800 dark:text-yellow-300">
                      <p className="font-semibold mb-1">{t('objectLockWarningTitle')}</p>
                      <ul className="list-disc list-inside space-y-1">
                        <li>{t('objectLockWarning1')}</li>
                        <li>{t('objectLockWarning2')}</li>
                      </ul>
                    </div>
                  </div>
                </div>

                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="objectLock"
                    checked={config.objectLockEnabled}
                    onChange={(e) => {
                      updateConfig('objectLockEnabled', e.target.checked);
                      if (e.target.checked) {
                        updateConfig('versioningEnabled', true);
                      }
                    }}
                    className="rounded border-border"
                  />
                  <label htmlFor="objectLock" className="text-sm font-medium text-foreground">
                    {t('enableObjectLock')}
                  </label>
                </div>

                {config.objectLockEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium text-foreground mb-2">
                        {t('retentionMode')}
                      </label>
                      <div className="space-y-3">
                        <label className="flex items-start space-x-3 p-3 border border-border rounded-md cursor-pointer hover:bg-secondary">
                          <input
                            type="radio"
                            name="retentionMode"
                            value=""
                            checked={config.retentionMode === ''}
                            onChange={() => updateConfig('retentionMode', '')}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm text-foreground">{t('noRetentionMode')}</div>
                            <div
                              className="text-xs text-muted-foreground mt-1"
                              dangerouslySetInnerHTML={{ __html: t('noRetentionModeDesc') }}
                            />
                          </div>
                        </label>

                        <label className="flex items-start space-x-3 p-3 border border-border rounded-md cursor-pointer hover:bg-secondary">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="COMPLIANCE"
                            checked={config.retentionMode === 'COMPLIANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm text-foreground">{t('complianceMode')}</div>
                            <div
                              className="text-xs text-muted-foreground mt-1"
                              dangerouslySetInnerHTML={{ __html: t('complianceModeDesc') }}
                            />
                          </div>
                        </label>

                        <label className="flex items-start space-x-3 p-3 border border-border rounded-md cursor-pointer hover:bg-secondary">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="GOVERNANCE"
                            checked={config.retentionMode === 'GOVERNANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm text-foreground">{t('governanceMode')}</div>
                            <div
                              className="text-xs text-muted-foreground mt-1"
                              dangerouslySetInnerHTML={{ __html: t('governanceModeDesc') }}
                            />
                          </div>
                        </label>
                      </div>
                    </div>

                    {config.retentionMode !== '' && (
                      <>
                        <div className="grid grid-cols-2 gap-4">
                          <div>
                            <label className="block text-sm font-medium text-foreground mb-2">{t('retentionDays')}</label>
                            <Input
                              type="number"
                              min="0"
                              value={config.retentionDays}
                              onChange={(e) => updateConfig('retentionDays', parseInt(e.target.value) || 0)}
                              placeholder="0"
                              className="bg-card text-foreground border-border"
                            />
                          </div>
                          <div>
                            <label className="block text-sm font-medium text-foreground mb-2">{t('retentionYears')}</label>
                            <Input
                              type="number"
                              min="0"
                              value={config.retentionYears}
                              onChange={(e) => updateConfig('retentionYears', parseInt(e.target.value) || 0)}
                              placeholder="0"
                              className="bg-card text-foreground border-border"
                            />
                          </div>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {t('retentionHelp')}
                        </p>
                      </>
                    )}

                    {config.retentionMode === 'COMPLIANCE' && (
                      <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-md p-3 text-sm text-red-800 dark:text-red-300">
                        {t('complianceWarning')}
                      </div>
                    )}
                  </>
                )}
            </div>
          )}

          {/* Lifecycle Tab */}
          {activeTab === 'lifecycle' && (
            <div className="space-y-4">
                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="lifecycle"
                    checked={config.lifecycleEnabled}
                    onChange={(e) => updateConfig('lifecycleEnabled', e.target.checked)}
                    className="rounded border-border"
                  />
                  <label htmlFor="lifecycle" className="text-sm font-medium text-foreground">
                    {t('enableLifecycle')}
                  </label>
                </div>

                {config.lifecycleEnabled && (
                  <div>
                    <label className="block text-sm font-medium text-foreground mb-2">
                      {t('expirationDays')}
                    </label>
                    <Input
                      type="number"
                      min="0"
                      value={config.expirationDays}
                      onChange={(e) => updateConfig('expirationDays', parseInt(e.target.value) || 0)}
                      placeholder="365"
                      className="bg-card text-foreground border-border"
                    />
                    <p className="text-xs text-muted-foreground mt-1">
                      {t('expirationHelp')}
                    </p>
                  </div>
                )}
            </div>
          )}

          {/* Encryption Tab */}
          {activeTab === 'encryption' && (
            <div className="space-y-4">
                {/* Server encryption globally enabled — informational banner */}
                {serverEncryptionEnabled && (
                  <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md p-3">
                    <div className="flex items-start gap-2">
                      <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                      <div>
                        <p className="text-sm font-semibold text-green-800 dark:text-green-300">
                          {t('serverEncryptionActive')}
                        </p>
                        <p
                          className="text-xs text-green-700 dark:text-green-400 mt-1"
                          dangerouslySetInnerHTML={{ __html: t('serverEncryptionActiveHelp') }}
                        />
                      </div>
                    </div>
                  </div>
                )}

                {/* Server encryption disabled — warning */}
                {!serverEncryptionEnabled && (
                  <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-md p-3">
                    <div className="flex items-start gap-2">
                      <AlertTriangle className="h-5 w-5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                      <div>
                        <p className="text-sm font-semibold text-amber-800 dark:text-amber-300">
                          {t('serverEncryptionDisabled')}
                        </p>
                        <p
                          className="text-xs text-amber-700 dark:text-amber-400 mt-1"
                          dangerouslySetInnerHTML={{ __html: t('serverEncryptionHelp') }}
                        />
                      </div>
                    </div>
                  </div>
                )}

                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="encryption"
                    checked={config.encryptionEnabled}
                    onChange={(e) => updateConfig('encryptionEnabled', e.target.checked)}
                    disabled={!serverEncryptionEnabled}
                    className={`rounded border-border ${!serverEncryptionEnabled ? 'opacity-50 cursor-not-allowed' : ''}`}
                  />
                  <label
                    htmlFor="encryption"
                    className={`text-sm font-medium ${!serverEncryptionEnabled ? 'text-muted-foreground cursor-not-allowed' : 'text-foreground'}`}
                  >
                    {t('enableEncryption')}
                  </label>
                </div>

                {serverEncryptionEnabled && config.encryptionEnabled && (
                  <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md p-3">
                    <p
                      className="text-sm text-blue-800 dark:text-blue-300"
                      dangerouslySetInnerHTML={{ __html: t('encryptionInfo') }}
                    />
                  </div>
                )}

                {serverEncryptionEnabled && !config.encryptionEnabled && (
                  <div className="bg-gray-50 dark:bg-gray-800/30 border border-border rounded-md p-3">
                    <p
                      className="text-sm text-muted-foreground"
                      dangerouslySetInnerHTML={{ __html: t('noEncryptionInfo') }}
                    />
                  </div>
                )}
            </div>
          )}

          {/* Access Control Tab */}
          {activeTab === 'access' && (
            <div className="space-y-4">
                <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md p-3 text-sm text-blue-800 dark:text-blue-300">
                  <Info className="h-4 w-4 inline mr-2" />
                  {t('accessControlInfo')}
                </div>

                <div className="space-y-3">
                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicAcls}
                      onChange={(e) => updateConfig('blockPublicAcls', e.target.checked)}
                      className="rounded border-border"
                    />
                    <span className="text-sm text-foreground">{t('blockPublicAcls')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.ignorePublicAcls}
                      onChange={(e) => updateConfig('ignorePublicAcls', e.target.checked)}
                      className="rounded border-border"
                    />
                    <span className="text-sm text-foreground">{t('ignorePublicAcls')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicPolicy}
                      onChange={(e) => updateConfig('blockPublicPolicy', e.target.checked)}
                      className="rounded border-border"
                    />
                    <span className="text-sm text-foreground">{t('blockPublicPolicy')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.restrictPublicBuckets}
                      onChange={(e) => updateConfig('restrictPublicBuckets', e.target.checked)}
                      className="rounded border-border"
                    />
                    <span className="text-sm text-foreground">{t('restrictPublicBuckets')}</span>
                  </label>
                </div>
            </div>
          )}
            </div>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-4 mt-8 pt-6 border-t border-border bg-gray-50 dark:bg-gray-800/50 rounded-lg p-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate('/buckets')}
            className="border-border text-foreground hover:bg-gradient-to-r hover:from-gray-50 hover:to-gray-100 dark:hover:from-gray-700 dark:hover:to-gray-700/50 transition-all duration-200"
          >
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={createBucketMutation.isPending}
            className="bg-brand-600 hover:bg-brand-700 text-white"
          >
            {createBucketMutation.isPending ? (
              <>
                <Loading size="sm" />
                {t('creating')}
              </>
            ) : (
              <>
                <CheckCircle2 className="h-4 w-4" />
                {t('createButton')}
              </>
            )}
          </Button>
        </div>
      </form>
    </div>
  );
}
