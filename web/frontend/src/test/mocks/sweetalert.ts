import { vi } from 'vitest';

// Mock SweetAlert2
export const mockSwal = {
  fire: vi.fn().mockResolvedValue({ isConfirmed: true }),
  close: vi.fn(),
  loading: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  successLogin: vi.fn(),
  apiError: vi.fn(),
  confirmDelete: vi.fn().mockResolvedValue({ isConfirmed: true }),
};

// Mock the SweetAlert module
vi.mock('@/lib/sweetalert', () => ({
  default: mockSwal,
}));

export default mockSwal;
