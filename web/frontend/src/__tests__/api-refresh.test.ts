import axios from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';

function token(expiresIn: number): string {
  const encode = (value: object) =>
    btoa(JSON.stringify(value)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  return `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode({ exp: Math.floor(Date.now() / 1000) + expiresIn })}.sig`;
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('Session renewal', () => {
  // Each renewal of an identity provider's session asks the provider; one that
  // rotates refresh tokens refuses a second use of one.
  it('sends one renewal at a time', async () => {
    vi.useFakeTimers();
    localStorage.setItem('auth_token', token(60));
    localStorage.setItem('refresh_token', 'refresh-1');
    let answer: (value: unknown) => void = () => {};
    const post = vi.spyOn(axios, 'post').mockImplementation(
      () => new Promise((resolve) => { answer = resolve; }),
    );

    const { apiClient } = await import('@/lib/api');
    apiClient.defaults.adapter = async (config) => ({ data: {}, status: 200, statusText: 'OK', headers: {}, config });
    await apiClient.get('/buckets');
    expect(post).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(10_000);
    expect(post).toHaveBeenCalledTimes(1);

    answer({ data: { access_token: token(900), refresh_token: 'refresh-2' } });
    await vi.advanceTimersByTimeAsync(60_000);
    expect(post).toHaveBeenCalledTimes(1);
  });
});
