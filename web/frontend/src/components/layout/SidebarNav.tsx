import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Home,
  Box,
  Users,
  Settings,
  BarChart3,
  Lock,
  Building2,
  X,
  ChevronDown,
  ChevronRight,
  Info,
  FileText,
  Server,
  ArrowUpCircle,
  CheckCircle2,
  Shield,
  ShieldCheck,
  PanelLeftClose,
  PanelLeftOpen,
  UsersRound,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { useBasePath } from '@/hooks/useBasePath';
import type { ServerConfig } from '@/types';

export interface NavItem {
  name: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  children?: NavItem[];
}

export const navigation: NavItem[] = [
  { name: 'Dashboard',    href: '/',                 icon: Home },
  { name: 'Buckets',      href: '/buckets',          icon: Box },
  {
    name: 'Users',
    href: '/users',
    icon: Users,
    children: [
      { name: 'Users',              href: '/users',              icon: Users },
      { name: 'Groups',             href: '/groups',             icon: UsersRound },
      { name: 'Access Keys',        href: '/users/access-keys',  icon: Lock },
      { name: 'Tenants',            href: '/tenants',            icon: Building2 },
      { name: 'Identity Providers', href: '/identity-providers', icon: Shield },
      { name: 'Role Capabilities',  href: '/roles/capabilities',  icon: ShieldCheck },
    ],
  },
  { name: 'Audit Logs', href: '/audit-logs', icon: FileText },
  { name: 'Metrics',    href: '/metrics',    icon: BarChart3 },
  { name: 'Security',   href: '/security',   icon: Lock },
  { name: 'Cluster',    href: '/cluster',    icon: Server },
  { name: 'Settings',   href: '/settings',   icon: Settings },
  { name: 'About',      href: '/about',      icon: Info },
];

interface SidebarNavProps {
  sidebarOpen: boolean;
  onClose: () => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
  filteredNavigation: NavItem[];
  isGlobalAdmin: boolean;
  hasNewVersion: boolean;
  latestVersion: string;
  serverConfig: ServerConfig | undefined;
}

