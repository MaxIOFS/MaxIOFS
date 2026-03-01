import React, { useState } from 'react';
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
  CheckCircle2
} from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useCurrentUser } from '@/hooks/useCurrentUser';

interface BucketCreationConfig {
  // General
  name: string;
  region: string;

  // Ownership
  ownerId: string;
  ownerType: 'user' | 'tenant' | '';
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

  // Fetch users and tenants for ownership selection
  const { data: users } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

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
      // Refetch to update immediately (buckets list and tenant counters)
      queryClient.refetchQueries({ queryKey: ['buckets'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });
      ModalManager.toast('success', t('bucketCreatedSuccess', { name: config.name }));
      navigate('/buckets');
    },
    onError: (error) => {
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

    if (config.objectLockEnabled && !config.retentionMode) {
      ModalManager.toast('error', t('validationRetentionModeRequired'));
      return;
    }

    if (config.objectLockEnabled && config.retentionDays === 0 && config.retentionYears === 0) {
      ModalManager.toast('error', t('validationRetentionPeriodRequired'));
      return;
    }

    const result = await ModalManager.fire({
      icon: 'question',
      title: t('confirmTitle'),
      html: `
        <div class="text-left space-y-2">
          <p><strong>${t('confirmName')}</strong> ${config.name}</p>
          <p><strong>${t('confirmRegion')}</strong> ${config.region}</p>
          ${config.objectLockEnabled ? `
            <p class="text-yellow-600"><strong>${t('confirmObjectLock')}</strong> ${config.retentionMode}</p>
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
            className="gap-2 hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 transition-all duration-200"
          >
            <ArrowLeft className="h-4 w-4" />
            {t('back')}
          </Button>
          <div>
            <h1 className="text-3xl font-bold bg-gradient-to-r from-gray-900 via-gray-800 to-gray-900 dark:from-white dark:via-gray-100 dark:to-white bg-clip-text text-transparent">
              {t('title')}
            </h1>
            <p className="text-gray-500 dark:text-gray-400 mt-1">
              {t('subtitle')}
            </p>
          </div>
        </div>
      </div>

      <form onSubmit={handleSubmit}>
        {/* Tabs */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
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
                        ? 'bg-white dark:bg-gray-800 text-brand-600 dark:text-brand-400 shadow-sm'
                        : 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200'
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
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    {t('bucketName')} <span className="text-red-500">*</span>
                  </label>
                  <Input
                    value={config.name}
                    onChange={(e) => updateConfig('name', e.target.value.toLowerCase())}
                    placeholder={t('bucketNamePlaceholder')}
                    className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                    required
                  />
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    {t('bucketNameHelp')}
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('region')}</label>
                  <select
                    value={config.region}
                    onChange={(e) => updateConfig('region', e.target.value)}
                    className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                  >
                    <option value="us-east-1">{t('regionUsEast')}</option>
                    <option value="us-west-2">{t('regionUsWest')}</option>
                    <option value="eu-west-1">{t('regionEuWest')}</option>
                    <option value="ap-southeast-1">{t('regionApSoutheast')}</option>
                  </select>
                </div>

                {/* Ownership Section - Only visible to global admins */}
                {isGlobalAdmin && (
                  <div className="border-t border-gray-200 dark:border-gray-700 pt-4 mt-4">
                    <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-3 flex items-center gap-2">
                      <Shield className="h-4 w-4" />
                      {t('ownershipTitle')}
                    </h3>

                    <div className="space-y-4">
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('ownerType')}</label>
                        <select
                          value={config.ownerType}
                          onChange={(e) => {
                            updateConfig('ownerType', e.target.value);
                            updateConfig('ownerId', ''); // Reset owner ID when type changes
                          }}
                          className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                        >
                          <option value="">{t('noOwner')}</option>
                          <option value="user">{t('ownerUser')}</option>
                          <option value="tenant">{t('ownerTenant')}</option>
                        </select>
                      </div>

                      {config.ownerType === 'user' && (
                        <div>
                          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('ownerUserLabel')}</label>
                          <select
                            value={config.ownerId}
                            onChange={(e) => updateConfig('ownerId', e.target.value)}
                            className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                          >
                            <option value="">{t('selectUser')}</option>
                            {users?.map((user) => (
                              <option key={user.id} value={user.id}>
                                {user.username} ({user.email || t('noEmail')})
                              </option>
                            ))}
                          </select>
                        </div>
                      )}

                      {config.ownerType === 'tenant' && (
                        <div>
                          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('ownerTenantLabel')}</label>
                          <select
                            value={config.ownerId}
                            onChange={(e) => updateConfig('ownerId', e.target.value)}
                            className="w-full px-3 py-2 border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
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
                          className="rounded border-gray-300 dark:border-gray-600"
                        />
                        <label htmlFor="isPublic" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                          {t('makePublic')}
                        </label>
                      </div>
                      <p className="text-xs text-gray-500 dark:text-gray-400 ml-6">
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
                    className="rounded border-gray-300 dark:border-gray-600"
                  />
                  <label htmlFor="versioning" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {t('enableVersioning')}
                  </label>
                </div>
                <p className="text-xs text-gray-500 dark:text-gray-400 ml-6">
                  {t('versioningHelp')}
                </p>

                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('tags')}</label>
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
                        <li>{t('objectLockWarning3')}</li>
                        <li>{t('objectLockWarning4')}</li>
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
                    className="rounded border-gray-300 dark:border-gray-600"
                  />
                  <label htmlFor="objectLock" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {t('enableObjectLock')}
                  </label>
                </div>

                {config.objectLockEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                        {t('retentionMode')} <span className="text-red-500">*</span>
                      </label>
                      <div className="space-y-3">
                        <label className="flex items-start space-x-3 p-3 border border-gray-200 dark:border-gray-700 rounded-md cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="COMPLIANCE"
                            checked={config.retentionMode === 'COMPLIANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm text-gray-900 dark:text-white">{t('complianceMode')}</div>
                            <div
                              className="text-xs text-gray-500 dark:text-gray-400 mt-1"
                              dangerouslySetInnerHTML={{ __html: t('complianceModeDesc') }}
                            />
                          </div>
                        </label>

                        <label className="flex items-start space-x-3 p-3 border border-gray-200 dark:border-gray-700 rounded-md cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="GOVERNANCE"
                            checked={config.retentionMode === 'GOVERNANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm text-gray-900 dark:text-white">{t('governanceMode')}</div>
                            <div
                              className="text-xs text-gray-500 dark:text-gray-400 mt-1"
                              dangerouslySetInnerHTML={{ __html: t('governanceModeDesc') }}
                            />
                          </div>
                        </label>
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('retentionDays')}</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionDays}
                          onChange={(e) => updateConfig('retentionDays', parseInt(e.target.value) || 0)}
                          placeholder="0"
                          className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('retentionYears')}</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionYears}
                          onChange={(e) => updateConfig('retentionYears', parseInt(e.target.value) || 0)}
                          placeholder="0"
                          className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                        />
                      </div>
                    </div>
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      {t('retentionHelp')}
                    </p>

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
                    className="rounded border-gray-300 dark:border-gray-600"
                  />
                  <label htmlFor="lifecycle" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                    {t('enableLifecycle')}
                  </label>
                </div>

                {config.lifecycleEnabled && (
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      {t('expirationDays')}
                    </label>
                    <Input
                      type="number"
                      min="0"
                      value={config.expirationDays}
                      onChange={(e) => updateConfig('expirationDays', parseInt(e.target.value) || 0)}
                      placeholder="365"
                      className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
                    />
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      {t('expirationHelp')}
                    </p>
                  </div>
                )}
            </div>
          )}

