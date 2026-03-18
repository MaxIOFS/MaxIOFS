import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';

interface IDPStatusBadgeProps {
  authProvider?: string;
  className?: string;
}

export function IDPStatusBadge({ authProvider, className }: IDPStatusBadgeProps) {
  const { t } = useTranslation('idp');

  if (!authProvider || authProvider === 'local' || authProvider === '') {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-secondary text-foreground', className)}>
        {t('badgeLocal')}
      </span>
    );
  }

  if (authProvider.startsWith('ldap:')) {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300', className)}>
        {t('badgeLDAP')}
      </span>
    );
  }

  if (authProvider.startsWith('oauth:')) {
    return (
      <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300', className)}>
        {t('badgeSSO')}
      </span>
    );
  }

  return (
    <span className={cn('inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-secondary text-foreground', className)}>
      {authProvider}
    </span>
  );
}
