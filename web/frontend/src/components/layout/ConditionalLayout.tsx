'use client';

import { useRouter } from 'next/router';
import { useAuth } from '@/hooks/useAuth';
import { AppLayout } from './AppLayout';
import { Loading } from '@/components/ui/Loading';

export function ConditionalLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { isAuthenticated, isLoading } = useAuth();

  // Public routes that don't need layout
  const isPublicRoute = router.pathname === '/login';

  // Show loading while checking authentication
  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Loading size="lg" />
      </div>
    );
  }

  if (isPublicRoute) {
    // Render children directly without layout for public routes
    return <>{children}</>;
  }

  // For private routes, redirect to login if not authenticated
  if (!isAuthenticated) {
    if (typeof window !== 'undefined') {
      router.push('/login');
    }
    return (
      <div className="flex items-center justify-center h-screen">
        <Loading size="lg" />
      </div>
    );
  }

  // Render with app layout (sidebar + top bar) for authenticated routes
  return <AppLayout>{children}</AppLayout>;
}
