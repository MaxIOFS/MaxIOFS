import React from 'react';
import { create } from 'zustand';
import { Modal, ConfirmModal, SuccessModal, AlertModal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { AlertTriangle, Info } from 'lucide-react';
import { cn } from '@/lib/utils';

// Simple HTML sanitizer — strips script tags, event handlers, and dangerous attributes
function sanitizeHtml(html: string): string {
  const div = document.createElement('div');
  div.innerHTML = html;
  // Remove script, iframe, object, embed, form tags
  const dangerous = div.querySelectorAll('script, iframe, object, embed, form, link, style');
  dangerous.forEach((el) => el.remove());
  // Remove event handler attributes (onclick, onerror, onload, etc.)
  const allElements = div.querySelectorAll('*');
  allElements.forEach((el) => {
    for (const attr of Array.from(el.attributes)) {
      if (attr.name.startsWith('on') || attr.value.startsWith('javascript:')) {
        el.removeAttribute(attr.name);
      }
    }
  });
  return div.innerHTML;
}

// Modal state types
type ModalType = 'confirm' | 'success' | 'error' | 'warning' | 'info' | 'confirmInput' | 'loading';

interface ModalState {
  isOpen: boolean;
  type: ModalType;
  title: string;
  message: string;
  isHtml?: boolean;
  confirmText?: string;
  cancelText?: string;
  variant?: 'danger' | 'warning' | 'info';
  onConfirm?: () => void;
  onClose?: () => void;
  expectedInput?: string;
  inputPlaceholder?: string;
  loading?: boolean;
}

interface ModalStore {
  modal: ModalState | null;
  setModal: (modal: ModalState | null) => void;
  closeModal: () => void;
}

// Zustand store for modal management
export const useModalStore = create<ModalStore>((set) => ({
  modal: null,
  setModal: (modal) => set({ modal }),
  closeModal: () => set({ modal: null }),
}));

// Toast state
interface Toast {
  id: string;
  type: 'success' | 'error' | 'warning' | 'info';
  message: string;
  duration?: number;
}

interface ToastStore {
  toasts: Toast[];
  addToast: (toast: Omit<Toast, 'id'>) => void;
  removeToast: (id: string) => void;
}

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],
  addToast: (toast) => {
    const id = Math.random().toString(36).substring(7);
    const duration = toast.duration || 3000;

    set((state) => ({
      toasts: [...state.toasts, { ...toast, id }],
    }));

    setTimeout(() => {
      set((state) => ({
        toasts: state.toasts.filter((t) => t.id !== id),
      }));
    }, duration);
  },
  removeToast: (id) =>
    set((state) => ({
      toasts: state.toasts.filter((t) => t.id !== id),
    })),
}));

