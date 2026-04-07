import { useEffect, useRef, useCallback } from 'react';

const LAST_ACTIVITY_KEY = 'last_activity';

interface UseIdleTimerOptions {
  timeout: number; // Timeout in milliseconds
  onIdle: () => void; // Callback when user becomes idle
  events?: string[]; // Events to track for activity
  isBlocked?: () => boolean; // If returns true, postpone logout instead of firing onIdle
}

/**
 * Hook to detect user inactivity and trigger a callback after a specified timeout.
 *
 * Last-activity timestamp is written to localStorage on every event so that
 * tab-close / page-reload are included in the inactivity window (the calling
 * code is responsible for checking the stored timestamp on mount).
 */
export function useIdleTimer({
  timeout,
  onIdle,
  events = ['mousedown', 'mousemove', 'keydown', 'scroll', 'touchstart', 'click'],
  isBlocked,
}: UseIdleTimerOptions) {
  const timeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Keep latest callbacks in refs so setTimeout closures never go stale
  const onIdleRef = useRef(onIdle);
  const isBlockedRef = useRef(isBlocked);
  const resetTimerRef = useRef<() => void>(() => {});

  useEffect(() => { onIdleRef.current = onIdle; }, [onIdle]);
  useEffect(() => { isBlockedRef.current = isBlocked; }, [isBlocked]);

  const resetTimer = useCallback(() => {
    if (timeoutId.current) {
      clearTimeout(timeoutId.current);
    }

    // Persist the timestamp so other tabs and page reloads know the user was active
    try { localStorage.setItem(LAST_ACTIVITY_KEY, String(Date.now())); } catch { /* storage unavailable */ }

    timeoutId.current = setTimeout(() => {
      // If blocked (e.g. active upload in progress), postpone instead of firing
      if (isBlockedRef.current?.()) {
        resetTimerRef.current();
        return;
      }
      onIdleRef.current();
    }, timeout);
  }, [timeout]);

  // Keep resetTimerRef in sync
  useEffect(() => { resetTimerRef.current = resetTimer; }, [resetTimer]);

  useEffect(() => {
    // Start the timer immediately
    resetTimer();

    events.forEach((event) => {
      window.addEventListener(event, resetTimer, { passive: true });
    });

    // Also reset when a background token refresh succeeds — this keeps the
    // idle timer in sync with the proactive refresh so the session isn't
    // killed right after the token was silently renewed.
    window.addEventListener('session-keep-alive', resetTimer);

    return () => {
      if (timeoutId.current) {
        clearTimeout(timeoutId.current);
      }
      events.forEach((event) => {
        window.removeEventListener(event, resetTimer);
      });
      window.removeEventListener('session-keep-alive', resetTimer);
    };
  }, [resetTimer, events]);

  return { resetTimer };
}

/** Returns the timestamp of the last recorded user activity (ms since epoch), or 0 if unknown. */
export function getLastActivityTimestamp(): number {
  try {
    return parseInt(localStorage.getItem(LAST_ACTIVITY_KEY) ?? '0', 10) || 0;
  } catch {
    return 0;
  }
}

/** Clears the stored last-activity timestamp (call on logout). */
export function clearLastActivityTimestamp(): void {
  try { localStorage.removeItem(LAST_ACTIVITY_KEY); } catch { /* ignore */ }
}
