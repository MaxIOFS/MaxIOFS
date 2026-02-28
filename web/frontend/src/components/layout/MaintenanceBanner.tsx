import React from 'react';
import { AlertTriangle } from 'lucide-react';

interface MaintenanceBannerProps {
  isMaintenanceMode: boolean;
}

export function MaintenanceBanner({ isMaintenanceMode }: MaintenanceBannerProps) {
  if (!isMaintenanceMode) return null;

  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-amber-50 dark:bg-amber-950/40 border-b border-amber-200 dark:border-amber-800">
      <AlertTriangle className="h-4 w-4 flex-shrink-0 text-amber-600 dark:text-amber-400" />
      <p className="text-sm font-medium text-amber-800 dark:text-amber-300">
        Server is in maintenance mode — write operations are temporarily disabled.
      </p>
    </div>
  );
}
