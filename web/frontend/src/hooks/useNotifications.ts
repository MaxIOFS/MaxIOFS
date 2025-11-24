import { useState, useEffect, useCallback } from 'react';

export interface Notification {
  id: string;
  type: string;
  message: string;
  data: {
    userId?: string;
    username?: string;
    tenantId?: string;
  };
  timestamp: number;
  tenantId?: string;
  read: boolean;
}

// Helper to get base path
const getBasePath = () => {
  if (typeof window !== 'undefined') {
    return ((window as any).BASE_PATH || '/').replace(/\/$/, '');
  }
  return '';
};

export function useNotifications() {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [connected, setConnected] = useState(false);
  const [token, setToken] = useState<string | null>(null);

  // Check for token changes periodically
  useEffect(() => {
    const checkToken = () => {
      const currentToken = localStorage.getItem('auth_token');
      setToken(currentToken);
    };

    // Check immediately
    checkToken();

    // Check every second for token changes (after login)
    const interval = setInterval(checkToken, 1000);

    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    // Load notifications from localStorage on mount
    const stored = localStorage.getItem('notifications');
    if (stored) {
      try {
        const parsed = JSON.parse(stored);
        setNotifications(parsed);
      } catch (e) {
        // Ignore parse errors
      }
    }

    // Get auth token
    if (!token) {
      return;
    }

    let aborted = false;
    const controller = new AbortController();

    // Connect to SSE endpoint using fetch (supports Authorization header)
    const connectSSE = async () => {
      try {
        const baseURL = `${getBasePath()}/api/v1`;
        const url = `${baseURL}/notifications/stream`;
        const response = await fetch(url, {
          headers: {
            'Authorization': `Bearer ${token}`,
            'Accept': 'text/event-stream',
          },
          signal: controller.signal,
        });

        if (!response.ok) {
          throw new Error(`SSE connection failed: ${response.status}`);
        }

        setConnected(true);

        const reader = response.body?.getReader();
        const decoder = new TextDecoder();

        if (!reader) {
          throw new Error('No reader available');
        }

        // Read stream - accumulate buffer for incomplete messages
        let buffer = '';
        while (!aborted) {
          const { done, value } = await reader.read();

          if (done) {
            break;
          }

          // Decode chunk and add to buffer
          const chunk = decoder.decode(value, { stream: true });
          buffer += chunk;

          // Process complete messages (separated by double newline)
          const messages = buffer.split('\n\n');

          // Keep last incomplete message in buffer
          buffer = messages.pop() || '';

          for (const message of messages) {
            if (!message.trim()) continue;

            const lines = message.split('\n');
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const data = line.substring(6).trim();
                try {
                  const parsed = JSON.parse(data);

                  // Skip connection message
                  if (parsed.type === 'connected') {
                    continue;
                  }

                  // Create notification with unique ID and read state
                  const notification: Notification = {
                    id: `${parsed.type}-${parsed.timestamp}`,
                    type: parsed.type,
                    message: parsed.message,
                    data: parsed.data || {},
                    timestamp: parsed.timestamp,
                    tenantId: parsed.tenantId,
                    read: false,
                  };

                  setNotifications((prev) => {
                    const updated = [notification, ...prev];
                    // Keep only last 3 notifications
                    const trimmed = updated.slice(0, 3);
                    // Save to localStorage
                    localStorage.setItem('notifications', JSON.stringify(trimmed));
                    return trimmed;
                  });
                } catch (error) {
                  // Ignore parse errors
                }
              }
            }
          }
        }
      } catch (error) {
        if (!aborted) {
          setConnected(false);
        }
      }
    };

    connectSSE();

    return () => {
      aborted = true;
      controller.abort();
      setConnected(false);
    };
  }, [token]);

  const markAsRead = useCallback((notificationId: string) => {
    setNotifications((prev) => {
      const updated = prev.map((n) =>
        n.id === notificationId ? { ...n, read: true } : n
      );
      localStorage.setItem('notifications', JSON.stringify(updated));
      return updated;
    });
  }, []);

  const markAllAsRead = useCallback(() => {
    setNotifications((prev) => {
      const updated = prev.map((n) => ({ ...n, read: true }));
      localStorage.setItem('notifications', JSON.stringify(updated));
      return updated;
    });
  }, []);

  const clearNotification = useCallback((notificationId: string) => {
    setNotifications((prev) => {
      const updated = prev.filter((n) => n.id !== notificationId);
      localStorage.setItem('notifications', JSON.stringify(updated));
      return updated;
    });
  }, []);

  const unreadCount = notifications.filter((n) => !n.read).length;

  return {
    notifications,
    unreadCount,
    connected,
    markAsRead,
    markAllAsRead,
    clearNotification,
  };
}
