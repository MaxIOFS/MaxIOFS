import React, { useState, useEffect, useCallback } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
  Home,
  Database,
  Users,
  Settings,
  BarChart3,
  Lock,
  User,
  LogOut,
  Bell,
  Building2,
  Menu,
  X,
  ChevronDown,
  ChevronRight,
  Moon,
  Sun,
  Info,
  FileText,
  Server,
  ArrowUpCircle,
  CheckCircle2,
  ShieldAlert,
  Shield,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuth } from '@/hooks/useAuth';
import { useNotifications } from '@/hooks/useNotifications';
import { useTheme } from '@/contexts/ThemeContext';
import ModalManager, { ModalRenderer, ToastNotifications } from '@/lib/modals';
import { useQuery, useMutation } from '@tanstack/react-query';
import APIClient from '@/lib/api';
import type { ServerConfig } from '@/types';

interface NavItem {
  name: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  children?: NavItem[];
}

const navigation: NavItem[] = [
  {
    name: 'Dashboard',
    href: '/',
    icon: Home,
  },
  {
    name: 'Buckets',
    href: '/buckets',
    icon: Database,
  },
  {
    name: 'Users',
    href: '/users',
    icon: Users,
    children: [
      {
        name: 'Users',
        href: '/users',
        icon: Users,
      },
      {
        name: 'Access Keys',
        href: '/users/access-keys',
        icon: Lock,
      },
      {
        name: 'Tenants',
        href: '/tenants',
        icon: Building2,
      },
      {
        name: 'Identity Providers',
        href: '/identity-providers',
        icon: Shield,
      },
    ],
  },
  {
    name: 'Audit Logs',
    href: '/audit-logs',
    icon: FileText,
  },
  {
    name: 'Metrics',
    href: '/metrics',
    icon: BarChart3,
  },
  {
    name: 'Security',
    href: '/security',
    icon: Lock,
  },
  {
    name: 'Cluster',
    href: '/cluster',
    icon: Server,
  },
  {
    name: 'Settings',
    href: '/settings',
    icon: Settings,
  },
  {
    name: 'About',
    href: '/about',
    icon: Info,
  },
];

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const pathname = location.pathname;
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { notifications, unreadCount, markAsRead, markAllAsRead } = useNotifications();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [expandedMenus, setExpandedMenus] = useState<string[]>([]);
  const { theme, effectiveTheme, setTheme } = useTheme();

  // Check if admin is still using default password (reactive to changes)
  const [hasDefaultPassword, setHasDefaultPassword] = useState(
    () => localStorage.getItem('default_password_warning') === 'true'
  );

  useEffect(() => {
    const handleStorageChange = () => {
      setHasDefaultPassword(localStorage.getItem('default_password_warning') === 'true');
    };
    // Listen for cross-tab storage events and custom in-tab event
    window.addEventListener('storage', handleStorageChange);
    window.addEventListener('default-password-changed', handleStorageChange);
    return () => {
      window.removeEventListener('storage', handleStorageChange);
      window.removeEventListener('default-password-changed', handleStorageChange);
    };
  }, []);

  const totalUnread = unreadCount + (hasDefaultPassword ? 1 : 0);

  // Get base path from window (injected by backend based on public_console_url)
  const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');

  // Get server config for version
  const { data: serverConfig } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
  });

  // Check if user is global admin (admin role + no tenant)
  const isGlobalAdmin = (user?.roles?.includes('admin') ?? false) && !user?.tenantId;
  const isTenantAdmin = (user?.roles?.includes('admin') ?? false) && !!user?.tenantId;
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  // Check for new version (proxied through backend to avoid CORS)
  const { data: latestVersionData } = useQuery<{ version: string }>({
    queryKey: ['latestVersion'],
    queryFn: () => APIClient.getVersionCheck(),
    staleTime: 1000 * 60 * 60, // check once per hour
    retry: 1,
    refetchOnWindowFocus: false,
    enabled: isGlobalAdmin,
  });

  // Compare semver strings (strips leading 'v' and trailing labels like '-beta')
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

  const { data: tenant } = useQuery({
    queryKey: ['tenant', user?.tenantId],
    queryFn: () => APIClient.getTenant(user!.tenantId!),
    enabled: !!user?.tenantId,
  });

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const tenantDisplayName = (tenant as any)?.display_name || tenant?.displayName || tenant?.name || user?.tenantId;

  const filteredNavigation = navigation.filter(item => {
    // Admin-only pages: Metrics, Security, Cluster, Settings (only for global admins)
    if ((item.name === 'Metrics' || item.name === 'Security' || item.name === 'Cluster' || item.name === 'Settings') && !isGlobalAdmin) {
      return false;
    }
    // Audit Logs: visible for global admins and tenant admins
    if (item.name === 'Audit Logs' && !isAnyAdmin) {
      return false;
    }
    // Tenants: only for admins (global or tenant)
    if (item.name === 'Tenants' && !isAnyAdmin) {
      return false;
    }
    // Users: everyone can see it (admins see list, users get redirected to their profile)
    return true;
  }).map(item => {
    // Filter children of Users menu to hide Tenants for non-admins
    if (item.name === 'Users' && item.children) {
      return {
        ...item,
        children: item.children.filter(child => {
          if (child.name === 'Tenants' && !isAnyAdmin) {
            return false;
          }
          if (child.name === 'Identity Providers' && !isAnyAdmin) {
            return false;
          }
          return true;
        })
      };
    }
    return item;
  });

  // Save theme preference to user profile
  const saveThemeMutation = useMutation({
    mutationFn: (newTheme: 'light' | 'dark' | 'system') =>
      APIClient.updateUserPreferences(user?.id || '', newTheme, user?.languagePreference || 'en'),
  });

  const toggleDarkMode = () => {
    const newTheme = effectiveTheme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    if (user?.id) {
      saveThemeMutation.mutate(newTheme);
    }
  };

  const handleLogout = async () => {
    try {
      const result = await ModalManager.confirmLogout();
      if (result.isConfirmed) {
        ModalManager.loading('Signing out...', 'See you soon');
        await logout();
        ModalManager.close();
      }
    } catch (error) {
      ModalManager.close();
      ModalManager.error('Error signing out', error as string);
    }
  };

  const isActiveRoute = (href: string, exact = false): boolean => {
    if (exact) {
      return pathname === href;
    }
    // Special case for root - must be exact match
    if (href === '/') {
      return pathname === '/';
    }
    // For routes with children, check exact match first
    if (href === '/users' && pathname.startsWith('/users/')) {
      // Only mark /users as active if we're exactly on /users, not on sub-routes
      return pathname === '/users';
    }
    return pathname.startsWith(href);
  };

  const toggleMenu = (menuName: string) => {
    setExpandedMenus(prev => 
      prev.includes(menuName) 
        ? prev.filter(m => m !== menuName)
        : [...prev, menuName]
    );
  };

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Sidebar */}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 w-64 lg:w-60 xl:w-64 2xl:w-64 bg-white dark:bg-gray-900 border-r border-gray-200 dark:border-gray-800 flex flex-col transform transition-all duration-300 ease-in-out lg:translate-x-0 lg:static lg:inset-0 shadow-soft-lg',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        {/* Logo Header */}
        <div className="flex items-center justify-center h-20 px-6 border-b border-gray-200 dark:border-gray-800">
          <Link to="/" className="flex items-center space-x-3 group">
            <div className="flex items-center justify-center w-10 h-10 3xl:w-12 3xl:h-12 4xl:w-14 4xl:h-14 rounded-button bg-brand-600 shadow-glow">
              <img
                src={`${basePath}/assets/img/icon.png`}
                alt="MaxIOFS"
                className="w-7 h-7 3xl:w-8 3xl:h-8 4xl:w-10 4xl:h-10 rounded"
              />
            </div>
            <div>
              <h1 className="text-xl font-bold text-gray-900 dark:text-white">MaxIOFS</h1>
              <p className="text-xs text-gray-500 dark:text-gray-400">Object Storage</p>
            </div>
          </Link>
          <button
            onClick={() => setSidebarOpen(false)}
            className="lg:hidden p-2 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-lg text-gray-600 dark:text-gray-400"
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Navigation Menu */}
        <nav className="flex-1 px-4 py-6 space-y-1 overflow-y-auto">
          {filteredNavigation.map((item) => {
            const isActive = isActiveRoute(item.href, !item.children);
            const isExpanded = item.children && expandedMenus.includes(item.name);
            const hasActiveChild = item.children && item.children.some(child => isActiveRoute(child.href));

            return (
              <div key={item.name}>
                {item.children ? (
                  <>
                    <button
                      onClick={() => toggleMenu(item.name)}
                      className={cn(
                        'flex items-center justify-between w-full px-4 py-3 rounded-button text-sm font-medium transition-all duration-200',
                        hasActiveChild || isExpanded
                          ? 'bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-white shadow-soft'
                          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white hover:shadow-soft'
                      )}
                    >
                      <div className="flex items-center space-x-3">
                        <item.icon className="h-5 w-5 flex-shrink-0" />
                        <span>{item.name}</span>
                      </div>
                      {isExpanded ? (
                        <ChevronDown className="h-4 w-4" />
                      ) : (
                        <ChevronRight className="h-4 w-4" />
                      )}
                    </button>
                    
                    {isExpanded && (
                      <div className="mt-1 ml-6 space-y-1 pl-4 border-l border-gray-200 dark:border-gray-700">
                        {item.children.map((child) => (
                          <Link
                            key={child.name}
                            to={child.href}
                            className={cn(
                              'flex items-center space-x-3 px-4 py-2.5 rounded-button text-sm transition-all duration-200',
                              isActiveRoute(child.href, true)
                                ? 'bg-brand-600 text-white font-medium shadow-glow-sm'
                                : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white hover:shadow-soft'
                            )}
                          >
                            <child.icon className="h-4 w-4" />
                            <span>{child.name}</span>
                          </Link>
                        ))}
                      </div>
                    )}
                  </>
                ) : (
                  <Link
                    to={item.href}
                    className={cn(
                      'flex items-center space-x-3 px-4 py-3 rounded-button text-sm font-medium transition-all duration-200 group',
                      isActive
                        ? 'bg-brand-600 text-white shadow-glow-sm'
                        : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-900 dark:hover:text-white hover:shadow-soft'
                    )}
                  >
                    <item.icon className="h-5 w-5" />
                    <span>{item.name}</span>
                  </Link>
                )}
              </div>
            );
          })}
        </nav>

        {/* Sidebar Footer */}
        <div className="p-4 border-t border-gray-200 dark:border-gray-800">
          <div className="flex items-center space-x-3 px-4 py-3 rounded-button bg-gray-100 dark:bg-gray-800 shadow-soft">
            <div className="flex items-center justify-center w-2 h-2">
              <div className="w-2 h-2 bg-success-500 rounded-full animate-pulse"></div>
            </div>
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-900 dark:text-white">System Status</p>
              <p className="text-xs text-gray-500 dark:text-gray-400">All systems operational</p>
            </div>
          </div>
          <div className="text-center mt-3">
            <p className="text-xs text-gray-500 dark:text-gray-400">
              {serverConfig?.version || 'Loading...'}
            </p>
            {isGlobalAdmin && (
              hasNewVersion ? (
                <a
                  href="https://maxiofs.com/downloads"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 mt-2 px-3 py-1.5 rounded-full bg-amber-50 dark:bg-amber-500/20 text-amber-700 dark:text-amber-300 text-xs font-medium hover:bg-amber-100 dark:hover:bg-amber-500/30 transition-colors duration-200 border border-amber-200 dark:border-amber-500/30 animate-pulse"
                >
                  <ArrowUpCircle className="h-3.5 w-3.5" />
                  New Release: {latestVersion}
                </a>
              ) : (
                <span className="inline-flex items-center gap-1.5 mt-2 px-3 py-1.5 rounded-full bg-emerald-50 dark:bg-emerald-500/20 text-emerald-700 dark:text-emerald-300 text-xs font-medium border border-emerald-200 dark:border-emerald-500/30">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  Latest Version
                </span>
              )
            )}
          </div>
        </div>
      </aside>

      {/* Mobile backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-gray-900/50 backdrop-blur-sm lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Main content area */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Top Header */}
        <header className="sticky top-0 z-30 flex w-full h-20 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shadow-soft-md backdrop-blur-sm bg-white/95 dark:bg-gray-900/95">
          <div className="flex flex-grow items-center justify-between px-6">
            {/* Left side */}
            <div className="flex items-center gap-2 sm:gap-4 lg:hidden">
              <button
                onClick={() => setSidebarOpen(true)}
                className="z-50 block rounded-button border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 p-1.5 shadow-soft hover:shadow-soft-md hover:bg-gray-50 dark:hover:bg-gray-700 lg:hidden transition-all duration-200"
              >
                <Menu className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              </button>
            </div>

            {/* Spacer to keep right side aligned */}
            <div className="flex-1"></div>

            {/* Right side */}
            <div className="flex items-center gap-3 2xl:gap-7">
              {/* Dark Mode Toggle */}
              <button
                onClick={toggleDarkMode}
                className="flex h-10 w-10 3xl:h-12 3xl:w-12 4xl:h-14 4xl:w-14 items-center justify-center rounded-full border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 transition-all duration-200 shadow-soft hover:shadow-soft-md"
                title={effectiveTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
              >
                {effectiveTheme === 'dark' ? (
                  <Sun className="h-5 w-5 3xl:h-6 3xl:w-6 4xl:h-7 4xl:w-7 text-yellow-500" />
                ) : (
                  <Moon className="h-5 w-5 3xl:h-6 3xl:w-6 4xl:h-7 4xl:w-7 text-gray-600" />
                )}
              </button>

              {/* Notification Menu */}
              <div className="relative">
                <button
                  onClick={() => setShowNotifications(!showNotifications)}
                  className="relative flex h-10 w-10 3xl:h-12 3xl:w-12 4xl:h-14 4xl:w-14 items-center justify-center rounded-full border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 transition-all duration-200 shadow-soft hover:shadow-soft-md"
                >
                  <Bell className="h-5 w-5 3xl:h-6 3xl:w-6 4xl:h-7 4xl:w-7 text-gray-600 dark:text-gray-400" />
                  {totalUnread > 0 && (
                    <span className="absolute -top-0.5 -right-0.5 z-1 h-5 w-5 rounded-full bg-error-600 flex items-center justify-center">
                      <span className="text-[10px] font-medium text-white">{totalUnread}</span>
                    </span>
                  )}
                </button>

                {showNotifications && (
                  <>
                    <div
                      className="fixed inset-0 z-40"
                      onClick={() => setShowNotifications(false)}
                    />
                    <div className="absolute -right-16 sm:right-0 mt-2.5 w-80 sm:w-96 bg-white dark:bg-gray-800 rounded-card shadow-soft-xl border border-gray-200 dark:border-gray-700 z-50">
                      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                        <h5 className="text-sm font-semibold text-gray-900 dark:text-white">
                          Notifications
                        </h5>
                        <div className="flex gap-2">
                          {unreadCount > 0 && (
                            <span className="rounded-full bg-brand-600 px-2.5 py-0.5 text-xs font-medium text-white">
                              {unreadCount} New
                            </span>
                          )}
                          {notifications.length > 0 && (
                            <button
                              onClick={markAllAsRead}
                              className="text-xs text-brand-600 hover:text-brand-700 font-medium"
                            >
                              Mark all read
                            </button>
                          )}
                        </div>
                      </div>

                      <div className="max-h-96 overflow-y-auto">
                        {/* Default password security warning */}
                        {hasDefaultPassword && (
                          <Link
                            to={`/users/${user?.id || 'admin'}`}
                            onClick={() => setShowNotifications(false)}
                            className="flex gap-4 border-b border-gray-200 dark:border-gray-700 px-5 py-4 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors bg-amber-50/50 dark:bg-amber-900/10"
                          >
                            <div className="h-12 w-12 rounded-full bg-amber-50 dark:bg-amber-900/30 flex items-center justify-center flex-shrink-0">
                              <ShieldAlert className="h-6 w-6 text-amber-600 dark:text-amber-400" />
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="flex items-start justify-between gap-2 mb-1">
                                <h6 className="text-sm text-gray-900 dark:text-white font-semibold">
                                  Security Warning
                                </h6>
                                <span className="h-2 w-2 rounded-full bg-amber-500 flex-shrink-0 mt-1.5" />
                              </div>
                              <p className="text-xs text-gray-600 dark:text-gray-400">
                                You are using the default admin password. Please change it immediately to secure your system.
                              </p>
                            </div>
                          </Link>
                        )}

                        {notifications.length === 0 && !hasDefaultPassword ? (
                          <div className="px-5 py-8 text-center">
                            <Bell className="h-12 w-12 text-gray-300 dark:text-gray-600 mx-auto mb-3" />
                            <p className="text-sm text-gray-500 dark:text-gray-400">No notifications</p>
                          </div>
                        ) : (
                          <div>
                            {notifications.map((notification) => {
                              const timestamp = new Date(notification.timestamp * 1000);
                              const now = new Date();
                              const diffMs = now.getTime() - timestamp.getTime();
                              const diffMins = Math.floor(diffMs / 60000);
                              const timeAgo =
                                diffMins < 1 ? 'Just now' :
                                diffMins < 60 ? `${diffMins}m ago` :
                                diffMins < 1440 ? `${Math.floor(diffMins / 60)}h ago` :
                                `${Math.floor(diffMins / 1440)}d ago`;

                              return (
                                <Link
                                  key={notification.id}
                                  to="/users"
                                  onClick={() => {
                                    markAsRead(notification.id);
                                    setShowNotifications(false);
                                  }}
                                  className={cn(
                                    "flex gap-4 border-b border-gray-200 dark:border-gray-700 px-5 py-4 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors",
                                    !notification.read && "bg-brand-50/50 dark:bg-brand-900/10"
                                  )}
                                >
                                  <div className="h-12 w-12 rounded-full bg-error-50 dark:bg-error-900/30 flex items-center justify-center flex-shrink-0">
                                    <Lock className="h-6 w-6 text-error-600" />
                                  </div>
                                  <div className="flex-1 min-w-0">
                                    <div className="flex items-start justify-between gap-2 mb-1">
                                      <h6 className={cn(
                                        "text-sm text-gray-900 dark:text-white",
                                        !notification.read && "font-semibold"
                                      )}>
                                        {notification.type === 'user_locked' ? 'Account Locked' : notification.type}
                                      </h6>
                                      {!notification.read && (
                                        <span className="h-2 w-2 rounded-full bg-brand-600 flex-shrink-0 mt-1.5" />
                                      )}
                                    </div>
                                    <p className="text-xs text-gray-600 dark:text-gray-400 break-words">
                                      {notification.message}
                                    </p>
                                    <p className="text-xs text-gray-500 dark:text-gray-500 mt-1">
                                      {timeAgo}
                                    </p>
                                  </div>
                                </Link>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    </div>
                  </>
                )}
              </div>

              {/* User Area */}
              <div className="relative">
                <button
                  onClick={() => setShowUserMenu(!showUserMenu)}
                  className="flex items-center gap-3 rounded-button hover:bg-gray-50 dark:hover:bg-gray-800 px-2 py-2 transition-all duration-200 hover:shadow-soft"
                >
                  <span className="hidden text-right lg:block">
                    <span className="block text-sm font-medium text-gray-900 dark:text-white">
                      {user?.username || 'Unknown'}
                    </span>
                    <span className="block text-xs text-gray-500 dark:text-gray-400">
                      {user?.tenantId ? tenantDisplayName : 'Global Admin'}
                    </span>
                  </span>

                  <span className="h-10 w-10 3xl:h-12 3xl:w-12 4xl:h-14 4xl:w-14 rounded-full bg-gradient-to-br from-brand-500 to-brand-600 flex items-center justify-center">
                    <span className="text-sm 3xl:text-base 4xl:text-lg font-semibold text-white">
                      {user?.username?.charAt(0).toUpperCase() || 'U'}
                    </span>
                  </span>

                  <ChevronDown className="hidden sm:block h-4 w-4 text-gray-400" />
                </button>

                {showUserMenu && (
                  <>
                    <div
                      className="fixed inset-0 z-40"
                      onClick={() => setShowUserMenu(false)}
                    />
                    <div className="absolute right-0 mt-2.5 w-56 rounded-card border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shadow-soft-xl z-50">
                      <div className="flex items-center gap-3 border-b border-gray-200 dark:border-gray-700 px-4 py-4">
                        <span className="h-12 w-12 rounded-full bg-gradient-to-br from-brand-500 to-brand-600 flex items-center justify-center flex-shrink-0">
                          <span className="text-base font-semibold text-white">
                            {user?.username?.charAt(0).toUpperCase() || 'U'}
                          </span>
                        </span>
                        <div className="flex-1 min-w-0">
                          <p className="text-sm font-medium text-gray-900 dark:text-white truncate">
                            {user?.username || 'Unknown'}
                          </p>
                          <p className="text-xs text-gray-500 dark:text-gray-400 truncate">
                            {user?.email || 'No email'}
                          </p>
                        </div>
                      </div>

                      <div className="p-2">
                        <button
                          onClick={() => {
                            setShowUserMenu(false);
                            navigate(`/users/${user?.id}`);
                          }}
                          className="flex w-full items-center gap-3 rounded-button px-3 py-2.5 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-all duration-200"
                        >
                          <User className="h-4 w-4" />
                          My Profile
                        </button>
                        <button
                          onClick={() => {
                            setShowUserMenu(false);
                            handleLogout();
                          }}
                          className="flex w-full items-center gap-3 rounded-button px-3 py-2.5 text-sm font-medium text-error-600 hover:bg-error-50 dark:hover:bg-error-900/30 transition-all duration-200"
                        >
                          <LogOut className="h-4 w-4" />
                          Log Out
                        </button>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </div>
          </div>
        </header>

        {/* Main content */}
        <main className="flex-1 overflow-x-hidden overflow-y-auto bg-gray-100 dark:bg-gray-900">
          <div className="mx-auto max-w-screen-2xl 3xl:max-w-[95%] 4xl:max-w-[90%] 5xl:max-w-[85%] p-4 md:p-6 2xl:p-10 3xl:p-12 4xl:p-16">
            {children}
          </div>
        </main>
      </div>

      {/* Modal Manager and Toast Notifications */}
      <ModalRenderer />
      <ToastNotifications />
    </div>
  );
}

export default AppLayout;
