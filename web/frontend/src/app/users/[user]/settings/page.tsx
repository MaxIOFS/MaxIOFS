'use client';

import React from 'react';
import { useParams, useRouter } from 'next/navigation';

export default function UserSettingsPage() {
  const params = useParams();
  const router = useRouter();
  const userId = params.user as string;

  React.useEffect(() => {
    router.push(`/users/${userId}`);
  }, [userId, router]);

  return <div>Redirecting...</div>;
}
