import { vi } from 'vitest';

// Mock window.location
export const mockLocation = {
  href: '',
  pathname: '/',
  search: '',
  hash: '',
  reload: vi.fn(),
  replace: vi.fn(),
};

// Setup window mocks
export const setupWindowMocks = () => {
  // Mock BASE_PATH
  (window as any).BASE_PATH = '/';

  // Mock location
  delete (window as any).location;
  (window as any).location = mockLocation;
};

// Reset window mocks
export const resetWindowMocks = () => {
  mockLocation.href = '';
  mockLocation.pathname = '/';
  mockLocation.reload.mockClear();
  mockLocation.replace.mockClear();
};
