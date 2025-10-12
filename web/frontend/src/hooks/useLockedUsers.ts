import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { User } from '@/types';

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
      const users = await APIClient.getUsers();

      // Filter users who are currently locked
      const now = Math.floor(Date.now() / 1000);
      const lockedUsers: LockedUser[] = users.filter((user: User) => {
        return user.lockedUntil && user.lockedUntil > now;
      }).map((user: User) => ({
        id: user.id,
        username: user.username,
        displayName: user.displayName || user.username,
        email: user.email || '',
        lockedUntil: user.lockedUntil || 0,
        failedAttempts: user.failedLoginAttempts || 0,
      }));

      return lockedUsers;
    },
    refetchInterval: 30000, // Refetch every 30 seconds
    staleTime: 25000, // Consider data fresh for 25 seconds
    gcTime: 60000, // Keep in cache for 1 minute
  });
}
