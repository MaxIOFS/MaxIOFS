import { useQuery } from '@tanstack/react-query';
import api from '@/lib/api';

export interface LockedUser {
  id: string;
  username: string;
  displayName: string;
  email: string;
  lockedUntil: number;
  failedAttempts: number;
}

export function useLockedUsers() {
  return useQuery({
    queryKey: ['locked-users'],
    queryFn: async () => {
      const response = await api.get('/users');
      const users = response.data.data || response.data || [];

      // Filter users who are currently locked
      const now = Math.floor(Date.now() / 1000);
      const lockedUsers: LockedUser[] = users.filter((user: any) => {
        return user.locked_until && user.locked_until > now;
      }).map((user: any) => ({
        id: user.id,
        username: user.username,
        displayName: user.display_name || user.username,
        email: user.email || '',
        lockedUntil: user.locked_until,
        failedAttempts: user.failed_login_attempts || 0,
      }));

      return lockedUsers;
    },
    refetchInterval: 30000, // Refetch every 30 seconds
    staleTime: 20000, // Consider data stale after 20 seconds
  });
}
