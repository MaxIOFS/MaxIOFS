'use client';

import React, { useState } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
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
  ChevronDown,
  Bell,
  Building2,
  Shield,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuth } from '@/hooks/useAuth';
import { useLockedUsers } from '@/hooks/useLockedUsers';
import SweetAlert from '@/lib/sweetalert';

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

export function TopBar() {
  const pathname = usePathname();
  const { user, logout } = useAuth();
  const { data: lockedUsers = [], isLoading: loadingLockedUsers } = useLockedUsers();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [expandedMenu, setExpandedMenu] = useState<string | null>(null);

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

  const isActiveRoute = (href: string): boolean => {
    if (href === '/') {
      return pathname === '/';
    }
    return pathname.startsWith(href);
  };

  return (
    <header className="bg-white shadow-sm border-b border-gray-200 sticky top-0 z-50">
      <div className="flex items-center justify-between h-16 px-6">
        {/* Left: Logo */}
        <div className="flex items-center space-x-8">
          <Link href="/" className="flex items-center space-x-3 hover:opacity-80 transition-opacity">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/assets/img/icon.png"
              alt="MaxIOFS"
              className="w-8 h-8 rounded-lg"
            />
            <div className="hidden sm:block">
              <h1 className="text-lg font-semibold text-gray-900">MaxIOFS</h1>
            </div>
          </Link>

          {/* Center: Navigation */}
          <nav className="hidden lg:flex items-center space-x-1">
            {navigation.map((item) => {
              const isActive = isActiveRoute(item.href);
              const hasChildren = item.children && item.children.length > 0;
              const isExpanded = expandedMenu === item.name;

              return (
                <div key={item.name} className="relative">
                  {hasChildren ? (
                    <button
                      onClick={() => setExpandedMenu(isExpanded ? null : item.name)}
                      onMouseEnter={() => setExpandedMenu(item.name)}
                      onMouseLeave={() => setExpandedMenu(null)}
                      className={cn(
                        'flex items-center space-x-2 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                        isActive
                          ? 'bg-blue-50 text-blue-700'
                          : 'text-gray-700 hover:bg-gray-100'
                      )}
                    >
                      <item.icon className="h-4 w-4" />
                      <span>{item.name}</span>
                      <ChevronDown className="h-3 w-3" />
                    </button>
                  ) : (
                    <Link
                      href={item.href}
                      className={cn(
                        'flex items-center space-x-2 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                        isActive
                          ? 'bg-blue-50 text-blue-700'
                          : 'text-gray-700 hover:bg-gray-100'
                      )}
                    >
                      <item.icon className="h-4 w-4" />
                      <span>{item.name}</span>
                    </Link>
                  )}

                  {/* Dropdown menu */}
                  {hasChildren && isExpanded && (
                    <div
                      className="absolute top-full left-0 mt-1 w-48 bg-white rounded-md shadow-lg border border-gray-200 py-1"
                      onMouseEnter={() => setExpandedMenu(item.name)}
                      onMouseLeave={() => setExpandedMenu(null)}
                    >
                      {item.children!.map((child) => (
                        <Link
                          key={child.name}
                          href={child.href}
                          className={cn(
                            'flex items-center space-x-2 px-4 py-2 text-sm transition-colors',
                            isActiveRoute(child.href)
                              ? 'bg-blue-50 text-blue-700'
                              : 'text-gray-700 hover:bg-gray-100'
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
        </div>

        {/* Right: Notifications + User */}
        <div className="flex items-center space-x-4">
          {/* Notifications */}
          <div className="relative">
            <button
              onClick={() => setShowNotifications(!showNotifications)}
              className="relative p-2 text-gray-500 hover:bg-gray-100 rounded-lg transition-colors"
            >
              <Bell className="h-5 w-5" />
              {/* Notification badge - shows count of locked users */}
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
                    {loadingLockedUsers ? (
                      <div className="p-4 text-sm text-gray-500 text-center">
                        Loading...
                      </div>
                    ) : lockedUsers.length === 0 ? (
                      <div className="p-4 text-sm text-gray-500 text-center">
                        No notifications
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
                              href="/users"
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
                        href="/users"
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
              <ChevronDown className="h-4 w-4 text-gray-500" />
            </button>

            {/* User dropdown menu */}
            {showUserMenu && (
              <>
                {/* Backdrop */}
                <div
                  className="fixed inset-0 z-10"
                  onClick={() => setShowUserMenu(false)}
                />

                {/* Menu */}
                <div className="absolute right-0 mt-2 w-56 bg-white rounded-md shadow-lg border border-gray-200 z-20">
                  <div className="py-1">
                    {/* User info */}
                    <div className="px-4 py-3 border-b border-gray-100">
                      <p className="text-sm font-medium text-gray-900">
                        {user?.username || 'Unknown User'}
                      </p>
                      <p className="text-xs text-gray-500 mt-1">
                        {user?.email || 'No email'}
                      </p>
                      {user?.tenantId && (
                        <p className="text-xs text-blue-600 mt-1">
                          Tenant ID: {user.tenantId}
                        </p>
                      )}
                    </div>

                    {/* Menu items */}
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
  );
}

export default TopBar;
