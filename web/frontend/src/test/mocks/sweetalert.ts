import { vi } from 'vitest';

// Mock ModalManager
export const mockModalManager = {
  fire: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  close: vi.fn(),
  loading: vi.fn(),
  success: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  error: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  warning: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  info: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirm: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirmDelete: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirmWithInput: vi.fn().mockResolvedValue({ isConfirmed: true, value: '', isDenied: false, isDismissed: false }),
  toast: vi.fn(),
  confirmDeleteBucket: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirmDeleteObject: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirmDeleteUser: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  successLogin: vi.fn(),
  successUpload: vi.fn(),
  successDownload: vi.fn(),
  successBucketCreated: vi.fn(),
  successBucketDeleted: vi.fn(),
  successUserCreated: vi.fn(),
  apiError: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  confirmLogout: vi.fn().mockResolvedValue({ isConfirmed: true, isDenied: false, isDismissed: false }),
  progress: vi.fn(),
  updateProgress: vi.fn(),
};

// Mock the ModalManager module
vi.mock('@/lib/modals', () => ({
  default: mockModalManager,
  ModalRenderer: () => null,
  ToastNotifications: () => null,
}));

export default mockModalManager;
