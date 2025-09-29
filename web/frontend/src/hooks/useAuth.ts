import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { useRouter } from 'next/navigation';
import APIClient from '@/lib/api';
import type { User, LoginRequest, APIError } from '@/types';

// Auth Context Type
interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  error: APIError | null;
  login: (credentials: LoginRequest) => Promise<void>;
  logout: () => Promise<void>;
  refreshAuth: () => Promise<void>;
  clearError: () => void;
}

// Create Auth Context
const AuthContext = createContext<AuthContextType | undefined>(undefined);

// Auth Hook
export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}

// Auth Provider Hook
export function useAuthProvider(): AuthContextType {
  const [user, setUser] = useState<User | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<APIError | null>(null);
  const router = useRouter();

  // Check if user is authenticated
  const isAuthenticated = !!user && APIClient.isAuthenticated();

  // Clear error
  const clearError = useCallback(() => {
    setError(null);
  }, []);

  // Initialize auth state
  useEffect(() => {
    const initializeAuth = async () => {
      try {
        if (APIClient.isAuthenticated()) {
          const currentUser = await APIClient.getCurrentUser();
          setUser(currentUser);
        }
      } catch (err) {
        console.error('Failed to initialize auth:', err);
        APIClient.clearAuth();
      } finally {
        setIsLoading(false);
      }
    };

    initializeAuth();
  }, []);

  // Login function
  const login = useCallback(async (credentials: LoginRequest) => {
    try {
      setIsLoading(true);
      setError(null);

      const response = await APIClient.login(credentials);

      if (response.success && response.user) {
        setUser(response.user);
        router.push('/dashboard');
      } else {
        throw new Error(response.error || 'Login failed');
      }
    } catch (err) {
      const apiError = err as APIError;
      setError({
        code: apiError.code || 'LOGIN_FAILED',
        message: apiError.message || 'Login failed. Please check your credentials.',
        details: apiError.details,
      });
      throw err;
    } finally {
      setIsLoading(false);
    }
  }, [router]);

  // Logout function
  const logout = useCallback(async () => {
    try {
      setIsLoading(true);
      await APIClient.logout();
    } catch (err) {
      console.error('Logout error:', err);
    } finally {
      setUser(null);
      setError(null);
      setIsLoading(false);
      router.push('/login');
    }
  }, [router]);

  // Refresh auth state
  const refreshAuth = useCallback(async () => {
    try {
      setIsLoading(true);
      if (APIClient.isAuthenticated()) {
        const currentUser = await APIClient.getCurrentUser();
        setUser(currentUser);
      } else {
        setUser(null);
      }
    } catch (err) {
      console.error('Failed to refresh auth:', err);
      setUser(null);
      APIClient.clearAuth();
    } finally {
      setIsLoading(false);
    }
  }, []);

  return {
    user,
    isAuthenticated,
    isLoading,
    error,
    login,
    logout,
    refreshAuth,
    clearError,
  };
}

// Export the context for use in AuthProvider
export { AuthContext };