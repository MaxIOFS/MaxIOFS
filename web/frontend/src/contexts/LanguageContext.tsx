import React, { createContext, useContext, useState, ReactNode } from 'react';
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

  // Initialize directly from localStorage — same source i18n.ts reads at module
  // load time, so state and i18n are already in sync without a useEffect.
  const [language, setLanguageState] = useState<Language>(() => {
    try {
      const stored = localStorage.getItem('language');
      return stored === 'en' || stored === 'es' ? stored : initialLanguage;
    } catch {
      return initialLanguage;
    }
  });

  const setLanguage = (newLanguage: Language) => {
    setLanguageState(newLanguage);
    localStorage.setItem('language', newLanguage);
    // Defer i18n.changeLanguage so React can commit the current interaction
    // (e.g. the button click) before the re-render cascade from the language
    // switch hits all useTranslation() subscribers at once.
    requestAnimationFrame(() => {
      i18n.changeLanguage(newLanguage);
    });
  };

  return (
    <LanguageContext.Provider value={{ language, setLanguage }}>
      {children}
    </LanguageContext.Provider>
  );
}
