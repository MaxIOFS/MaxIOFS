import React, { useState } from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Bucket, IntegrityResult, LastIntegrityScan } from '@/types';
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  EyeOff,
  FileX,
  ShieldAlert,
  ShieldCheck,
  SkipForward,
  Timer,
  XCircle,
} from 'lucide-react';

// ── Exported types ─────────────────────────────────────────────────────────────

export type ScanPhase = 'running' | 'done' | 'error';

export interface BucketScanState {
  phase: ScanPhase;
  checked: number;
  ok: number;
  corrupted: number;
  skipped: number;
  errors: number;
  issues: IntegrityResult[];
  duration: string;
  startedAt: Date;
  finishedAt?: Date;
  errorMessage?: string;
  /** 'manual' = triggered by user, 'scrubber' = background auto-scan */
  source?: 'manual' | 'scrubber';
}

// ── Component props ────────────────────────────────────────────────────────────

interface Props {
  isOpen: boolean;
  bucket: Bucket;
  objectCount: number;
  /** Current scan state, or null if never scanned this session */
  scanState: BucketScanState | null;
  /** Full scan history from the server (newest first) */
  history: LastIntegrityScan[];
  /** If set, manual scans are rate-limited until this date */
  rateLimitUntil: Date | null;
  /** Whether the current user can start/retry scans (global admin only) */
  canRunScan?: boolean;
  onStart: () => void;
  onCancel: () => void;
  /** Close the modal WITHOUT stopping the scan */
  onHide: () => void;
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function formatRemaining(until: Date): string {
  const ms = until.getTime() - Date.now();
  if (ms <= 0) return '0s';
  const totalSecs = Math.ceil(ms / 1000);
  const mins = Math.floor(totalSecs / 60);
  const secs = totalSecs % 60;
  return mins > 0 ? `${mins}m ${secs}s` : `${secs}s`;
}

// ── Main component ─────────────────────────────────────────────────────────────

export function BucketIntegrityModal({
  isOpen,
  bucket,
  objectCount,
  scanState,
  history,
  rateLimitUntil,
  canRunScan = false,
  onStart,
  onCancel,
  onHide,
}: Props) {
  const phase = scanState?.phase ?? null;
  const isRateLimited = rateLimitUntil !== null && rateLimitUntil > new Date();

  const progressPct = objectCount > 0 && scanState
    ? Math.min(100, Math.round((scanState.checked / objectCount) * 100))
    : null;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onHide}
      title="Verify Bucket Integrity"
      description={`Bucket: ${bucket.name}`}
      size="xl"
      closeOnOverlay
      closeOnEscape
    >
      <div className="space-y-6">

        {/* ── Idle: never scanned ── */}
        {phase === null && (
          <div className="text-center space-y-4 py-4">
            <div className="mx-auto flex items-center justify-center w-16 h-16 rounded-full bg-gradient-to-br from-brand-100 to-blue-100 dark:from-brand-900/40 dark:to-blue-900/40">
              <ShieldCheck className="h-8 w-8 text-brand-600 dark:text-brand-400" />
            </div>
            <div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                {canRunScan ? 'Ready to scan' : 'No scan results yet'}
              </h3>
              <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {canRunScan
                  ? <>Each object's MD5 is recomputed from disk and compared against its stored ETag.
                      {objectCount > 0 && (
                        <> This bucket has{' '}
                          <span className="font-semibold text-gray-700 dark:text-gray-200">
                            {objectCount.toLocaleString()}
                          </span>{' '}
                          object{objectCount !== 1 ? 's' : ''}.
                        </>
                      )}
                    </>
                  : 'Integrity verification has not been run for this bucket yet. Contact a global administrator to run a scan.'
                }
              </p>
            </div>

            {canRunScan && (
              isRateLimited ? (
                <RateLimitBanner until={rateLimitUntil!} />
              ) : (
                <Button
                  onClick={onStart}
                  className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white shadow-md"
                >
                  <ShieldCheck className="h-4 w-4 mr-2" />
                  Start Verification
                </Button>
              )
            )}

            {history.length > 0 && <ScanHistory history={history} />}

            {!canRunScan && (
              <div className="flex justify-end pt-2 border-t border-gray-100 dark:border-gray-800">
                <Button onClick={onHide}>Close</Button>
              </div>
            )}
          </div>
        )}

