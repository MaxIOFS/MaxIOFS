import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { APIClient } from '@/lib/api';
import { Bucket, IntegrityResult } from '@/types';
import {
  AlertTriangle,
  CheckCircle2,
  Clock,
  FileX,
  ShieldAlert,
  ShieldCheck,
  SkipForward,
  XCircle,
} from 'lucide-react';

type Phase = 'idle' | 'running' | 'done' | 'error';

interface Totals {
  checked: number;
  ok: number;
  corrupted: number;
  skipped: number;
  errors: number;
  issues: IntegrityResult[];
  duration: string;
}

interface Props {
  isOpen: boolean;
  onClose: () => void;
  bucket: Bucket;
}

export function BucketIntegrityModal({ isOpen, onClose, bucket }: Props) {
  const [phase, setPhase] = useState<Phase>('idle');
  const [totals, setTotals] = useState<Totals>({
    checked: 0,
    ok: 0,
    corrupted: 0,
    skipped: 0,
    errors: 0,
    issues: [],
    duration: '',
  });
  const [errorMessage, setErrorMessage] = useState('');
  const abortRef = useRef(false);
  const startTimeRef = useRef(0);

  // Reset state when modal opens
  useEffect(() => {
    if (isOpen) {
      abortRef.current = false;
      setPhase('idle');
      setTotals({ checked: 0, ok: 0, corrupted: 0, skipped: 0, errors: 0, issues: [], duration: '' });
      setErrorMessage('');
    }
  }, [isOpen]);

  // Abort loop when modal closes while running
  useEffect(() => {
    if (!isOpen) {
      abortRef.current = true;
    }
  }, [isOpen]);

  const runScan = useCallback(async () => {
    abortRef.current = false;
    setPhase('running');
    setTotals({ checked: 0, ok: 0, corrupted: 0, skipped: 0, errors: 0, issues: [], duration: '' });
    startTimeRef.current = Date.now();

    const tenantId = bucket.tenant_id || bucket.tenantId;
    let marker = '';
    let acc: Totals = { checked: 0, ok: 0, corrupted: 0, skipped: 0, errors: 0, issues: [], duration: '' };

    try {
      do {
        if (abortRef.current) return;

        const report = await APIClient.verifyBucketIntegrity(bucket.name, {
          marker,
          maxKeys: 500,
          tenantId,
        });

        acc = {
          checked:   acc.checked   + report.checked,
          ok:        acc.ok        + report.ok,
          corrupted: acc.corrupted + report.corrupted,
          skipped:   acc.skipped   + report.skipped,
          errors:    acc.errors    + report.errors,
          issues:    [...acc.issues, ...(report.issues ?? [])],
          duration:  '',
        };

        setTotals({ ...acc });

        marker = report.nextMarker ?? '';
      } while (marker !== '');
    } catch (err: any) {
      if (abortRef.current) return;
      setErrorMessage(err?.response?.data?.error ?? err?.message ?? 'Unknown error');
      setPhase('error');
      return;
    }

    if (abortRef.current) return;

    const elapsed = ((Date.now() - startTimeRef.current) / 1000).toFixed(2) + 's';
    setTotals(prev => ({ ...prev, duration: elapsed }));
    setPhase('done');
  }, [bucket]);

  const objectCount = bucket.object_count ?? bucket.objectCount ?? 0;
  const progressPct = objectCount > 0
    ? Math.min(100, Math.round((totals.checked / objectCount) * 100))
    : (phase === 'running' ? null : 100); // null = indeterminate

  const handleClose = () => {
    abortRef.current = true;
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Verify Bucket Integrity"
      description={`Bucket: ${bucket.name}`}
      size="xl"
      closeOnOverlay={phase !== 'running'}
      closeOnEscape={phase !== 'running'}
    >
      <div className="space-y-6">

        {/* ── Idle: start button ── */}
        {phase === 'idle' && (
          <div className="text-center space-y-4 py-4">
            <div className="mx-auto flex items-center justify-center w-16 h-16 rounded-full bg-gradient-to-br from-brand-100 to-blue-100 dark:from-brand-900/40 dark:to-blue-900/40">
              <ShieldCheck className="h-8 w-8 text-brand-600 dark:text-brand-400" />
            </div>
            <div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                Ready to scan
              </h3>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                Each object's MD5 will be recomputed from disk and compared against its stored ETag.
                {objectCount > 0 && (
                  <> This bucket has <span className="font-semibold text-gray-700 dark:text-gray-200">{objectCount.toLocaleString()}</span> object{objectCount !== 1 ? 's' : ''}.</>
                )}
              </p>
            </div>
            <Button
              onClick={runScan}
              className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md"
            >
              <ShieldCheck className="h-4 w-4 mr-2" />
              Start Verification
            </Button>
          </div>
        )}

        {/* ── Running ── */}
        {phase === 'running' && (
          <div className="space-y-5">
            {/* Progress bar */}
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium text-gray-700 dark:text-gray-300 flex items-center gap-2">
                  <Clock className="h-4 w-4 text-brand-500 animate-pulse" />
                  Scanning…
                </span>
                <span className="text-gray-500 dark:text-gray-400">
                  {totals.checked.toLocaleString()} / {objectCount > 0 ? objectCount.toLocaleString() : '?'} objects
                </span>
              </div>
              <div className="h-3 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                {progressPct !== null ? (
                  <div
                    className="h-full bg-gradient-to-r from-brand-500 to-blue-500 rounded-full transition-all duration-300"
                    style={{ width: `${progressPct}%` }}
                  />
                ) : (
                  // Indeterminate stripe animation when total is unknown
                  <div className="h-full w-full bg-gradient-to-r from-brand-400 via-blue-400 to-brand-400 rounded-full animate-pulse" />
                )}
              </div>
              {progressPct !== null && (
                <p className="text-xs text-right text-gray-400 dark:text-gray-500">{progressPct}%</p>
              )}
            </div>

            {/* Live counters */}
            <LiveCounters totals={totals} />

            {/* Live issues (rolling) */}
            {totals.issues.length > 0 && (
              <IssueTable issues={totals.issues} />
            )}

            <div className="flex justify-end">
              <Button variant="outline" onClick={handleClose}>
                Cancel
              </Button>
            </div>
          </div>
        )}

        {/* ── Done ── */}
        {phase === 'done' && (
          <div className="space-y-5">
            {/* Summary banner */}
            {totals.corrupted === 0 && totals.errors === 0 ? (
              <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-success-50 to-green-50 dark:from-success-900/20 dark:to-green-900/20 border border-success-200 dark:border-success-800">
                <CheckCircle2 className="h-8 w-8 text-success-600 dark:text-success-400 shrink-0" />
                <div>
                  <p className="font-semibold text-success-800 dark:text-success-300">
                    All objects verified — no corruption found
                  </p>
                  <p className="text-sm text-success-700 dark:text-success-400 mt-0.5">
                    {totals.checked.toLocaleString()} object{totals.checked !== 1 ? 's' : ''} checked in {totals.duration}
                  </p>
                </div>
              </div>
            ) : (
              <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-error-50 to-red-50 dark:from-error-900/20 dark:to-red-900/20 border border-error-200 dark:border-error-800">
                <ShieldAlert className="h-8 w-8 text-error-600 dark:text-error-400 shrink-0" />
                <div>
                  <p className="font-semibold text-error-800 dark:text-error-300">
                    {totals.corrupted} corrupted / missing object{totals.corrupted !== 1 ? 's' : ''} detected
                  </p>
                  <p className="text-sm text-error-700 dark:text-error-400 mt-0.5">
                    {totals.checked.toLocaleString()} checked in {totals.duration}
                    {totals.errors > 0 && ` · ${totals.errors} read error${totals.errors !== 1 ? 's' : ''}`}
                  </p>
                </div>
              </div>
            )}

            {/* Full counters */}
            <LiveCounters totals={totals} />

            {/* Issues table */}
            {totals.issues.length > 0 && (
              <IssueTable issues={totals.issues} />
            )}

            <div className="flex justify-between items-center">
              <Button variant="outline" onClick={runScan}>
                <ShieldCheck className="h-4 w-4 mr-2" />
                Scan again
              </Button>
              <Button
                onClick={handleClose}
                className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white"
              >
                Close
              </Button>
            </div>
          </div>
        )}

        {/* ── Error ── */}
        {phase === 'error' && (
          <div className="space-y-5">
            <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-error-50 to-red-50 dark:from-error-900/20 dark:to-red-900/20 border border-error-200 dark:border-error-800">
              <XCircle className="h-8 w-8 text-error-600 dark:text-error-400 shrink-0" />
              <div>
                <p className="font-semibold text-error-800 dark:text-error-300">Verification failed</p>
                <p className="text-sm text-error-700 dark:text-error-400 mt-0.5">{errorMessage}</p>
              </div>
            </div>
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={handleClose}>Close</Button>
              <Button onClick={runScan} className="bg-gradient-to-r from-brand-600 to-brand-700 text-white">
                Retry
              </Button>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

// ── Sub-components ────────────────────────────────────────────────────────────

function LiveCounters({ totals }: { totals: Totals }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <CounterCard
        label="Checked"
        value={totals.checked}
        icon={<ShieldCheck className="h-4 w-4" />}
        color="brand"
      />
      <CounterCard
        label="OK"
        value={totals.ok}
        icon={<CheckCircle2 className="h-4 w-4" />}
        color="success"
      />
      <CounterCard
        label="Corrupted / Missing"
        value={totals.corrupted}
        icon={<AlertTriangle className="h-4 w-4" />}
        color={totals.corrupted > 0 ? 'error' : 'gray'}
      />
      <CounterCard
        label="Skipped"
        value={totals.skipped}
        icon={<SkipForward className="h-4 w-4" />}
        color="gray"
      />
    </div>
  );
}

type CounterColor = 'brand' | 'success' | 'error' | 'gray';

function CounterCard({
  label, value, icon, color,
}: {
  label: string; value: number; icon: React.ReactNode; color: CounterColor;
}) {
  const colorMap: Record<CounterColor, string> = {
    brand:   'bg-brand-50 dark:bg-brand-900/20 text-brand-600 dark:text-brand-400 border-brand-200 dark:border-brand-800',
    success: 'bg-success-50 dark:bg-success-900/20 text-success-600 dark:text-success-400 border-success-200 dark:border-success-800',
    error:   'bg-error-50 dark:bg-error-900/20 text-error-600 dark:text-error-400 border-error-200 dark:border-error-800',
    gray:    'bg-gray-50 dark:bg-gray-800 text-gray-500 dark:text-gray-400 border-gray-200 dark:border-gray-700',
  };

  return (
    <div className={`flex items-center gap-3 p-3 rounded-lg border ${colorMap[color]}`}>
      <span className="shrink-0">{icon}</span>
      <div className="min-w-0">
        <p className="text-lg font-bold leading-none">{value.toLocaleString()}</p>
        <p className="text-xs mt-1 opacity-75 truncate">{label}</p>
      </div>
    </div>
  );
}

function IssueTable({ issues }: { issues: IntegrityResult[] }) {
  const statusIcon = (s: IntegrityResult['status']) => {
    if (s === 'corrupted') return <AlertTriangle className="h-4 w-4 text-error-500" />;
    if (s === 'missing')   return <FileX          className="h-4 w-4 text-warning-500" />;
    return                        <XCircle        className="h-4 w-4 text-gray-400" />;
  };

  const statusLabel = (s: IntegrityResult['status']) => {
    const map: Record<string, string> = {
      corrupted: 'Corrupted',
      missing:   'Missing',
      error:     'Error',
      skipped:   'Skipped',
      ok:        'OK',
    };
    return map[s] ?? s;
  };

  return (
    <div>
      <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">
        Issues found ({issues.length})
      </h4>
      <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
        <div className="max-h-64 overflow-y-auto">
          <table className="w-full text-xs">
            <thead className="bg-gray-50 dark:bg-gray-800 sticky top-0">
              <tr>
                <th className="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400">Key</th>
                <th className="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400 whitespace-nowrap">Status</th>
                <th className="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400 whitespace-nowrap">Stored ETag</th>
                <th className="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-400 whitespace-nowrap">Actual ETag</th>
              </tr>
            </thead>
            <tbody>
              {issues.map((issue, idx) => (
                <tr
                  key={idx}
                  className="border-t border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50"
                >
                  <td className="px-3 py-2 font-mono text-gray-800 dark:text-gray-200 max-w-xs truncate" title={issue.key}>
                    {issue.key}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    <span className="flex items-center gap-1.5">
                      {statusIcon(issue.status)}
                      {statusLabel(issue.status)}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-mono text-gray-500 dark:text-gray-400 max-w-[120px] truncate" title={issue.storedETag}>
                    {issue.storedETag || '—'}
                  </td>
                  <td className="px-3 py-2 font-mono text-gray-500 dark:text-gray-400 max-w-[120px] truncate" title={issue.computedETag}>
                    {issue.computedETag || (issue.status === 'missing' ? '(missing)' : '—')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
