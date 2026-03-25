import React, { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useNavigate } from 'react-router-dom';
import { Loading } from '@/components/ui/Loading';
import { Button } from '@/components/ui/Button';
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
  const { t } = useTranslation('security');
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
          <h1 className="text-2xl font-bold text-foreground">{t('securityOverview')}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t('monitorAuthAccess')}
          </p>
        </div>
        <Button variant="default" onClick={() => navigate('/settings')}>
          <Settings className="h-4 w-4" />
          {t('configureSettings')}
        </Button>
      </div>

      {/* Security Status */}
      <div>
        <h2 className="text-xl font-semibold text-foreground mb-4">{t('securityStatus')}</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          <MetricCard
            title={t('activeUsers')}
            value={activeUsers}
            icon={Users}
            description={t('totalUsers', { count: totalUsers })}
            color="success"
          />

          <MetricCard
            title={t('usersWith2FA')}
            value={users2FA}
            icon={KeyRound}
            description={t('percentOfUsers', { percent: Math.round((users2FA / totalUsers) * 100) })}
            color="brand"
          />

          <MetricCard
            title={t('lockedAccounts')}
            value={lockedUsers.length}
            icon={lockedUsers.length > 0 ? AlertTriangle : Lock}
            description={t('dueToFailedLogins')}
            color={lockedUsers.length > 0 ? 'error' : 'success'}
          />

          <MetricCard
            title={t('tenantAdmins')}
            value={tenantAdminCount}
            icon={Shield}
            description={t('globalAdmin', { count: globalAdminCount })}
            color="blue-light"
          />

          <MetricCard
            title={t('sessionTimeout')}
            value={formatDuration(getSetting('security.session_timeout', '86400'))}
            icon={Clock}
            description={t('autoLogoutIdleSessions')}
            color="warning"
          />
        </div>
      </div>

      {/* User Statistics */}
      <div>
        <h2 className="text-xl font-semibold text-foreground mb-4">{t('userStatistics')}</h2>
        <div className="grid gap-4 md:grid-cols-2">
          {/* User Status Card */}
          <div className="bg-card rounded-lg border border-border shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-border">
              <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                <Users className="h-5 w-5 text-muted-foreground" />
                {t('userStatus')}
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('totalUsersLabel')}</span>
                <span className="text-sm font-bold text-foreground">{totalUsers}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('activeUsers')}</span>
                <span className="flex items-center gap-2 text-green-600 dark:text-green-400 font-medium">
                  <CheckCircle className="h-4 w-4" />
                  {activeUsers}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('inactiveUsers')}</span>
                <span className="flex items-center gap-2 text-muted-foreground font-medium">
                  <UserX className="h-4 w-4" />
                  {inactiveUsers}
                </span>
              </div>
              <div className="pt-2 border-t border-border">
                <Link
                  to="/users"
                  className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium"
                >
                  {t('manageUsers')} →
                </Link>
              </div>
            </div>
          </div>

          {/* Account Security Card */}
          <div className="bg-card rounded-lg border border-border shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-border">
              <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                <Lock className="h-5 w-5 text-muted-foreground" />
                {t('accountSecurity')}
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('lockedAccounts')}</span>
                <span className={`text-sm font-bold ${lockedUsers.length > 0 ? 'text-orange-600 dark:text-orange-400' : 'text-green-600 dark:text-green-400'}`}>
                  {lockedUsers.length}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('lockoutDuration')}</span>
                <span className="text-sm text-muted-foreground">{formatDuration(getSetting('security.lockout_duration', '900'))}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground">{t('maxFailedAttempts')}</span>
                <span className="text-sm text-muted-foreground">{t('attemptsCount', { count: parseInt(getSetting('security.max_failed_attempts', '5'), 10) })}</span>
              </div>
              {lockedUsers.length > 0 && (
                <div className="pt-2 border-t border-border">
                  <Link
                    to="/users"
                    className="text-sm text-orange-600 dark:text-orange-400 hover:text-orange-700 dark:hover:text-orange-300 font-medium"
                  >
                    {t('viewLockedUsers')} →
                  </Link>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Active Security Features */}
      <div>
        <h2 className="text-xl font-semibold text-foreground mb-4">{t('activeSecurityFeatures')}</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {/* Authentication Features */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <Shield className="h-5 w-5 text-muted-foreground" />
              {t('authenticationAccess')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('twoFactorAuth')}</p>
                  <p className="text-sm text-muted-foreground">{t('twoFactorAuthDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('jwtS3Auth')}</p>
                  <p className="text-sm text-muted-foreground">{t('jwtS3AuthDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('identityProviders')}</p>
                  <p className="text-sm text-muted-foreground">{t('identityProvidersDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('tlsClusterComm')}</p>
                  <p className="text-sm text-muted-foreground">{t('tlsClusterCommDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('bcryptPassword')}</p>
                  <p className="text-sm text-muted-foreground">{t('bcryptPasswordDesc')}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Security Controls */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <Lock className="h-5 w-5 text-muted-foreground" />
              {t('securityControls')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('rateLimiting')}</p>
                  <p className="text-sm text-muted-foreground">{t('rateLimitingDesc', { count: parseInt(getSetting('security.ratelimit_login_per_minute', '5'), 10) })}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('accountLockout')}</p>
                  <p className="text-sm text-muted-foreground">{t('accountLockoutDesc', { duration: formatDuration(getSetting('security.lockout_duration', '900')), attempts: getSetting('security.max_failed_attempts', '5') })}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('sessionManagement')}</p>
                  <p className="text-sm text-muted-foreground">{t('sessionManagementDesc', { duration: formatDuration(getSetting('security.session_timeout', '86400')) })}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('rbac')}</p>
                  <p className="text-sm text-muted-foreground">{t('rbacDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('maintenanceMode')}</p>
                  <p className="text-sm text-muted-foreground">{t('maintenanceModeDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('bucketPolicyConditions')}</p>
                  <p className="text-sm text-muted-foreground">{t('bucketPolicyConditionsDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('publicAccessBlockEnforcement')}</p>
                  <p className="text-sm text-muted-foreground">{t('publicAccessBlockEnforcementDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('consoleSecurityHeaders')}</p>
                  <p className="text-sm text-muted-foreground">{t('consoleSecurityHeadersDesc')}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Data Protection & Replication */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <HardDrive className="h-5 w-5 text-muted-foreground" />
              {t('dataProtectionReplication')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('serverSideEncryption')}</p>
                  <p className="text-sm text-muted-foreground">{t('serverSideEncryptionDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('tlsClusterReplication')}</p>
                  <p className="text-sm text-muted-foreground">{t('tlsClusterReplicationDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('crossRegionReplication')}</p>
                  <p className="text-sm text-muted-foreground">{t('crossRegionReplicationDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('objectLockVersioning')}</p>
                  <p className="text-sm text-muted-foreground">{t('objectLockVersioningDesc')}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Infrastructure Security */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <Users className="h-5 w-5 text-muted-foreground" />
              {t('multiTenancyIsolation')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('tenantIsolation')}</p>
                  <p className="text-sm text-muted-foreground">{t('tenantIsolationDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('resourceQuotas')}</p>
                  <p className="text-sm text-muted-foreground">{t('resourceQuotasDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('selfReplicationPrevention')}</p>
                  <p className="text-sm text-muted-foreground">{t('selfReplicationPreventionDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('bucketPermissions')}</p>
                  <p className="text-sm text-muted-foreground">{t('bucketPermissionsDesc')}</p>
                </div>
              </div>
            </div>
          </div>

          {/* Event Monitoring & Logging */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <Bell className="h-5 w-5 text-muted-foreground" />
              {t('eventMonitoringLogging')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('advancedLoggingSystem')}</p>
                  <p className="text-sm text-muted-foreground">{t('advancedLoggingSystemDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('comprehensiveAuditLogging')}</p>
                  <p className="text-sm text-muted-foreground">{t('comprehensiveAuditLoggingDesc', { days: getSetting('audit.retention_days', '90') })}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('bucketNotifications')}</p>
                  <p className="text-sm text-muted-foreground">{t('bucketNotificationsDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('prometheusMetrics')}</p>
                  <p className="text-sm text-muted-foreground">{t('prometheusMetricsDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('emailAlerts')}</p>
                  <p className="text-sm text-muted-foreground">{t('emailAlertsDesc')}</p>
                </div>
              </div>
              <div className="pt-2 border-t border-border">
                <Link
                  to="/audit-logs"
                  className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium"
                >
                  {t('viewAuditLogs')} →
                </Link>
              </div>
            </div>
          </div>

          {/* Compliance */}
          <div className="bg-card rounded-lg border border-border shadow-sm p-6">
            <h3 className="text-lg font-semibold text-foreground mb-4 flex items-center gap-2">
              <FileText className="h-5 w-5 text-muted-foreground" />
              {t('complianceStandards')}
            </h3>
            <div className="space-y-3">
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('complianceReady')}</p>
                  <p className="text-sm text-muted-foreground">{t('complianceReadyDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('auditTrailAccess')}</p>
                  <p className="text-sm text-muted-foreground">{t('auditTrailAccessDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('readOnlyAuditMode')}</p>
                  <p className="text-sm text-muted-foreground">{t('readOnlyAuditModeDesc')}</p>
                </div>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-foreground">{t('csvExport')}</p>
                  <p className="text-sm text-muted-foreground">{t('csvExportDesc')}</p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
