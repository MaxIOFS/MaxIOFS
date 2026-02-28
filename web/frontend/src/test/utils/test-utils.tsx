import { ReactElement } from 'react';
import { render, RenderOptions } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from '@/contexts/ThemeContext';
import { LanguageProvider } from '@/contexts/LanguageContext';
import { AuthContext } from '@/hooks/useAuth';
import type { User } from '@/types';

// Default test user — global admin, no tenant. Override per-test by wrapping
// with a custom AuthContext.Provider if needed.
const mockUser: User = {
  id: '1',
  username: 'admin',
  email: 'admin@example.com',
  roles: ['admin'],
  status: 'active',
  tenantId: undefined,
  createdAt: '2024-01-01T00:00:00Z',
};

const mockAuthValue = {
  user: mockUser,
  isAuthenticated: true,
  isLoading: false,
  error: null,
  login: async () => {},
  logout: async () => {},
  refreshAuth: async () => {},
  clearError: () => {},
};

// Create a custom render function that includes all app providers.
// AuthContext is provided directly with a mock value to avoid real API
// calls (APIClient.getCurrentUser) that AuthProvider triggers on mount.
const AllTheProviders = ({ children }: { children: React.ReactNode }) => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false, // Don't retry on test failures
        gcTime: 0, // Don't cache in tests
      },
    },
  });

  return (
    <ThemeProvider>
      <LanguageProvider>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <AuthContext.Provider value={mockAuthValue}>
              {children}
            </AuthContext.Provider>
          </BrowserRouter>
        </QueryClientProvider>
      </LanguageProvider>
    </ThemeProvider>
  );
};

const customRender = (
  ui: ReactElement,
  options?: Omit<RenderOptions, 'wrapper'>
) => render(ui, { wrapper: AllTheProviders, ...options });

// Re-export everything
export * from '@testing-library/react';
export { customRender as render };