        {/* ── Running ── */}
        {phase === 'running' && scanState && (
          <div className="space-y-5">
            {/* Progress bar */}
            <div className="space-y-2">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium text-gray-700 dark:text-gray-300 flex items-center gap-2">
                  <Clock className="h-4 w-4 text-amber-500 animate-pulse" />
                  Scanning in progress…
                </span>
                <span className="text-gray-500 dark:text-gray-400">
                  {scanState.checked.toLocaleString()} / {objectCount > 0 ? objectCount.toLocaleString() : '?'} objects
                </span>
              </div>
              <div className="h-3 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                {progressPct !== null ? (
                  <div
                    className="h-full bg-gradient-to-r from-amber-400 to-brand-500 rounded-full transition-all duration-500"
                    style={{ width: `${progressPct}%` }}
                  />
                ) : (
                  <div className="h-full w-1/3 bg-gradient-to-r from-amber-400 to-brand-500 rounded-full animate-[scan_1.5s_ease-in-out_infinite]" />
                )}
              </div>
              <div className="flex items-center justify-between text-xs text-gray-400 dark:text-gray-500">
                <span>Started {scanState.startedAt.toLocaleTimeString()}</span>
                {progressPct !== null && <span>{progressPct}% complete</span>}
              </div>
            </div>

            <LiveCounters scanState={scanState} />

            {scanState.issues.length > 0 && <IssueTable issues={scanState.issues} />}

