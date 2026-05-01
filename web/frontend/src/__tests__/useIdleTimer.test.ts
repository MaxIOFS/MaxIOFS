import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useIdleTimer } from '@/hooks/useIdleTimer';

describe('useIdleTimer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('does not treat background token refresh events as user activity', () => {
    const onIdle = vi.fn();

    renderHook(() => useIdleTimer({
      timeout: 1000,
      onIdle,
      events: ['click'],
    }));

    act(() => {
      vi.advanceTimersByTime(900);
      window.dispatchEvent(new CustomEvent('session-keep-alive'));
      vi.advanceTimersByTime(100);
    });

    expect(onIdle).toHaveBeenCalledTimes(1);
  });

  it('resets the inactivity timer on real browser activity', () => {
    const onIdle = vi.fn();

    renderHook(() => useIdleTimer({
      timeout: 1000,
      onIdle,
      events: ['click'],
    }));

    act(() => {
      vi.advanceTimersByTime(900);
      window.dispatchEvent(new Event('click'));
      vi.advanceTimersByTime(100);
    });

    expect(onIdle).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(900);
    });

    expect(onIdle).toHaveBeenCalledTimes(1);
  });
});