// Modal Renderer Component (renders active modal)
export function ModalRenderer() {
  const { modal, closeModal } = useModalStore();
  const [inputValue, setInputValue] = React.useState('');
  const [inputError, setInputError] = React.useState('');

  if (!modal) return null;

  const handleClose = () => {
    if (modal.onClose) {
      modal.onClose();
    }
    setInputValue('');
    setInputError('');
    closeModal();
  };

  const handleConfirm = () => {
    // For input confirmation, validate first
    if (modal.type === 'confirmInput' && modal.expectedInput) {
      if (inputValue !== modal.expectedInput) {
        setInputError(`You must type exactly: ${modal.expectedInput}`);
        return;
      }
    }

    if (modal.onConfirm) {
      modal.onConfirm();
    }
    setInputValue('');
    setInputError('');
    closeModal();
  };

  // Loading modal (simple centered spinner)
  if (modal.type === 'loading') {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 dark:bg-black/80 backdrop-blur-sm">
        <div className="bg-white dark:bg-gray-900 rounded-2xl p-8 shadow-2xl border border-gray-200/50 dark:border-gray-700/50 max-w-sm">
          <div className="text-center">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-brand-600 dark:border-brand-400 mx-auto mb-4"></div>
            <h3 className="text-lg font-bold text-gray-900 dark:text-white mb-2">{modal.title}</h3>
            {modal.message && <p className="text-sm text-gray-600 dark:text-gray-400">{modal.message}</p>}
          </div>
        </div>
      </div>
    );
  }

  // Confirm with input modal
  if (modal.type === 'confirmInput') {
    return (
      <Modal
        isOpen={true}
        onClose={handleClose}
        size="sm"
        showCloseButton={false}
      >
        <div className="text-center">
          {/* Icon */}
          <div className="mx-auto flex items-center justify-center w-16 h-16 rounded-full bg-gradient-to-br from-error-100 to-red-100 dark:from-error-900/40 dark:to-red-900/40 mb-4">
            <AlertTriangle className="h-8 w-8 text-error-600 dark:text-error-400" />
          </div>

          {/* Title */}
          <h3 className="text-lg font-bold text-gray-900 dark:text-white mb-2">
            {modal.title}
          </h3>

          {/* Content */}
          <div className="text-left space-y-4 mb-6">
            <p className="text-sm text-gray-600 dark:text-gray-400">{modal.message}</p>
            {modal.expectedInput && (
              <p className="text-sm text-gray-600 dark:text-gray-400">
                To confirm, type: <strong className="text-gray-900 dark:text-white">{modal.expectedInput}</strong>
              </p>
            )}
            <input
              type="text"
              value={inputValue}
              onChange={(e) => {
                setInputValue(e.target.value);
                setInputError('');
              }}
              placeholder={modal.inputPlaceholder || 'Type here...'}
              className="w-full px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-gray-900 dark:text-white focus:ring-2 focus:ring-brand-500 focus:border-brand-500"
              autoFocus
            />
            {inputError && (
              <p className="text-sm text-error-600 dark:text-error-400">{inputError}</p>
            )}
          </div>

          {/* Actions */}
          <div className="flex gap-3">
            <Button
              variant="outline"
              onClick={handleClose}
              disabled={modal.loading}
              className="flex-1 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              {modal.cancelText || 'Cancel'}
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirm}
              loading={modal.loading}
              className="flex-1"
            >
              {modal.confirmText || 'Confirm'}
            </Button>
          </div>
        </div>
      </Modal>
    );
  }

  // Success modal
  if (modal.type === 'success') {
    return (
      <SuccessModal
        isOpen={true}
        onClose={handleClose}
        title={modal.title}
        message={modal.message}
        buttonText={modal.confirmText}
      />
    );
  }

  // Error modal
  if (modal.type === 'error') {
    return (
      <AlertModal
        isOpen={true}
        onClose={handleClose}
        title={modal.title}
        message={modal.message}
        buttonText={modal.confirmText}
      />
    );
  }

  // Confirm modals (warning, info)
  const variant = modal.type === 'warning' ? 'warning' : modal.type === 'info' ? 'info' : 'danger';

  // If it has HTML content, render a custom modal
  if (modal.isHtml) {
    const variantConfig = {
      danger: {
        icon: AlertTriangle,
        iconBg: 'bg-gradient-to-br from-error-100 to-red-100 dark:from-error-900/40 dark:to-red-900/40',
        iconColor: 'text-error-600 dark:text-error-400',
      },
      warning: {
        icon: AlertTriangle,
        iconBg: 'bg-gradient-to-br from-warning-100 to-amber-100 dark:from-warning-900/40 dark:to-amber-900/40',
        iconColor: 'text-warning-600 dark:text-warning-400',
      },
      info: {
        icon: Info,
        iconBg: 'bg-gradient-to-br from-brand-100 to-blue-100 dark:from-brand-900/40 dark:to-blue-900/40',
        iconColor: 'text-brand-600 dark:text-brand-400',
      },
    };

    const config = variantConfig[variant];
    const Icon = config.icon;

    return (
      <Modal
        isOpen={true}
        onClose={handleClose}
        size="sm"
        showCloseButton={false}
      >
        <div className="text-center">
          {/* Icon */}
          <div className={cn('mx-auto flex items-center justify-center w-16 h-16 rounded-full mb-4', config.iconBg)}>
            <Icon className={cn('h-8 w-8', config.iconColor)} />
          </div>

          {/* Title */}
          <h3 className="text-lg font-bold text-gray-900 dark:text-white mb-2">
            {modal.title}
          </h3>

          {/* HTML Message */}
          <div
            className="text-sm text-gray-600 dark:text-gray-400 mb-6"
            dangerouslySetInnerHTML={{ __html: sanitizeHtml(modal.message) }}
          />

          {/* Actions */}
          <div className="flex gap-3">
            <Button
              variant="outline"
              onClick={handleClose}
              disabled={modal.loading}
              className="flex-1 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
            >
              {modal.cancelText || 'Cancel'}
            </Button>
            <Button
              variant={variant === 'danger' ? 'destructive' : 'default'}
              onClick={handleConfirm}
              loading={modal.loading}
              className="flex-1"
            >
              {modal.confirmText || 'Confirm'}
            </Button>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <ConfirmModal
      isOpen={true}
      onClose={handleClose}
      onConfirm={handleConfirm}
      title={modal.title}
      message={modal.message}
      confirmText={modal.confirmText || 'Confirm'}
      cancelText={modal.cancelText || 'Cancel'}
      variant={variant}
      loading={modal.loading}
    />
  );
}

