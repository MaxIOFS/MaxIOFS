import React from 'react';
import { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface MetricCardProps {
  title: string;
  value: string | number;
  icon?: LucideIcon;
  trend?: {
    value: number;
    isPositive: boolean;
  };
  description?: string;
  color?: 'brand' | 'success' | 'error' | 'warning' | 'blue-light';
  className?: string;
  /** Compact mode: reduced padding, smaller text and icon. Use on sub-pages. */
  compact?: boolean;
}

export function MetricCard({
  title,
  value,
  icon: Icon,
  trend,
  description,
  color = 'brand',
  className,
  compact = false,
}: MetricCardProps) {
  const colorClasses = {
    brand: {
      icon: 'text-brand-600 bg-brand-50 dark:bg-brand-900/20',
      gradient: 'from-brand-500/10 to-brand-600/5',
      glow: 'shadow-soft-md hover:shadow-soft-lg',
    },
    success: {
      icon: 'text-success-600 bg-success-50 dark:bg-success-900/20',
      gradient: 'from-success-500/10 to-success-600/5',
      glow: 'shadow-soft-md hover:shadow-soft-lg',
    },
    error: {
      icon: 'text-error-600 bg-error-50 dark:bg-error-900/20',
      gradient: 'from-error-500/10 to-error-600/5',
      glow: 'shadow-soft-md hover:shadow-soft-lg',
    },
    warning: {
      icon: 'text-warning-600 bg-warning-50 dark:bg-warning-900/20',
      gradient: 'from-warning-500/10 to-warning-600/5',
      glow: 'shadow-soft-md hover:shadow-soft-lg',
    },
    'blue-light': {
      icon: 'text-blue-light-600 bg-blue-light-50 dark:bg-blue-light-900/20',
      gradient: 'from-blue-light-500/10 to-blue-light-600/5',
      glow: 'shadow-soft-md hover:shadow-soft-lg',
    },
  };

  const colors = colorClasses[color];

  return (
    <div
      className={cn(
        'relative overflow-hidden rounded-card bg-card border border-border',
        'transition-all duration-300',
        colors.glow,
        compact ? 'p-4' : 'p-6',
        className
      )}
    >
      {/* Background Gradient */}
      <div className={cn('absolute inset-0 bg-gradient-to-br opacity-50', colors.gradient)} />

      {/* Content */}
      <div className="relative">
        <div className="flex items-start justify-between">
          <div className="flex-1 min-w-0">
            <p className={cn('font-medium text-muted-foreground', compact ? 'text-xs mb-1' : 'text-sm mb-2')}>
              {title}
            </p>
            <p className={cn('font-bold text-foreground truncate', compact ? 'text-xl mb-0.5' : 'text-3xl mb-1')}>
              {value}
            </p>

            {description && (
              <p className={cn('text-xs text-muted-foreground truncate', compact ? 'mt-1' : 'mt-2')}>
                {description}
              </p>
            )}

            {trend && (
              <div className={cn('flex items-center gap-1', compact ? 'mt-1' : 'mt-2')}>
                <span
                  className={cn(
                    'text-xs font-medium',
                    trend.isPositive ? 'text-success-600' : 'text-error-600'
                  )}
                >
                  {trend.isPositive ? '+' : '-'}{Math.abs(trend.value)}%
                </span>
                <span className="text-xs text-muted-foreground">vs last period</span>
              </div>
            )}
          </div>

          {Icon && (
            <div className={cn('shrink-0 rounded-button', colors.icon, compact ? 'p-2 ml-3' : 'p-3 ml-4')}>
              <Icon className={compact ? 'h-4 w-4' : 'h-6 w-6'} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
