'use client';

import React, { useState } from 'react';
import { useParams } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  ArrowLeft,
  Shield,
  Users,
  Globe,
  Key,
  Archive,
  AlertTriangle,
  Save,
  Trash2
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { BucketPolicy, BucketSettings } from '@/types';

export default function BucketSettingsPage() {
  const params = useParams();
  const bucketName = params.bucket as string;
  const queryClient = useQueryClient();

  const [isEditing, setIsEditing] = useState(false);
  const [settings, setSettings] = useState<Partial<BucketSettings>>({});

  const { data: bucket, isLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => APIClient.getBucket(bucketName),
    onSuccess: (data) => {
      if (data?.data?.settings) {
        setSettings(data.data.settings);
      }
    },
  });

  const updateSettingsMutation = useMutation({
    mutationFn: (data: Partial<BucketSettings>) =>
      APIClient.updateBucketSettings(bucketName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsEditing(false);
    },
  });

  const deleteBucketMutation = useMutation({
    mutationFn: () => APIClient.deleteBucket(bucketName),
    onSuccess: () => {
      window.location.href = '/buckets';
    },
  });

  const handleSaveSettings = () => {
    updateSettingsMutation.mutate(settings);
  };

  const handleDeleteBucket = () => {
    if (confirm(`Are you sure you want to delete bucket "${bucketName}"? This action cannot be undone and will delete all objects in the bucket.`)) {
      deleteBucketMutation.mutate();
    }
  };

  const updateSetting = (key: keyof BucketSettings, value: any) => {
    setSettings(prev => ({ ...prev, [key]: value }));
  };

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
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => window.history.back()}
            className="gap-2"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Settings</h1>
            <p className="text-muted-foreground">Configure {bucketName} bucket settings</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {isEditing ? (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setIsEditing(false);
                  if (bucket?.data?.settings) {
                    setSettings(bucket.data.settings);
                  }
                }}
              >
                Cancel
              </Button>
              <Button
                onClick={handleSaveSettings}
                disabled={updateSettingsMutation.isPending}
                className="gap-2"
              >
                <Save className="h-4 w-4" />
                {updateSettingsMutation.isPending ? 'Saving...' : 'Save Changes'}
              </Button>
            </>
          ) : (
            <Button onClick={() => setIsEditing(true)}>
              Edit Settings
            </Button>
          )}
        </div>
      </div>

      {/* General Settings */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Globe className="h-5 w-5" />
            General Settings
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">Bucket Name</label>
              <Input value={bucketName} disabled />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Region</label>
              <Input
                value={settings.region || bucket?.data?.region || 'us-east-1'}
                onChange={(e) => updateSetting('region', e.target.value)}
                disabled={!isEditing}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Description</label>
            <Input
              value={settings.description || ''}
              onChange={(e) => updateSetting('description', e.target.value)}
              placeholder="Optional bucket description"
              disabled={!isEditing}
            />
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="public-read"
              checked={settings.publicRead || false}
              onChange={(e) => updateSetting('publicRead', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="public-read" className="text-sm font-medium">
              Enable public read access
            </label>
          </div>
        </CardContent>
      </Card>

      {/* Access Control */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            Access Control
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="block-public-access"
              checked={settings.blockPublicAccess !== false}
              onChange={(e) => updateSetting('blockPublicAccess', !e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="block-public-access" className="text-sm font-medium">
              Block all public access
            </label>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="versioning"
              checked={settings.versioning || false}
              onChange={(e) => updateSetting('versioning', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="versioning" className="text-sm font-medium">
              Enable versioning
            </label>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">CORS Configuration</label>
            <textarea
              value={settings.corsConfiguration || ''}
              onChange={(e) => updateSetting('corsConfiguration', e.target.value)}
              placeholder="Enter CORS configuration in JSON format"
              disabled={!isEditing}
              rows={4}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            />
          </div>
        </CardContent>
      </Card>

      {/* Lifecycle Management */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Archive className="h-5 w-5" />
            Lifecycle Management
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="lifecycle-enabled"
              checked={settings.lifecycleEnabled || false}
              onChange={(e) => updateSetting('lifecycleEnabled', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="lifecycle-enabled" className="text-sm font-medium">
              Enable lifecycle management
            </label>
          </div>

          {settings.lifecycleEnabled && (
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium mb-2">
                  Transition to IA after (days)
                </label>
                <Input
                  type="number"
                  value={settings.transitionToIA || 30}
                  onChange={(e) => updateSetting('transitionToIA', parseInt(e.target.value))}
                  disabled={!isEditing}
                  min="1"
                />
              </div>
              <div>
                <label className="block text-sm font-medium mb-2">
                  Delete after (days)
                </label>
                <Input
                  type="number"
                  value={settings.deleteAfter || 365}
                  onChange={(e) => updateSetting('deleteAfter', parseInt(e.target.value))}
                  disabled={!isEditing}
                  min="1"
                />
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Encryption */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            Encryption
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Server-side encryption</label>
            <select
              value={settings.encryption || 'none'}
              onChange={(e) => updateSetting('encryption', e.target.value)}
              disabled={!isEditing}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            >
              <option value="none">No encryption</option>
              <option value="AES256">AES-256</option>
              <option value="aws:kms">AWS KMS</option>
            </select>
          </div>

          {settings.encryption === 'aws:kms' && (
            <div>
              <label className="block text-sm font-medium mb-2">KMS Key ID</label>
              <Input
                value={settings.kmsKeyId || ''}
                onChange={(e) => updateSetting('kmsKeyId', e.target.value)}
                placeholder="Enter KMS Key ID"
                disabled={!isEditing}
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Monitoring */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Users className="h-5 w-5" />
            Monitoring & Logging
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="access-logging"
              checked={settings.accessLogging || false}
              onChange={(e) => updateSetting('accessLogging', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="access-logging" className="text-sm font-medium">
              Enable access logging
            </label>
          </div>

          {settings.accessLogging && (
            <div>
              <label className="block text-sm font-medium mb-2">Log destination bucket</label>
              <Input
                value={settings.logDestinationBucket || ''}
                onChange={(e) => updateSetting('logDestinationBucket', e.target.value)}
                placeholder="logs-bucket-name"
                disabled={!isEditing}
              />
            </div>
          )}

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="notifications"
              checked={settings.notifications || false}
              onChange={(e) => updateSetting('notifications', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="notifications" className="text-sm font-medium">
              Enable event notifications
            </label>
          </div>
        </CardContent>
      </Card>

      {/* Danger Zone */}
      <Card className="border-red-200">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-red-600">
            <AlertTriangle className="h-5 w-5" />
            Danger Zone
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between p-4 border border-red-200 rounded-md">
            <div>
              <h3 className="font-semibold text-red-600">Delete Bucket</h3>
              <p className="text-sm text-muted-foreground">
                Permanently delete this bucket and all its contents. This action cannot be undone.
              </p>
            </div>
            <Button
              variant="destructive"
              onClick={handleDeleteBucket}
              disabled={deleteBucketMutation.isPending}
              className="gap-2"
            >
              <Trash2 className="h-4 w-4" />
              {deleteBucketMutation.isPending ? 'Deleting...' : 'Delete Bucket'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}