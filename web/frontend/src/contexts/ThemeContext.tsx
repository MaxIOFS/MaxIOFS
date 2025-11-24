import React, { createContext, useContext, useEffect, useState, ReactNode } from 'react';

type Theme = 'light' | 'dark' | 'system';

interface ThemeContextType {
  theme: Theme;
  effectiveTheme: 'light' | 'dark'; // The actual theme being applied
  setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
}

interface ThemeProviderProps {
  children: ReactNode;
  initialTheme?: Theme;
}

export function ThemeProvider({ children, initialTheme = 'system' }: ThemeProviderProps) {
  const [theme, setThemeState] = useState<Theme>(initialTheme);
  const [effectiveTheme, setEffectiveTheme] = useState<'light' | 'dark'>('light');

  // Detect system theme
  const getSystemTheme = (): 'light' | 'dark' => {
    if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
      return 'dark';
    }
    return 'light';
  };

  // Calculate effective theme based on user preference
  const calculateEffectiveTheme = (userTheme: Theme): 'light' | 'dark' => {
    if (userTheme === 'system') {
      return getSystemTheme();
    }
    return userTheme;
  };

  // Update theme
  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme);
    localStorage.setItem('theme', newTheme);
  };

  // Apply theme to document
  useEffect(() => {
    const effective = calculateEffectiveTheme(theme);
    setEffectiveTheme(effective);

    // Apply theme class to document
    document.documentElement.classList.remove('light', 'dark');
    document.documentElement.classList.add(effective);
  }, [theme]);

  // Listen to system theme changes when in system mode
  useEffect(() => {
    if (theme !== 'system') return;

    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleChange = () => {
      const effective = calculateEffectiveTheme('system');
      setEffectiveTheme(effective);
      document.documentElement.classList.remove('light', 'dark');
      document.documentElement.classList.add(effective);
    };

    mediaQuery.addEventListener('change', handleChange);
    return () => mediaQuery.removeEventListener('change', handleChange);
  }, [theme]);

  const value: ThemeContextType = {
    theme,
    effectiveTheme,
    setTheme
  };

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
