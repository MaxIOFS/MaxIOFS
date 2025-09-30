'use client';

import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  Settings,
  Save,
  Server,
  Shield,
  Database,
  Globe,
  Key,
  Monitor,
  HardDrive,
  Users,
  AlertTriangle,
  CheckCircle,
  RefreshCw
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { SystemConfig } from '@/types';

export default function SettingsPage() {
  const [isEditing, setIsEditing] = useState(false);
  const [config, setConfig] = useState<Partial<SystemConfig>>({});
  const queryClient = useQueryClient();

  const { data: systemConfig, isLoading } = useQuery({
    queryKey: ['systemConfig'],
    queryFn: APIClient.getSystemConfig,
    onSuccess: (data) => {
      if (data?.data) {
        setConfig(data.data);
      }
    },
  });

  const updateConfigMutation = useMutation({
    mutationFn: (data: Partial<SystemConfig>) => APIClient.updateSystemConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['systemConfig'] });
      setIsEditing(false);
    },
  });

  const testConnectionMutation = useMutation({
    mutationFn: () => APIClient.testStorageConnection(),
  });

  const handleSaveConfig = () => {
    updateConfigMutation.mutate(config);
  };

  const handleTestConnection = () => {
    testConnectionMutation.mutate();
  };

  const updateConfig = (key: keyof SystemConfig, value: any) => {
    setConfig(prev => ({ ...prev, [key]: value }));
  };

  const updateNestedConfig = (section: string, key: string, value: any) => {
    setConfig(prev => ({
      ...prev,
      [section]: {
        ...(prev[section as keyof SystemConfig] as object || {}),
        [key]: value
      }
    }));
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
        <div>
          <h1 className="text-3xl font-bold tracking-tight">System Settings</h1>
          <p className="text-muted-foreground">
            Configure MaxIOFS system-wide settings
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isEditing ? (
            <>
              <Button
                variant="outline"
                onClick={() => {
                  setIsEditing(false);
                  if (systemConfig?.data) {
                    setConfig(systemConfig.data);
                  }
                }}
              >
                Cancel
              </Button>
              <Button
                onClick={handleSaveConfig}
                disabled={updateConfigMutation.isPending}
                className="gap-2"
              >
                <Save className="h-4 w-4" />
                {updateConfigMutation.isPending ? 'Saving...' : 'Save Changes'}
              </Button>
            </>
          ) : (
            <Button onClick={() => setIsEditing(true)} className="gap-2">
              <Settings className="h-4 w-4" />
              Edit Settings
            </Button>
          )}
        </div>
      </div>

      {/* Server Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Server className="h-5 w-5" />
            Server Configuration
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">HTTP Port</label>
              <Input
                type="number"
                value={config.server?.httpPort || 8080}
                onChange={(e) => updateNestedConfig('server', 'httpPort', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">HTTPS Port</label>
              <Input
                type="number"
                value={config.server?.httpsPort || 8443}
                onChange={(e) => updateNestedConfig('server', 'httpsPort', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Bind Address</label>
            <Input
              value={config.server?.bindAddress || '0.0.0.0'}
              onChange={(e) => updateNestedConfig('server', 'bindAddress', e.target.value)}
              disabled={!isEditing}
            />
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-https"
              checked={config.server?.enableHTTPS || false}
              onChange={(e) => updateNestedConfig('server', 'enableHTTPS', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-https" className="text-sm font-medium">
              Enable HTTPS
            </label>
          </div>
        </CardContent>
      </Card>

      {/* Storage Backend Configuration */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="h-5 w-5" />
            Storage Backend
          </CardTitle>
          <Button
            variant="outline"
            size="sm"
            onClick={handleTestConnection}
            disabled={testConnectionMutation.isPending}
            className="gap-2"
          >
            <RefreshCw className={`h-4 w-4 ${testConnectionMutation.isPending ? 'animate-spin' : ''}`} />
            Test Connection
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Storage Type</label>
            <select
              value={config.storage?.type || 'filesystem'}
              onChange={(e) => updateNestedConfig('storage', 'type', e.target.value)}
              disabled={!isEditing}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            >
              <option value="filesystem">File System</option>
              <option value="s3">Amazon S3</option>
              <option value="gcs">Google Cloud Storage</option>
              <option value="azure">Azure Blob Storage</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Storage Path</label>
            <Input
              value={config.storage?.path || './data'}
              onChange={(e) => updateNestedConfig('storage', 'path', e.target.value)}
              disabled={!isEditing}
              placeholder="./data"
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">Max File Size (MB)</label>
              <Input
                type="number"
                value={config.storage?.maxFileSize || 100}
                onChange={(e) => updateNestedConfig('storage', 'maxFileSize', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Cleanup Interval (hours)</label>
              <Input
                type="number"
                value={config.storage?.cleanupInterval || 24}
                onChange={(e) => updateNestedConfig('storage', 'cleanupInterval', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
          </div>

          {testConnectionMutation.data && (
            <div className={`rounded-md p-4 ${testConnectionMutation.data.success ? 'bg-green-50' : 'bg-red-50'}`}>
              <div className={`flex items-center gap-2 text-sm ${testConnectionMutation.data.success ? 'text-green-700' : 'text-red-700'}`}>
                {testConnectionMutation.data.success ? (
                  <CheckCircle className="h-4 w-4" />
                ) : (
                  <AlertTriangle className="h-4 w-4" />
                )}
                {testConnectionMutation.data.message}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Security Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            Security Settings
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="require-auth"
              checked={config.security?.requireAuth !== false}
              onChange={(e) => updateNestedConfig('security', 'requireAuth', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="require-auth" className="text-sm font-medium">
              Require Authentication
            </label>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">JWT Secret</label>
              <Input
                type="password"
                value={config.security?.jwtSecret || ''}
                onChange={(e) => updateNestedConfig('security', 'jwtSecret', e.target.value)}
                disabled={!isEditing}
                placeholder="Enter JWT secret key"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Token Expiry (hours)</label>
              <Input
                type="number"
                value={config.security?.tokenExpiry || 24}
                onChange={(e) => updateNestedConfig('security', 'tokenExpiry', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-cors"
              checked={config.security?.enableCORS || false}
              onChange={(e) => updateNestedConfig('security', 'enableCORS', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-cors" className="text-sm font-medium">
              Enable CORS
            </label>
          </div>

          {config.security?.enableCORS && (
            <div>
              <label className="block text-sm font-medium mb-2">Allowed Origins</label>
              <Input
                value={config.security?.allowedOrigins || '*'}
                onChange={(e) => updateNestedConfig('security', 'allowedOrigins', e.target.value)}
                disabled={!isEditing}
                placeholder="https://example.com, https://app.example.com"
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Monitoring & Logging */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Monitor className="h-5 w-5" />
            Monitoring & Logging
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-metrics"
              checked={config.monitoring?.enableMetrics || false}
              onChange={(e) => updateNestedConfig('monitoring', 'enableMetrics', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-metrics" className="text-sm font-medium">
              Enable Prometheus Metrics
            </label>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Log Level</label>
            <select
              value={config.monitoring?.logLevel || 'info'}
              onChange={(e) => updateNestedConfig('monitoring', 'logLevel', e.target.value)}
              disabled={!isEditing}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            >
              <option value="debug">Debug</option>
              <option value="info">Info</option>
              <option value="warn">Warning</option>
              <option value="error">Error</option>
            </select>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Log Format</label>
            <select
              value={config.monitoring?.logFormat || 'json'}
              onChange={(e) => updateNestedConfig('monitoring', 'logFormat', e.target.value)}
              disabled={!isEditing}
              className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
            >
              <option value="json">JSON</option>
              <option value="text">Text</option>
              <option value="structured">Structured</option>
            </select>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-audit-log"
              checked={config.monitoring?.enableAuditLog || false}
              onChange={(e) => updateNestedConfig('monitoring', 'enableAuditLog', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-audit-log" className="text-sm font-medium">
              Enable Audit Logging
            </label>
          </div>
        </CardContent>
      </Card>

      {/* User Management */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Users className="h-5 w-5" />
            User Management
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="allow-registration"
              checked={config.users?.allowRegistration || false}
              onChange={(e) => updateNestedConfig('users', 'allowRegistration', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="allow-registration" className="text-sm font-medium">
              Allow User Registration
            </label>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">Default Role</label>
              <select
                value={config.users?.defaultRole || 'read'}
                onChange={(e) => updateNestedConfig('users', 'defaultRole', e.target.value)}
                disabled={!isEditing}
                className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
              >
                <option value="read">Read Only</option>
                <option value="write">Read/Write</option>
                <option value="admin">Administrator</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Max Access Keys per User</label>
              <Input
                type="number"
                value={config.users?.maxAccessKeys || 5}
                onChange={(e) => updateNestedConfig('users', 'maxAccessKeys', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Password Policy</label>
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <input
                  type="checkbox"
                  id="require-strong-password"
                  checked={config.users?.passwordPolicy?.requireStrong || false}
                  onChange={(e) => updateNestedConfig('users', 'passwordPolicy', {
                    ...config.users?.passwordPolicy,
                    requireStrong: e.target.checked
                  })}
                  disabled={!isEditing}
                  className="rounded border-gray-300"
                />
                <label htmlFor="require-strong-password" className="text-sm">
                  Require strong passwords (8+ chars, mixed case, numbers, symbols)
                </label>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Performance Settings */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Database className="h-5 w-5" />
            Performance Settings
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-2">Cache Size (MB)</label>
              <Input
                type="number"
                value={config.performance?.cacheSize || 128}
                onChange={(e) => updateNestedConfig('performance', 'cacheSize', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-2">Worker Threads</label>
              <Input
                type="number"
                value={config.performance?.workerThreads || 4}
                onChange={(e) => updateNestedConfig('performance', 'workerThreads', parseInt(e.target.value))}
                disabled={!isEditing}
              />
            </div>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-compression"
              checked={config.performance?.enableCompression || false}
              onChange={(e) => updateNestedConfig('performance', 'enableCompression', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-compression" className="text-sm font-medium">
              Enable Compression
            </label>
          </div>

          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="enable-encryption"
              checked={config.performance?.enableEncryption || false}
              onChange={(e) => updateNestedConfig('performance', 'enableEncryption', e.target.checked)}
              disabled={!isEditing}
              className="rounded border-gray-300"
            />
            <label htmlFor="enable-encryption" className="text-sm font-medium">
              Enable Server-Side Encryption
            </label>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}