export function SidebarNav({
  sidebarOpen,
  onClose,
  collapsed,
  onToggleCollapse,
  filteredNavigation,
  isGlobalAdmin,
  hasNewVersion,
  latestVersion,
  serverConfig,
}: SidebarNavProps) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const basePath = useBasePath();
  const { t: tNav } = useTranslation('navigation');
  const { t: tLayout } = useTranslation('layout');
  const [expandedMenus, setExpandedMenus] = useState<string[]>([]);

  const navLabels: Record<string, string> = {
    'Dashboard':          tNav('dashboard'),
    'Buckets':            tNav('buckets'),
    'Users':              tNav('users'),
    'Groups':             tNav('groups'),
    'Access Keys':        tNav('accessKeys'),
    'Tenants':            tNav('tenants'),
    'Identity Providers': tNav('identityProviders'),
    'Role Capabilities':  tNav('roleCapabilities'),
    'Audit Logs':         tNav('auditLogs'),
    'Metrics':            tNav('metrics'),
    'Security':           tNav('security'),
    'Cluster':            tNav('cluster'),
    'Settings':           tNav('settings'),
    'About':              tNav('about'),
  };

  const isActiveRoute = (href: string, exact = false): boolean => {
    if (exact || href === '/') return pathname === href;
    if (href === '/users' && pathname.startsWith('/users/')) return pathname === '/users';
    return pathname.startsWith(href);
  };

  const toggleMenu = (menuName: string) => {
    setExpandedMenus(prev =>
      prev.includes(menuName) ? prev.filter(m => m !== menuName) : [...prev, menuName]
    );
  };

  return (
    <aside
      className={cn(
        // Base floating appearance
        'flex flex-col bg-card overflow-hidden transition-all duration-300 ease-in-out',
        'shadow-float rounded-card',
        // Mobile: fixed overlay with margins, slides in/out
        'fixed top-3 left-3 bottom-3 z-50',
        sidebarOpen ? 'translate-x-0' : '-translate-x-[calc(100%+12px)]',
        // Desktop: static in flex flow, always visible, not fixed
        'lg:relative lg:static lg:translate-x-0 lg:top-auto lg:left-auto lg:bottom-auto lg:z-auto',
        // Height on desktop: fills the parent (parent is p-3 flex row)
        'lg:h-full',
        // Width
        collapsed ? 'w-16 lg:w-16' : 'w-72 lg:w-64',
      )}
    >
      {/* Logo Header */}
      <div className={cn(
        'flex items-center h-16 border-b border-border/50 flex-shrink-0',
        collapsed ? 'justify-center px-0' : 'justify-between px-4',
      )}>
        {collapsed ? (
          <Link to="/" className="flex items-center justify-center w-10 h-10 rounded-button bg-brand-600 shadow-glow flex-shrink-0">
            <img
              src={`${basePath}/assets/img/icon.png`}
              alt="MaxIOFS"
              className="w-6 h-6 rounded"
            />
          </Link>
        ) : (
          <Link to="/" className="flex items-center space-x-3 group min-w-0">
            <div className="flex items-center justify-center w-9 h-9 rounded-button bg-brand-600 shadow-glow flex-shrink-0">
              <img
                src={`${basePath}/assets/img/icon.png`}
                alt="MaxIOFS"
                className="w-6 h-6 rounded"
              />
            </div>
            <div className="min-w-0">
              <h1 className="text-base font-bold text-foreground leading-tight">MaxIOFS</h1>
              <p className="text-[10px] text-muted-foreground leading-tight">{tLayout('objectStorage')}</p>
            </div>
          </Link>
        )}

        {/* Mobile close button */}
        {!collapsed && (
          <button
            onClick={onClose}
            aria-label={tLayout('closeNavigation')}
            className="lg:hidden p-1.5 hover:bg-secondary rounded-lg text-muted-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Navigation Menu */}
      <nav className="flex-1 py-4 overflow-y-auto overflow-x-hidden">
        <div className={cn('space-y-0.5', collapsed ? 'px-2' : 'px-3')}>
          {filteredNavigation.map((item) => {
            const isActive = isActiveRoute(item.href, !item.children);
            const isExpanded = item.children && expandedMenus.includes(item.name);
            const hasActiveChild = item.children && item.children.some(child => isActiveRoute(child.href));
            const label = navLabels[item.name] ?? item.name;

            if (collapsed) {
              // Collapsed: icon only with title tooltip
              return (
                <div key={item.name}>
                  <button
                    title={label}
                    onClick={() => {
                      // Parent href is the canonical section root (e.g. /users), not children[0],
                      // so we never navigate to the wrong subsection if child order or filters change.
                      navigate(item.href);
                      onClose();
                    }}
                    className={cn(
                      'flex items-center justify-center w-10 h-10 rounded-button transition-all duration-200 mx-auto',
                      isActive || hasActiveChild
                        ? 'bg-brand-600 text-white shadow-glow-sm'
                        : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                    )}
                  >
                    <item.icon className="h-5 w-5 flex-shrink-0" />
                  </button>
                </div>
              );
            }

            // Expanded
            return (
              <div key={item.name}>
                {item.children ? (
                  <>
                    <button
                      onClick={() => toggleMenu(item.name)}
                      aria-expanded={!!isExpanded}
                      className={cn(
                        'flex items-center justify-between w-full px-3 py-2.5 rounded-button text-sm font-medium transition-all duration-200',
                        hasActiveChild || isExpanded
                          ? 'bg-secondary text-foreground'
                          : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                      )}
                    >
                      <div className="flex items-center space-x-2.5">
                        <item.icon className="h-4 w-4 flex-shrink-0" />
                        <span className="truncate">{label}</span>
                      </div>
                      {isExpanded
                        ? <ChevronDown className="h-3.5 w-3.5 flex-shrink-0" />
                        : <ChevronRight className="h-3.5 w-3.5 flex-shrink-0" />}
                    </button>

                    {isExpanded && (
                      <div className="mt-0.5 ml-4 space-y-0.5 pl-3 border-l border-border/50">
                        {item.children.map((child) => (
                          <Link
                            key={child.name}
                            to={child.href}
                            aria-current={isActiveRoute(child.href, true) ? 'page' : undefined}
                            onClick={onClose}
                            className={cn(
                              'flex items-center space-x-2.5 px-3 py-2 rounded-button text-sm transition-all duration-200',
                              isActiveRoute(child.href, true)
                                ? 'bg-brand-600 text-white font-medium shadow-glow-sm'
                                : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                            )}
                          >
                            <child.icon className="h-3.5 w-3.5 flex-shrink-0" />
                            <span className="truncate">{navLabels[child.name] ?? child.name}</span>
                          </Link>
                        ))}
                      </div>
                    )}
                  </>
                ) : (
                  <Link
                    to={item.href}
                    aria-current={isActive ? 'page' : undefined}
                    onClick={onClose}
                    className={cn(
                      'flex items-center space-x-2.5 px-3 py-2.5 rounded-button text-sm font-medium transition-all duration-200',
                      isActive
                        ? 'bg-brand-600 text-white shadow-glow-sm'
                        : 'text-muted-foreground hover:bg-secondary hover:text-foreground',
                    )}
                  >
                    <item.icon className="h-4 w-4 flex-shrink-0" />
                    <span className="truncate">{label}</span>
                  </Link>
                )}
              </div>
            );
          })}
        </div>
      </nav>

      {/* Footer */}
      <div className={cn(
        'flex-shrink-0 border-t border-border/50',
        collapsed ? 'p-2' : 'p-3',
      )}>
        {!collapsed && (
          <div className="flex items-center space-x-2 px-3 py-2 rounded-button bg-secondary mb-2">
            <div className="w-2 h-2 bg-success-500 rounded-full animate-pulse flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <p className="text-xs font-medium text-foreground leading-tight">{tLayout('systemStatus')}</p>
              <p className="text-[10px] text-muted-foreground leading-tight truncate">{tLayout('allSystemsOperational')}</p>
            </div>
          </div>
        )}

        {!collapsed && (
          <div className="text-center mb-2">
            <p className="text-[10px] text-muted-foreground">
              {serverConfig?.version || tLayout('loadingVersion')}
            </p>
            {isGlobalAdmin && (
              hasNewVersion ? (
                <a
                  href="https://maxiofs.com/downloads"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 mt-1 px-2 py-1 rounded-full bg-amber-100 dark:bg-amber-500/20 text-amber-700 dark:text-amber-300 text-[10px] font-medium border border-amber-200 dark:border-amber-500/30 animate-pulse"
                >
                  <ArrowUpCircle className="h-3 w-3" />
                  {tLayout('newRelease', { version: latestVersion })}
                </a>
              ) : (
                <span className="inline-flex items-center gap-1 mt-1 px-2 py-1 rounded-full bg-emerald-50 dark:bg-emerald-500/20 text-emerald-700 dark:text-emerald-300 text-[10px] font-medium border border-emerald-200 dark:border-emerald-500/30">
                  <CheckCircle2 className="h-3 w-3" />
                  {tLayout('latestVersion')}
                </span>
              )
            )}
          </div>
        )}

        {/* Collapse toggle — desktop only */}
        <button
          onClick={onToggleCollapse}
          title={collapsed ? tLayout('expandSidebar') ?? 'Expand' : tLayout('collapseSidebar') ?? 'Collapse'}
          className={cn(
            'hidden lg:flex items-center justify-center w-full py-2 rounded-button text-muted-foreground hover:bg-secondary hover:text-foreground transition-all duration-200',
            collapsed ? 'px-0' : 'px-3 gap-2 text-xs',
          )}
        >
          {collapsed
            ? <PanelLeftOpen className="h-4 w-4" />
            : <><PanelLeftClose className="h-4 w-4" /><span>{tLayout('collapseSidebar') ?? 'Collapse'}</span></>
          }
        </button>
      </div>
    </aside>
  );
}
