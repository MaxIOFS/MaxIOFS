import { useEffect, useRef, useCallback } from 'react';

interface UseIdleTimerOptions {
  timeout: number; // Timeout in milliseconds
  onIdle: () => void; // Callback when user becomes idle
  events?: string[]; // Events to track for activity
}

/**
 * Hook to detect user inactivity and trigger a callback after a specified timeout
 * @param options Configuration options for the idle timer
 */
export function useIdleTimer({
  timeout,
  onIdle,
  events = ['mousedown', 'mousemove', 'keydown', 'scroll', 'touchstart', 'click'],
}: UseIdleTimerOptions) {
  const timeoutId = useRef<NodeJS.Timeout | null>(null);
  const lastActivity = useRef<number>(Date.now());

  const resetTimer = useCallback(() => {
    // Clear existing timeout
    if (timeoutId.current) {
      clearTimeout(timeoutId.current);
    }

    // Update last activity time
    lastActivity.current = Date.now();

    // Set new timeout
    timeoutId.current = setTimeout(() => {
      onIdle();
    }, timeout);
  }, [timeout, onIdle]);

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
    lastActivity: lastActivity.current,
    resetTimer,
  };
}
