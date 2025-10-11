import React from 'react';
import { useRouter } from 'next/router';

export default function UserSettingsPage() {
  const router = useRouter();
  const { user } = router.query;
  const userId = user as string;

  React.useEffect(() => {
    router.push(`/users/${userId}`);
  }, [userId, router]);

  return <div>Redirecting...</div>;
}
