import React from 'react';
import { useParams, useNavigate, Navigate } from 'react-router-dom';

export default function UserSettingsPage() {
  const { user } = useParams<{ user: string }>();
  const navigate = useNavigate();
  const userId = user as string;

  React.useEffect(() => {
    if (userId) {
      navigate(`/users/${userId}`);
    }
  }, [userId, navigate]);

  // Or simply use Navigate component for immediate redirect
  if (userId) {
    return <Navigate to={`/users/${userId}`} replace />;
  }

  return <div>Redirecting...</div>;
}
