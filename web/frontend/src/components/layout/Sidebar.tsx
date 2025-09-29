'use client';

import React from 'react';
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
  HardDrive,
  Activity,
  Shield,
} from 'lucide-react';
import { cn } from '@/lib/utils';

export interface SidebarProps {
  isOpen?: boolean;
  onClose?: () => void;
}

interface NavItem {
  name: string;
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  badge?: string | number;
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
    name: 'Objects',
    href: '/objects',
    icon: FolderOpen,
  },
  {
    name: 'Users & Access',
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
        icon: Shield,
      },
    ],
  },
  {
    name: 'Metrics',
    href: '/metrics',
    icon: BarChart3,
    children: [
      {
        name: 'Overview',
        href: '/metrics',
        icon: BarChart3,
      },
      {
        name: 'Storage',
        href: '/metrics/storage',
        icon: HardDrive,
      },
      {
        name: 'Performance',
        href: '/metrics/performance',
        icon: Activity,
      },
    ],
  },
  {
    name: 'Security',
    href: '/security',
    icon: Lock,
    children: [
      {
        name: 'Object Lock',
        href: '/security/object-lock',
        icon: Lock,
      },
      {
        name: 'Encryption',
        href: '/security/encryption',
        icon: Shield,
      },
    ],
  },
  {
    name: 'Settings',
    href: '/settings',
    icon: Settings,
  },
];

export function Sidebar({ isOpen = true, onClose }: SidebarProps) {
  const pathname = usePathname();

  const isActiveRoute = (href: string, exact = false): boolean => {
    if (exact) {
      return pathname === href;
    }
    return pathname.startsWith(href) && href !== '/';
  };

  return (
    <>
      {/* Mobile backdrop */}
      {isOpen && (
        <div
          className="fixed inset-0 z-40 lg:hidden bg-black bg-opacity-50"
          onClick={onClose}
        />
      )}

      {/* Sidebar */}
      <div
        className={cn(
          'fixed inset-y-0 left-0 z-50 w-64 bg-white border-r border-gray-200 transform transition-transform duration-200 ease-in-out lg:translate-x-0 lg:static lg:inset-0',
          isOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        {/* Logo */}
        <div className="flex items-center h-16 px-6 border-b border-gray-200">
          <div className="flex items-center space-x-3">
            <div className="w-8 h-8 bg-blue-600 rounded-lg flex items-center justify-center">
              <HardDrive className="h-5 w-5 text-white" />
            </div>
            <div>
              <h1 className="text-lg font-semibold text-gray-900">MaxIOFS</h1>
              <p className="text-xs text-gray-500">Object Storage</p>
            </div>
          </div>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-4 py-6 space-y-2 overflow-y-auto">
          {navigation.map((item) => (
            <NavItem
              key={item.name}
              item={item}
              pathname={pathname}
              isActiveRoute={isActiveRoute}
            />
          ))}
        </nav>

        {/* Footer */}
        <div className="p-4 border-t border-gray-200">
          <div className="flex items-center space-x-3 text-sm text-gray-500">
            <div className="w-2 h-2 bg-green-500 rounded-full" />
            <span>System Online</span>
          </div>
          <p className="text-xs text-gray-400 mt-1">
            Version 1.0.0
          </p>
        </div>
      </div>
    </>
  );
}

interface NavItemProps {
  item: NavItem;
  pathname: string;
  isActiveRoute: (href: string, exact?: boolean) => boolean;
  level?: number;
}

function NavItem({ item, pathname, isActiveRoute, level = 0 }: NavItemProps) {
  const hasChildren = item.children && item.children.length > 0;
  const isActive = isActiveRoute(item.href, !hasChildren);
  const isExpanded = hasChildren && item.children.some(child => isActiveRoute(child.href));

  return (
    <div>
      <Link
        href={item.href}
        className={cn(
          'flex items-center space-x-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          level > 0 && 'ml-4',
          isActive
            ? 'bg-blue-50 text-blue-700 border-r-2 border-blue-700'
            : 'text-gray-700 hover:bg-gray-100'
        )}
      >
        <item.icon className={cn(
          'h-5 w-5',
          isActive ? 'text-blue-700' : 'text-gray-500'
        )} />
        <span className="flex-1">{item.name}</span>
        {item.badge && (
          <span className={cn(
            'px-2 py-1 text-xs rounded-full',
            isActive
              ? 'bg-blue-100 text-blue-700'
              : 'bg-gray-100 text-gray-600'
          )}>
            {item.badge}
          </span>
        )}
      </Link>

      {/* Submenu */}
      {hasChildren && isExpanded && (
        <div className="mt-1 space-y-1">
          {item.children!.map((child) => (
            <NavItem
              key={child.name}
              item={child}
              pathname={pathname}
              isActiveRoute={isActiveRoute}
              level={level + 1}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default Sidebar;