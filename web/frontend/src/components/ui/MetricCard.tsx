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
}

export function MetricCard({
  title,
  value,
  icon: Icon,
  trend,
  description,
  color = 'brand',
  className,
}: MetricCardProps) {
  const colorClasses = {
    brand: {
      icon: 'text-brand-600 bg-brand-50 dark:bg-brand-900/20',
      gradient: 'from-brand-500/10 to-brand-600/5',
      glow: 'shadow-glow-sm hover:shadow-glow',
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
        'relative overflow-hidden rounded-card bg-card border border-border p-6',
        'transition-all duration-300',
        colors.glow,
        className
      )}
    >
      {/* Background Gradient */}
      <div className={cn('absolute inset-0 bg-gradient-to-br opacity-50', colors.gradient)} />

      {/* Content */}
      <div className="relative">
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <p className="text-sm font-medium text-muted-foreground mb-2">{title}</p>
            <p className="text-3xl font-bold text-foreground mb-1">{value}</p>

            {description && (
              <p className="text-xs text-muted-foreground mt-2">{description}</p>
            )}

            {trend && (
              <div className="flex items-center gap-1 mt-2">
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
            <div className={cn('p-3 rounded-button', colors.icon)}>
              <Icon className="h-6 w-6" />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
