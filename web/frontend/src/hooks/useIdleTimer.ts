import { useEffect, useRef, useCallback } from 'react';

interface UseIdleTimerOptions {
  timeout: number; // Timeout in milliseconds
  onIdle: () => void; // Callback when user becomes idle
  events?: string[]; // Events to track for activity
  isBlocked?: () => boolean; // If returns true, postpone logout instead of firing onIdle
}

/**
 * Hook to detect user inactivity and trigger a callback after a specified timeout
 * @param options Configuration options for the idle timer
 */
export function useIdleTimer({
  timeout,
  onIdle,
  events = ['mousedown', 'mousemove', 'keydown', 'scroll', 'touchstart', 'click'],
  isBlocked,
}: UseIdleTimerOptions) {
  const timeoutId = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastActivity = useRef<number | null>(null);
  // Keep latest callbacks in refs so the setTimeout closure never goes stale
  const onIdleRef = useRef(onIdle);
  const isBlockedRef = useRef(isBlocked);
  const resetTimerRef = useRef<() => void>(() => {});

  useEffect(() => { onIdleRef.current = onIdle; }, [onIdle]);
  useEffect(() => { isBlockedRef.current = isBlocked; }, [isBlocked]);

  useEffect(() => {
    lastActivity.current = Date.now();
  }, []);

  const resetTimer = useCallback(() => {
    // Clear existing timeout
    if (timeoutId.current) {
      clearTimeout(timeoutId.current);
    }

    // Update last activity time
    lastActivity.current = Date.now();

    // Set new timeout — always reads latest callbacks via refs
    timeoutId.current = setTimeout(() => {
      // If blocked (e.g. active upload in progress), postpone instead of firing
      if (isBlockedRef.current?.()) {
        resetTimerRef.current();
        return;
      }
      onIdleRef.current();
    }, timeout);
  }, [timeout]); // timeout is the only real dep; callbacks are accessed via refs

  // Keep resetTimerRef in sync
  useEffect(() => { resetTimerRef.current = resetTimer; }, [resetTimer]);

  useEffect(() => {
    // Start the timer initially
    resetTimer();

    // Attach event listeners for user activity
    events.forEach((event) => {
      window.addEventListener(event, resetTimer, { passive: true });
    });

    // Cleanup
    return () => {
      if (timeoutId.current) {
        clearTimeout(timeoutId.current);
      }
      events.forEach((event) => {
        window.removeEventListener(event, resetTimer);
      });
    };
  }, [resetTimer, events]);

  return {
    getLastActivity: () => lastActivity.current,
    resetTimer,
  };
}
