import { useTranslation } from 'react-i18next';
import { RefreshCw, PauseCircle, CheckCircle, Circle, AlertCircle, Play } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Button } from '@/components/ui/Button';

// EncryptionWorkerStatus shows the progress of the background worker that
// converts pre-existing plaintext objects to envelope encryption. Rendered in
// the Security settings tab, below the recovery bundle section.
export default function EncryptionWorkerStatus() {
  const { t } = useTranslation('settings');
  const queryClient = useQueryClient();

  const { data: status } = useQuery({
    queryKey: ['encryptionWorkerStatus'],
    queryFn: () => APIClient.getEncryptionWorkerStatus(),
    refetchInterval: (query) => {
      // Poll faster while a pass is active.
      const s = query.state.data?.status;
      return s === 'running' || s === 'waiting_load' ? 10000 : 60000;
    },
  });

  const runMutation = useMutation({
    mutationFn: () => APIClient.runEncryptionWorker(),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['encryptionWorkerStatus'] });
    },
  });

  if (!status) return null;

  const badge = (() => {
    switch (status.status) {
      case 'running':
        return {
          icon: <RefreshCw className="h-3 w-3 animate-spin" />,
          text: t('encWorkerRunning'),
          cls: 'bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-300',
        };
      case 'waiting_load':
        return {
          icon: <PauseCircle className="h-3 w-3" />,
          text: t('encWorkerWaitingLoad'),
          cls: 'bg-amber-100 dark:bg-amber-900/30 text-amber-800 dark:text-amber-300',
        };
      case 'done':
        return {
          icon: <CheckCircle className="h-3 w-3" />,
          text: t('encWorkerDone'),
          cls: 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300',
        };
      default:
        return {
          icon: <Circle className="h-3 w-3" />,
          text: t('encWorkerIdle'),
          cls: 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-300',
        };
    }
  })();

  const isActive = status.status === 'running' || status.status === 'waiting_load';
  const progressPct = status.bucketsTotal > 0
    ? Math.min(100, Math.round((status.bucketsDone / status.bucketsTotal) * 100))
    : 0;

  return (
    <div className="mb-6 pb-6 border-b border-border">
      <div className="flex items-center gap-2 mb-1">
        <RefreshCw className="h-4 w-4 text-brand-600 dark:text-brand-400" />
        <h4 className="text-sm font-semibold text-foreground">{t('encWorkerTitle')}</h4>
        <span className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium ${badge.cls}`}>
          {badge.icon}
          {badge.text}
        </span>
        {!isActive && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => runMutation.mutate()}
            disabled={runMutation.isPending}
            className="ml-auto"
          >
            <Play className="h-3 w-3" />
            {t('encWorkerRunNow')}
          </Button>
        )}
      </div>
      <p className="text-xs text-muted-foreground leading-relaxed mb-3">
        {t('encWorkerDesc')}
      </p>

      {isActive && (
        <div className="mb-3">
          <div className="flex justify-between text-xs text-muted-foreground mb-1">
            <span>
              {status.currentBucket
                ? t('encWorkerCurrentBucket', { bucket: status.currentBucket })
                : t('encWorkerScanning')}
            </span>
            <span>{t('encWorkerBucketsProgress', { done: status.bucketsDone, total: status.bucketsTotal })}</span>
          </div>
          <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-2">
            <div
              className="bg-blue-600 h-2 rounded-full transition-all"
              style={{ width: `${progressPct}%` }}
            />
          </div>
        </div>
      )}

      <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
        <span>{t('encWorkerConverted', { count: status.converted })}</span>
        <span>{t('encWorkerSkipped', { count: status.skipped })}</span>
        {status.failed > 0 && (
          <span className="text-red-600 dark:text-red-400">{t('encWorkerFailed', { count: status.failed })}</span>
        )}
        {status.status === 'done' && status.lastRunEnd && (
          <span>{t('encWorkerLastRun', { date: new Date(status.lastRunEnd * 1000).toLocaleString() })}</span>
        )}
      </div>

      {status.lastError && (
        <div className="mt-2 flex items-center gap-2 text-xs text-red-700 dark:text-red-400">
          <AlertCircle className="h-3 w-3 flex-shrink-0" />
          {status.lastError}
        </div>
      )}
    </div>
  );
}
