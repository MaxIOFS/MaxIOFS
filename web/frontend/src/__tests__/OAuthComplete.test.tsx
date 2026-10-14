import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import OAuthCompletePage from '@/pages/auth/oauth-complete';
import APIClient from '@/lib/api';

vi.mock('@/lib/api', () => ({
  default: { verify2FA: vi.fn() },
  APIClient: { verify2FA: vi.fn() },
}));

// The page exchanges the single-use code the identity provider's sign-in
// sent the browser with.
// The test setup replaces window.location with a plain object.
const location = window.location as unknown as { href: string; search: string };

function exchangeAnswers(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve(body) }));
}

describe('OAuth sign-in completion', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    location.href = '';
    location.search = '?code=single-use';
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    location.search = '';
  });

  it('keeps the session the exchange gives', async () => {
    exchangeAnswers({ access_token: 'access', refresh_token: 'refresh' });
    render(<OAuthCompletePage />);
    await waitFor(() => {
      expect(localStorage.getItem('auth_token')).toBe('access');
    });
    expect(APIClient.verify2FA).not.toHaveBeenCalled();
  });

  it('asks for the second factor and sends it with the token of the first step', async () => {
    const user = userEvent.setup();
    exchangeAnswers({ requires_2fa: true, two_factor_token: 'two-factor-token' });
    vi.mocked(APIClient.verify2FA).mockResolvedValue({ success: true, token: 'session' });
    render(<OAuthCompletePage />);

    await waitFor(() => {
      expect(screen.getAllByRole('textbox')).toHaveLength(6);
    });
    expect(localStorage.getItem('auth_token')).toBeNull();
    await user.keyboard('123456');
    await waitFor(() => {
      expect(APIClient.verify2FA).toHaveBeenCalledWith('two-factor-token', '123456');
    });
  });
});
