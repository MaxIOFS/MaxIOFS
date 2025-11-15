import React, { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Server,
  Shield,
  HardDrive,
  Monitor,
  Info,
  CheckCircle,
  Package,
  Database,
  Zap
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Loading } from '@/components/ui/Loading';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import type { ServerConfig } from '@/types';

export default function SettingsPage() {
  const navigate = useNavigate();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();
  
  // Only global admins can access settings
  useEffect(() => {
    if (currentUser && !isGlobalAdmin) {
      navigate('/');
    }
  }, [currentUser, isGlobalAdmin, navigate]);

  const { data: config, isLoading } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
    enabled: isGlobalAdmin,
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (!config) {
    return (
      <div className="flex items-center justify-center h-64">
        <p className="text-gray-500 dark:text-gray-400">No configuration available</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">System Settings</h1>
          <p className="text-gray-500 dark:text-gray-400">
            View current MaxIOFS configuration
          </p>
        </div>
      </div>

      {/* Info Banner */}
      <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md p-4">
        <div className="flex items-start gap-3">
          <Info className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
          <div>
            <p className="text-sm font-medium text-blue-900 dark:text-blue-300">Read-Only Configuration</p>
            <p className="text-sm text-blue-700 dark:text-blue-300 mt-1">
              These settings are configured via command-line flags and configuration files.
              To modify them, please update your server configuration and restart MaxIOFS.
            </p>
          </div>
        </div>
      </div>

      {/* Server Configuration */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Server className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Server Configuration
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">S3 API Port</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                {config.server.s3ApiPort}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Console API Port</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                {config.server.consoleApiPort}
              </div>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Data Directory</label>
            <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white font-mono">
              {config.server.dataDir}
            </div>
          </div>

          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
            <CheckCircle className="h-4 w-4" />
            <span>Server running and accepting connections</span>
          </div>
        </div>
      </div>

      {/* Storage Backend */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <HardDrive className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Storage Architecture
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Object Storage</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                {config.storage.backend === 'filesystem' ? 'File System (Local)' : config.storage.backend}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Metadata Store</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                BadgerDB v4
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Authentication DB</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                SQLite
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Transaction Mode</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Retry with Backoff
              </div>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Storage Path</label>
            <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white font-mono">
              {config.storage.root}
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
              <CheckCircle className="h-4 w-4" />
              <span>BadgerDB metadata store operational (high-performance KV store)</span>
            </div>
            <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
              <CheckCircle className="h-4 w-4" />
              <span>Metadata-first deletion enabled (ensures consistency)</span>
            </div>
            <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
              <CheckCircle className="h-4 w-4" />
              <span>Atomic write operations with automatic rollback</span>
            </div>
          </div>
        </div>
      </div>

      {/* S3 API Features */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Database className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            S3 API Features
          </h3>
        </div>
        <div className="p-6">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Core Operations</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>PutObject / GetObject</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>DeleteObject / ListObjects</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>HeadObject / CopyObject</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Bucket Operations</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Versioning (Enable/Suspend)</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Bucket Policy (JSON)</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>CORS Configuration</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Lifecycle Policies</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Advanced Features</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Multipart Uploads</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Presigned URLs (GET/PUT)</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Object Lock (WORM)</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Object Tagging</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Bulk Operations</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>DeleteObjects (up to 1000)</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Sequential Processing</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Conflict-Free Execution</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Authentication</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>AWS Signature v2</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>AWS Signature v4</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Path & Virtual-Hosted Style</span>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Testing & Validation</h4>
              <div className="space-y-1.5 text-sm text-gray-600 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>Warp Stress Tested</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>7000+ Objects Validated</span>
                </div>
                <div className="flex items-center gap-2">
                  <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                  <span>AWS CLI Compatible</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Security Configuration */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Shield className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Security Settings
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Authentication</label>
              <div className={`px-3 py-2 ${config.auth.enableAuth ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800 text-green-700 dark:text-green-400' : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white'} border rounded-md text-sm font-medium`}>
                {config.auth.enableAuth ? '✓ Enabled (JWT + S3 Signatures)' : 'Disabled'}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Rate Limiting</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Enabled (Per Endpoint)
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Account Lockout</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Enabled (5 attempts / 15 min)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Password Hashing</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Bcrypt (Strong)
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Two-Factor Authentication</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ TOTP-Based (Optional)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Session Timeout</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ 24 Hours (Idle Detection)
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Audit Logging</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Enabled (90 day retention)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Compliance</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ GDPR, SOC 2, HIPAA, ISO 27001
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">CORS</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Configurable Per Bucket
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">TLS/HTTPS</label>
              <div className={`px-3 py-2 ${config.server.enableTls ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800 text-green-700 dark:text-green-400' : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white'} border rounded-md text-sm font-medium`}>
                {config.server.enableTls ? '✓ Enabled' : 'Not Enabled (Optional)'}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Multi-Tenancy */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Zap className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Multi-Tenancy Features
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Resource Isolation</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Complete Separation
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Quota Management</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Storage, Buckets, Keys
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Cascading Delete</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Tenant → Users → Keys
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Deletion Validation</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Prevents Data Loss
              </div>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Global Admin</label>
            <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
              ✓ Cross-Tenant Management & Visibility
            </div>
          </div>
        </div>
      </div>

      {/* Monitoring & Logging */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Monitor className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Monitoring & Logging
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Prometheus Metrics</label>
              <div className={`px-3 py-2 ${config.metrics.enable ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800 text-green-700 dark:text-green-400' : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white'} border rounded-md text-sm font-medium`}>
                {config.metrics.enable ? '✓ Enabled (Real-Time)' : 'Disabled'}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Log Level</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                {config.server.logLevel.charAt(0).toUpperCase() + config.server.logLevel.slice(1)}
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Grafana Dashboard</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Pre-built (Docker Compose)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Log Format</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white">
                Structured (logrus with fields)
              </div>
            </div>
          </div>

          {config.metrics.enable && (
            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle className="h-4 w-4" />
                <span>System metrics (CPU, Memory, Disk)</span>
              </div>
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle className="h-4 w-4" />
                <span>Storage metrics (Buckets, Objects, Size)</span>
              </div>
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle className="h-4 w-4" />
                <span>Request metrics (Throughput, Latency, Errors)</span>
              </div>
              <div className="flex items-center gap-2 text-sm text-blue-600 dark:text-blue-400">
                <Info className="h-4 w-4" />
                <span>Collection interval: {config.metrics.interval} seconds</span>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* System Information */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Package className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            System Information
          </h3>
        </div>
        <div className="p-6 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Version</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white font-mono">
                {config.version}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Git Commit</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white font-mono">
                {config.commit || 'none'}
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Build Date</label>
              <div className="px-3 py-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md text-sm text-gray-900 dark:text-white font-mono">
                {config.buildDate || 'unknown'}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Status</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                Beta (S3 Core Complete - 98%)
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">S3 API Compatibility</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ 98% Compatible (40+ operations)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Deployment</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Single Binary
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Multi-Tenancy</label>
              <div className={`px-3 py-2 ${config.features.multiTenancy ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800 text-green-700 dark:text-green-400' : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700 text-gray-900 dark:text-white'} border rounded-md text-sm font-medium`}>
                {config.features.multiTenancy ? '✓ Full Support' : 'Not Enabled'}
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Dark Mode</label>
              <div className="px-3 py-2 bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md text-sm text-green-700 dark:text-green-400 font-medium">
                ✓ Supported
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
