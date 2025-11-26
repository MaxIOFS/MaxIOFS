import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Sun, Moon, Monitor, Save, CheckCircle } from 'lucide-react';
import { useTheme } from '@/contexts/ThemeContext';
import { useLanguage } from '@/contexts/LanguageContext';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { APIClient } from '@/lib/api';

type Theme = 'light' | 'dark' | 'system';
type Language = 'en' | 'es';

export function UserPreferences() {
  const { t, i18n } = useTranslation();
  const { theme, setTheme } = useTheme();
  const { language } = useLanguage();
  const { user } = useCurrentUser();
  const queryClient = useQueryClient();

  const [localTheme, setLocalTheme] = useState<Theme>(theme);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const hasChanges = localTheme !== theme;

  // Update preferences mutation
  const updateMutation = useMutation({
    mutationFn: (data: { themePreference: Theme; languagePreference: Language }) =>
      APIClient.updateUserPreferences(user?.id || '', data.themePreference, data.languagePreference),
    onSuccess: () => {
      // Apply theme locally
      setTheme(localTheme);

      // Invalidate user query to refetch updated preferences
      queryClient.invalidateQueries({ queryKey: ['currentUser'] });

      setSaveSuccess(true);
      setSaveError(null);
      setTimeout(() => setSaveSuccess(false), 3000);
    },
    onError: (error: any) => {
      setSaveError(error.response?.data?.error || t('preferences.saveFailed') || 'Failed to save preferences');
      setTimeout(() => setSaveError(null), 5000);
    }
  });

  const handleSave = () => {
    if (!hasChanges || !user?.id) return;

    updateMutation.mutate({
      themePreference: localTheme,
      languagePreference: language // Keep current language
    });
  };

  const handleReset = () => {
    setLocalTheme(theme);
    setSaveError(null);
  };

  const themeOptions: { value: Theme; icon: React.ComponentType<any>; label: string }[] = [
    { value: 'light', icon: Sun, label: t('preferences.themeLight') },
    { value: 'dark', icon: Moon, label: t('preferences.themeDark') },
    { value: 'system', icon: Monitor, label: t('preferences.themeSystem') }
  ];

  return (
    <div className="space-y-6">
      {/* Success Message */}
      {saveSuccess && (
        <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
            <div>
              <p className="text-sm font-medium text-green-900 dark:text-green-300">
                {t('preferences.preferencesUpdated')}
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Error Message */}
      {saveError && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <svg className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
            <div>
              <p className="text-sm font-medium text-red-900 dark:text-red-300">Error</p>
              <p className="text-sm text-red-700 dark:text-red-400 mt-1">{saveError}</p>
            </div>
          </div>
        </div>
      )}

      {/* Theme Selection */}
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-3">
          {t('preferences.theme')}
        </label>
        <div className="grid grid-cols-3 gap-2">
          {themeOptions.map(({ value, icon: Icon, label }) => (
            <button
              key={value}
              onClick={() => setLocalTheme(value)}
              className={`flex flex-col items-center justify-center p-3 rounded-lg border transition-all ${
                localTheme === value
                  ? 'border-blue-600 bg-blue-50 dark:bg-blue-900/20'
                  : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'
              }`}
            >
              <Icon className={`h-5 w-5 mb-1.5 ${
                localTheme === value
                  ? 'text-blue-600 dark:text-blue-400'
                  : 'text-gray-600 dark:text-gray-400'
              }`} />
              <span className={`text-xs font-medium ${
                localTheme === value
                  ? 'text-blue-900 dark:text-blue-300'
                  : 'text-gray-700 dark:text-gray-300'
              }`}>
                {label}
              </span>
            </button>
          ))}
        </div>
      </div>

      {/* Save/Reset Buttons */}
      {hasChanges && (
        <div className="flex items-center gap-3 pt-4 border-t border-gray-200 dark:border-gray-700">
          <button
            onClick={handleReset}
            disabled={updateMutation.isPending}
            className="flex-1 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50 transition-colors"
          >
            {t('common.cancel')}
          </button>
          <button
            onClick={handleSave}
            disabled={updateMutation.isPending}
            className="flex-1 px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 flex items-center justify-center gap-2 transition-colors"
          >
            <Save className="h-4 w-4" />
            {updateMutation.isPending ? t('common.loading') : t('preferences.savePreferences')}
          </button>
        </div>
      )}
    </div>
  );
}