// Toast Notification Component
export function ToastNotifications() {
  const { toasts, removeToast } = useToastStore();

  return (
    <div className="fixed top-4 right-4 z-50 space-y-2">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={`
            flex items-center gap-3 px-4 py-3 rounded-xl shadow-lg border backdrop-blur-xl
            animate-in slide-in-from-right duration-300
            ${
              toast.type === 'success'
                ? 'bg-success-50/95 dark:bg-success-900/95 border-success-200 dark:border-success-800 text-success-800 dark:text-success-200'
                : toast.type === 'error'
                ? 'bg-error-50/95 dark:bg-error-900/95 border-error-200 dark:border-error-800 text-error-800 dark:text-error-200'
                : toast.type === 'warning'
                ? 'bg-warning-50/95 dark:bg-warning-900/95 border-warning-200 dark:border-warning-800 text-warning-800 dark:text-warning-200'
                : 'bg-brand-50/95 dark:bg-brand-900/95 border-brand-200 dark:border-brand-800 text-brand-800 dark:text-brand-200'
            }
          `}
        >
          <div className="flex-shrink-0">
            {toast.type === 'success' && (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
              </svg>
            )}
            {toast.type === 'error' && (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clipRule="evenodd" />
              </svg>
            )}
            {toast.type === 'warning' && (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
              </svg>
            )}
            {toast.type === 'info' && (
              <svg className="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
                <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
              </svg>
            )}
          </div>
          <p className="text-sm font-medium flex-1">{toast.message}</p>
          <button
            onClick={() => removeToast(toast.id)}
            className="flex-shrink-0 opacity-60 hover:opacity-100 transition-opacity"
          >
            <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>
      ))}
    </div>
  );
}

// Wrapper functions that match SweetAlert API
class ModalManager {
  private static setModal = useModalStore.getState().setModal;
  private static closeModal = useModalStore.getState().closeModal;
  private static addToast = useToastStore.getState().addToast;

