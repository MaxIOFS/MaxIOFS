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