            <div className="flex items-center justify-between pt-1 border-t border-gray-100 dark:border-gray-800">
              <p className="text-xs text-gray-400 dark:text-gray-500 flex items-center gap-1.5">
                <EyeOff className="h-3.5 w-3.5" />
                Closing the window does <strong>not</strong> stop the scan
              </p>
              <div className="flex gap-2">
                <Button variant="outline" onClick={onHide}>
                  <EyeOff className="h-4 w-4 mr-1.5" />
                  Hide
                </Button>
                <Button
                  variant="destructive"
                  onClick={onCancel}
                  className="bg-error-600 hover:bg-error-700 text-white"
                >
                  <XCircle className="h-4 w-4 mr-1.5" />
                  Cancel scan
                </Button>
              </div>
            </div>
          </div>
        )}

        {/* ── Done ── */}
        {phase === 'done' && scanState && (
          <div className="space-y-5">
            {/* Source label for background scrubber results */}
            {scanState.source === 'scrubber' && (
              <p className="text-xs text-gray-500 dark:text-gray-400 flex items-center gap-1.5">
                <Clock className="h-3.5 w-3.5" />
                Background scrubber · scanned {scanState.finishedAt?.toLocaleString() ?? 'recently'}
              </p>
            )}

            {scanState.corrupted === 0 && scanState.errors === 0 ? (
              <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-success-50 to-green-50 dark:from-success-900/20 dark:to-green-900/20 border border-success-200 dark:border-success-800">
                <CheckCircle2 className="h-8 w-8 text-success-600 dark:text-success-400 shrink-0" />
                <div>
                  <p className="font-semibold text-success-800 dark:text-success-300">
                    All objects verified — no corruption found
                  </p>
                  <p className="text-sm text-success-700 dark:text-success-400 mt-0.5">
                    {scanState.checked.toLocaleString()} object{scanState.checked !== 1 ? 's' : ''} checked
                    {scanState.skipped > 0 && ` · ${scanState.skipped} skipped`}
                    {' · '}completed in {scanState.duration}
                  </p>
                </div>
              </div>
            ) : (
              <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-error-50 to-red-50 dark:from-error-900/20 dark:to-red-900/20 border border-error-200 dark:border-error-800">
                <ShieldAlert className="h-8 w-8 text-error-600 dark:text-error-400 shrink-0" />
                <div>
                  <p className="font-semibold text-error-800 dark:text-error-300">
                    {scanState.corrupted} corrupted / missing object{scanState.corrupted !== 1 ? 's' : ''} detected
                  </p>
                  <p className="text-sm text-error-700 dark:text-error-400 mt-0.5">
                    {scanState.checked.toLocaleString()} checked · completed in {scanState.duration}
                    {scanState.errors > 0 && ` · ${scanState.errors} read error${scanState.errors !== 1 ? 's' : ''}`}
                  </p>
                </div>
              </div>
            )}

            <LiveCounters scanState={scanState} />
            {scanState.issues.length > 0 && <IssueTable issues={scanState.issues} />}
            {history.length > 0 && <ScanHistory history={history} />}

            <div className="flex items-center justify-between pt-1 border-t border-gray-100 dark:border-gray-800">
              <div className="flex flex-col gap-1">
                {canRunScan && (
                  isRateLimited ? (
                    <RateLimitBanner until={rateLimitUntil!} compact />
                  ) : (
                    <Button variant="outline" onClick={onStart}>
                      <ShieldCheck className="h-4 w-4 mr-1.5" />
                      Scan again
                    </Button>
                  )
                )}
              </div>
              <Button
                onClick={onHide}
                className="bg-gradient-to-r from-brand-600 to-brand-700 hover:from-brand-700 hover:to-brand-800 text-white"
              >
                Close
              </Button>
            </div>
          </div>
        )}

        {/* ── Error ── */}
        {phase === 'error' && scanState && (
          <div className="space-y-5">
            <div className="flex items-center gap-4 p-4 rounded-xl bg-gradient-to-r from-error-50 to-red-50 dark:from-error-900/20 dark:to-red-900/20 border border-error-200 dark:border-error-800">
              <XCircle className="h-8 w-8 text-error-600 dark:text-error-400 shrink-0" />
              <div>
                <p className="font-semibold text-error-800 dark:text-error-300">Verification failed</p>
                <p className="text-sm text-error-700 dark:text-error-400 mt-0.5">{scanState.errorMessage}</p>
              </div>
            </div>
            {history.length > 0 && <ScanHistory history={history} />}
            <div className="flex justify-end gap-3 pt-1 border-t border-gray-100 dark:border-gray-800">
              <Button variant="outline" onClick={onHide}>Close</Button>
              {canRunScan && !isRateLimited && (
                <Button
                  onClick={onStart}
                  className="bg-gradient-to-r from-brand-600 to-brand-700 text-white"
                >
                  Retry
                </Button>
              )}
              {canRunScan && isRateLimited && <RateLimitBanner until={rateLimitUntil!} compact />}
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

// ── Sub-components ─────────────────────────────────────────────────────────────

function RateLimitBanner({ until, compact }: { until: Date; compact?: boolean }) {
  const [, forceUpdate] = useState(0);
  // Tick every second so the countdown updates.
  React.useEffect(() => {
    const id = setInterval(() => forceUpdate(n => n + 1), 1000);
    return () => clearInterval(id);
  }, []);

  const remaining = formatRemaining(until);

  if (compact) {
    return (
      <p className="text-xs text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
        <Timer className="h-3.5 w-3.5 shrink-0" />
        Rate limited — next scan in {remaining}
      </p>
    );
  }

  return (
    <div className="flex items-center gap-3 px-4 py-3 rounded-xl bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-sm text-amber-800 dark:text-amber-300">
      <Timer className="h-5 w-5 shrink-0 text-amber-500" />
      <div>
        <p className="font-semibold">Rate limit active</p>
        <p className="text-xs mt-0.5">
          To protect storage, manual scans are limited to once per hour.
          Next scan available in <strong>{remaining}</strong>.
        </p>
      </div>
    </div>
  );
}

function LiveCounters({ scanState }: { scanState: BucketScanState }) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <CounterCard
        label="Checked"
        value={scanState.checked}
        icon={<ShieldCheck className="h-4 w-4" />}
        color="brand"
      />
      <CounterCard
        label="OK"
        value={scanState.ok}
        icon={<CheckCircle2 className="h-4 w-4" />}
        color="success"
      />
      <CounterCard
        label="Issues"
        value={scanState.corrupted}
        icon={<AlertTriangle className="h-4 w-4" />}
        color={scanState.corrupted > 0 ? 'error' : 'gray'}
      />
      <CounterCard
        label="Skipped"
        value={scanState.skipped}
        icon={<SkipForward className="h-4 w-4" />}
        color="gray"
      />
    </div>
  );
}

