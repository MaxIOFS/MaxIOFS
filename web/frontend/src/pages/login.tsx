import { useState, FormEvent } from 'react';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import { TwoFactorInput } from '@/components/TwoFactorInput';
import { useQuery } from '@tanstack/react-query';
import type { ServerConfig } from '@/types';

export default function LoginPage() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    username: '',
    password: '',
  });
  const [focusedField, setFocusedField] = useState<string | null>(null);
  const [show2FA, setShow2FA] = useState(false);
  const [userId, setUserId] = useState<string | null>(null);

  // Get server config for version
  const { data: config } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
  });

  // Get base path from window (injected by backend)
  const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');
  const version = config?.version || 'v0.8.0-beta';

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    try {
      // Show loading indicator
      ModalManager.loading('Signing in...', 'Verifying credentials');

      const response = await APIClient.login({
        username: formData.username,
        password: formData.password,
      });

      ModalManager.close();

      // Check if 2FA is required
      if (response.requires_2fa && response.user_id) {
        setUserId(response.user_id);
        setShow2FA(true);
        setLoading(false);
        return;
      }

      if (response.success && response.token) {
        // Show welcome message (don't await - let it show while redirecting)
        ModalManager.successLogin(formData.username);

        // Redirect to dashboard using hard redirect to ensure auth state is initialized
        // Use BASE_PATH from window (injected by backend based on public_console_url)
        const basePath = (window as any).BASE_PATH || '/';
        window.location.href = basePath;
      } else {
        await ModalManager.error('Authentication error', response.error || 'Invalid credentials');
        setError(response.error || 'Login failed');
      }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (err: any) {
      ModalManager.close();

      // Handle 401 specifically for login - invalid credentials
      if (err.response?.status === 401 || err.message?.includes('401')) {
        await ModalManager.error('Invalid Credentials', 'Username or password is incorrect. Please try again.');
        setError('Username or password is incorrect');
      } else {
        await ModalManager.apiError(err);
        setError(err.message || 'Failed to login. Please check your credentials.');
      }
    } finally {
      setLoading(false);
    }
  };

  const handleVerify2FA = async (code: string) => {
    if (!userId) return;

    setLoading(true);
    setError(null);

    try {
      ModalManager.loading('Verifying...', 'Checking 2FA code');

      const response = await APIClient.verify2FA(userId, code);

      ModalManager.close();

      if (response.success && response.token) {
        // Show welcome message (don't await - let it show while redirecting)
        ModalManager.successLogin(formData.username);

        // Redirect to dashboard
        const basePath = (window as any).BASE_PATH || '/';
        window.location.href = basePath;
      } else {
        setError(response.error || 'Invalid 2FA code');
      }
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } catch (err: any) {
      ModalManager.close();
      setError(err.message || 'Invalid 2FA code. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  const handleCancel2FA = () => {
    setShow2FA(false);
    setUserId(null);
    setError(null);
  };

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setFormData({
      ...formData,
      [e.target.name]: e.target.value,
    });
  };

  const handleFocus = (field: string) => {
    setFocusedField(field);
  };

  const handleBlur = (field: string, value: string) => {
    if (!value) {
      setFocusedField(null);
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-[#465fff] via-[#6207f3] to-[#0B0723] login-wave-container relative overflow-hidden">
      {/* Animated Wave Effect - FULL PAGE */}
      <div className="login-wave" />
      <div className="login-wave login-wave-2" />
      <div className="login-wave login-wave-3" />

      {/* Content Grid Over Blue Background */}
      <div className="relative z-10 min-h-screen grid grid-cols-1 lg:grid-cols-2">
        {/* Left Side - Logo & Features */}
        <div className="hidden lg:flex items-center justify-center p-8 px-16">
          <div className="max-w-md mx-auto space-y-12">
            {/* Logo and Tagline */}
            <div className="text-center space-y-6">
              <div className="flex justify-center">
                <img
                  src={`${basePath}/assets/img/logo.png`}
                  alt="MaxIOFS"
                  className="h-32 3xl:h-40 4xl:h-48 w-auto object-contain"
                  style={{
                    filter: 'drop-shadow(0 8px 16px rgba(0, 0, 0, 0.4))'
                  }}
                />
              </div>
              <div className="text-white space-y-2">
                <p
                  className="text-xl 3xl:text-2xl 4xl:text-3xl text-blue-100 font-light"
                  style={{
                    textShadow: '0 2px 8px rgba(0, 0, 0, 0.4)'
                  }}
                >
                  High-Performance Object Storage
                </p>
                <p
                  className="text-sm text-blue-200/80 font-light"
                  style={{
                    textShadow: '0 1px 4px rgba(0, 0, 0, 0.3)'
                  }}
                >
                  S3-Compatible • Secure • Scalable
                </p>
              </div>
            </div>

            {/* Key Features - Icons Only */}
            <div className="flex justify-center gap-6">
              {/* Lightning Fast */}
              <div className="group relative">
                <div className="w-12 h-12 bg-white/10 rounded-lg flex items-center justify-center backdrop-blur-sm transition-all duration-300 group-hover:bg-white/20 group-hover:scale-110 cursor-pointer">
                  <svg className="w-7 h-7 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
                  </svg>
                </div>
                {/* Tooltip */}
                <div className="absolute -bottom-10 left-1/2 transform -translate-x-1/2 opacity-0 group-hover:opacity-100 transition-opacity duration-200 pointer-events-none whitespace-nowrap">
                  <div className="bg-gray-900 text-white text-xs px-3 py-1.5 rounded-lg shadow-lg">
                    Lightning Fast
                  </div>
                </div>
              </div>

              {/* Security */}
              <div className="group relative">
                <div className="w-12 h-12 bg-white/10 rounded-lg flex items-center justify-center backdrop-blur-sm transition-all duration-300 group-hover:bg-white/20 group-hover:scale-110 cursor-pointer">
                  <svg className="w-7 h-7 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                  </svg>
                </div>
                {/* Tooltip */}
                <div className="absolute -bottom-10 left-1/2 transform -translate-x-1/2 opacity-0 group-hover:opacity-100 transition-opacity duration-200 pointer-events-none whitespace-nowrap">
                  <div className="bg-gray-900 text-white text-xs px-3 py-1.5 rounded-lg shadow-lg">
                    Enterprise Security
                  </div>
                </div>
              </div>

              {/* S3 Compatible */}
              <div className="group relative">
                <div className="w-12 h-12 bg-white/10 rounded-lg flex items-center justify-center backdrop-blur-sm transition-all duration-300 group-hover:bg-white/20 group-hover:scale-110 cursor-pointer">
                  <svg className="w-7 h-7 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
                  </svg>
                </div>
                {/* Tooltip */}
                <div className="absolute -bottom-10 left-1/2 transform -translate-x-1/2 opacity-0 group-hover:opacity-100 transition-opacity duration-200 pointer-events-none whitespace-nowrap">
                  <div className="bg-gray-900 text-white text-xs px-3 py-1.5 rounded-lg shadow-lg">
                    98% S3 Compatible
                  </div>
                </div>
              </div>

              {/* Cluster */}
              <div className="group relative">
                <div className="w-12 h-12 bg-white/10 rounded-lg flex items-center justify-center backdrop-blur-sm transition-all duration-300 group-hover:bg-white/20 group-hover:scale-110 cursor-pointer">
                  <svg className="w-7 h-7 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
                  </svg>
                </div>
                {/* Tooltip */}
                <div className="absolute -bottom-10 left-1/2 transform -translate-x-1/2 opacity-0 group-hover:opacity-100 transition-opacity duration-200 pointer-events-none whitespace-nowrap">
                  <div className="bg-gray-900 text-white text-xs px-3 py-1.5 rounded-lg shadow-lg">
                    Multi-Node Cluster
                  </div>
                </div>
              </div>
            </div>

            {/* Version & Website Info */}
            <div className="flex justify-center">
              <a
                href="https://maxiofs.com"
                target="_blank"
                rel="noopener noreferrer"
                className="group inline-flex items-center gap-3 px-6 py-3 rounded-full bg-white/10 backdrop-blur-sm border border-white/20 hover:bg-white/15 hover:border-white/30 transition-all duration-300 hover:scale-105"
              >
                <div className="flex items-center gap-2">
                  <div className="w-1.5 h-1.5 bg-green-400 rounded-full animate-pulse" />
                  <span className="text-sm text-white font-medium" style={{ textShadow: '0 1px 2px rgba(0, 0, 0, 0.3)' }}>
                    {version}
                  </span>
                </div>
                <div className="w-px h-4 bg-white/30" />
                <div className="flex items-center gap-2">
                  <span className="text-sm text-white/90 group-hover:text-white font-light transition-colors" style={{ textShadow: '0 1px 2px rgba(0, 0, 0, 0.3)' }}>
                    maxiofs.com
                  </span>
                  <svg className="w-3.5 h-3.5 text-white/60 group-hover:text-white group-hover:translate-x-0.5 transition-all duration-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" />
                  </svg>
                </div>
              </a>
            </div>
          </div>
        </div>

        {/* Right Side - Login Form Card */}
        <div className="flex items-center justify-center p-6 sm:p-8 lg:p-12">
          {/* Mobile Logo */}
          <div className="lg:hidden absolute top-8 left-1/2 transform -translate-x-1/2">
            <img
              src={`${basePath}/assets/img/logo.png`}
              alt="MaxIOFS"
              className="h-16 w-auto object-contain"
              style={{
                filter: 'drop-shadow(0 4px 8px rgba(0, 0, 0, 0.4))'
              }}
            />
          </div>

          {/* Login Card */}
          <div className="w-full max-w-md 3xl:max-w-lg 4xl:max-w-xl mt-20 lg:mt-0">
            <div className="relative bg-white/95 dark:bg-gray-900/90 backdrop-blur-xl rounded-[2rem] shadow-2xl p-8 sm:p-10 border border-white/20 dark:border-white/10">
              {/* Gradient overlay for dark mode */}
              <div className="absolute inset-0 bg-gradient-to-br from-blue-500/5 via-purple-500/5 to-indigo-500/5 dark:from-blue-400/10 dark:via-purple-400/10 dark:to-indigo-400/10 rounded-[2rem] pointer-events-none" />

              {/* Content wrapper */}
              <div className="relative z-10">
              {/* Show 2FA Input if required */}
              {show2FA ? (
                <TwoFactorInput
                  onSubmit={handleVerify2FA}
                  onCancel={handleCancel2FA}
                  loading={loading}
                  error={error}
                />
              ) : (
                <>
                  {/* Header */}
                  <div className="text-center">
                    <h1 className="text-4xl font-light text-gray-900 dark:text-white mb-2 bg-gradient-to-br from-gray-900 via-gray-800 to-gray-900 dark:from-white dark:via-blue-100 dark:to-white bg-clip-text text-transparent">
                      Web Console
                    </h1>
                    <p className="text-sm text-gray-600 dark:text-gray-300">
                      Sign in to access your object storage
                    </p>
                  </div>

                  {/* Login Form */}
                  <form onSubmit={handleSubmit} className="space-y-6 mt-8">
                    {error && (
                      <div className="rounded-lg bg-red-50 dark:bg-red-500/10 p-4 border-l-4 border-red-500 dark:border-red-400 backdrop-blur-sm">
                        <div className="text-sm text-red-800 dark:text-red-200 font-medium">{error}</div>
                      </div>
                    )}

                    {/* Username Input */}
                    <div className="relative">
                      <div className="relative">
                        <svg
                          className="absolute left-0 top-5 h-6 w-6 transition-colors duration-200"
                          style={{
                            color: focusedField === 'username' || formData.username ? '#2563eb' : '#9ca3af'
                          }}
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                        >
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
                        </svg>
                        <input
                          id="username"
                          name="username"
                          type="text"
                          required
                          className="peer w-full pl-8 pr-4 py-3 pt-6 pb-2 border-b-2 border-gray-300 dark:border-gray-500 bg-transparent text-gray-900 dark:text-white placeholder-transparent focus:outline-none focus:border-blue-600 dark:focus:border-blue-400 transition-colors duration-200 hover:bg-gray-50 dark:hover:bg-white/5"
                          placeholder="Username"
                          value={formData.username}
                          onChange={handleChange}
                          onFocus={() => handleFocus('username')}
                          onBlur={(e) => handleBlur('username', e.target.value)}
                          disabled={loading}
                        />
                        <label
                          htmlFor="username"
                          className="absolute left-8 text-sm font-bold transition-all duration-200 pointer-events-none"
                          style={{
                            top: focusedField === 'username' || formData.username ? '0' : '1.25rem',
                            fontSize: focusedField === 'username' || formData.username ? '0.75rem' : '1rem',
                            color: focusedField === 'username' || formData.username ? '#2563eb' : '#9ca3af'
                          }}
                        >
                          Username
                        </label>
                      </div>
                    </div>

                    {/* Password Input */}
                    <div className="relative">
                      <div className="relative">
                        <svg
                          className="absolute left-0 top-5 h-6 w-6 transition-colors duration-200"
                          style={{
                            color: focusedField === 'password' || formData.password ? '#2563eb' : '#9ca3af'
                          }}
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                        >
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                        </svg>
                        <input
                          id="password"
                          name="password"
                          type="password"
                          required
                          className="peer w-full pl-8 pr-4 py-3 pt-6 pb-2 border-b-2 border-gray-300 dark:border-gray-500 bg-transparent text-gray-900 dark:text-white placeholder-transparent focus:outline-none focus:border-blue-600 dark:focus:border-blue-400 transition-colors duration-200 hover:bg-gray-50 dark:hover:bg-white/5"
                          placeholder="Password"
                          value={formData.password}
                          onChange={handleChange}
                          onFocus={() => handleFocus('password')}
                          onBlur={(e) => handleBlur('password', e.target.value)}
                          disabled={loading}
                        />
                        <label
                          htmlFor="password"
                          className="absolute left-8 text-sm font-bold transition-all duration-200 pointer-events-none"
                          style={{
                            top: focusedField === 'password' || formData.password ? '0' : '1.25rem',
                            fontSize: focusedField === 'password' || formData.password ? '0.75rem' : '1rem',
                            color: focusedField === 'password' || formData.password ? '#2563eb' : '#9ca3af'
                          }}
                        >
                          Password
                        </label>
                      </div>
                    </div>

                    {/* Submit Button */}
                    <div className="pt-4">
                      <button
                        type="submit"
                        disabled={loading}
                        className="w-full py-3 px-6 rounded-full text-lg font-medium text-white bg-gradient-to-r from-blue-600 to-blue-700 dark:from-blue-500 dark:to-blue-600 border-2 border-blue-600 dark:border-blue-400 hover:from-white hover:to-white dark:hover:from-gray-800 dark:hover:to-gray-900 hover:text-blue-600 dark:hover:text-blue-400 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 dark:focus:ring-offset-gray-900 disabled:opacity-50 disabled:cursor-not-allowed transition-all duration-300 shadow-lg hover:shadow-xl"
                      >
                        {loading ? (
                          <span className="flex items-center justify-center">
                            <svg className="animate-spin -ml-1 mr-3 h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                            </svg>
                            Signing in...
                          </span>
                        ) : (
                          'Sign In'
                        )}
                      </button>
                    </div>
                  </form>

                  {/* Footer */}
                  <div className="mt-8 pt-6 border-t border-gray-200 dark:border-white/10">
                    <div className="text-center">
                      <p className="text-xs text-gray-500 dark:text-gray-300">
                        © {new Date().getFullYear()} MaxIOFS. All rights reserved.
                      </p>
                      <p className="text-xs text-gray-400 dark:text-gray-400 mt-1">
                        High-Performance Object Storage Solution
                      </p>
                    </div>
                  </div>
                </>
              )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
