import React, { useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
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
      { name: 'Access Keys',        href: '/users/access-keys',  icon: Lock },
      { name: 'Tenants',            href: '/tenants',            icon: Building2 },
      { name: 'Identity Providers', href: '/identity-providers', icon: Shield },
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
  filteredNavigation: NavItem[];
  isGlobalAdmin: boolean;
  hasNewVersion: boolean;
  latestVersion: string;
  serverConfig: ServerConfig | undefined;
}

export function SidebarNav({
  sidebarOpen,
  onClose,
  filteredNavigation,
  isGlobalAdmin,
  hasNewVersion,
  latestVersion,
  serverConfig,
}: SidebarNavProps) {
  const { pathname } = useLocation();
  const basePath = useBasePath();
  const [expandedMenus, setExpandedMenus] = useState<string[]>([]);

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
          onClick={onClose}
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
                    {isExpanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
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
            <div className="w-2 h-2 bg-success-500 rounded-full animate-pulse" />
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
  );
}