          {/* Encryption Tab */}
          {activeTab === 'encryption' && (
            <div className="space-y-4">
                {/* Server encryption status warning */}
                {!serverEncryptionEnabled && (
                  <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-md p-3 mb-4">
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
                    checked={serverEncryptionEnabled && config.encryptionEnabled}
                    onChange={(e) => updateConfig('encryptionEnabled', e.target.checked)}
                    disabled={!serverEncryptionEnabled}
                    className={`rounded border-gray-300 dark:border-gray-600 ${!serverEncryptionEnabled ? 'opacity-50 cursor-not-allowed' : ''}`}
                  />
                  <label
                    htmlFor="encryption"
                    className={`text-sm font-medium ${!serverEncryptionEnabled ? 'text-gray-400 dark:text-gray-500 cursor-not-allowed' : 'text-gray-700 dark:text-gray-300'}`}
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
                  <div className="bg-gray-50 dark:bg-gray-800/30 border border-gray-200 dark:border-gray-700 rounded-md p-3">
                    <p
                      className="text-sm text-gray-600 dark:text-gray-400"
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
                      className="rounded border-gray-300 dark:border-gray-600"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">{t('blockPublicAcls')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.ignorePublicAcls}
                      onChange={(e) => updateConfig('ignorePublicAcls', e.target.checked)}
                      className="rounded border-gray-300 dark:border-gray-600"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">{t('ignorePublicAcls')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicPolicy}
                      onChange={(e) => updateConfig('blockPublicPolicy', e.target.checked)}
                      className="rounded border-gray-300 dark:border-gray-600"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">{t('blockPublicPolicy')}</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.restrictPublicBuckets}
                      onChange={(e) => updateConfig('restrictPublicBuckets', e.target.checked)}
                      className="rounded border-gray-300 dark:border-gray-600"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">{t('restrictPublicBuckets')}</span>
                  </label>
                </div>
            </div>
          )}
            </div>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-4 mt-8 pt-6 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 rounded-lg p-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate('/buckets')}
            className="border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gradient-to-r hover:from-gray-50 hover:to-gray-100 dark:hover:from-gray-700 dark:hover:to-gray-700/50 transition-all duration-200"
          >
            {t('cancel')}
          </Button>
          <Button
            type="submit"
            disabled={createBucketMutation.isPending}
            className="gap-2 bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md hover:shadow-lg transition-all duration-200"
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
