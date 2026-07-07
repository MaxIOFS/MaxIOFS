import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { KeyRound, Download, CheckCircle, AlertCircle, ShieldAlert } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';

const MIN_PASSPHRASE_LEN = 8;

// EncryptionRecovery renders the recovery-bundle section inside the Security
// settings tab. The encryption KEK lives in the database; this lets the admin
// export it as a passphrase-encrypted bundle to store outside the system —
// the disaster-recovery path if the database is ever lost.
export default function EncryptionRecovery() {
  const { t } = useTranslation('settings');
  const queryClient = useQueryClient();

  const [passphrase, setPassphrase] = useState('');
  const [confirmPassphrase, setConfirmPassphrase] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [downloaded, setDownloaded] = useState(false);

  const { data: status } = useQuery({
    queryKey: ['encryptionRecoveryStatus'],
    queryFn: () => APIClient.getEncryptionRecoveryStatus(),
  });

  const downloadMutation = useMutation({
    mutationFn: (pass: string) => APIClient.downloadRecoveryBundle(pass),
    onSuccess: (blob) => {
      // Trigger the browser download of the bundle file.
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `maxiofs-recovery-bundle-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);

      setPassphrase('');
      setConfirmPassphrase('');
      setError(null);
      setDownloaded(true);
      setTimeout(() => setDownloaded(false), 5000);
      queryClient.invalidateQueries({ queryKey: ['encryptionRecoveryStatus'] });
    },
    onError: async (err: any) => {
      // Blob response type means error bodies arrive as blobs too.
      let message = t('recoveryBundleDownloadFailed');
      try {
        const text = await err.response?.data?.text?.();
        if (text) message = JSON.parse(text).error || message;
      } catch { /* keep default message */ }
      setError(message);
    },
  });

  const handleDownload = () => {
    setError(null);
    if (passphrase.length < MIN_PASSPHRASE_LEN) {
      setError(t('recoveryPassphraseTooShort', { min: MIN_PASSPHRASE_LEN }));
      return;
    }
    if (passphrase !== confirmPassphrase) {
      setError(t('recoveryPassphraseMismatch'));
      return;
    }
    downloadMutation.mutate(passphrase);
  };

  return (
    <div className="mb-6 pb-6 border-b border-border">
      <div className="flex items-center gap-2 mb-1">
        <KeyRound className="h-4 w-4 text-brand-600 dark:text-brand-400" />
        <h4 className="text-sm font-semibold text-foreground">{t('recoveryBundleTitle')}</h4>
        {status && (
          status.bundleDownloaded ? (
            <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300">
              <CheckCircle className="h-3 w-3" />
              {t('recoveryBundleDownloadedBadge')}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-amber-100 dark:bg-amber-900/30 text-amber-800 dark:text-amber-300">
              <ShieldAlert className="h-3 w-3" />
              {t('recoveryBundlePendingBadge')}
            </span>
          )
        )}
      </div>
      <p className="text-xs text-muted-foreground leading-relaxed mb-1">
        {t('recoveryBundleDesc')}
      </p>
      <p className="text-xs text-amber-700 dark:text-amber-400 leading-relaxed mb-4">
        {t('recoveryBundleWarning')}
      </p>

      <div className="flex flex-wrap items-end gap-3">
        <div>
          <label className="block text-xs font-medium text-muted-foreground mb-1">
            {t('recoveryPassphrase')}
          </label>
          <input
            type="password"
            value={passphrase}
            onChange={(e) => setPassphrase(e.target.value)}
            autoComplete="new-password"
            className="w-64 px-4 py-2.5 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-colors"
          />
        </div>
        <div>
          <label className="block text-xs font-medium text-muted-foreground mb-1">
            {t('recoveryPassphraseConfirm')}
          </label>
          <input
            type="password"
            value={confirmPassphrase}
            onChange={(e) => setConfirmPassphrase(e.target.value)}
            autoComplete="new-password"
            className="w-64 px-4 py-2.5 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-colors"
          />
        </div>
        <Button
          onClick={handleDownload}
          disabled={downloadMutation.isPending || passphrase.length === 0}
        >
          <Download className="h-4 w-4" />
          {downloadMutation.isPending ? t('recoveryBundleDownloading') : t('recoveryBundleDownload')}
        </Button>
      </div>

      {status?.bundleDownloaded && status.downloadedAt && (
        <p className="mt-2 text-xs text-muted-foreground">
          {t('recoveryBundleLastDownloaded', { date: new Date(status.downloadedAt).toLocaleString() })}
        </p>
      )}

      {downloaded && (
        <div className="mt-3 flex items-center gap-2 text-sm text-green-700 dark:text-green-400">
          <CheckCircle className="h-4 w-4 flex-shrink-0" />
          {t('recoveryBundleDownloadSuccess')}
        </div>
      )}
      {error && (
        <div className="mt-3 flex items-center gap-2 text-sm text-red-700 dark:text-red-400">
          <AlertCircle className="h-4 w-4 flex-shrink-0" />
          {error}
        </div>
      )}
    </div>
  );
}
