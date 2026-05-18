import React, { createContext, useContext, useState, ReactNode } from 'react';
import i18n, { loadLanguage } from '../i18n';

type Language = 'en' | 'es' | 'fr' | 'de' | 'it' | 'pt';

interface LanguageContextType {
  language: Language;
  setLanguage: (language: Language) => void;
}

const LanguageContext = createContext<LanguageContextType | undefined>(undefined);

export function useLanguage() {
  const context = useContext(LanguageContext);
  if (!context) {
    throw new Error('useLanguage must be used within a LanguageProvider');
  }
  return context;
}

interface LanguageProviderProps {
  children: ReactNode;
  initialLanguage?: Language;
}

export function LanguageProvider({ children, initialLanguage = 'en' }: LanguageProviderProps) {
  // Initialize directly from localStorage — same source i18n.ts reads at module
  // load time, so state and i18n are already in sync without a useEffect.
  const [language, setLanguageState] = useState<Language>(() => {
    try {
      const stored = localStorage.getItem('language');
      return stored === 'en' || stored === 'es' || stored === 'fr' || stored === 'de' || stored === 'it' || stored === 'pt' ? stored : initialLanguage;
    } catch {
      return initialLanguage;
    }
  });

  const setLanguage = (newLanguage: Language) => {
    localStorage.setItem('language', newLanguage);
    // Load the language bundle (no-op for en/es which are always bundled),
    // then switch. requestAnimationFrame defers the re-render cascade until
    // React has committed the current interaction (e.g. the button click).
    loadLanguage(newLanguage).then(() => {
      setLanguageState(newLanguage);
      requestAnimationFrame(() => {
        i18n.changeLanguage(newLanguage);
      });
    });
  };

  return (
    <LanguageContext.Provider value={{ language, setLanguage }}>
      {children}
    </LanguageContext.Provider>
  );
}
