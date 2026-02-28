import { getBasePath } from '@/lib/basePath';

/**
 * Hook to get the base path for the Console.
 * Use in React components for assets, links, and API URLs.
 */
export function useBasePath(): string {
  return getBasePath();
}
