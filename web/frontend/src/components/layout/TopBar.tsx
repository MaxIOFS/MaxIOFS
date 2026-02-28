import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  Bell,
  ChevronDown,
  Lock,
  LogOut,
  Menu,
  Moon,
  ShieldAlert,
  Sun,
  User,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import type { User as UserType } from '@/types';
import type { Notification } from '@/hooks/useNotifications';

interface TopBarProps {
  onMenuOpen: () => void;
  user: UserType | null;
  isGlobalAdmin: boolean;
  tenantDisplayName: string | undefined;
  effectiveTheme: 'light' | 'dark';
  onToggleDarkMode: () => void;
  notifications: Notification[];
  unreadCount: number;
  totalUnread: number;
  hasDefaultPassword: boolean;
  onMarkAsRead: (id: string) => void;
  onMarkAllAsRead: () => void;
  onLogout: () => void;
}

export function TopBar({
  onMenuOpen,
  user,
  isGlobalAdmin,
  tenantDisplayName,
  effectiveTheme,
  onToggleDarkMode,
  notifications,
  unreadCount,
  totalUnread,
  hasDefaultPassword,
  onMarkAsRead,
  onMarkAllAsRead,
  onLogout,
}: TopBarProps) {
  const navigate = useNavigate();
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);

  const userLabel = user?.tenantId
    ? tenantDisplayName
    : isGlobalAdmin ? 'Global Admin' : 'Global User';

  return (
    <header className="sticky top-0 z-30 flex w-full h-20 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shadow-soft-md backdrop-blur-sm bg-white/95 dark:bg-gray-900/95">
      <div className="flex flex-grow items-center justify-between px-6">
        {/* Mobile menu button */}
        <div className="flex items-center gap-2 sm:gap-4 lg:hidden">
          <button
            onClick={onMenuOpen}
            className="z-50 block rounded-button border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 p-1.5 shadow-soft hover:shadow-soft-md hover:bg-gray-50 dark:hover:bg-gray-700 lg:hidden transition-all duration-200"
          >
            <Menu className="h-5 w-5 text-gray-600 dark:text-gray-400" />
          </button>
        </div>

        <div className="flex-1" />

        {/* Right side actions */}
        <div className="flex items-center gap-3 2xl:gap-7">
          {/* Dark Mode Toggle */}
          <button
            onClick={onToggleDarkMode}
            className="flex h-10 w-10 3xl:h-12 3xl:w-12 4xl:h-14 4xl:w-14 items-center justify-center rounded-full border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 transition-all duration-200 shadow-soft hover:shadow-soft-md"
            title={effectiveTheme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
          >
            {effectiveTheme === 'dark' ? (
              <Sun className="h-5 w-5 3xl:h-6 3xl:w-6 4xl:h-7 4xl:w-7 text-yellow-500" />
            ) : (
              <Moon className="h-5 w-5 3xl:h-6 3xl:w-6 4xl:h-7 4xl:w-7 text-gray-600" />
            )}
          </button>

          {/* Notifications */}
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
                <div className="fixed inset-0 z-40" onClick={() => setShowNotifications(false)} />
                <div className="absolute -right-16 sm:right-0 mt-2.5 w-80 sm:w-96 bg-white dark:bg-gray-800 rounded-card shadow-soft-xl border border-gray-200 dark:border-gray-700 z-50">
                  <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                    <h5 className="text-sm font-semibold text-gray-900 dark:text-white">Notifications</h5>
                    <div className="flex gap-2">
                      {unreadCount > 0 && (
                        <span className="rounded-full bg-brand-600 px-2.5 py-0.5 text-xs font-medium text-white">
                          {unreadCount} New
                        </span>
                      )}
                      {notifications.length > 0 && (
                        <button
                          onClick={onMarkAllAsRead}
                          className="text-xs text-brand-600 hover:text-brand-700 font-medium"
                        >
                          Mark all read
                        </button>
                      )}
                    </div>
                  </div>

                  <div className="max-h-96 overflow-y-auto">
                    {/* Default password warning */}
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
                            <h6 className="text-sm text-gray-900 dark:text-white font-semibold">Security Warning</h6>
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
                          const diffMins = Math.floor((Date.now() - timestamp.getTime()) / 60000);
                          const timeAgo =
                            diffMins < 1 ? 'Just now' :
                            diffMins < 60 ? `${diffMins}m ago` :
                            diffMins < 1440 ? `${Math.floor(diffMins / 60)}h ago` :
                            `${Math.floor(diffMins / 1440)}d ago`;

                          return (
                            <Link
                              key={notification.id}
                              to="/users"
                              onClick={() => { onMarkAsRead(notification.id); setShowNotifications(false); }}
                              className={cn(
                                'flex gap-4 border-b border-gray-200 dark:border-gray-700 px-5 py-4 hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors',
                                !notification.read && 'bg-brand-50/50 dark:bg-brand-900/10'
                              )}
                            >
                              <div className="h-12 w-12 rounded-full bg-error-50 dark:bg-error-900/30 flex items-center justify-center flex-shrink-0">
                                <Lock className="h-6 w-6 text-error-600" />
                              </div>
                              <div className="flex-1 min-w-0">
                                <div className="flex items-start justify-between gap-2 mb-1">
                                  <h6 className={cn('text-sm text-gray-900 dark:text-white', !notification.read && 'font-semibold')}>
                                    {notification.type === 'user_locked' ? 'Account Locked' : notification.type}
                                  </h6>
                                  {!notification.read && (
                                    <span className="h-2 w-2 rounded-full bg-brand-600 flex-shrink-0 mt-1.5" />
                                  )}
                                </div>
                                <p className="text-xs text-gray-600 dark:text-gray-400 break-words">{notification.message}</p>
                                <p className="text-xs text-gray-500 dark:text-gray-500 mt-1">{timeAgo}</p>
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

          {/* User Menu */}
          <div className="relative">
            <button
              onClick={() => setShowUserMenu(!showUserMenu)}
              className="flex items-center gap-3 rounded-button hover:bg-gray-50 dark:hover:bg-gray-800 px-2 py-2 transition-all duration-200 hover:shadow-soft"
            >
              <span className="hidden text-right lg:block">
                <span className="block text-sm font-medium text-gray-900 dark:text-white">
                  {user?.username || 'Unknown'}
                </span>
                <span className="block text-xs text-gray-500 dark:text-gray-400">{userLabel}</span>
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
                <div className="fixed inset-0 z-40" onClick={() => setShowUserMenu(false)} />
                <div className="absolute right-0 mt-2.5 w-56 rounded-card border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800 shadow-soft-xl z-50">
                  <div className="flex items-center gap-3 border-b border-gray-200 dark:border-gray-700 px-4 py-4">
                    <span className="h-12 w-12 rounded-full bg-gradient-to-br from-brand-500 to-brand-600 flex items-center justify-center flex-shrink-0">
                      <span className="text-base font-semibold text-white">
                        {user?.username?.charAt(0).toUpperCase() || 'U'}
                      </span>
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-gray-900 dark:text-white truncate">{user?.username || 'Unknown'}</p>
                      <p className="text-xs text-gray-500 dark:text-gray-400 truncate">{user?.email || 'No email'}</p>
                    </div>
                  </div>
                  <div className="p-2">
                    <button
                      onClick={() => { setShowUserMenu(false); navigate(`/users/${user?.id}`); }}
                      className="flex w-full items-center gap-3 rounded-button px-3 py-2.5 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700 transition-all duration-200"
                    >
                      <User className="h-4 w-4" />
                      My Profile
                    </button>
                    <button
                      onClick={() => { setShowUserMenu(false); onLogout(); }}
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
  );
}
