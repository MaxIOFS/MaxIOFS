import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from '@/components/providers/AuthProvider';
import { QueryProvider } from '@/components/providers/QueryProvider';
import { ThemeProvider } from '@/contexts/ThemeContext';
import { LanguageProvider } from '@/contexts/LanguageContext';
import { getBasePath } from '@/lib/basePath';
import { useAuth } from '@/hooks/useAuth';
import { AppLayout } from '@/components/layout/AppLayout';

// Extend Window interface to include BASE_PATH
declare global {
  interface Window {
    BASE_PATH?: string;
  }
}

// Pages
import Dashboard from '@/pages/index';
import Login from '@/pages/login';
import Buckets from '@/pages/buckets/index';
import BucketDetail from '@/pages/buckets/[bucket]/index';
import BucketSettings from '@/pages/buckets/[bucket]/settings';
import BucketCreate from '@/pages/buckets/create';
import Users from '@/pages/users/index';
import UserDetail from '@/pages/users/[user]/index';
import AccessKeys from '@/pages/users/access-keys';
import AuditLogs from '@/pages/audit-logs/index';
import Metrics from '@/pages/metrics/index';
import Security from '@/pages/security/index';
import Settings from '@/pages/settings/index';
import Tenants from '@/pages/tenants/index';
import About from '@/pages/about/index';
import ClusterOverview from '@/pages/cluster/Overview';
import ClusterBuckets from '@/pages/cluster/BucketReplication';
import ClusterNodes from '@/pages/cluster/Nodes';
import ClusterMigrations from '@/pages/cluster/Migrations';
import IdentityProviders from '@/pages/identity-providers/index';
import OAuthComplete from '@/pages/auth/oauth-complete';

// Error Boundary to catch React render crashes and show a recovery UI
class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { hasError: boolean; error: Error | null }
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('ErrorBoundary caught:', error, errorInfo);
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex items-center justify-center h-screen bg-gray-50 dark:bg-gray-900">
          <div className="text-center max-w-md p-8">
            <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">Something went wrong</h2>
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
              {this.state.error?.message || 'An unexpected error occurred'}
            </p>
            <button
              onClick={() => {
                this.setState({ hasError: false, error: null });
                window.location.reload();
              }}
              className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 transition-colors"
            >
              Reload page
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, isLoading } = useAuth();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-screen bg-gray-50">
        <div className="flex flex-col items-center space-y-4">
          <svg className="animate-spin h-12 w-12 text-blue-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <p className="text-sm text-gray-600">Loading...</p>
        </div>
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return <ErrorBoundary>{children}</ErrorBoundary>;
}

function App() {
  const basePath = getBasePath();
  const basename = basePath === '' ? undefined : basePath;

  return (
    <BrowserRouter basename={basename}>
      <ThemeProvider>
        <LanguageProvider>
          <QueryProvider>
            <AuthProvider>
                <Routes>
            {/* Public routes */}
            <Route path="/login" element={<Login />} />
            <Route path="/auth/oauth/complete" element={<OAuthComplete />} />

            {/* Protected routes with layout */}
            <Route
              path="/"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Dashboard />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/buckets"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Buckets />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/buckets/create"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <BucketCreate />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            {/* Tenant-scoped bucket routes (must come before global bucket routes) */}
            <Route
              path="/buckets/:tenantId/:bucket"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <BucketDetail />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/buckets/:tenantId/:bucket/settings"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <BucketSettings />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            {/* Global bucket routes (fallback) */}
            <Route
              path="/buckets/:bucket"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <BucketDetail />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/buckets/:bucket/settings"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <BucketSettings />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/users"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Users />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/users/:user"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <UserDetail />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/users/:user/settings"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <UserDetail />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/users/access-keys"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <AccessKeys />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/tenants"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Tenants />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/audit-logs"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <AuditLogs />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/metrics"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Metrics />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/security"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Security />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/settings"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Settings />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/cluster"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <ClusterOverview />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/cluster/buckets"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <ClusterBuckets />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/cluster/nodes"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <ClusterNodes />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/cluster/migrations"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <ClusterMigrations />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/identity-providers"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <IdentityProviders />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/about"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <About />
                  </AppLayout>
                </ProtectedRoute>
              }
            />

            {/* Catch all - redirect to dashboard */}
            <Route path="*" element={<Navigate to="/" replace />} />
                </Routes>
            </AuthProvider>
          </QueryProvider>
        </LanguageProvider>
      </ThemeProvider>
    </BrowserRouter>
  );
}

export default App;
