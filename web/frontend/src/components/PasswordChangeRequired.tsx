import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { KeyRound, LogOut } from 'lucide-react';
import { APIClient } from '@/lib/api';
import { useAuth } from '@/hooks/useAuth';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ModalRenderer, ToastNotifications } from '@/lib/modals';

// Shown in place of the whole console while the account owes a password change.
// The API refuses everything else anyway; this is what the user sees instead of
// a page full of permission errors.
//
// This screen replaces AppLayout rather than sitting inside it, so it has to
// bring its own modal and toast hosts and report failures in the form itself —
// otherwise an error has nowhere to appear and the button looks inert.
export function PasswordChangeRequired() {
  const { t } = useTranslation('users');
  const { user, logout } = useAuth();
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    if (!user?.id || saving) return;

    if (!newPassword || !confirmPassword) {
      setError(t('newPasswordRequired'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setError(t('passwordsMismatch'));
      return;
    }

    setError('');
    setSaving(true);
    try {
      // No current password: this session already proved knowledge of the one
      // the administrator handed over, at login.
      await APIClient.changePassword(user.id, '', newPassword);
      // Start clean: the console was refusing every request a moment ago and
      // its caches are full of those refusals.
      window.location.reload();
    } catch (err) {
      setError(
        (err as { message?: string })?.message ?? t('passwordChangeFailed'),
      );
      setSaving(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <div className="w-full max-w-md bg-card border border-border rounded-xl shadow-lg p-8">
        <div className="flex items-center gap-3 mb-2">
          <span className="h-10 w-10 rounded-full bg-amber-100 dark:bg-amber-900/30 flex items-center justify-center">
            <KeyRound className="h-5 w-5 text-amber-600 dark:text-amber-400" />
          </span>
          <h1 className="text-xl font-semibold text-foreground">{t('passwordChangeRequiredTitle')}</h1>
        </div>
        <p className="text-sm text-muted-foreground mb-6">{t('passwordChangeRequiredBody')}</p>

        <div className="space-y-4">
          <div>
            <label className="text-sm font-medium text-foreground mb-2 block">{t('newPassword')}</label>
            <Input
              type="password"
              autoFocus
              placeholder={t('enterNewPassword')}
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submit()}
              className="bg-card text-foreground border-border"
            />
          </div>
          <div>
            <label className="text-sm font-medium text-foreground mb-2 block">{t('confirmNewPassword')}</label>
            <Input
              type="password"
              placeholder={t('confirmNewPasswordPlaceholder')}
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && submit()}
              className="bg-card text-foreground border-border"
            />
          </div>

          {error && (
            <p role="alert" className="text-sm text-red-600 dark:text-red-400">
              {error}
            </p>
          )}

          <Button className="w-full" onClick={submit} disabled={saving}>
            {saving ? t('changing') : t('changePassword')}
          </Button>

          <button
            type="button"
            onClick={() => logout()}
            className="w-full flex items-center justify-center gap-2 text-sm text-muted-foreground hover:text-foreground"
          >
            <LogOut className="h-4 w-4" />
            {t('logout', { ns: 'navigation' })}
          </button>
        </div>
      </div>

      <ModalRenderer />
      <ToastNotifications />
    </div>
  );
}
