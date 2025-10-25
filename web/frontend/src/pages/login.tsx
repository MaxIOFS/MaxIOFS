import { useState, FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import APIClient from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

export default function LoginPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    username: '',
    password: '',
  });
  const [focusedField, setFocusedField] = useState<string | null>(null);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    try {
      // Show loading indicator
      SweetAlert.loading('Signing in...', 'Verifying credentials');

      const response = await APIClient.login({
        username: formData.username,
        password: formData.password,
      });

      if (response.success && response.token) {
        // Close loading modal
        SweetAlert.close();

        // Show welcome message (don't await - let it show while redirecting)
        SweetAlert.successLogin(formData.username);

        // Redirect to dashboard using hard redirect to ensure auth state is initialized
        window.location.href = '/';
      } else {
        SweetAlert.close();
        await SweetAlert.error('Authentication error', response.error || 'Invalid credentials');
        setError(response.error || 'Login failed');
      }
    } catch (err: any) {
      SweetAlert.close();
      await SweetAlert.apiError(err);
      setError(err.message || 'Failed to login. Please check your credentials.');
    } finally {
      setLoading(false);
    }
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
    <div className="min-h-screen grid grid-cols-1 lg:grid-cols-2">
      {/* Left Side - Brand & Waves */}
      <div className="hidden lg:flex items-center justify-center bg-gradient-to-br from-blue-600 via-blue-700 to-gray-800 relative overflow-hidden">
        {/* Decorative waves pattern */}
        <div className="absolute inset-0 opacity-10">
          <svg className="absolute bottom-0 w-full h-full" preserveAspectRatio="none" viewBox="0 0 1200 600">
            <path
              d="M0,300 Q300,450 600,300 T1200,300 L1200,600 L0,600 Z"
              fill="white"
              opacity="0.1"
            />
            <path
              d="M0,350 Q300,500 600,350 T1200,350 L1200,600 L0,600 Z"
              fill="white"
              opacity="0.05"
            />
          </svg>
        </div>

        {/* Logo */}
        <div className="relative z-10 text-center space-y-6 px-8">
          <div className="flex justify-center">
            <img
              src="/assets/img/logo.png"
              alt="MaxIOFS"
              className="h-32 w-auto object-contain drop-shadow-2xl"
            />
          </div>
          <div className="text-white space-y-2">
            <p className="text-xl text-blue-200">High-Performance Object Storage</p>
          </div>
        </div>
      </div>

      {/* Right Side - Login Form */}
      <div className="flex items-center justify-center bg-white dark:bg-gray-900 p-8">
        <div className="w-full max-w-md space-y-8">
          {/* Mobile Logo */}
          <div className="lg:hidden text-center mb-8">
            <div className="flex justify-center mb-4">
              <img
                src="/assets/img/logo.png"
                alt="MaxIOFS"
                className="h-20 w-auto object-contain"
              />
            </div>
          </div>

          {/* Header */}
          <div className="text-center">
            <h1 className="text-4xl font-light text-gray-900 dark:text-white mb-2">
              Web Console
            </h1>
            <p className="text-sm text-gray-600 dark:text-gray-400">
              Sign in to access your object storage
            </p>
          </div>

          {/* Login Form */}
          <form onSubmit={handleSubmit} className="space-y-6 mt-8">
            {error && (
              <div className="rounded-lg bg-red-50 dark:bg-red-900/20 p-4 border-l-4 border-red-500">
                <div className="text-sm text-red-800 dark:text-red-200">{error}</div>
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
                  className="peer w-full pl-8 pr-4 py-3 pt-6 pb-2 border-b-2 border-gray-300 dark:border-gray-600 bg-transparent text-gray-900 dark:text-white placeholder-transparent focus:outline-none focus:border-blue-600 dark:focus:border-blue-500 transition-colors duration-200 hover:bg-gray-50 dark:hover:bg-gray-800"
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
                  className="peer w-full pl-8 pr-4 py-3 pt-6 pb-2 border-b-2 border-gray-300 dark:border-gray-600 bg-transparent text-gray-900 dark:text-white placeholder-transparent focus:outline-none focus:border-blue-600 dark:focus:border-blue-500 transition-colors duration-200 hover:bg-gray-50 dark:hover:bg-gray-800"
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
                className="w-full py-3 px-6 rounded-full text-lg font-medium text-white bg-blue-600 dark:bg-blue-500 border-2 border-blue-600 dark:border-blue-500 hover:bg-white dark:hover:bg-gray-900 hover:text-blue-600 dark:hover:text-blue-500 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed transition-all duration-300"
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
          <div className="mt-8 pt-6 border-t border-gray-200 dark:border-gray-700">
            <div className="text-center">
              <p className="text-xs text-gray-500 dark:text-gray-400">
                © {new Date().getFullYear()} MaxIOFS. All rights reserved.
              </p>
              <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
                High-Performance Object Storage Solution
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
