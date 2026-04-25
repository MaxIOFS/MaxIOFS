import React, { useState, useEffect, useCallback, Suspense } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/hooks/useAuth';
import { useNotifications } from '@/hooks/useNotifications';
import { useTheme } from '@/contexts/ThemeContext';
import { useLanguage } from '@/contexts/LanguageContext';
import { useQuery, useMutation } from '@tanstack/react-query';
import APIClient from '@/lib/api';
import ModalManager, { ModalRenderer, ToastNotifications } from '@/lib/modals';
import { BackgroundTaskBar } from '@/components/ui/BackgroundTaskBar';
import { SidebarNav, navigation } from './SidebarNav';
import { TopBar } from './TopBar';
import { MaintenanceBanner } from './MaintenanceBanner';
import type { ServerConfig } from '@/types';

export function AppLayout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation('layout');
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { effectiveTheme, setTheme } = useTheme();
  const { language, setLanguage } = useLanguage();

  const isGlobalAdmin = (user?.roles?.includes('admin') ?? false) && !user?.tenantId;
  const isTenantAdmin = (user?.roles?.includes('admin') ?? false) && !!user?.tenantId;
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  // Mobile sidebar open/close
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Desktop sidebar collapsed/expanded — persisted
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    return localStorage.getItem('sidebar_collapsed') === 'true';
  });

  const toggleSidebarCollapse = useCallback(() => {
    setSidebarCollapsed(prev => {
      const next = !prev;
      localStorage.setItem('sidebar_collapsed', String(next));
      return next;
    });
  }, []);

  // Default password warning (reactive to profile change)
  const [hasDefaultPassword, setHasDefaultPassword] = useState(
    () => localStorage.getItem('default_password_warning') === 'true'
  );
  useEffect(() => {
    const handleStorageChange = () => {
      setHasDefaultPassword(localStorage.getItem('default_password_warning') === 'true');
    };
    window.addEventListener('storage', handleStorageChange);
    window.addEventListener('default-password-changed', handleStorageChange);
    return () => {
      window.removeEventListener('storage', handleStorageChange);
      window.removeEventListener('default-password-changed', handleStorageChange);
    };
  }, []);

  const isAdminUser = user?.roles?.includes('admin') ?? false;
  const { notifications, unreadCount, markAsRead, markAllAsRead, clearNotification } = useNotifications(isAdminUser);
  const totalUnread = unreadCount + (hasDefaultPassword ? 1 : 0);

  // Server config — also drives the maintenance banner (polls every 30s)
  const { data: serverConfig } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  });

  // Update S3 base URL from server config so it's not hardcoded to :8080
  useEffect(() => {
    if (serverConfig?.server?.publicApiUrl) {
      APIClient.updateS3BaseUrl(serverConfig.server.publicApiUrl);
    }
  }, [serverConfig?.server?.publicApiUrl]);

  // Latest version check (global admin only, once per hour)
  const { data: latestVersionData } = useQuery<{ version: string }>({
    queryKey: ['latestVersion'],
    queryFn: () => APIClient.getVersionCheck(),
    staleTime: 1000 * 60 * 60,
    retry: 1,
    refetchOnWindowFocus: false,
    enabled: isGlobalAdmin,
  });

  const isNewerVersion = (latest: string, current: string): boolean => {
    const parse = (v: string) => {
      const clean = v.replace(/^v/, '');
      const dashIdx = clean.indexOf('-');
      const main = dashIdx === -1 ? clean : clean.slice(0, dashIdx);
      const pre  = dashIdx === -1 ? '' : clean.slice(dashIdx + 1); // e.g. 'beta', 'rc1'
      const nums = main.split('.').map(Number);
      return { nums, pre };
    };
    // Stable release has higher precedence than any pre-release of the same version.
    // Ranking: stable(3) > rc(2) > beta(1) > alpha(0) > other(1)
    const preRank = (pre: string): number => {
      if (!pre) return 3;
      if (/^rc/.test(pre)) return 2;
      if (/^beta/.test(pre)) return 1;
      if (/^alpha/.test(pre)) return 0;
      return 1;
    };
    const preNum = (pre: string): number => parseInt(pre.replace(/^\D+/, '') || '0', 10);

    const l = parse(latest);
    const c = parse(current);
    const [lMaj = 0, lMin = 0, lPat = 0] = l.nums;
    const [cMaj = 0, cMin = 0, cPat = 0] = c.nums;

    if (lMaj !== cMaj) return lMaj > cMaj;
    if (lMin !== cMin) return lMin > cMin;
    if (lPat !== cPat) return lPat > cPat;
    // Same numeric version — compare pre-release (e.g. 1.0.0 > 1.0.0-rc1 > 1.0.0-beta)
    const lRank = preRank(l.pre);
    const cRank = preRank(c.pre);
    if (lRank !== cRank) return lRank > cRank;
    // Same tier — compare numeric suffix (rc2 > rc1)
    return preNum(l.pre) > preNum(c.pre);
  };

  const currentVersion = serverConfig?.version || '';
  const latestVersion = latestVersionData?.version || '';
  const hasNewVersion = isGlobalAdmin && !!latestVersion && !!currentVersion && isNewerVersion(latestVersion, currentVersion);

  // Tenant display name
  const { data: tenant } = useQuery({
    queryKey: ['tenant', user?.tenantId],
    queryFn: () => APIClient.getTenant(user!.tenantId!),
    enabled: !!user?.tenantId,
  });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const tenantDisplayName = (tenant as any)?.display_name || (tenant as any)?.displayName || (tenant as any)?.name || user?.tenantId;

  // Navigation filtered by role
  const filteredNavigation = navigation.filter(item => {
    if ((item.name === 'Metrics' || item.name === 'Security' || item.name === 'Cluster' || item.name === 'Settings') && !isGlobalAdmin) return false;
    if (item.name === 'Audit Logs' && !isAnyAdmin) return false;
    return true;
  }).map(item => {
    if (item.name === 'Users' && item.children) {
      return {
        ...item,
        children: item.children.filter(child => {
          if ((child.name === 'Tenants' || child.name === 'Identity Providers' || child.name === 'Groups') && !isAnyAdmin) return false;
          if (child.name === 'Role Capabilities' && !isGlobalAdmin) return false;
          return true;
        }),
      };
    }
    return item;
  });

  // Theme / language — also persist to user profile
  const savePreferencesMutation = useMutation({
    mutationFn: ({ theme, lang }: { theme: 'light' | 'dark' | 'system'; lang: string }) =>
      APIClient.updateUserPreferences(user?.id || '', theme, lang),
  });

  const handleToggleDarkMode = useCallback(() => {
    const newTheme = effectiveTheme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    if (user?.id) savePreferencesMutation.mutate({ theme: newTheme, lang: language });
  }, [effectiveTheme, setTheme, user?.id, language]);

  const handleLanguageChange = useCallback((lang: 'en' | 'es') => {
    setLanguage(lang);
    if (user?.id) savePreferencesMutation.mutate({ theme: effectiveTheme, lang });
  }, [setLanguage, user?.id, effectiveTheme]);

  const handleLogout = useCallback(async () => {
    try {
      const result = await ModalManager.confirmLogout();
      if (result.isConfirmed) {
        ModalManager.loading(t('signingOut'), t('seeyouSoon'));
        await logout();
        ModalManager.close();
      }
    } catch {
      ModalManager.close();
    }
  }, [logout]);

  return (
    <div className="flex h-screen bg-background overflow-hidden p-3 gap-3">
      {/* Floating Sidebar */}
      <SidebarNav
        sidebarOpen={sidebarOpen}
        onClose={() => setSidebarOpen(false)}
        collapsed={sidebarCollapsed}
        onToggleCollapse={toggleSidebarCollapse}
        filteredNavigation={filteredNavigation}
        isGlobalAdmin={isGlobalAdmin}
        hasNewVersion={hasNewVersion}
        latestVersion={latestVersion}
        serverConfig={serverConfig}
      />

      {/* Mobile backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Main content area */}
      <div className="flex-1 flex flex-col overflow-hidden rounded-card bg-background min-w-0">
        <TopBar
          onMenuOpen={() => setSidebarOpen(true)}
          user={user}
          isGlobalAdmin={isGlobalAdmin}
          tenantDisplayName={tenantDisplayName}
          effectiveTheme={effectiveTheme}
          onToggleDarkMode={handleToggleDarkMode}
          language={language}
          onLanguageChange={handleLanguageChange}
          notifications={notifications}
          unreadCount={unreadCount}
          totalUnread={totalUnread}
          hasDefaultPassword={hasDefaultPassword}
          onMarkAsRead={markAsRead}
          onMarkAllAsRead={markAllAsRead}
          onClearNotification={clearNotification}
          onLogout={handleLogout}
        />

        <MaintenanceBanner isMaintenanceMode={!!serverConfig?.maintenanceMode} />

        <main className="flex-1 overflow-x-hidden overflow-y-auto">
          <div className="mx-auto max-w-screen-2xl 3xl:max-w-[95%] 4xl:max-w-[90%] 5xl:max-w-[85%] p-2 md:p-3 2xl:p-4">
            <Suspense fallback={
              <div className="flex items-center justify-center h-64">
                <svg className="animate-spin h-8 w-8 text-brand-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                </svg>
              </div>
            }>
              {children}
            </Suspense>
          </div>
        </main>
      </div>

      <ModalRenderer />
      <ToastNotifications />
      <BackgroundTaskBar />
    </div>
  );
}

export default AppLayout;
