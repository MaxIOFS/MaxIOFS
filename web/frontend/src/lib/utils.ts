import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatBytes(bytes: number, decimals = 2): string {
  if (bytes === 0) return '0 Bytes';

  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB', 'ZB', 'YB'];

  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

export function formatDate(date: string | Date | number, options?: Intl.DateTimeFormatOptions): string {
  const dateObj = typeof date === 'string' 
    ? new Date(date) 
    : typeof date === 'number'
    ? new Date(date * 1000) // Assume Unix timestamp
    : date;

  const defaultOptions: Intl.DateTimeFormatOptions = {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  };

  return dateObj.toLocaleDateString('en-US', { ...defaultOptions, ...options });
}

export function formatRelativeTime(date: string | Date): string {
  const dateObj = typeof date === 'string' ? new Date(date) : date;
  const now = new Date();
  const diffInMs = now.getTime() - dateObj.getTime();
  const diffInMinutes = Math.floor(diffInMs / (1000 * 60));
  const diffInHours = Math.floor(diffInMs / (1000 * 60 * 60));
  const diffInDays = Math.floor(diffInMs / (1000 * 60 * 60 * 24));

  if (diffInMinutes < 1) return 'Just now';
  if (diffInMinutes < 60) return `${diffInMinutes} minute${diffInMinutes > 1 ? 's' : ''} ago`;
  if (diffInHours < 24) return `${diffInHours} hour${diffInHours > 1 ? 's' : ''} ago`;
  if (diffInDays < 7) return `${diffInDays} day${diffInDays > 1 ? 's' : ''} ago`;

  return formatDate(dateObj);
}

// ==================== Error Helpers ====================

/**
 * Type-safe error message extraction from unknown catch values.
 * Handles Axios errors (response.data.error), APIError (message), and plain Error objects.
 */
export function getErrorMessage(err: unknown, fallback = 'An unexpected error occurred'): string {
  if (typeof err === 'string') return err;
  if (err instanceof Error) return err.message;
  if (isErrorWithResponse(err)) {
    return err.response?.data?.error || err.response?.data?.Error || err.message || fallback;
  }
  if (isErrorWithMessage(err)) return err.message;
  return fallback;
}

/** Extract HTTP status code from an Axios-like error */
export function getErrorStatus(err: unknown): number | undefined {
  if (isErrorWithResponse(err)) return err.response?.status;
  return undefined;
}

/** Check if an error has a specific HTTP status code */
export function isHttpStatus(err: unknown, status: number): boolean {
  return getErrorStatus(err) === status;
}

// Type guards

function isErrorWithMessage(err: unknown): err is { message: string } {
  return (
    typeof err === 'object' &&
    err !== null &&
    'message' in err &&
    typeof (err as { message: unknown }).message === 'string'
  );
}

export function isErrorWithResponse(err: unknown): err is {
  message?: string;
  response?: { status?: number; data?: { error?: string; Error?: string; sso_hint?: boolean } };
} {
  return typeof err === 'object' && err !== null && 'response' in err;
}