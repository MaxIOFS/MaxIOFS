import React from 'react';
import { LucideIcon } from 'lucide-react';
import { Button } from './Button';

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
  showAction?: boolean;
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  actionLabel,
  onAction,
  showAction = true,
}: EmptyStateProps) {
  return (
    <div className="text-center py-12">
      <Icon className="mx-auto h-12 w-12 text-gray-400 dark:text-gray-600" />
      <h3 className="mt-4 text-lg font-semibold text-gray-900 dark:text-white">
        {title}
      </h3>
      <p className="mt-2 text-sm text-gray-500 dark:text-gray-400 max-w-sm mx-auto">
        {description}
      </p>
      {showAction && actionLabel && onAction && (
        <div className="mt-6">
          <Button onClick={onAction}>
            {actionLabel}
          </Button>
        </div>
      )}
    </div>
  );
}
