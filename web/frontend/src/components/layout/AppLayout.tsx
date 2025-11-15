import React, { useState, useEffect } from 'react';
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
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuth } from '@/hooks/useAuth';
import { useLockedUsers } from '@/hooks/useLockedUsers';
import SweetAlert from '@/lib/sweetalert';
import { useQuery } from '@tanstack/react-query';
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
  const { data: lockedUsers = [] } = useLockedUsers();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [expandedMenus, setExpandedMenus] = useState<string[]>([]);
  const [darkMode, setDarkMode] = useState(false);

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

  const { data: tenant } = useQuery({
    queryKey: ['tenant', user?.tenantId],
    queryFn: () => APIClient.getTenant(user!.tenantId!),
    enabled: !!user?.tenantId,
  });

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const tenantDisplayName = (tenant as any)?.display_name || tenant?.displayName || tenant?.name || user?.tenantId;

  const filteredNavigation = navigation.filter(item => {
    // Admin-only pages: Metrics, Security, Settings (only for global admins)
    if ((item.name === 'Metrics' || item.name === 'Security' || item.name === 'Settings') && !isGlobalAdmin) {
      return false;
    }
    // Audit Logs: visible for global admins and tenant admins
    if (item.name === 'Audit Logs' && !isAnyAdmin) {
      return false;
    }
    // Tenants and Users: only for admins (global or tenant)
    if ((item.name === 'Tenants' || item.name === 'Users') && !isAnyAdmin) {
      return false;
    }
    return true;
  });

  // Dark Mode Toggle
  useEffect(() => {
    const isDark = localStorage.getItem('darkMode') === 'true';
    setDarkMode(isDark);
    if (isDark) {
      document.documentElement.classList.add('dark');
    }
  }, []);

  const toggleDarkMode = () => {
    const newDarkMode = !darkMode;
    setDarkMode(newDarkMode);
    localStorage.setItem('darkMode', String(newDarkMode));
    if (newDarkMode) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  };

  const handleLogout = async () => {
    try {
      const result = await SweetAlert.confirmLogout();
      if (result.isConfirmed) {
        SweetAlert.loading('Signing out...', 'See you soon');
        await logout();
        SweetAlert.close();
      }
    } catch (error) {
      SweetAlert.close();
      SweetAlert.error('Error signing out', error as string);
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
          <p className="text-xs text-gray-500 dark:text-gray-400 text-center mt-3">
            {serverConfig?.version || 'Loading...'}
          </p>
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
                title={darkMode ? 'Switch to light mode' : 'Switch to dark mode'}
              >
                {darkMode ? (
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
                  {lockedUsers.length > 0 && (
                    <span className="absolute -top-0.5 -right-0.5 z-1 h-5 w-5 rounded-full bg-error-600 flex items-center justify-center">
                      <span className="text-[10px] font-medium text-white">{lockedUsers.length}</span>
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
                        {lockedUsers.length > 0 && (
                          <span className="rounded-full bg-brand-600 px-2.5 py-0.5 text-xs font-medium text-white">
                            {lockedUsers.length} New
                          </span>
                        )}
                      </div>

                      <div className="max-h-96 overflow-y-auto">
                        {lockedUsers.length === 0 ? (
                          <div className="px-5 py-8 text-center">
                            <Bell className="h-12 w-12 text-gray-300 dark:text-gray-600 mx-auto mb-3" />
                            <p className="text-sm text-gray-500 dark:text-gray-400">No notifications</p>
                          </div>
                        ) : (
                          <div>
                            {lockedUsers.map((lockedUser) => {
                              const remainingTime = lockedUser.lockedUntil - Math.floor(Date.now() / 1000);
                              const minutes = Math.floor(remainingTime / 60);
                              const seconds = remainingTime % 60;

                              return (
                                <Link
                                  key={lockedUser.id}
                                  to="/users"
                                  onClick={() => setShowNotifications(false)}
                                  className="flex gap-4 border-b border-gray-200 dark:border-gray-700 px-5 py-4 hover:bg-gray-50 dark:hover:bg-gray-700"
                                >
                                  <div className="h-12 w-12 rounded-full bg-error-50 dark:bg-error-900/30 flex items-center justify-center flex-shrink-0">
                                    <Lock className="h-6 w-6 text-error-600" />
                                  </div>
                                  <div className="flex-1">
                                    <h6 className="text-sm font-medium text-gray-900 dark:text-white mb-1">
                                      Account Locked
                                    </h6>
                                    <p className="text-xs text-gray-600 dark:text-gray-400">
                                      {lockedUser.displayName} - {lockedUser.failedAttempts} failed attempts
                                    </p>
                                    <p className="text-xs text-gray-500 dark:text-gray-500 mt-1">
                                      Unlocks in {minutes}m {seconds}s
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
        <main className="flex-1 overflow-x-hidden overflow-y-auto bg-gray-50 dark:bg-gray-900">
          <div className="mx-auto max-w-screen-2xl 3xl:max-w-[95%] 4xl:max-w-[90%] 5xl:max-w-[85%] p-4 md:p-6 2xl:p-10 3xl:p-12 4xl:p-16">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}

export default AppLayout;
