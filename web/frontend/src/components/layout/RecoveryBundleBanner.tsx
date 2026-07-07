import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { KeyRound } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

interface RecoveryBundleBannerProps {
  isGlobalAdmin: boolean;
}

// RecoveryBundleBanner shows an informative banner to global admins until the
// encryption recovery bundle has been downloaded. The KEK lives only in the
// database — without an external backup, losing the database means losing
// every encrypted object. The banner disappears once the bundle is downloaded.
export function RecoveryBundleBanner({ isGlobalAdmin }: RecoveryBundleBannerProps) {
  const { t } = useTranslation('layout');

  const { data: status } = useQuery({
    queryKey: ['encryptionRecoveryStatus'],
    queryFn: () => APIClient.getEncryptionRecoveryStatus(),
    enabled: isGlobalAdmin,
    refetchOnWindowFocus: false,
    staleTime: 60000,
  });

  if (!isGlobalAdmin || !status || status.bundleDownloaded) return null;

  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-blue-50 dark:bg-blue-950/40 border-b border-blue-200 dark:border-blue-800">
      <KeyRound className="h-4 w-4 flex-shrink-0 text-blue-600 dark:text-blue-400" />
      <p className="text-sm font-medium text-blue-800 dark:text-blue-300">
        {t('recoveryBundleBanner')}{' '}
        <Link to="/settings" className="underline hover:text-blue-950 dark:hover:text-blue-100">
          {t('recoveryBundleBannerLink')}
        </Link>
      </p>
    </div>
  );
}
