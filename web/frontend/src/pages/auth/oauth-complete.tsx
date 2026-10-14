import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import APIClient from '@/lib/api';
import { getBasePath } from '@/lib/basePath';
import { getErrorMessage } from '@/lib/utils';
import { TwoFactorInput } from '@/components/TwoFactorInput';

// What the exchange of the single-use code answers: the session, or the token
// of the second step for a user with two-factor authentication.
interface ExchangeAnswer {
  access_token?: string;
  refresh_token?: string;
  requires_2fa?: boolean;
  two_factor_token?: string;
}

export default function OAuthCompletePage() {
  const { t } = useTranslation('auth');
  const [error, setError] = useState<string | null>(null);
  const [twoFactorToken, setTwoFactorToken] = useState<string | null>(null);
  const [verifying, setVerifying] = useState(false);
  const [codeError, setCodeError] = useState<string | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const errorMsg = params.get('error');

    if (errorMsg) {
      setError(errorMsg);
      return;
    }

    const code = params.get('code');
    if (!code) {
      setError('No authentication code received');
      return;
    }

    fetch(`${getBasePath()}/api/v1/auth/oauth/exchange-code?code=${encodeURIComponent(code)}`)
      .then((res) => {
        if (!res.ok) throw new Error(`Exchange failed: ${res.status}`);
        return res.json() as Promise<ExchangeAnswer>;
      })
      .then(({ access_token, refresh_token, requires_2fa, two_factor_token }) => {
        if (requires_2fa && two_factor_token) {
          setTwoFactorToken(two_factor_token);
          return;
        }
        if (!access_token || !refresh_token) {
          throw new Error('Authentication failed');
        }
        const secureFlag = window.location.protocol === 'https:' ? '; Secure' : '';
        localStorage.setItem('auth_token', access_token);
        localStorage.setItem('refresh_token', refresh_token);
        document.cookie = `auth_token=${access_token}; path=/; max-age=${24 * 60 * 60}${secureFlag}; SameSite=Strict`;
        document.cookie = `refresh_token=${refresh_token}; path=/; max-age=${24 * 60 * 60}${secureFlag}; SameSite=Strict`;
        window.location.href = getBasePath() || '/';
      })
      .catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : 'Authentication failed';
        setError(msg);
      });
  }, []);

  const handleVerify2FA = async (code: string) => {
    if (!twoFactorToken) return;
    setVerifying(true);
    setCodeError(null);
    try {
      const response = await APIClient.verify2FA(twoFactorToken, code);
      if (response.success && response.token) {
        window.location.href = getBasePath() || '/';
        return;
      }
      setCodeError(response.error || t('invalid2FACode'));
    } catch (err: unknown) {
      setCodeError(getErrorMessage(err, t('invalid2FACodeMessage')));
    } finally {
      setVerifying(false);
    }
  };

  if (twoFactorToken && !error) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900 p-4">
        <div className="w-full max-w-md bg-card rounded-2xl shadow-xl border border-border p-8">
          <TwoFactorInput
            onSubmit={handleVerify2FA}
            onCancel={() => {
              window.location.href = `${getBasePath()}/login`;
            }}
            loading={verifying}
            error={codeError}
          />
        </div>
      </div>
    );
  }

  if (error) {
    const basePath = getBasePath() || '/';
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="text-center max-w-md p-8">
          <div className="mx-auto w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center mb-4">
            <svg className="w-6 h-6 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </div>
          <h2 className="text-xl font-bold text-foreground mb-2">{t('oauthFailed')}</h2>
          <p className="text-sm text-muted-foreground mb-6">{error}</p>
          <a
            href={`${basePath}/login`}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
          >
            {t('backToLogin')}
          </a>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900">
      <div className="text-center">
        <svg className="animate-spin h-12 w-12 text-blue-600 mx-auto mb-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        <p className="text-sm text-muted-foreground">{t('completingSignIn')}</p>
      </div>
    </div>
  );
}