  // Show success message
  static async success(title: string, text?: string): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'success',
        title,
        message: text || '',
        onClose: () => resolve({ isConfirmed: true }),
      });
    });
  }

  // Show error message
  static async error(title: string, text?: string): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'error',
        title,
        message: text || '',
        confirmText: 'Understood',
        onClose: () => resolve({ isConfirmed: true }),
      });
    });
  }

  // Show warning message
  static async warning(title: string, text?: string): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'warning',
        title,
        message: text || '',
        confirmText: 'Understood',
        cancelText: 'Cancel',
        onConfirm: () => resolve({ isConfirmed: true }),
        onClose: () => resolve({ isConfirmed: false }),
      });
    });
  }

  // Show info message
  static async info(title: string, text?: string): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'info',
        title,
        message: text || '',
        confirmText: 'Understood',
        cancelText: 'Cancel',
        onConfirm: () => resolve({ isConfirmed: true }),
        onClose: () => resolve({ isConfirmed: false }),
      });
    });
  }

  // Generic confirmation dialog
  static async confirm(
    title: string,
    text: string,
    onConfirm?: () => void,
    options?: any
  ): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'warning',
        title,
        message: text,
        confirmText: options?.confirmButtonText || 'Yes, confirm',
        cancelText: options?.cancelButtonText || 'Cancel',
        variant: options?.icon === 'warning' ? 'warning' : 'warning',
        onConfirm: () => {
          if (onConfirm) onConfirm();
          resolve({ isConfirmed: true });
        },
        onClose: () => resolve({ isConfirmed: false }),
      });
    });
  }

  // Confirm a destructive action
  static async confirmDelete(itemName: string, itemType: string = 'item'): Promise<{ isConfirmed: boolean; isDenied?: boolean; isDismissed?: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'confirm',
        title: `Delete ${itemType}?`,
        message: `You are about to delete "${itemName}"\n\nThis action cannot be undone`,
        confirmText: 'Yes, delete',
        cancelText: 'Cancel',
        variant: 'danger',
        onConfirm: () => resolve({ isConfirmed: true, isDenied: false, isDismissed: false }),
        onClose: () => resolve({ isConfirmed: false, isDenied: false, isDismissed: true }),
      });
    });
  }

  // Confirm an action with text input
  static async confirmWithInput(
    title: string,
    text: string,
    expectedInput: string,
    inputPlaceholder: string = 'Type here...'
  ): Promise<{ isConfirmed: boolean; value?: string }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'confirmInput',
        title,
        message: text,
        expectedInput,
        inputPlaceholder,
        confirmText: 'Confirm',
        cancelText: 'Cancel',
        variant: 'danger',
        onConfirm: () => resolve({ isConfirmed: true, value: expectedInput }),
        onClose: () => resolve({ isConfirmed: false }),
      });
    });
  }

  // Show loading state
  static async loading(title: string = 'Loading...', text?: string): Promise<void> {
    this.setModal({
      isOpen: true,
      type: 'loading',
      title,
      message: text || '',
    });
  }

  // Close any open modal
  static close(): void {
    this.closeModal();
  }

  // Show toast (light notification)
  static async toast(
    type: 'success' | 'error' | 'warning' | 'info',
    message: string,
    duration: number = 3000
  ): Promise<void> {
    this.addToast({ type, message, duration });
  }

  // Specific confirmation for bucket deletion
  static async confirmDeleteBucket(bucketName: string): Promise<{ isConfirmed: boolean }> {
    return this.confirmWithInput(
      '⚠️ Delete Bucket',
      `You are about to permanently delete bucket "${bucketName}" and all its data.`,
      bucketName,
      'Bucket name'
    );
  }

  // Confirmation for object deletion
  static async confirmDeleteObject(objectName: string): Promise<{ isConfirmed: boolean }> {
    return this.confirmDelete(objectName, 'object');
  }

  // Confirmation for user deletion
  static async confirmDeleteUser(username: string): Promise<{ isConfirmed: boolean }> {
    return this.confirmDelete(username, 'user');
  }

  // Success notifications for common operations
  static async successUpload(fileName: string): Promise<void> {
    return this.toast('success', `File "${fileName}" uploaded successfully`);
  }

  static async successDownload(fileName: string): Promise<void> {
    return this.toast('success', `File "${fileName}" downloaded successfully`);
  }

  static async successBucketCreated(bucketName: string): Promise<void> {
    return this.toast('success', `Bucket "${bucketName}" created successfully`);
  }

  static async successBucketDeleted(bucketName: string): Promise<void> {
    return this.toast('success', `Bucket "${bucketName}" deleted successfully`);
  }

  static async successUserCreated(username: string): Promise<void> {
    return this.toast('success', `User "${username}" created successfully`);
  }

  // Handling common API errors
  static async apiError(error: any): Promise<{ isConfirmed: boolean }> {
    let title = 'Operation error';
    let text = 'An unexpected error has occurred';

    // Try different properties where the error message might be
    if (error?.details?.error) {
      text = error.details.error;
    } else if (error?.details?.Error) {
      text = error.details.Error;
    } else if (error?.response?.data?.error) {
      text = error.response.data.error;
    } else if (error?.response?.data?.Error) {
      text = error.response.data.Error;
    } else if (error?.message) {
      text = error.message;
    } else if (typeof error === 'string') {
      text = error;
    }

    // Customize messages for common errors
    if (text.includes('404')) {
      title = 'Resource not found';
      text = 'The requested resource does not exist or has been deleted';
    } else if (text.includes('401') || text.includes('unauthorized')) {
      title = 'Unauthorized';
      text = 'Your session has expired. Please sign in again';
    } else if (text.includes('403') || text.includes('forbidden') || text.includes('Forbidden')) {
      title = 'Access denied';
      if (!text.includes('retention') && !text.includes('COMPLIANCE') && !text.includes('GOVERNANCE') && !text.includes('protected by')) {
        text = 'You do not have permission to perform this operation';
      }
    } else if (text.includes('already exists')) {
      title = 'Duplicate entry';
    } else if (text.includes('500')) {
      title = 'Server error';
      text = 'An internal server error has occurred. Please try again later';
    } else if (text.includes('network') || text.includes('Network')) {
      title = 'Connection error';
      text = 'Could not connect to the server. Check your internet connection';
    }

    return this.error(title, text);
  }

  // Show successful login confirmation
  static async successLogin(username: string): Promise<void> {
    return this.toast('success', `Welcome, ${username}!`);
  }

  // Logout confirmation
  static async confirmLogout(): Promise<{ isConfirmed: boolean }> {
    return new Promise((resolve) => {
      this.setModal({
        isOpen: true,
        type: 'info',
        title: 'Sign out?',
        message: 'Are you sure you want to sign out?',
        confirmText: 'Yes, sign out',
        cancelText: 'Cancel',
        variant: 'info',
        onConfirm: () => resolve({ isConfirmed: true }),
        onClose: () => resolve({ isConfirmed: false }),
      });
    });
  }

  // Generic fire method for compatibility with SweetAlert2
  static async fire(options: any): Promise<{ isConfirmed: boolean; isDenied?: boolean; isDismissed?: boolean; value?: any }> {
    // Extract common options
    const {
      icon,
      title,
      text,
      html,
      showCancelButton,
      confirmButtonText = 'OK',
      cancelButtonText = 'Cancel',
      input,
      inputPlaceholder,
      preConfirm,
    } = options;

    // If it has input, use confirmWithInput logic
    if (input === 'text' && preConfirm) {
      return new Promise((resolve) => {
        this.setModal({
          isOpen: true,
          type: 'confirmInput',
          title: title || '',
          message: html || text || '',
          isHtml: !!html,
          confirmText: confirmButtonText,
          cancelText: cancelButtonText,
          variant: icon === 'warning' ? 'warning' : 'danger',
          onConfirm: () => resolve({ isConfirmed: true, value: '' }),
          onClose: () => resolve({ isConfirmed: false, isDismissed: true }),
        });
      });
    }

    // For confirmation dialogs
    if (showCancelButton) {
      return new Promise((resolve) => {
        const variant = icon === 'warning' ? 'warning' : icon === 'question' ? 'info' : 'danger';
        this.setModal({
          isOpen: true,
          type: variant === 'info' ? 'info' : 'warning',
          title: title || '',
          message: html || text || '',
          isHtml: !!html,
          confirmText: confirmButtonText,
          cancelText: cancelButtonText,
          variant,
          onConfirm: () => resolve({ isConfirmed: true }),
          onClose: () => resolve({ isConfirmed: false, isDismissed: true }),
        });
      });
    }

    // For simple messages (success, error, info)
    if (icon === 'success') {
      const result = await this.success(title, text || html);
      return { ...result, isDismissed: !result.isConfirmed };
    }
    if (icon === 'error') {
      const result = await this.error(title, text || html);
      return { ...result, isDismissed: !result.isConfirmed };
    }
    if (icon === 'info') {
      const result = await this.info(title, text || html);
      return { ...result, isDismissed: !result.isConfirmed };
    }

    // Default
    return { isConfirmed: true };
  }

  // Progress bar methods
  static async progress(title: string, text?: string): Promise<void> {
    this.setModal({
      isOpen: true,
      type: 'loading',
      title,
      message: text || '',
    });
  }

  static updateProgress(percentage: number): void {
    // Progress updates are handled by the loading modal
    // This is a no-op for compatibility
  }
}

export default ModalManager;
