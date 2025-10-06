'use client';

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import SweetAlert from '@/lib/sweetalert';
import {
  ArrowLeft,
  Database,
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
  const router = useRouter();
  const queryClient = useQueryClient();
  const { isGlobalAdmin } = useCurrentUser();
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
    encryptionEnabled: true,
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
      // Invalidate buckets cache so the list refreshes when we navigate back
      queryClient.invalidateQueries({ queryKey: ['buckets'] });
      SweetAlert.toast('success', `Bucket "${config.name}" created successfully`);
      router.push('/buckets');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // Validations
    if (!config.name) {
      SweetAlert.toast('error', 'Bucket name is required');
      return;
    }

    if (!/^[a-z0-9][a-z0-9.-]*[a-z0-9]$/.test(config.name)) {
      SweetAlert.toast('error', 'Invalid bucket name. Must contain only lowercase letters, numbers, dots and hyphens');
      return;
    }

    if (config.objectLockEnabled && !config.versioningEnabled) {
      SweetAlert.toast('error', 'Object Lock requires versioning to be enabled');
      return;
    }

    if (config.objectLockEnabled && !config.retentionMode) {
      SweetAlert.toast('error', 'You must select a retention mode for Object Lock');
      return;
    }

    if (config.objectLockEnabled && config.retentionDays === 0 && config.retentionYears === 0) {
      SweetAlert.toast('error', 'You must specify at least days or years of retention');
      return;
    }

    const result = await SweetAlert.fire({
      icon: 'question',
      title: 'Create bucket?',
      html: `
        <div class="text-left space-y-2">
          <p><strong>Name:</strong> ${config.name}</p>
          <p><strong>Region:</strong> ${config.region}</p>
          ${config.objectLockEnabled ? `
            <p class="text-yellow-600"><strong>⚠️ Object Lock:</strong> ${config.retentionMode}</p>
            <p class="text-sm text-red-600">This bucket will be IMMUTABLE and cannot be deleted until retention expires</p>
          ` : ''}
          ${config.versioningEnabled ? '<p><strong>✓</strong> Versioning enabled</p>' : ''}
          ${config.encryptionEnabled ? '<p><strong>✓</strong> Encryption enabled</p>' : ''}
        </div>
      `,
      showCancelButton: true,
      confirmButtonText: 'Create Bucket',
      cancelButtonText: 'Cancel',
    });

    if (result.isConfirmed) {
      SweetAlert.loading('Creating bucket...', `Configuring "${config.name}"`);
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
    { id: 'general', label: 'General', icon: Database },
    { id: 'objectlock', label: 'Object Lock & WORM', icon: Lock },
    { id: 'lifecycle', label: 'Lifecycle', icon: Clock },
    { id: 'encryption', label: 'Encryption', icon: Shield },
    { id: 'access', label: 'Access Control', icon: Settings },
  ];

  return (
    <div className="space-y-6 p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => router.push('/buckets')}
            className="gap-2"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Create New Bucket</h1>
            <p className="text-muted-foreground">
              Configure all advanced options for your new S3 bucket
            </p>
          </div>
        </div>
      </div>

      <form onSubmit={handleSubmit}>
        {/* Tabs */}
        <div className="border-b border-gray-200 mb-6">
          <nav className="-mb-px flex space-x-8">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              return (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`
                    flex items-center gap-2 py-4 px-1 border-b-2 font-medium text-sm
                    ${activeTab === tab.id
                      ? 'border-blue-500 text-blue-600'
                      : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                    }
                  `}
                >
                  <Icon className="h-5 w-5" />
                  {tab.label}
                </button>
              );
            })}
          </nav>
        </div>

        {/* Tab Content */}
        <div className="space-y-6">
          {/* General Tab */}
          {activeTab === 'general' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Database className="h-5 w-5" />
                  General Configuration
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Bucket Name <span className="text-red-500">*</span>
                  </label>
                  <Input
                    value={config.name}
                    onChange={(e) => updateConfig('name', e.target.value.toLowerCase())}
                    placeholder="my-s3-bucket"
                    required
                  />
                  <p className="text-xs text-muted-foreground mt-1">
                    Only lowercase letters, numbers, dots (.) and hyphens (-). Must be globally unique.
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-medium mb-2">Region</label>
                  <select
                    value={config.region}
                    onChange={(e) => updateConfig('region', e.target.value)}
                    className="w-full px-3 py-2 border border-input bg-background rounded-md"
                  >
                    <option value="us-east-1">US East (N. Virginia)</option>
                    <option value="us-west-2">US West (Oregon)</option>
                    <option value="eu-west-1">EU (Ireland)</option>
                    <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
                  </select>
                </div>

                {/* Ownership Section - Only visible to global admins */}
                {isGlobalAdmin && (
                  <div className="border-t pt-4 mt-4">
                    <h3 className="text-sm font-semibold mb-3 flex items-center gap-2">
                      <Shield className="h-4 w-4" />
                      Ownership & Access Control
                    </h3>

                    <div className="space-y-4">
                      <div>
                        <label className="block text-sm font-medium mb-2">Owner Type (Optional)</label>
                        <select
                          value={config.ownerType}
                          onChange={(e) => {
                            updateConfig('ownerType', e.target.value);
                            updateConfig('ownerId', ''); // Reset owner ID when type changes
                          }}
                          className="w-full px-3 py-2 border border-input bg-background rounded-md"
                        >
                          <option value="">No specific owner (Global)</option>
                          <option value="user">User</option>
                          <option value="tenant">Tenant</option>
                        </select>
                      </div>

                      {config.ownerType === 'user' && (
                        <div>
                          <label className="block text-sm font-medium mb-2">Owner User</label>
                          <select
                            value={config.ownerId}
                            onChange={(e) => updateConfig('ownerId', e.target.value)}
                            className="w-full px-3 py-2 border border-input bg-background rounded-md"
                          >
                            <option value="">Select a user</option>
                            {users?.map((user) => (
                              <option key={user.id} value={user.id}>
                                {user.username} ({user.email || 'no email'})
                              </option>
                            ))}
                          </select>
                        </div>
                      )}

                      {config.ownerType === 'tenant' && (
                        <div>
                          <label className="block text-sm font-medium mb-2">Owner Tenant</label>
                          <select
                            value={config.ownerId}
                            onChange={(e) => updateConfig('ownerId', e.target.value)}
                            className="w-full px-3 py-2 border border-input bg-background rounded-md"
                          >
                            <option value="">Select a tenant</option>
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
                          className="rounded border-gray-300"
                        />
                        <label htmlFor="isPublic" className="text-sm font-medium">
                          Make bucket public
                        </label>
                      </div>
                      <p className="text-xs text-muted-foreground ml-6">
                        Public buckets allow anonymous access. Not recommended for sensitive data.
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
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="versioning" className="text-sm font-medium">
                    Enable Object Versioning
                  </label>
                </div>
                <p className="text-xs text-muted-foreground ml-6">
                  Keeps multiple versions of each object. Required for Object Lock.
                </p>

                <div>
                  <label className="block text-sm font-medium mb-2">Tags</label>
                  <div className="space-y-2">
                    {config.tags.map((tag, index) => (
                      <div key={index} className="flex gap-2">
                        <Input
                          placeholder="Key"
                          value={tag.key}
                          onChange={(e) => updateTag(index, 'key', e.target.value)}
                          className="flex-1"
                        />
                        <Input
                          placeholder="Value"
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
                      + Add Tag
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {/* Object Lock Tab */}
          {activeTab === 'objectlock' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Lock className="h-5 w-5" />
                  Object Lock & WORM (Write Once Read Many)
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4">
                  <div className="flex gap-2">
                    <AlertTriangle className="h-5 w-5 text-yellow-600 flex-shrink-0" />
                    <div className="text-sm text-yellow-800">
                      <p className="font-semibold mb-1">⚠️ Important: Object Lock is PERMANENT</p>
                      <ul className="list-disc list-inside space-y-1">
                        <li>Once enabled, it CANNOT BE DISABLED</li>
                        <li>Objects cannot be deleted until their retention period expires</li>
                        <li>COMPLIANCE mode: Not even the root user can delete objects</li>
                        <li>GOVERNANCE mode: Only users with special permissions can bypass</li>
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
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="objectLock" className="text-sm font-medium">
                    Enable Object Lock (WORM)
                  </label>
                </div>

                {config.objectLockEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium mb-2">
                        Retention Mode <span className="text-red-500">*</span>
                      </label>
                      <div className="space-y-3">
                        <label className="flex items-start space-x-3 p-3 border rounded-md cursor-pointer hover:bg-gray-50">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="COMPLIANCE"
                            checked={config.retentionMode === 'COMPLIANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm">COMPLIANCE (Regulatory Compliance)</div>
                            <div className="text-xs text-muted-foreground mt-1">
                              <strong>Maximum protection.</strong> No one can delete or modify objects, not even the root user.
                              Ideal for legal and regulatory requirements (SEC, FINRA, HIPAA).
                            </div>
                          </div>
                        </label>

                        <label className="flex items-start space-x-3 p-3 border rounded-md cursor-pointer hover:bg-gray-50">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="GOVERNANCE"
                            checked={config.retentionMode === 'GOVERNANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm">GOVERNANCE</div>
                            <div className="text-xs text-muted-foreground mt-1">
                              <strong>Flexible protection.</strong> Users with special permissions can bypass retention.
                              Useful for testing and scenarios where flexibility is needed.
                            </div>
                          </div>
                        </label>
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-medium mb-2">Retention Days</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionDays}
                          onChange={(e) => updateConfig('retentionDays', parseInt(e.target.value) || 0)}
                          placeholder="0"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium mb-2">Retention Years</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionYears}
                          onChange={(e) => updateConfig('retentionYears', parseInt(e.target.value) || 0)}
                          placeholder="0"
                        />
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Specify at least one. Objects cannot be deleted during this period.
                    </p>

                    {config.retentionMode === 'COMPLIANCE' && (
                      <div className="bg-red-50 border border-red-200 rounded-md p-3 text-sm text-red-800">
                        <strong>⚠️ COMPLIANCE mode selected:</strong> This bucket will be IMMUTABLE.
                        Objects cannot be deleted under any circumstances until retention expires.
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          )}

          {/* Lifecycle Tab */}
          {activeTab === 'lifecycle' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Clock className="h-5 w-5" />
                  Lifecycle Policies
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="lifecycle"
                    checked={config.lifecycleEnabled}
                    onChange={(e) => updateConfig('lifecycleEnabled', e.target.checked)}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="lifecycle" className="text-sm font-medium">
                    Enable Lifecycle Rules
                  </label>
                </div>

                {config.lifecycleEnabled && (
                  <div>
                    <label className="block text-sm font-medium mb-2">
                      Object Expiration (days)
                    </label>
                    <Input
                      type="number"
                      min="0"
                      value={config.expirationDays}
                      onChange={(e) => updateConfig('expirationDays', parseInt(e.target.value) || 0)}
                      placeholder="365"
                    />
                    <p className="text-xs text-muted-foreground mt-1">
                      Objects are permanently deleted after N days (0 = no expiration)
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Encryption Tab */}
          {activeTab === 'encryption' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Shield className="h-5 w-5" />
                  Encryption
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="encryption"
                    checked={config.encryptionEnabled}
                    onChange={(e) => updateConfig('encryptionEnabled', e.target.checked)}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="encryption" className="text-sm font-medium">
                    Enable Default Encryption
                  </label>
                </div>

                {config.encryptionEnabled && (
                  <div className="bg-blue-50 border border-blue-200 rounded-md p-3">
                    <p className="text-sm text-blue-800">
                      <strong>AES-256 Encryption</strong> - All objects will be encrypted at rest using AES-256-GCM
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Access Control Tab */}
          {activeTab === 'access' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Settings className="h-5 w-5" />
                  Public Access Control
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="bg-blue-50 border border-blue-200 rounded-md p-3 text-sm text-blue-800">
                  <Info className="h-4 w-4 inline mr-2" />
                  It is recommended to block all public access unless you specifically need to share data.
                </div>

                <div className="space-y-3">
                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicAcls}
                      onChange={(e) => updateConfig('blockPublicAcls', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Block public ACLs</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.ignorePublicAcls}
                      onChange={(e) => updateConfig('ignorePublicAcls', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Ignore existing public ACLs</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicPolicy}
                      onChange={(e) => updateConfig('blockPublicPolicy', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Block public bucket policies</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.restrictPublicBuckets}
                      onChange={(e) => updateConfig('restrictPublicBuckets', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Restrict public buckets</span>
                  </label>
                </div>

              </CardContent>
            </Card>
          )}
        </div>

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-4 mt-8 pt-6 border-t">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.push('/buckets')}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={createBucketMutation.isPending}
            className="gap-2"
          >
            {createBucketMutation.isPending ? (
              <>
                <Loading size="sm" />
                Creating...
              </>
            ) : (
              <>
                <CheckCircle2 className="h-4 w-4" />
                Create Bucket
              </>
            )}
          </Button>
        </div>
      </form>
    </div>
  );
}
