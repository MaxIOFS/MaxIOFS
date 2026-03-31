import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { AuthProvider } from '@/components/providers/AuthProvider';
import { QueryProvider } from '@/components/providers/QueryProvider';
import { ThemeProvider } from '@/contexts/ThemeContext';
import { LanguageProvider } from '@/contexts/LanguageContext';
import { getBasePath } from '@/lib/basePath';
import { useAuth } from '@/hooks/useAuth';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { AppLayout } from '@/components/layout/AppLayout';

// Extend Window interface to include BASE_PATH
declare global {
  interface Window {
    BASE_PATH?: string;
  }
}

// Critical path — always in main bundle (first pages users land on)
import Login from '@/pages/login';
import Dashboard from '@/pages/index';
import OAuthComplete from '@/pages/auth/oauth-complete';

// Lazy-loaded pages — each becomes its own chunk, loaded on demand
const Buckets        = React.lazy(() => import('@/pages/buckets/index'));
const BucketDetail   = React.lazy(() => import('@/pages/buckets/[bucket]/index'));
const BucketSettings = React.lazy(() => import('@/pages/buckets/[bucket]/settings'));
const BucketCreate   = React.lazy(() => import('@/pages/buckets/create'));
const Users          = React.lazy(() => import('@/pages/users/index'));
const UserDetail     = React.lazy(() => import('@/pages/users/[user]/index'));
const AccessKeys     = React.lazy(() => import('@/pages/users/access-keys'));
const Groups         = React.lazy(() => import('@/pages/groups/index'));
const GroupDetail    = React.lazy(() => import('@/pages/groups/[group]/index'));
const Tenants        = React.lazy(() => import('@/pages/tenants/index'));
const AuditLogs      = React.lazy(() => import('@/pages/audit-logs/index'));
const Metrics        = React.lazy(() => import('@/pages/metrics/index'));
const Security       = React.lazy(() => import('@/pages/security/index'));
const Settings       = React.lazy(() => import('@/pages/settings/index'));
const About          = React.lazy(() => import('@/pages/about/index'));
const IdentityProviders  = React.lazy(() => import('@/pages/identity-providers/index'));
// Cluster pages grouped in a single chunk — always visited together
const ClusterOverview    = React.lazy(() => import('@/pages/cluster/Overview'));
const ClusterBuckets     = React.lazy(() => import('@/pages/cluster/BucketReplication'));
const ClusterNodes       = React.lazy(() => import('@/pages/cluster/Nodes'));
const ClusterMigrations  = React.lazy(() => import('@/pages/cluster/Migrations'));

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

function ProfileRedirect() {
  const { user } = useAuth();
  if (!user?.id) return <Navigate to="/" replace />;
  return <Navigate to={`/users/${user.id}`} replace />;
}

/**
 * /users is the admin user list (heavy lazy chunk + shared Input chunk).
 * Non-admins who open "Users" from the sidebar hit /users — same destination as
 * "My profile" (/users/:id) but previously started that lazy load + <Navigate>,
 * which raced dynamic imports and broke with "error loading dynamically imported module".
 * Redirect before mounting the list chunk so behavior matches the top bar.
 */
function UsersRoute() {
  const { isGlobalAdmin, isTenantAdmin, user, isLoading } = useCurrentUser();
  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <svg className="animate-spin h-8 w-8 text-brand-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
          <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
        </svg>
      </div>
    );
  }
  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;
  if (user?.id && !isAnyAdmin) {
    return <Navigate to={`/users/${user.id}`} replace />;
  }
  return <Users />;
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
                    <UsersRoute />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            {/* Static /users/* paths must be registered before /users/:user so :user never matches "access-keys". */}
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
              path="/profile"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <ProfileRedirect />
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
              path="/groups"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <Groups />
                  </AppLayout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/groups/:group"
              element={
                <ProtectedRoute>
                  <AppLayout>
                    <GroupDetail />
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
