import React, { useEffect } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Loading } from '@/components/ui/Loading';
import { MetricCard } from '@/components/ui/MetricCard';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import {
  Shield,
  Lock,
  Users,
  AlertTriangle,
  CheckCircle,
  UserX,
  KeyRound,
  Clock,
  Settings,
  HardDrive,
  Bell,
  FileText,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

export default function SecurityPage() {
  const navigate = useNavigate();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();

  // Only global admins can access security page
  useEffect(() => {
    if (currentUser && !isGlobalAdmin) {
      navigate('/');
    }
  }, [currentUser, isGlobalAdmin, navigate]);

  const { data: users = [], isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    refetchInterval: 5000, // Poll every 5 seconds to detect locked accounts
    staleTime: 5000, // Consider data fresh for 5 seconds
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false, // Prevent refetch on window focus
  });

  // Fetch settings for dynamic values
  const { data: settings = [] } = useQuery({
    queryKey: ['settings'],
    queryFn: () => APIClient.listSettings(),
    enabled: isGlobalAdmin,
  });

  // Helper to get setting value
  const getSetting = (key: string, defaultValue: string = 'N/A'): string => {
    const setting = settings.find((s: any) => s.key === key);
    return setting?.value || defaultValue;
  };

  // Parse duration settings (e.g., "24h" -> "24 hours", "15m" -> "15 minutes", "900" -> "15 minutes")
  const formatDuration = (value: string): string => {
    // Handle suffixed values (e.g., "24h", "15m", "90d")
    if (value.endsWith('h')) {
      const hours = parseInt(value);
      return hours === 1 ? '1 hour' : `${hours} hours`;
    }
    if (value.endsWith('m')) {
      const minutes = parseInt(value);
      return minutes === 1 ? '1 minute' : `${minutes} minutes`;
    }
    if (value.endsWith('d')) {
      const days = parseInt(value);
      return days === 1 ? '1 day' : `${days} days`;
    }

    // Handle raw seconds (e.g., "900" -> "15 minutes")
    const seconds = parseInt(value);
    if (!isNaN(seconds)) {
      if (seconds < 60) {
        return seconds === 1 ? '1 second' : `${seconds} seconds`;
      }
      if (seconds < 3600) {
        const minutes = Math.floor(seconds / 60);
        return minutes === 1 ? '1 minute' : `${minutes} minutes`;
      }
      if (seconds < 86400) {
        const hours = Math.floor(seconds / 3600);
        return hours === 1 ? '1 hour' : `${hours} hours`;
      }
      const days = Math.floor(seconds / 86400);
      return days === 1 ? '1 day' : `${days} days`;
    }

    return value;
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  // Calculate locked users directly from the users data
  const now = Math.floor(Date.now() / 1000);
  const lockedUsers = users.filter((u: any) => u.lockedUntil && u.lockedUntil > now);

  const activeUsers = users.filter((u: any) => u.status === 'active').length;
  const totalUsers = users.length;
  const inactiveUsers = users.filter((u: any) => u.status === 'inactive').length;
  const users2FA = users.filter((u: any) => u.twoFactorEnabled).length;
  // Global admin is admin WITHOUT tenantId
  const globalAdminCount = users.filter((u: any) => u.roles?.includes('admin') && !u.tenantId).length;
  const tenantAdminCount = users.filter((u: any) => u.roles?.includes('admin') && u.tenantId).length;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Security Overview</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Monitor authentication and user access
          </p>
        </div>
        <Link
          to="/settings"
          className="inline-flex items-center gap-2 px-4 py-2 bg-brand-600 hover:bg-brand-700 text-white rounded-lg font-medium transition-colors"
        >
          <Settings className="h-4 w-4" />
          Configure Settings
        </Link>
      </div>

      {/* Security Status */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Security Status</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          <MetricCard
            title="Active Users"
            value={activeUsers}
            icon={Users}
            description={`${totalUsers} total users`}
            color="brand"
          />

          <MetricCard
            title="Users with 2FA"
            value={users2FA}
            icon={KeyRound}
            description={`${Math.round((users2FA / totalUsers) * 100)}% of users`}
            color="blue-light"
          />

          <MetricCard
            title="Locked Accounts"
            value={lockedUsers.length}
            icon={lockedUsers.length > 0 ? AlertTriangle : Lock}
            description="Due to failed logins"
            color={lockedUsers.length > 0 ? 'warning' : 'success'}
          />

          <MetricCard
            title="Tenant Admins"
            value={tenantAdminCount}
            icon={Shield}
            description={`Global admin: ${globalAdminCount}`}
            color="error"
          />

          <MetricCard
            title="Session Timeout"
            value={formatDuration(getSetting('security.session_timeout', '86400'))}
            icon={Clock}
            description="Auto-logout idle sessions"
            color="success"
          />
        </div>
      </div>

      {/* User Statistics */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">User Statistics</h2>
        <div className="grid gap-4 md:grid-cols-2">
          {/* User Status Card */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                <Users className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                User Status
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Total Users</span>
                <span className="text-sm font-bold text-gray-900 dark:text-white">{totalUsers}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Active Users</span>
                <span className="flex items-center gap-2 text-green-600 dark:text-green-400 font-medium">
                  <CheckCircle className="h-4 w-4" />
                  {activeUsers}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Inactive Users</span>
                <span className="flex items-center gap-2 text-gray-600 dark:text-gray-400 font-medium">
                  <UserX className="h-4 w-4" />
                  {inactiveUsers}
                </span>
              </div>
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <Link
                  to="/users"
                  className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium"
                >
                  Manage Users →
                </Link>
              </div>
            </div>
          </div>

          {/* Account Security Card */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                <Lock className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                Account Security
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Locked Accounts</span>
                <span className={`text-sm font-bold ${lockedUsers.length > 0 ? 'text-orange-600 dark:text-orange-400' : 'text-green-600 dark:text-green-400'}`}>
                  {lockedUsers.length}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Lockout Duration</span>
                <span className="text-sm text-gray-600 dark:text-gray-400">{formatDuration(getSetting('security.lockout_duration', '900'))}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Max Failed Attempts</span>
                <span className="text-sm text-gray-600 dark:text-gray-400">{getSetting('security.max_login_attempts', '5')} attempts</span>
              </div>
              {lockedUsers.length > 0 && (
                <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                  <Link
                    to="/users"
                    className="text-sm text-orange-600 dark:text-orange-400 hover:text-orange-700 dark:hover:text-orange-300 font-medium"
                  >
                    View Locked Users →
                  </Link>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Active Security Features */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Active Security Features</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {/* Authentication Features */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <Shield className="h-5 w-5 text-brand-600" />
              Authentication & Access
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Two-Factor Authentication (2FA)</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">TOTP-based 2FA with backup codes for enhanced security</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">JWT & S3 Signature Authentication</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Token-based (JWT) for Console, AWS Signature v2/v4 for S3 API</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">HMAC-SHA256 Cluster Auth</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Secure inter-node communication with cryptographic signatures</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Bcrypt Password Hashing</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Industry-standard password encryption with salt</p>
                </div>
              </div>
            </div>
          </div>

          {/* Security Controls */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <Lock className="h-5 w-5 text-brand-600" />
              Security Controls
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Rate Limiting</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">IP-based rate limiting ({getSetting('security.rate_limit_login', '5')} login attempts per minute)</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Account Lockout</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic {formatDuration(getSetting('security.lockout_duration', '900'))} lockout after {getSetting('security.max_login_attempts', '5')} failed login attempts</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Session Management</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic session timeout and idle detection ({formatDuration(getSetting('security.session_timeout', '86400'))})</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Role-Based Access Control (RBAC)</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">4 roles: Admin, User, Read-Only, Guest with granular permissions</p>
                </div>
              </div>
            </div>
          </div>

          {/* Data Protection & Replication */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <HardDrive className="h-5 w-5 text-brand-600" />
              Data Protection & Replication
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Server-Side Encryption (SSE)</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">AES-256-CTR streaming encryption for all stored objects</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Encrypted Cluster Replication</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic decrypt-on-source, re-encrypt-on-destination for HA</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Cross-Region Replication</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Secure bucket replication to AWS S3, MinIO with credential encryption</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Object Lock & Versioning</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">WORM compliance, versioning with delete markers, lifecycle policies</p>
                </div>
              </div>
            </div>
          </div>

          {/* Infrastructure Security */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <Users className="h-5 w-5 text-brand-600" />
              Multi-Tenancy & Isolation
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Tenant Isolation</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Complete data isolation between tenants with separate namespaces</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Resource Quotas</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Per-tenant storage, bucket, and access key limits with usage tracking</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Self-Replication Prevention</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic validation to prevent circular replication loops</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Bucket Permissions (ACLs)</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Fine-grained per-bucket access control with canned ACLs</p>
                </div>
              </div>
            </div>
          </div>

          {/* Event Monitoring & Logging */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <Bell className="h-5 w-5 text-brand-600" />
              Event Monitoring & Logging
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Advanced Logging System</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">HTTP output with batching and Syslog integration (TCP/UDP)</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Comprehensive Audit Logging</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">20+ event types tracked with automatic retention ({getSetting('audit.retention_days', '90')} days)</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Bucket Notifications (Webhooks)</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Real-time HTTP webhooks for S3 events (ObjectCreated, ObjectRemoved)</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Prometheus Metrics</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Real-time metrics export for monitoring and alerting</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Dynamic Settings System</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Runtime configuration management without server restarts</p>
                </div>
              </div>
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <Link
                  to="/audit-logs"
                  className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium"
                >
                  View Audit Logs →
                </Link>
              </div>
            </div>
          </div>

          {/* Compliance */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <FileText className="h-5 w-5 text-brand-600" />
              Compliance & Standards
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Compliance Ready</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">GDPR, SOC 2, HIPAA, ISO 27001, PCI DSS support</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Audit Trail Access</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Complete logging of authentication and access events</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Read-Only Audit Mode</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Global admins can audit tenant buckets without modification</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">CSV Export</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Export audit logs for compliance reporting and analysis</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
