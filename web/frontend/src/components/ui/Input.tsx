import React, { forwardRef } from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '@/lib/utils';

const inputVariants = cva(
  'flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
  {
    variants: {
      variant: {
        default: '',
        error: 'border-destructive focus-visible:ring-destructive',
        success: 'border-green-500 focus-visible:ring-green-500',
      },
      size: {
        default: 'h-10',
        sm: 'h-9',
        lg: 'h-11',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
);

export interface InputProps
  extends React.InputHTMLAttributes<HTMLInputElement>,
    VariantProps<typeof inputVariants> {
  label?: string;
  error?: string;
  helpText?: string;
  leftIcon?: React.ReactNode;
  rightIcon?: React.ReactNode;
  rightElement?: React.ReactNode;
}

const Input = forwardRef<HTMLInputElement, InputProps>(
  ({
    className,
    variant,
    size,
    type = 'text',
    label,
    error,
    helpText,
    leftIcon,
    rightIcon,
    rightElement,
    id,
    ...props
  }, ref) => {
    const inputId = id || `input-${Math.random().toString(36).substr(2, 9)}`;
    const hasError = !!error;
    const inputVariant = hasError ? 'error' : variant;

    return (
      <div className="space-y-2">
        {label && (
          <label
            htmlFor={inputId}
            className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
          >
            {label}
          </label>
        )}

        <div className="relative">
          {leftIcon && (
            <div className="absolute left-3 top-1/2 transform -translate-y-1/2 text-muted-foreground">
              {leftIcon}
            </div>
          )}

          <input
            type={type}
            className={cn(
              inputVariants({ variant: inputVariant, size, className }),
              leftIcon && 'pl-10',
              (rightIcon || rightElement) && 'pr-10'
            )}
            ref={ref}
            id={inputId}
            {...props}
          />

          {(rightIcon || rightElement) && (
            <div className="absolute right-3 top-1/2 transform -translate-y-1/2">
              {rightElement || (
                <div className="text-muted-foreground">
                  {rightIcon}
                </div>
              )}
            </div>
          )}
        </div>

        {(error || helpText) && (
          <p className={cn(
            'text-sm',
            hasError ? 'text-destructive' : 'text-muted-foreground'
          )}>
            {error || helpText}
          </p>
        )}
      </div>
    );
  }
);

Input.displayName = 'Input';

export { Input, inputVariants };