import { useEffect, useState } from 'react';
import {
  isPasswordChangePending,
  onPasswordChangePendingChange,
} from '@/lib/pendingPasswordChange';

// Tracks whether this session still owes a password change. The value is set at
// login and by any API call the server refuses for that reason, so a session
// that was marked while it was open notices on its next request.
export function usePasswordChangePending(): boolean {
  const [pending, setPending] = useState(isPasswordChangePending);

  useEffect(() => {
    return onPasswordChangePendingChange(() => setPending(isPasswordChangePending()));
  }, []);

  return pending;
}
