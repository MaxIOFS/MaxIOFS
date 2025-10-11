import { useState, FormEvent } from 'react';
import { useRouter } from 'next/router';
import APIClient from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

export default function LoginPage() {
  const router = useRouter();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [formData, setFormData] = useState({
    username: '',
    password: '',
  });

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

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-blue-50 to-indigo-100">
      <div className="max-w-md w-full space-y-4 p-8 bg-white rounded-xl shadow-lg">
        <div className="text-center">
          {/* Logo */}
          <div className="flex justify-center mb-3">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/assets/img/logo.png"
              alt="MaxIOFS Logo"
              className="h-auto object-contain"
              style={{ width: '22rem', maxHeight: '200px' }}
            />
          </div>
          <h2 className="text-3xl font-extrabold text-gray-900">
            Web Console
          </h2>
          <p className="mt-2 text-sm text-gray-600">
            Sign in to access your object storage
          </p>
        </div>

        <form className="mt-6 space-y-6" onSubmit={handleSubmit}>
          {error && (
            <div className="rounded-md bg-red-50 p-4">
              <div className="text-sm text-red-800">{error}</div>
            </div>
          )}

          <div className="rounded-md shadow-sm -space-y-px">
            <div>
              <label htmlFor="username" className="sr-only">
                Username
              </label>
              <input
                id="username"
                name="username"
                type="text"
                required
                className="appearance-none rounded-none relative block w-full px-3 py-2 border border-gray-300 placeholder-gray-500 text-gray-900 rounded-t-md focus:outline-none focus:ring-blue-500 focus:border-blue-500 focus:z-10 sm:text-sm"
                placeholder="Username"
                value={formData.username}
                onChange={handleChange}
                disabled={loading}
              />
            </div>
            <div>
              <label htmlFor="password" className="sr-only">
                Password
              </label>
              <input
                id="password"
                name="password"
                type="password"
                required
                className="appearance-none rounded-none relative block w-full px-3 py-2 border border-gray-300 placeholder-gray-500 text-gray-900 rounded-b-md focus:outline-none focus:ring-blue-500 focus:border-blue-500 focus:z-10 sm:text-sm"
                placeholder="Password"
                value={formData.password}
                onChange={handleChange}
                disabled={loading}
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={loading}
              className="group relative w-full flex justify-center py-2 px-4 border border-transparent text-sm font-medium rounded-md text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {loading ? (
                <span className="flex items-center">
                  <svg className="animate-spin -ml-1 mr-3 h-5 w-5 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                  </svg>
                  Signing in...
                </span>
              ) : (
                'Sign in'
              )}
            </button>
          </div>
        </form>

        {/* Footer with copyright */}
        <div className="mt-6 pt-4 border-t border-gray-200">
          <div className="text-center">
            <p className="text-xs text-gray-500">
              © {new Date().getFullYear()} MaxIOFS. All rights reserved.
            </p>
            <p className="text-xs text-gray-400 mt-1">
              High-Performance Object Storage Solution
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
