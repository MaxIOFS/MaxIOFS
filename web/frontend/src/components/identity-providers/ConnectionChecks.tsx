import { CheckCircle2, AlertTriangle, XCircle } from 'lucide-react';
import type { IDPConnectionCheck } from '@/types';

const outcomes = {
  passed: { Icon: CheckCircle2, color: 'text-success-600 dark:text-success-400' },
  warning: { Icon: AlertTriangle, color: 'text-warning-600 dark:text-warning-400' },
  failed: { Icon: XCircle, color: 'text-error-600 dark:text-error-400' },
};

// The steps of a provider's connection test, each with how it went and what
// it found.
export function ConnectionChecks({ checks }: { checks: IDPConnectionCheck[] }) {
  return (
    <ul className="space-y-3 text-left">
      {checks.map((check) => {
        const { Icon, color } = outcomes[check.status] ?? outcomes.failed;
        return (
          <li key={check.name} className="flex gap-2" data-status={check.status}>
            <Icon className={`h-4 w-4 mt-0.5 shrink-0 ${color}`} aria-label={check.status} />
            <div className="min-w-0">
              <p className="font-medium text-foreground">{check.name}</p>
              <p className="break-words">{check.detail}</p>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
