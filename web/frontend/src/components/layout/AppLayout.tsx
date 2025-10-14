import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
  Home,
  Database,
  FolderOpen,
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
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuth } from '@/hooks/useAuth';
import { useLockedUsers } from '@/hooks/useLockedUsers';
import SweetAlert from '@/lib/sweetalert';
import { useQuery } from '@tanstack/react-query';
import APIClient from '@/lib/api';

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
  // Objects page removed - access objects through individual buckets
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
        name: 'Tenants',
        href: '/tenants',
        icon: Building2,
      },
    ],
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
];

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const pathname = location.pathname;
  const navigate = useNavigate();
  const { user, logout } = useAuth();
  const { data: lockedUsers = [], isLoading: loadingLockedUsers } = useLockedUsers();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Check if user is Global Admin (no tenantId)
  const isGlobalAdmin = !user?.tenantId;

  // Get tenant name if user belongs to a tenant
  const { data: tenant } = useQuery({
    queryKey: ['tenant', user?.tenantId],
    queryFn: () => APIClient.getTenant(user!.tenantId!),
    enabled: !!user?.tenantId,
  });

  // Backend returns display_name (snake_case) but TypeScript expects displayName (camelCase)
  const tenantDisplayName = (tenant as any)?.display_name || tenant?.displayName || tenant?.name || user?.tenantId;

  // Filter navigation based on user role
  const filteredNavigation = navigation.filter(item => {
    // Hide Metrics, Security, and Settings for non-Global Admins
    if ((item.name === 'Metrics' || item.name === 'Security' || item.name === 'Settings') && !isGlobalAdmin) {
      return false;
    }
    return true;
  });

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
      SweetAlert.error('Error signing out', 'Could not sign out properly');
    }
  };

  const isActiveRoute = (href: string, exact = false): boolean => {
    if (exact) {
      return pathname === href;
    }
    return pathname.startsWith(href) && href !== '/';
  };

  return (
    <div className="flex h-screen bg-gray-50">
      {/* Sidebar */}
      <div
        className={cn(
          'fixed inset-y-0 left-0 z-50 w-64 bg-white border-r border-gray-200 transform transition-transform duration-200 ease-in-out lg:translate-x-0 lg:static',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        {/* Logo */}
        <div className="flex items-center justify-between h-16 px-6 border-b border-gray-200">
          <Link to="/" className="flex items-center space-x-3 hover:opacity-80 transition-opacity">
            <img
              src="/assets/img/icon.png"
              alt="MaxIOFS"
              className="w-8 h-8 rounded-lg"
            />
            <div>
              <h1 className="text-lg font-semibold text-gray-900">MaxIOFS</h1>
              <p className="text-xs text-gray-500">Object Storage</p>
            </div>
          </Link>
          <button
            onClick={() => setSidebarOpen(false)}
            className="lg:hidden p-2 hover:bg-gray-100 rounded-lg"
          >
            <X className="h-5 w-5 text-gray-500" />
          </button>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-4 py-6 space-y-2 overflow-y-auto">
          {filteredNavigation.map((item) => {
            const isActive = isActiveRoute(item.href, !item.children);
            const isExpanded = item.children && item.children.some(child => isActiveRoute(child.href));

            return (
              <div key={item.name}>
                <Link
                  to={item.href}
                  className={cn(
                    'flex items-center space-x-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-blue-50 text-blue-700'
                      : 'text-gray-700 hover:bg-gray-100'
                  )}
                >
                  <item.icon className={cn(
                    'h-5 w-5',
                    isActive ? 'text-blue-700' : 'text-gray-500'
                  )} />
                  <span className="flex-1">{item.name}</span>
                </Link>

                {/* Submenu */}
                {item.children && isExpanded && (
                  <div className="mt-1 ml-4 space-y-1">
                    {item.children.map((child) => (
                      <Link
                        key={child.name}
                        to={child.href}
                        className={cn(
                          'flex items-center space-x-3 px-3 py-2 rounded-lg text-sm transition-colors',
                          isActiveRoute(child.href)
                            ? 'bg-blue-50 text-blue-700'
                            : 'text-gray-600 hover:bg-gray-100'
                        )}
                      >
                        <child.icon className="h-4 w-4" />
                        <span>{child.name}</span>
                      </Link>
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </nav>

        {/* Footer */}
        <div className="p-4 border-t border-gray-200">
          <div className="flex items-center space-x-3 text-sm text-gray-500">
            <div className="w-2 h-2 bg-green-500 rounded-full" />
            <span>System Online</span>
          </div>
          <p className="text-xs text-gray-400 mt-1">
            Version 0.2.0-alpha
          </p>
        </div>
      </div>

      {/* Mobile backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 lg:hidden bg-black bg-opacity-50"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Main content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Top bar */}
        <header className="bg-white shadow-sm border-b border-gray-200 sticky top-0 z-30">
          <div className="flex items-center justify-between h-16 px-6">
            {/* Left: Mobile menu button */}
            <button
              onClick={() => setSidebarOpen(true)}
              className="lg:hidden p-2 hover:bg-gray-100 rounded-lg"
            >
              <Menu className="h-5 w-5 text-gray-500" />
            </button>

            {/* Center: Page title or breadcrumb could go here */}
            <div className="flex-1" />

            {/* Right: Notifications + User */}
            <div className="flex items-center space-x-4">
              {/* Notifications */}
              <div className="relative">
                <button
                  onClick={() => setShowNotifications(!showNotifications)}
                  className="relative p-2 text-gray-500 hover:bg-gray-100 rounded-lg transition-colors"
                >
                  <Bell className="h-5 w-5" />
                  {lockedUsers.length > 0 && (
                    <span className="absolute top-1 right-1 flex items-center justify-center min-w-5 h-5 bg-red-500 text-white text-xs font-bold rounded-full px-1">
                      {lockedUsers.length}
                    </span>
                  )}
                </button>

                {/* Notifications dropdown */}
                {showNotifications && (
                  <>
                    <div
                      className="fixed inset-0 z-10"
                      onClick={() => setShowNotifications(false)}
                    />
                    <div className="absolute right-0 mt-2 w-96 bg-white rounded-md shadow-lg border border-gray-200 z-20">
                      <div className="p-4 border-b border-gray-200 flex items-center justify-between">
                        <h3 className="text-sm font-semibold text-gray-900">Notifications</h3>
                        {lockedUsers.length > 0 && (
                          <span className="text-xs bg-red-100 text-red-700 px-2 py-1 rounded-full font-medium">
                            {lockedUsers.length} locked
                          </span>
                        )}
                      </div>
                      <div className="max-h-96 overflow-y-auto">
                        {lockedUsers.length === 0 ? (
                          <div className="p-8 text-center">
                            <Bell className="h-12 w-12 text-gray-300 mx-auto mb-3" />
                            <p className="text-sm text-gray-500 font-medium">No new notifications</p>
                            <p className="text-xs text-gray-400 mt-1">You're all caught up!</p>
                          </div>
                        ) : (
                          <div className="divide-y divide-gray-100">
                            {lockedUsers.map((lockedUser) => {
                              const remainingTime = lockedUser.lockedUntil - Math.floor(Date.now() / 1000);
                              const minutes = Math.floor(remainingTime / 60);
                              const seconds = remainingTime % 60;

                              return (
                                <Link
                                  key={lockedUser.id}
                                  to="/users"
                                  onClick={() => setShowNotifications(false)}
                                  className="block p-4 hover:bg-gray-50 transition-colors"
                                >
                                  <div className="flex items-start space-x-3">
                                    <div className="flex-shrink-0">
                                      <div className="w-10 h-10 bg-red-100 rounded-full flex items-center justify-center">
                                        <Lock className="h-5 w-5 text-red-600" />
                                      </div>
                                    </div>
                                    <div className="flex-1 min-w-0">
                                      <p className="text-sm font-medium text-gray-900">
                                        Account Locked
                                      </p>
                                      <p className="text-sm text-gray-600 mt-1">
                                        {lockedUser.displayName} ({lockedUser.username})
                                      </p>
                                      <p className="text-xs text-red-600 mt-1">
                                        {lockedUser.failedAttempts} failed attempts
                                      </p>
                                      <p className="text-xs text-gray-500 mt-1">
                                        Unlocks in {minutes}m {seconds}s
                                      </p>
                                    </div>
                                  </div>
                                </Link>
                              );
                            })}
                          </div>
                        )}
                      </div>
                      {lockedUsers.length > 0 && (
                        <div className="p-3 border-t border-gray-200 bg-gray-50">
                          <Link
                            to="/users"
                            onClick={() => setShowNotifications(false)}
                            className="text-xs text-blue-600 hover:text-blue-700 font-medium"
                          >
                            View all users →
                          </Link>
                        </div>
                      )}
                    </div>
                  </>
                )}
              </div>

              {/* User menu */}
              <div className="relative">
                <button
                  onClick={() => setShowUserMenu(!showUserMenu)}
                  className="flex items-center space-x-3 p-2 hover:bg-gray-100 rounded-lg transition-colors"
                >
                  <div className="flex items-center justify-center w-8 h-8 bg-blue-100 text-blue-700 rounded-full">
                    <User className="h-4 w-4" />
                  </div>
                  <div className="hidden md:block text-left">
                    <p className="text-sm font-medium text-gray-900">
                      {user?.username || 'Unknown User'}
                    </p>
                    <p className="text-xs text-gray-500">
                      {user?.email || 'No email'}
                    </p>
                  </div>
                </button>

                {/* User dropdown menu */}
                {showUserMenu && (
                  <>
                    <div
                      className="fixed inset-0 z-10"
                      onClick={() => setShowUserMenu(false)}
                    />
                    <div className="absolute right-0 mt-2 w-56 bg-white rounded-md shadow-lg border border-gray-200 z-20">
                      <div className="py-1">
                        {/* User info */}
                        <div className="px-4 py-3 border-b border-gray-100">
                          <p className="text-sm font-medium text-gray-900 truncate">
                            {user?.username || 'Unknown User'}
                          </p>
                          <p className="text-xs text-gray-500 mt-1 truncate">
                            {user?.email || 'No email'}
                          </p>
                          {user?.tenantId && (
                            <p className="text-xs text-blue-600 mt-1 truncate">
                              {tenantDisplayName}
                            </p>
                          )}
                        </div>

                        {/* Menu items */}
                        <button
                          className="flex items-center w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
                          onClick={() => {
                            setShowUserMenu(false);
                            navigate(`/users/${user?.id}`);
                          }}
                        >
                          <User className="h-4 w-4 mr-3" />
                          Profile
                        </button>
                        <button
                          className="flex items-center w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50"
                          onClick={() => {
                            setShowUserMenu(false);
                            handleLogout();
                          }}
                        >
                          <LogOut className="h-4 w-4 mr-3" />
                          Sign out
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
        <main className="flex-1 overflow-x-hidden overflow-y-auto bg-gray-50 p-6">
          {children}
        </main>
      </div>
    </div>
  );
}

export default AppLayout;