type CounterColor = 'brand' | 'success' | 'error' | 'gray';

function CounterCard({ label, value, icon, color }: {
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
    if (s === 'corrupted') return <AlertTriangle className="h-3.5 w-3.5 text-error-500" />;
    if (s === 'missing')   return <FileX          className="h-3.5 w-3.5 text-warning-500" />;
    return                        <XCircle        className="h-3.5 w-3.5 text-gray-400" />;
  };
  const statusLabel: Record<string, string> = {
    corrupted: 'Corrupted', missing: 'Missing', error: 'Error', skipped: 'Skipped', ok: 'OK',
  };

  return (
    <div>
      <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">
        Issues found ({issues.length})
      </h4>
      <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
        <div className="max-h-56 overflow-y-auto">
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
                <tr key={idx} className="border-t border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/50">
                  <td className="px-3 py-2 font-mono text-gray-800 dark:text-gray-200 max-w-xs truncate" title={issue.key}>
                    {issue.key}
                  </td>
                  <td className="px-3 py-2 whitespace-nowrap">
                    <span className="flex items-center gap-1.5">
                      {statusIcon(issue.status)}
                      {statusLabel[issue.status] ?? issue.status}
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

function ScanHistory({ history }: { history: LastIntegrityScan[] }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
      <button
        onClick={() => setExpanded(e => !e)}
        className="w-full flex items-center justify-between px-4 py-2.5 bg-gray-50 dark:bg-gray-800/60 hover:bg-gray-100 dark:hover:bg-gray-800 text-sm font-medium text-gray-600 dark:text-gray-400 transition-colors"
      >
        <span className="flex items-center gap-2">
          <Clock className="h-3.5 w-3.5" />
          Scan history ({history.length} run{history.length !== 1 ? 's' : ''})
        </span>
        {expanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
      </button>

      {expanded && (
        <div className="divide-y divide-gray-100 dark:divide-gray-800 max-h-48 overflow-y-auto">
          {history.map((entry, idx) => (
            <div key={idx} className="flex items-center justify-between px-4 py-2.5 text-xs">
              <div className="flex items-center gap-2">
                {entry.corrupted === 0 && entry.errors === 0 ? (
                  <CheckCircle2 className="h-3.5 w-3.5 text-success-500 shrink-0" />
                ) : (
                  <ShieldAlert className="h-3.5 w-3.5 text-error-500 shrink-0" />
                )}
                <span className="text-gray-600 dark:text-gray-400">
                  {new Date(entry.scannedAt).toLocaleString()}
                </span>
                <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${
                  entry.source === 'scrubber'
                    ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-400'
                    : 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'
                }`}>
                  {entry.source === 'scrubber' ? 'auto' : 'manual'}
                </span>
              </div>
              <div className="flex items-center gap-3 text-gray-500 dark:text-gray-400">
                <span>{entry.checked.toLocaleString()} objects</span>
                {entry.corrupted > 0
                  ? <span className="text-error-600 dark:text-error-400 font-medium">{entry.corrupted} issue{entry.corrupted !== 1 ? 's' : ''}</span>
                  : <span className="text-success-600 dark:text-success-400">Clean</span>
                }
                <span>{entry.duration}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
