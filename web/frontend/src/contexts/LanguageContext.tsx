import React, { createContext, useContext, useEffect, useState, ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

type Language = 'en' | 'es';

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
  const { i18n } = useTranslation();
  const [language, setLanguageState] = useState<Language>(initialLanguage);

  // Update language
  const setLanguage = (newLanguage: Language) => {
    setLanguageState(newLanguage);
    i18n.changeLanguage(newLanguage);
    localStorage.setItem('language', newLanguage);
  };

  // Initialize language from i18n on mount
  useEffect(() => {
    const currentLang = i18n.language;
    if (currentLang === 'en' || currentLang === 'es') {
      setLanguageState(currentLang);
    } else {
      // If i18n has a different language, fallback to English
      setLanguageState('en');
      i18n.changeLanguage('en');
    }
  }, [i18n]);

  const value: LanguageContextType = {
    language,
    setLanguage
  };

  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}
