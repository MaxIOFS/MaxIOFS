import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { User } from '@/types';

// adminOf is what the signed-in user administers, as the server says: the
// admin or tenant-admin role or the TenantAdmin permission make a tenant's
// administrator, and only the server knows which the user holds.
export function adminOf(user?: User | null) {
  const isGlobalAdmin = user?.isGlobalAdmin ?? false;
  const isTenantAdmin = (user?.isAdmin ?? false) && !isGlobalAdmin;
  return { isGlobalAdmin, isTenantAdmin, isAnyAdmin: isGlobalAdmin || isTenantAdmin };
}

// hasAdminRole reports whether roles make a user an administrator: admin, or
// tenant-admin for a user of a tenant. A permission can make one too, which
// only the signed-in user's adminOf knows.
export function hasAdminRole(roles?: string[], tenantId?: string) {
  return !!roles && (roles.includes('admin') || (!!tenantId && roles.includes('tenant-admin')));
}

export function useCurrentUser() {
  const { data: user, isLoading, error } = useQuery<User>({
    queryKey: ['currentUser'],
    queryFn: APIClient.getCurrentUser,
    staleTime: 5 * 60 * 1000, // 5 minutes
    retry: false,
  });

  const { isGlobalAdmin, isTenantAdmin } = adminOf(user);
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
