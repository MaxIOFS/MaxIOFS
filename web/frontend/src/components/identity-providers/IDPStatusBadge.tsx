import { cn } from '@/lib/utils';

interface IDPStatusBadgeProps {
  authProvider?: string;
  className?: string;
}

export function IDPStatusBadge({ authProvider, className }: IDPStatusBadgeProps) {
  if (!authProvider || authProvider === 'local' || authProvider === '') {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300', className)}>
        Local
      </span>
    );
  }

  if (authProvider.startsWith('ldap:')) {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300', className)}>
        LDAP
      </span>
    );
  }

  if (authProvider.startsWith('oauth:')) {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300', className)}>
        SSO
      </span>
    );
  }

  return (
    <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300', className)}>
      {authProvider}
    </span>
  );
}
