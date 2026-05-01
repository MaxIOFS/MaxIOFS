import { useState, useEffect, useCallback, createContext, useContext } from 'react';
import { useNavigate } from 'react-router-dom';
import APIClient, { getActiveUploadCount } from '@/lib/api';
import { getBasePath, isLoginPath } from '@/lib/basePath';
import type { User, LoginRequest, APIError } from '@/types';
import { useIdleTimer, getLastActivityTimestamp, clearLastActivityTimestamp } from './useIdleTimer';
import { useTheme } from '@/contexts/ThemeContext';
import { useLanguage } from '@/contexts/LanguageContext';
import { isHttpStatus } from '@/lib/utils';

// 15 minutes of inactivity → logout
const IDLE_TIMEOUT_MS = 15 * 60 * 1000;

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
  const { setTheme } = useTheme();
  const { setLanguage } = useLanguage();

  const isAuthenticated = !!user && APIClient.isAuthenticated();

  const applyUserPreferences = useCallback((userData: User) => {
    if (userData.themePreference) {
      setTheme(userData.themePreference as 'light' | 'dark' | 'system');
    }
    if (userData.languagePreference) {
      setLanguage(userData.languagePreference as 'en' | 'es');
    }
  }, [setTheme, setLanguage]);

  const clearError = useCallback(() => setError(null), []);

  // Initialize auth state on mount
  useEffect(() => {
    const initializeAuth = async () => {
      if (typeof window !== 'undefined' && isLoginPath()) {
        setUser(null);
        setIsLoading(false);
        return;
      }

      // If the user has no token at all, nothing to restore
      if (!APIClient.isAuthenticated()) {
        setUser(null);
        setIsLoading(false);
        return;
      }

      // Check if the user was inactive for too long while the tab was closed.
      // last_activity is written to localStorage on every UI interaction by useIdleTimer.
      const lastActivity = getLastActivityTimestamp();
      if (lastActivity > 0 && Date.now() - lastActivity > IDLE_TIMEOUT_MS) {
        APIClient.clearAuth();
        clearLastActivityTimestamp();
        setUser(null);
        setIsLoading(false);
        if (!isLoginPath()) {
          window.location.href = `${getBasePath()}/login`;
        }
        return;
      }

      try {
        const currentUser = await APIClient.getCurrentUser();
        setUser(currentUser);
        applyUserPreferences(currentUser);
      } catch (err: unknown) {
        // 401 → token invalid; redirect to login
        if (isHttpStatus(err, 401)) {
          APIClient.clearAuth();
          clearLastActivityTimestamp();
          if (!isLoginPath()) {
            window.location.href = `${getBasePath()}/login`;
          }
        }
        setUser(null);
      } finally {
        setIsLoading(false);
      }
    };

    initializeAuth();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const login = useCallback(async (credentials: LoginRequest) => {
    try {
      setIsLoading(true);
      setError(null);

      const response = await APIClient.login(credentials);

      if (response.success && response.user) {
        setUser(response.user);
        applyUserPreferences(response.user);
        if (typeof window !== 'undefined') {
          window.location.href = getBasePath() || '/';
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
  }, [applyUserPreferences]);

  const logout = useCallback(async () => {
    try {
      setIsLoading(true);
      await APIClient.logout();
    } finally {
      clearLastActivityTimestamp();
      setUser(null);
      setError(null);
      setIsLoading(false);
      navigate('/login');
    }
  }, [navigate]);

  const refreshAuth = useCallback(async () => {
    try {
      setIsLoading(true);
      if (APIClient.isAuthenticated()) {
        const currentUser = await APIClient.getCurrentUser();
        setUser(currentUser);
        applyUserPreferences(currentUser);
      } else {
        setUser(null);
      }
    } catch {
      setUser(null);
      APIClient.clearAuth();
    } finally {
      setIsLoading(false);
    }
  }, [applyUserPreferences]);

  // Idle timer — fires after 15 min of no user interaction.
  // Blocked during active uploads so the session doesn't die mid-transfer.
  const handleIdle = useCallback(() => {
    if (isAuthenticated && typeof window !== 'undefined') {
      APIClient.clearAuth();
      clearLastActivityTimestamp();
      setUser(null);
      setError({
        code: 'SESSION_TIMEOUT',
        message: 'Your session has expired due to inactivity. Please log in again.',
        details: null,
      });
      window.location.href = `${getBasePath()}/login`;
    }
  }, [isAuthenticated]);

  useIdleTimer({
    timeout: IDLE_TIMEOUT_MS,
    onIdle: handleIdle,
    isBlocked: () => getActiveUploadCount() > 0,
  });

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
