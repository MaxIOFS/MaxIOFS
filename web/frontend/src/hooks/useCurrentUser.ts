import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User } from '@/types';

export function useCurrentUser() {
  const { data: user, isLoading, error } = useQuery<User>({
    queryKey: ['currentUser'],
    queryFn: APIClient.getCurrentUser,
    staleTime: 5 * 60 * 1000, // 5 minutes
    retry: false,
  });

  const isGlobalAdmin = user?.roles?.includes('admin') && !user?.tenantId;
  const isTenantAdmin = user?.roles?.includes('admin') && !!user?.tenantId;
  const isTenantUser = !!user?.tenantId;

  return {
    user,
    isLoading,
    error,
    isGlobalAdmin,
    isTenantAdmin,
    isTenantUser,
    isAuthenticated: !!user,
  };
}
