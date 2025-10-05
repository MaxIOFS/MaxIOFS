'use client';

import { usePathname } from 'next/navigation';
import { useAuth } from '@/hooks/useAuth';
import { AppLayout } from './AppLayout';
import { Loading } from '@/components/ui/Loading';

export function ConditionalLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { isAuthenticated, isLoading } = useAuth();

  // Public routes that don't need layout
  const isPublicRoute = pathname === '/login';

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

  // For private routes, show content if authenticated, otherwise show loading
  // The middleware will handle redirects to login
  if (!isAuthenticated) {
    return (
      <div className="flex items-center justify-center h-screen">
        <Loading size="lg" />
      </div>
    );
  }

  // Render with app layout (sidebar + top bar) for authenticated routes
  return <AppLayout>{children}</AppLayout>;
}
