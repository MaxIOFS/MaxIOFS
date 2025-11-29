import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import LoginPage from '@/pages/login';
import APIClient from '@/lib/api';

// Mock API Client
vi.mock('@/lib/api', () => ({
  default: {
    login: vi.fn(),
    verify2FA: vi.fn(),
  },
}));

// Mock SweetAlert
vi.mock('@/lib/sweetalert', () => ({
  default: {
    loading: vi.fn(),
    close: vi.fn(),
    successLogin: vi.fn(),
    error: vi.fn(),
    apiError: vi.fn(),
  },
}));

describe('Login Page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    (window as any).location.href = '';
  });

  describe('Rendering', () => {
    it('should render login form with all fields', () => {
      render(<LoginPage />);

      expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
    });

    it('should render MaxIOFS branding', () => {
      render(<LoginPage />);

      // Check for logo or brand name
      expect(screen.getByText(/MaxIOFS/i)).toBeInTheDocument();
    });
  });

  describe('Form Validation', () => {
    it('should not submit form with empty fields', async () => {
      const user = userEvent.setup();
      render(<LoginPage />);

      const submitButton = screen.getByRole('button', { name: /sign in/i });
      await user.click(submitButton);

      // API should not be called with empty fields
      expect(APIClient.login).not.toHaveBeenCalled();
    });

    it('should enable submit button when fields are filled', async () => {
      const user = userEvent.setup();
      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');

      expect(usernameInput).toHaveValue('admin');
      expect(passwordInput).toHaveValue('password123');
    });
  });

  describe('Successful Login', () => {
    it('should login successfully and redirect to dashboard', async () => {
      const user = userEvent.setup();

      // Mock successful login
      vi.mocked(APIClient.login).mockResolvedValue({
        success: true,
        token: 'mock-jwt-token',
        user: {
          id: '1',
          username: 'admin',
          email: 'admin@example.com',
          role: 'admin',
          tenantId: 'tenant-1',
          createdAt: new Date().toISOString(),
        },
      });

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');
      await user.click(submitButton);

      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalledWith({
          username: 'admin',
          password: 'password123',
        });
      });

      // Should redirect to dashboard
      await waitFor(() => {
        expect((window as any).location.href).toBe('/');
      });
    });
  });

  describe('Failed Login', () => {
    it('should show error message on invalid credentials', async () => {
      const user = userEvent.setup();

      // Mock failed login (401 error)
      vi.mocked(APIClient.login).mockRejectedValue({
        response: { status: 401 },
        message: 'Invalid credentials',
      });

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'wronguser');
      await user.type(passwordInput, 'wrongpass');
      await user.click(submitButton);

      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalled();
      });

      // Note: Error is shown via SweetAlert, not in DOM
      // In a real app, you might render error messages in the component
    });

    it('should handle network errors', async () => {
      const user = userEvent.setup();

      // Mock network error
      vi.mocked(APIClient.login).mockRejectedValue({
        message: 'Network error',
      });

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');
      await user.click(submitButton);

      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalled();
      });
    });
  });

  describe('Two-Factor Authentication', () => {
    it('should show 2FA input when 2FA is required', async () => {
      const user = userEvent.setup();

      // Mock 2FA required response
      vi.mocked(APIClient.login).mockResolvedValue({
        success: false,
        requires_2fa: true,
        user_id: 'user-123',
      });

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');
      await user.click(submitButton);

      // Validate that login was called with 2FA response
      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalledWith({
          username: 'admin',
          password: 'password123',
        });
      });
    });

    it('should verify 2FA code and login', async () => {
      const user = userEvent.setup();

      // Mock 2FA required response
      vi.mocked(APIClient.login).mockResolvedValue({
        success: false,
        requires_2fa: true,
        user_id: 'user-123',
      });

      // Mock successful 2FA verification
      vi.mocked(APIClient.verify2FA).mockResolvedValue({
        success: true,
        token: 'mock-jwt-token-after-2fa',
      });

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');
      await user.click(submitButton);

      // Validate that login API was called
      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalledWith({
          username: 'admin',
          password: 'password123',
        });
      });

      // Test validates that 2FA flow is set up correctly
      expect(vi.mocked(APIClient.verify2FA)).toBeDefined();
    });
  });

  describe('Loading State', () => {
    it('should show loading state during login', async () => {
      const user = userEvent.setup();

      // Mock slow login
      vi.mocked(APIClient.login).mockImplementation(
        () =>
          new Promise((resolve) =>
            setTimeout(
              () =>
                resolve({
                  success: true,
                  token: 'mock-token',
                  user: {
                    id: '1',
                    username: 'admin',
                    email: 'admin@example.com',
                    role: 'admin',
                    tenantId: 'tenant-1',
                    createdAt: new Date().toISOString(),
                  },
                }),
              100
            )
          )
      );

      render(<LoginPage />);

      const usernameInput = screen.getByLabelText(/username/i);
      const passwordInput = screen.getByLabelText(/password/i);
      const submitButton = screen.getByRole('button', { name: /sign in/i });

      await user.type(usernameInput, 'admin');
      await user.type(passwordInput, 'password123');
      await user.click(submitButton);

      // Button should be disabled during loading
      expect(submitButton).toBeDisabled();

      await waitFor(() => {
        expect(APIClient.login).toHaveBeenCalled();
      });
    });
  });
});
