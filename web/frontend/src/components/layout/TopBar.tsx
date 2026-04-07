import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  AlertTriangle,
  Bell,
  ChevronDown,
  HardDrive,
  Lock,
  LogOut,
  Menu,
  Moon,
  Network,
  ShieldAlert,
  Sun,
  User,
  X,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import type { User as UserType } from '@/types';
import type { Notification } from '@/hooks/useNotifications';

type Language = 'en' | 'es';

const LANGUAGES: { code: Language; flag: string; label: string }[] = [
  { code: 'en', flag: '🇬🇧', label: 'EN' },
  { code: 'es', flag: '🇪🇸', label: 'ES' },
];

interface TopBarProps {
  onMenuOpen: () => void;
  user: UserType | null;
  isGlobalAdmin: boolean;
  tenantDisplayName: string | undefined;
  effectiveTheme: 'light' | 'dark';
  onToggleDarkMode: () => void;
  language: Language;
  onLanguageChange: (lang: Language) => void;
  notifications: Notification[];
  unreadCount: number;
  totalUnread: number;
  hasDefaultPassword: boolean;
  onMarkAsRead: (id: string) => void;
  onMarkAllAsRead: () => void;
  onClearNotification: (id: string) => void;
  onLogout: () => void;
}

export function TopBar({
  onMenuOpen,
  user,
  isGlobalAdmin,
  tenantDisplayName,
  effectiveTheme,
  onToggleDarkMode,
  language,
  onLanguageChange,
  notifications,
  unreadCount,
  totalUnread,
  hasDefaultPassword,
  onMarkAsRead,
  onMarkAllAsRead,
  onClearNotification,
  onLogout,
}: TopBarProps) {
  const navigate = useNavigate();
  const { t } = useTranslation('layout');
  const [showUserMenu, setShowUserMenu] = useState(false);
  const [showNotifications, setShowNotifications] = useState(false);
  const [showLanguageMenu, setShowLanguageMenu] = useState(false);

  const currentLang = LANGUAGES.find(l => l.code === language) ?? LANGUAGES[0];

  const userLabel = user?.tenantId
    ? tenantDisplayName
    : isGlobalAdmin ? t('globalAdmin') : t('globalUser');

  return (
    <header className="flex w-full h-16 flex-shrink-0 bg-transparent">
      <div className="flex flex-grow items-center justify-between px-4">
        {/* Mobile menu button */}
        <div className="flex items-center gap-2 lg:hidden">
          <button
            onClick={onMenuOpen}
            aria-label={t('openMenu')}
            className="rounded-button border border-border bg-card p-1.5 hover:bg-secondary transition-all duration-200"
          >
            <Menu className="h-5 w-5 text-muted-foreground" />
          </button>
        </div>

        <div className="flex-1" />

        {/* Right side actions */}
        <div className="flex items-center gap-2">
          {/* Dark Mode Toggle */}
          <button
            onClick={onToggleDarkMode}
            aria-label={effectiveTheme === 'dark' ? t('switchToLightMode') : t('switchToDarkMode')}
            className="flex h-9 w-9 3xl:h-10 3xl:w-10 items-center justify-center rounded-button border border-border bg-card hover:bg-secondary transition-all duration-200"
          >
            {effectiveTheme === 'dark' ? (
              <Sun className="h-4 w-4 text-yellow-500" />
            ) : (
              <Moon className="h-4 w-4 text-muted-foreground" />
            )}
          </button>

          {/* Language Selector */}
          <div className="relative">
            <button
              onClick={() => setShowLanguageMenu(!showLanguageMenu)}
              aria-label={t('changeLanguage')}
              aria-expanded={showLanguageMenu}
              className="flex h-9 items-center gap-1.5 px-2.5 rounded-button border border-border bg-card hover:bg-secondary transition-all duration-200"
            >
              <span className="text-base leading-none">{currentLang.flag}</span>
              <span className="text-xs font-semibold text-muted-foreground">{currentLang.label}</span>
            </button>

            {showLanguageMenu && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowLanguageMenu(false)} />
                <div className="absolute right-0 mt-2 w-40 rounded-card border border-border bg-card shadow-float z-50 overflow-hidden">
                  <div className="px-3 py-2 border-b border-border/50">
                    <p className="text-xs font-medium text-muted-foreground">{t('changeLanguage')}</p>
                  </div>
                  {LANGUAGES.map((lang) => (
                    <button
                      key={lang.code}
                      onClick={() => { onLanguageChange(lang.code); setShowLanguageMenu(false); }}
                      className={cn(
                        'flex w-full items-center gap-3 px-3 py-2.5 text-sm transition-colors',
                        language === lang.code
                          ? 'bg-brand-600/10 text-brand-600 dark:text-brand-400 font-semibold'
                          : 'text-foreground hover:bg-secondary'
                      )}
                    >
                      <span className="text-base">{lang.flag}</span>
                      <span>{lang.code === 'en' ? t('english') : t('spanish')}</span>
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>

          {/* Notifications */}
          <div className="relative">
            <button
              onClick={() => setShowNotifications(!showNotifications)}
              aria-label={t('openNotifications')}
              aria-expanded={showNotifications}
              className="relative flex h-9 w-9 3xl:h-10 3xl:w-10 items-center justify-center rounded-button border border-border bg-card hover:bg-secondary transition-all duration-200"
            >
              <Bell className="h-4 w-4 text-muted-foreground" />
              {totalUnread > 0 && (
                <span className="absolute -top-0.5 -right-0.5 z-1 h-5 w-5 rounded-full bg-error-600 flex items-center justify-center">
                  <span className="text-[10px] font-medium text-white">{totalUnread}</span>
                </span>
              )}
            </button>

            {showNotifications && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowNotifications(false)} />
                <div className="absolute -right-16 sm:right-0 mt-2 w-80 sm:w-96 bg-card rounded-card shadow-float border border-border z-50">
                  <div className="flex items-center justify-between px-5 py-4 border-b border-border/50">
                    <h5 className="text-sm font-semibold text-foreground">{t('notifications')}</h5>
                    <div className="flex gap-2">
                      {unreadCount > 0 && (
                        <span className="rounded-full bg-brand-600 px-2.5 py-0.5 text-xs font-medium text-white">
                          {unreadCount} {t('newNotifications')}
                        </span>
                      )}
                      {notifications.length > 0 && (
                        <button
                          onClick={onMarkAllAsRead}
                          className="text-xs text-brand-600 hover:text-brand-700 font-medium"
                        >
                          {t('markAllRead')}
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
                        className="flex gap-4 border-b border-border/50 px-5 py-4 hover:bg-secondary transition-colors bg-amber-500/5"
                      >
                        <div className="h-12 w-12 rounded-full bg-amber-500/10 flex items-center justify-center flex-shrink-0">
                          <ShieldAlert className="h-6 w-6 text-amber-600 dark:text-amber-400" />
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-start justify-between gap-2 mb-1">
                            <h6 className="text-sm text-foreground font-semibold">{t('securityWarning')}</h6>
                            <span className="h-2 w-2 rounded-full bg-amber-500 flex-shrink-0 mt-1.5" />
                          </div>
                          <p className="text-xs text-muted-foreground">
                            {t('defaultPasswordWarning')}
                          </p>
                        </div>
                      </Link>
                    )}

                    {notifications.length === 0 && !hasDefaultPassword ? (
                      <div className="px-5 py-8 text-center">
                        <Bell className="h-12 w-12 text-muted-foreground/30 mx-auto mb-3" />
                        <p className="text-sm text-muted-foreground">{t('noNotifications')}</p>
                      </div>
                    ) : (
                      <div>
                        {notifications.map((notification) => {
                          const timestamp = new Date(notification.timestamp * 1000);
                          const diffMins = Math.floor((Date.now() - timestamp.getTime()) / 60000);
                          const timeAgo =
                            diffMins < 1 ? t('justNow') :
                            diffMins < 60 ? t('minutesAgo', { count: diffMins }) :
                            diffMins < 1440 ? t('hoursAgo', { count: Math.floor(diffMins / 60) }) :
                            t('daysAgo', { count: Math.floor(diffMins / 1440) });

                          // Per-type visual config
                          type NotifConfig = {
                            Icon: React.ElementType;
                            iconClass: string;
                            bgClass: string;
                            dotClass: string;
                            title: string;
                            to: string;
                          };
                          const CONFIG: Record<string, NotifConfig> = {
                            user_locked: {
                              Icon: Lock, iconClass: 'text-error-600', bgClass: 'bg-error-500/10',
                              dotClass: 'bg-error-500', title: t('accountLocked'), to: '/users',
                            },
                            disk_warning: {
                              Icon: HardDrive, iconClass: 'text-amber-600 dark:text-amber-400', bgClass: 'bg-amber-500/10',
                              dotClass: 'bg-amber-500', title: t('diskWarning'), to: '/settings',
                            },
                            disk_critical: {
                              Icon: HardDrive, iconClass: 'text-error-600', bgClass: 'bg-error-500/10',
                              dotClass: 'bg-error-500', title: t('diskCritical'), to: '/settings',
                            },
                            cluster_node_warning: {
                              Icon: Network, iconClass: 'text-amber-600 dark:text-amber-400', bgClass: 'bg-amber-500/10',
                              dotClass: 'bg-amber-500', title: t('clusterNodeWarning'), to: '/cluster/ha',
                            },
                            cluster_node_critical: {
                              Icon: Network, iconClass: 'text-error-600', bgClass: 'bg-error-500/10',
                              dotClass: 'bg-error-500', title: t('clusterNodeCritical'), to: '/cluster/ha',
                            },
                          };
                          const cfg: NotifConfig = CONFIG[notification.type] ?? {
                            Icon: AlertTriangle, iconClass: 'text-amber-600', bgClass: 'bg-amber-500/10',
                            dotClass: 'bg-amber-500', title: notification.type, to: '/',
                          };

                          return (
                            <div
                              key={notification.id}
                              className={cn(
                                'flex gap-3 border-b border-border/50 px-4 py-3 transition-colors',
                                !notification.read && 'bg-brand-600/5'
                              )}
                            >
                              {/* Clickable area navigates to relevant page */}
                              <Link
                                to={cfg.to}
                                onClick={() => { onMarkAsRead(notification.id); setShowNotifications(false); }}
                                className="flex gap-3 flex-1 min-w-0 hover:opacity-80 transition-opacity"
                              >
                                <div className={cn('h-10 w-10 rounded-full flex items-center justify-center flex-shrink-0', cfg.bgClass)}>
                                  <cfg.Icon className={cn('h-5 w-5', cfg.iconClass)} />
                                </div>
                                <div className="flex-1 min-w-0">
                                  <div className="flex items-center gap-2 mb-0.5">
                                    <h6 className={cn('text-sm text-foreground truncate', !notification.read && 'font-semibold')}>
                                      {cfg.title}
                                    </h6>
                                    {!notification.read && (
                                      <span className={cn('h-2 w-2 rounded-full flex-shrink-0', cfg.dotClass)} />
                                    )}
                                  </div>
                                  <p className="text-xs text-muted-foreground break-words line-clamp-2">{notification.message}</p>
                                  <p className="text-xs text-muted-foreground/70 mt-0.5">{timeAgo}</p>
                                </div>
                              </Link>
                              {/* Dismiss button — removes from panel without requiring confirmation */}
                              <button
                                onClick={() => onClearNotification(notification.id)}
                                aria-label={t('dismissNotification')}
                                className="flex-shrink-0 self-start mt-1 p-1 rounded hover:bg-secondary text-muted-foreground/50 hover:text-muted-foreground transition-colors"
                              >
                                <X className="h-3.5 w-3.5" />
                              </button>
                            </div>
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
              className="flex items-center gap-2 rounded-button hover:bg-secondary px-2 py-1.5 transition-all duration-200"
            >
              <span className="hidden text-right lg:block">
                <span className="block text-sm font-medium text-foreground">
                  {user?.username || 'Unknown'}
                </span>
                <span className="block text-xs text-muted-foreground">{userLabel}</span>
              </span>
              <span className="h-9 w-9 3xl:h-10 3xl:w-10 rounded-full bg-gradient-to-br from-brand-500 to-brand-600 flex items-center justify-center flex-shrink-0">
                <span className="text-sm font-semibold text-white">
                  {user?.username?.charAt(0).toUpperCase() || 'U'}
                </span>
              </span>
              <ChevronDown className="hidden sm:block h-4 w-4 text-muted-foreground" />
            </button>

            {showUserMenu && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowUserMenu(false)} />
                <div className="absolute right-0 mt-2 w-56 rounded-card border border-border bg-card shadow-float z-50">
                  <div className="flex items-center gap-3 border-b border-border/50 px-4 py-4">
                    <span className="h-10 w-10 rounded-full bg-gradient-to-br from-brand-500 to-brand-600 flex items-center justify-center flex-shrink-0">
                      <span className="text-sm font-semibold text-white">
                        {user?.username?.charAt(0).toUpperCase() || 'U'}
                      </span>
                    </span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-foreground truncate">{user?.username || 'Unknown'}</p>
                      <p className="text-xs text-muted-foreground truncate">{user?.email || t('noEmail')}</p>
                    </div>
                  </div>
                  <div className="p-2">
                    <button
                      onClick={() => { setShowUserMenu(false); navigate(`/users/${user?.id}`); }}
                      className="flex w-full items-center gap-3 rounded-button px-3 py-2.5 text-sm font-medium text-foreground hover:bg-secondary transition-all duration-200"
                    >
                      <User className="h-4 w-4" />
                      {t('myProfile')}
                    </button>
                    <button
                      onClick={() => { setShowUserMenu(false); onLogout(); }}
                      className="flex w-full items-center gap-3 rounded-button px-3 py-2.5 text-sm font-medium text-error-600 hover:bg-error-500/10 transition-all duration-200"
                    >
                      <LogOut className="h-4 w-4" />
                      {t('logOut')}
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
