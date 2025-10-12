import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
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
  const navigate = useNavigate();

  // Check if user is authenticated
  const isAuthenticated = !!user && APIClient.isAuthenticated();

  // Clear error
  const clearError = useCallback(() => {
    setError(null);
  }, []);

  // Initialize auth state
  useEffect(() => {
    const initializeAuth = async () => {
      // Don't try to authenticate on login page
      if (typeof window !== 'undefined' && window.location.pathname === '/login') {
        setUser(null);
        setIsLoading(false);
        return;
      }

      try {
        if (APIClient.isAuthenticated()) {
          const currentUser = await APIClient.getCurrentUser();
          setUser(currentUser);
          setIsLoading(false);
        } else {
          setUser(null);
          setIsLoading(false);
        }
      } catch (err: any) {
        // If we get a 401, the token is invalid
        if (err?.response?.status === 401) {
          APIClient.clearAuth();
          setUser(null);
          setIsLoading(false);
          // Redirect to login if not already on login page
          if (typeof window !== 'undefined' && !window.location.pathname.includes('/login')) {
            setTimeout(() => {
              window.location.href = '/login';
            }, 0);
          }
        } else {
          APIClient.clearAuth();
          setUser(null);
          setIsLoading(false);
        }
      }
    };

    initializeAuth();
  }, [navigate]);

  // Login function
  const login = useCallback(async (credentials: LoginRequest) => {
    try {
      setIsLoading(true);
      setError(null);

      const response = await APIClient.login(credentials);

      if (response.success && response.user) {
        setUser(response.user);
        // Use hard redirect to ensure auth state is properly initialized
        if (typeof window !== 'undefined') {
          window.location.href = '/';
        }
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
  }, [navigate]);

  // Logout function
  const logout = useCallback(async () => {
    try {
      setIsLoading(true);
      await APIClient.logout();
    } finally {
      setUser(null);
      setError(null);
      setIsLoading(false);
      navigate('/login');
    }
  }, [navigate]);

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