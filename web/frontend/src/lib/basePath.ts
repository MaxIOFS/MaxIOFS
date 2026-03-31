/**
 * Central base path resolution for the Console.
 * The backend injects window.BASE_PATH based on public_console_url.
 * Use getBasePath() in non-React code; use useBasePath() in components.
 */
export function getBasePath(): string {
  if (typeof window !== 'undefined') {
    return (window.BASE_PATH || '/').replace(/\/$/, '');
  }
  return '';
}

/** Whether the current location is the login route (works when the console is under a subpath, e.g. /ui). */
export function isLoginPath(): boolean {
  if (typeof window === 'undefined') {
    return false;
  }
  const path = window.location.pathname.replace(/\/$/, '') || '/';
  const base = getBasePath();
  const login = base ? `${base}/login` : '/login';
  return path === login;
}
