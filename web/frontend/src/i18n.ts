import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

// EN is always bundled as the fallback language.
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
import en_idp from './locales/en/idp.json';
import en_cluster from './locales/en/cluster.json';
import en_groups from './locales/en/groups.json';

// All other locales are loaded on demand via dynamic imports.
// import.meta.glob is resolved statically by Vite at build time.
// manualChunks in vite.config.ts ensures every file under locales/{lang}/
// ends up in a single output chunk (lang-fr.js, lang-de.js, etc.),
// so switching languages triggers exactly one network request.
const dynamicLocales = import.meta.glob<{ default: Record<string, unknown> }>([
  './locales/*/*.json',
  '!./locales/en/*.json',
]);

const STATIC_LANGS = new Set(['en']);

// In-flight promises — concurrent callers share the same fetch.
const loadingPromises = new Map<string, Promise<void>>();

export function loadLanguage(lang: string): Promise<void> {
  if (STATIC_LANGS.has(lang) || i18n.hasResourceBundle(lang, 'common')) {
    return Promise.resolve();
  }

  const existing = loadingPromises.get(lang);
  if (existing) return existing;

  // Collect all glob entries that belong to this language.
  const entries = Object.entries(dynamicLocales).filter(([path]) =>
    path.startsWith(`./locales/${lang}/`)
  );

  if (entries.length === 0) return Promise.resolve();

  const promise = Promise.all(
    entries.map(async ([path, loader]) => {
      const ns = path.match(/\/([^/]+)\.json$/)?.[1];
      if (!ns) return;
      const mod = await loader();
      i18n.addResourceBundle(lang, ns, mod.default ?? mod, true, true);
    })
  ).then(() => {
    loadingPromises.delete(lang);
  });

  loadingPromises.set(lang, promise);
  return promise;
}

const getSavedLanguage = (): 'en' | 'es' | 'fr' | 'de' | 'it' | 'pt' | 'zh' | 'ja' | 'ru' => {
  try {
    const stored = localStorage.getItem('language');
    return stored === 'en' || stored === 'es' || stored === 'fr' || stored === 'de' || stored === 'it' || stored === 'pt' || stored === 'zh' || stored === 'ja' || stored === 'ru' ? stored : 'en';
  } catch {
    return 'en';
  }
};

const savedLang = getSavedLanguage();

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
        idp: en_idp,
        cluster: en_cluster,
        groups: en_groups,
      },
    },
    ns: ['common', 'navigation', 'auth', 'users', 'preferences', 'buckets', 'settings', 'security', 'metrics', 'auditLogs', 'about', 'layout', 'dashboard', 'tenants', 'createBucket', 'bucketSettings', 'idp', 'cluster', 'groups'],
    defaultNS: 'common',
    lng: STATIC_LANGS.has(savedLang) ? savedLang : 'en',
    load: 'languageOnly',
    fallbackLng: 'en',
    debug: false,

    interpolation: {
      escapeValue: false,
    },

    react: {
      useSuspense: false,
      bindI18nStore: 'languageChanged',
    },
  });

// If the user has a dynamic language saved, load its chunk immediately
// and switch once ready. On a local server this resolves in milliseconds.
if (!STATIC_LANGS.has(savedLang)) {
  loadLanguage(savedLang).then(() => {
    i18n.changeLanguage(savedLang);
  });
}

export default i18n;
