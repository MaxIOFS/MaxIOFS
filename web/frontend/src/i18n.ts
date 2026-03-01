import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

// EN namespaces
import en_common from './locales/en/common.json';
import en_navigation from './locales/en/navigation.json';
import en_auth from './locales/en/auth.json';
import en_users from './locales/en/users.json';
import en_preferences from './locales/en/preferences.json';
import en_buckets from './locales/en/buckets.json';
import en_settings from './locales/en/settings.json';
import en_security from './locales/en/security.json';
import en_metrics from './locales/en/metrics.json';
import en_auditLogs from './locales/en/auditLogs.json';
import en_about from './locales/en/about.json';
import en_layout from './locales/en/layout.json';
import en_dashboard from './locales/en/dashboard.json';
import en_tenants from './locales/en/tenants.json';
import en_createBucket from './locales/en/createBucket.json';
import en_bucketSettings from './locales/en/bucketSettings.json';

// ES namespaces
import es_common from './locales/es/common.json';
import es_navigation from './locales/es/navigation.json';
import es_auth from './locales/es/auth.json';
import es_users from './locales/es/users.json';
import es_preferences from './locales/es/preferences.json';
import es_buckets from './locales/es/buckets.json';
import es_settings from './locales/es/settings.json';
import es_security from './locales/es/security.json';
import es_metrics from './locales/es/metrics.json';
import es_auditLogs from './locales/es/auditLogs.json';
import es_about from './locales/es/about.json';
import es_layout from './locales/es/layout.json';
import es_dashboard from './locales/es/dashboard.json';
import es_tenants from './locales/es/tenants.json';
import es_createBucket from './locales/es/createBucket.json';
import es_bucketSettings from './locales/es/bucketSettings.json';

// Read the saved language once synchronously at module load time.
// This avoids LanguageDetector's multi-step async search (localStorage →
// navigator → htmlTag) which triggers a language-switch cascade before
// the first React paint when the detected locale differs from the default.
const getSavedLanguage = (): 'en' | 'es' => {
  try {
    const stored = localStorage.getItem('language');
    return stored === 'en' || stored === 'es' ? stored : 'en';
  } catch {
    return 'en';
  }
};

i18n
  .use(initReactI18next)
  .init({
    resources: {
      en: {
        common: en_common,
        navigation: en_navigation,
        auth: en_auth,
        users: en_users,
        preferences: en_preferences,
        buckets: en_buckets,
        settings: en_settings,
        security: en_security,
        metrics: en_metrics,
        auditLogs: en_auditLogs,
        about: en_about,
        layout: en_layout,
        dashboard: en_dashboard,
        tenants: en_tenants,
        createBucket: en_createBucket,
        bucketSettings: en_bucketSettings,
      },
      es: {
        common: es_common,
        navigation: es_navigation,
        auth: es_auth,
        users: es_users,
        preferences: es_preferences,
        buckets: es_buckets,
        settings: es_settings,
        security: es_security,
        metrics: es_metrics,
        auditLogs: es_auditLogs,
        about: es_about,
        layout: es_layout,
        dashboard: es_dashboard,
        tenants: es_tenants,
        createBucket: es_createBucket,
        bucketSettings: es_bucketSettings,
      },
    },
    ns: ['common', 'navigation', 'auth', 'users', 'preferences', 'buckets', 'settings', 'security', 'metrics', 'auditLogs', 'about', 'layout', 'dashboard', 'tenants', 'createBucket', 'bucketSettings'],
    defaultNS: 'common',
    lng: getSavedLanguage(),   // Explicit language — no runtime detection
    load: 'languageOnly',      // Don't try to load 'en-US', 'en-GB', etc.
    fallbackLng: 'en',
    debug: false,

    interpolation: {
      escapeValue: false, // React already escapes values
    },

    react: {
      useSuspense: false,           // Prevent Suspense-related freezes
      bindI18nStore: 'languageChanged', // Only re-render on explicit language switch
    },
  });

export default i18n;
