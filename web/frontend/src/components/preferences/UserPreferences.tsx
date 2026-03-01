import React, { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Sun, Moon, Monitor, Save } from 'lucide-react';
import { useTheme } from '@/contexts/ThemeContext';
import { useLanguage } from '@/contexts/LanguageContext';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { APIClient } from '@/lib/api';
import ModalManager from '@/lib/modals';

type Theme = 'light' | 'dark' | 'system';
type Language = 'en' | 'es';

interface UserPreferencesProps {
  disabled?: boolean;
}

export function UserPreferences({ disabled = false }: UserPreferencesProps) {
  const { t } = useTranslation(['preferences', 'common']);
  const { theme, setTheme } = useTheme();
  const { language, setLanguage } = useLanguage();
  const { user } = useCurrentUser();
  const queryClient = useQueryClient();

  const [localTheme, setLocalTheme] = useState<Theme>(theme);
  const [localLanguage, setLocalLanguage] = useState<Language>(language);

  const hasChanges = localTheme !== theme || localLanguage !== language;

  const updateMutation = useMutation({
    mutationFn: (data: { themePreference: Theme; languagePreference: Language }) =>
      APIClient.updateUserPreferences(user?.id || '', data.themePreference, data.languagePreference),
    onSuccess: () => {
      setTheme(localTheme);
      setLanguage(localLanguage);
      queryClient.invalidateQueries({ queryKey: ['currentUser'] });
      ModalManager.toast('success', t('preferencesUpdated'));
    },
    onError: (error: any) => {
      ModalManager.toast('error', error.response?.data?.error || t('errorTitle'));
    },
  });

  const handleSave = () => {
    if (!hasChanges || !user?.id) return;
    updateMutation.mutate({ themePreference: localTheme, languagePreference: localLanguage });
  };

  const handleReset = () => {
    setLocalTheme(theme);
    setLocalLanguage(language);
  };

  const themeOptions: { value: Theme; icon: React.ComponentType<any>; label: string }[] = [
    { value: 'light', icon: Sun, label: t('themeLight') },
    { value: 'dark', icon: Moon, label: t('themeDark') },
    { value: 'system', icon: Monitor, label: t('themeSystem') },
  ];

  const languageOptions: { value: Language; flag: string; label: string }[] = [
    { value: 'en', flag: '🇬🇧', label: t('languageEnglish') },
    { value: 'es', flag: '🇪🇸', label: t('languageSpanish') },
  ];

  return (
    <div className="flex flex-col gap-5">
      {/* Theme + Language side by side */}
      <div className="grid grid-cols-2 gap-4">
        {/* Theme */}
        <div>
          <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('theme')}</p>
          <div className="grid grid-cols-3 gap-1.5">
            {themeOptions.map(({ value, icon: Icon, label }) => (
              <button
                key={value}
                onClick={() => !disabled && setLocalTheme(value)}
                disabled={disabled}
                className={`flex flex-col items-center justify-center p-2 rounded-lg border transition-all ${
                  disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'
                } ${
                  localTheme === value
                    ? 'border-blue-600 bg-blue-50 dark:bg-blue-900/20'
                    : `border-gray-200 dark:border-gray-700 ${!disabled ? 'hover:border-gray-300 dark:hover:border-gray-600' : ''}`
                }`}
              >
                <Icon className={`h-4 w-4 mb-1 ${
                  localTheme === value ? 'text-blue-600 dark:text-blue-400' : 'text-gray-500 dark:text-gray-400'
                }`} />
                <span className={`text-xs font-medium ${
                  localTheme === value ? 'text-blue-900 dark:text-blue-300' : 'text-gray-700 dark:text-gray-300'
                }`}>
                  {label}
                </span>
              </button>
            ))}
          </div>
          {disabled && (
            <p className="text-xs text-gray-400 dark:text-gray-500 mt-1.5">{t('ownThemeOnly')}</p>
          )}
        </div>

        {/* Language */}
        <div className="flex flex-col">
          <p className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">{t('language')}</p>
          <select
            value={localLanguage}
            onChange={(e) => !disabled && setLocalLanguage(e.target.value as Language)}
            disabled={disabled}
            className={`flex-1 w-full px-3 py-2 rounded-lg border text-sm transition-all bg-white dark:bg-gray-900 text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:ring-blue-500 ${
              disabled
                ? 'opacity-50 cursor-not-allowed border-gray-200 dark:border-gray-700'
                : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'
            }`}
          >
            {languageOptions.map(({ value, flag, label }) => (
              <option key={value} value={value}>
                {flag} {label}
              </option>
            ))}
          </select>
          {disabled && (
            <p className="text-xs text-gray-400 dark:text-gray-500 mt-1.5">{t('ownLanguageOnly')}</p>
          )}
        </div>
      </div>

      {/* Always-visible Save / Cancel */}
      <div className="flex items-center gap-2 pt-3 border-t border-gray-200 dark:border-gray-700">
        <button
          onClick={handleReset}
          disabled={!hasChanges || updateMutation.isPending}
          className="flex-1 px-3 py-1.5 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-lg transition-colors enabled:hover:bg-gray-50 dark:enabled:hover:bg-gray-700 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {t('common:cancel')}
        </button>
        <button
          onClick={handleSave}
          disabled={!hasChanges || updateMutation.isPending}
          className="flex-1 px-3 py-1.5 text-sm font-medium text-white bg-blue-600 rounded-lg transition-colors enabled:hover:bg-blue-700 disabled:opacity-40 disabled:cursor-not-allowed flex items-center justify-center gap-1.5"
        >
          <Save className="h-3.5 w-3.5" />
          {updateMutation.isPending ? t('common:loading') : t('savePreferences')}
        </button>
      </div>
    </div>
  );
}
