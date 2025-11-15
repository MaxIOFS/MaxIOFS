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
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  // Calculate locked users directly from the users data
  const now = Math.floor(Date.now() / 1000);
  const lockedUsers = users.filter((u: any) => u.locked_until && u.locked_until > now);

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
            value="24h"
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
                <span className="text-sm text-gray-600 dark:text-gray-400">15 minutes</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Max Failed Attempts</span>
                <span className="text-sm text-gray-600 dark:text-gray-400">5 attempts</span>
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
        <div className="grid gap-4 md:grid-cols-2">
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
                  <p className="font-medium text-gray-900 dark:text-white">JWT Authentication</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Token-based authentication for Console API requests</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">S3 Signature v2/v4</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">AWS-compatible signature authentication for S3 API</p>
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
                  <p className="text-sm text-gray-500 dark:text-gray-400">IP-based rate limiting (5 login attempts per minute)</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Account Lockout</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic 15-minute lockout after 5 failed login attempts</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Session Management</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Automatic session timeout and idle detection (24 hours)</p>
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
                  <p className="text-sm text-gray-500 dark:text-gray-400">Complete data isolation between tenants</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Resource Quotas</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Per-tenant storage limits and usage tracking</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Bucket Permissions</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Fine-grained per-bucket access control (read/write/admin)</p>
                </div>
              </div>
            </div>
          </div>

          {/* Monitoring & Audit */}
          <div className="bg-white dark:bg-gray-800 rounded-card border border-gray-200 dark:border-gray-700 shadow-card p-6">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center gap-2">
              <AlertTriangle className="h-5 w-5 text-brand-600" />
              Monitoring & Compliance
            </h3>
            <div className="space-y-3">
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
                  <p className="font-medium text-gray-900 dark:text-white">Comprehensive Audit Logging</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">20+ event types tracked with automatic retention (90 days)</p>
                </div>
              </div>
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
                  <p className="text-sm text-gray-500 dark:text-gray-400">Login attempts and access key usage logging</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-success-600 dark:text-success-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-gray-900 dark:text-white">Read-Only Audit Mode</p>
                  <p className="text-sm text-gray-500 dark:text-gray-400">Global admins can audit tenant buckets without modification</p>
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
        </div>
      </div>
    </div>
  );
}
