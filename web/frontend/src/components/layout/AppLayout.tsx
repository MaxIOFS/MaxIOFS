import React, { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '@/hooks/useAuth';
import { useNotifications } from '@/hooks/useNotifications';
import { useTheme } from '@/contexts/ThemeContext';
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

  const isGlobalAdmin = (user?.roles?.includes('admin') ?? false) && !user?.tenantId;
  const isTenantAdmin = (user?.roles?.includes('admin') ?? false) && !!user?.tenantId;
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  const [sidebarOpen, setSidebarOpen] = useState(false);

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
  const { notifications, unreadCount, markAsRead, markAllAsRead } = useNotifications(isAdminUser);
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
    const parse = (v: string) => v.replace(/^v/, '').replace(/-.*$/, '').split('.').map(Number);
    const [lMaj, lMin = 0, lPat = 0] = parse(latest);
    const [cMaj, cMin = 0, cPat = 0] = parse(current);
    if (lMaj !== cMaj) return lMaj > cMaj;
    if (lMin !== cMin) return lMin > cMin;
    return lPat > cPat;
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
          if ((child.name === 'Tenants' || child.name === 'Identity Providers') && !isAnyAdmin) return false;
          return true;
        }),
      };
    }
    return item;
  });

  // Theme toggle — also persists to user profile
  const saveThemeMutation = useMutation({
    mutationFn: (newTheme: 'light' | 'dark' | 'system') =>
      APIClient.updateUserPreferences(user?.id || '', newTheme, user?.languagePreference || 'en'),
  });

  const handleToggleDarkMode = useCallback(() => {
    const newTheme = effectiveTheme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    if (user?.id) saveThemeMutation.mutate(newTheme);
  }, [effectiveTheme, setTheme, user?.id]);

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
    <div className="flex h-screen overflow-hidden">
      <SidebarNav
        sidebarOpen={sidebarOpen}
        onClose={() => setSidebarOpen(false)}
        filteredNavigation={filteredNavigation}
        isGlobalAdmin={isGlobalAdmin}
        hasNewVersion={hasNewVersion}
        latestVersion={latestVersion}
        serverConfig={serverConfig}
      />

      {/* Mobile backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-gray-900/50 backdrop-blur-sm lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      <div className="flex-1 flex flex-col overflow-hidden">
        <TopBar
          onMenuOpen={() => setSidebarOpen(true)}
          user={user}
          isGlobalAdmin={isGlobalAdmin}
          tenantDisplayName={tenantDisplayName}
          effectiveTheme={effectiveTheme}
          onToggleDarkMode={handleToggleDarkMode}
          notifications={notifications}
          unreadCount={unreadCount}
          totalUnread={totalUnread}
          hasDefaultPassword={hasDefaultPassword}
          onMarkAsRead={markAsRead}
          onMarkAllAsRead={markAllAsRead}
          onLogout={handleLogout}
        />

        <MaintenanceBanner isMaintenanceMode={!!serverConfig?.maintenanceMode} />

        <main className="flex-1 overflow-x-hidden overflow-y-auto bg-gray-100 dark:bg-gray-900">
          <div className="mx-auto max-w-screen-2xl 3xl:max-w-[95%] 4xl:max-w-[90%] 5xl:max-w-[85%] p-4 md:p-6 2xl:p-10 3xl:p-12 4xl:p-16">
            {children}
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
