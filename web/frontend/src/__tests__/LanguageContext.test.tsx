import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LanguageProvider, useLanguage } from '@/contexts/LanguageContext';

// Mock i18n so we can spy on changeLanguage without triggering real translation updates
vi.mock('@/i18n', () => ({
  default: { changeLanguage: vi.fn().mockResolvedValue(undefined) },
}));

// Execute requestAnimationFrame callbacks synchronously so setLanguage's deferred
// i18n.changeLanguage call runs within the same test tick.
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => { cb(0); return 0; });
});

// Consumer component that exposes context values for assertions
function LanguageConsumer() {
  const { language, setLanguage } = useLanguage();
  return (
    <div>
      <span data-testid="language">{language}</span>
      <button onClick={() => setLanguage('es')}>Switch to ES</button>
      <button onClick={() => setLanguage('en')}>Switch to EN</button>
    </div>
  );
}

describe('LanguageContext', () => {
  describe('LanguageProvider', () => {
    it('defaults to "en" when localStorage has no saved language', () => {
      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      expect(screen.getByTestId('language')).toHaveTextContent('en');
    });

    it('reads the initial language from localStorage', () => {
      localStorage.setItem('language', 'es');

      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      expect(screen.getByTestId('language')).toHaveTextContent('es');
    });

    it('uses initialLanguage prop as fallback when localStorage is empty', () => {
      render(
        <LanguageProvider initialLanguage="es">
          <LanguageConsumer />
        </LanguageProvider>
      );

      expect(screen.getByTestId('language')).toHaveTextContent('es');
    });

    it('updates language state when setLanguage is called', async () => {
      const user = userEvent.setup();
      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      await user.click(screen.getByText('Switch to ES'));

      expect(screen.getByTestId('language')).toHaveTextContent('es');
    });

    it('persists the new language to localStorage', async () => {
      const user = userEvent.setup();
      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      await user.click(screen.getByText('Switch to ES'));

      expect(localStorage.setItem).toHaveBeenCalledWith('language', 'es');
    });

    it('calls i18n.changeLanguage with the new language', async () => {
      const { default: i18n } = await import('@/i18n');
      const user = userEvent.setup();

      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      await user.click(screen.getByText('Switch to ES'));

      expect(i18n.changeLanguage).toHaveBeenCalledWith('es');
    });

    it('can switch back from es to en', async () => {
      const user = userEvent.setup();
      localStorage.setItem('language', 'es');

      render(
        <LanguageProvider>
          <LanguageConsumer />
        </LanguageProvider>
      );

      expect(screen.getByTestId('language')).toHaveTextContent('es');

      await user.click(screen.getByText('Switch to EN'));

      expect(screen.getByTestId('language')).toHaveTextContent('en');
      expect(localStorage.setItem).toHaveBeenCalledWith('language', 'en');
    });
  });

  describe('useLanguage', () => {
    it('throws when used outside a LanguageProvider', () => {
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      expect(() => {
        render(<LanguageConsumer />);
      }).toThrow('useLanguage must be used within a LanguageProvider');

      consoleSpy.mockRestore();
    });
  });
});
