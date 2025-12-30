import React, { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { X, AlertTriangle, CheckCircle2, Info, AlertCircle } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from './Button';

export interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title?: string;
  description?: string;
  size?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | 'full';
  closeOnOverlay?: boolean;
  closeOnEscape?: boolean;
  showCloseButton?: boolean;
  children: React.ReactNode;
  footer?: React.ReactNode;
  className?: string;
}

const sizeClasses = {
  sm: 'max-w-md 3xl:max-w-lg 4xl:max-w-xl',
  md: 'max-w-lg 3xl:max-w-xl 4xl:max-w-2xl',
  lg: 'max-w-2xl 3xl:max-w-3xl 4xl:max-w-4xl',
  xl: 'max-w-4xl 3xl:max-w-5xl 4xl:max-w-6xl',
  '2xl': 'max-w-6xl 3xl:max-w-7xl 4xl:max-w-[90vw]',
  full: 'max-w-screen-xl 3xl:max-w-screen-2xl 4xl:max-w-[90vw] mx-4',
};

export function Modal({
  isOpen,
  onClose,
  title,
  description,
  size = 'md',
  closeOnOverlay = true,
  closeOnEscape = true,
  showCloseButton = true,
  children,
  footer,
  className,
}: ModalProps) {
  const modalRef = useRef<HTMLDivElement>(null);

  // Handle escape key
  useEffect(() => {
    if (!isOpen || !closeOnEscape) return;

    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };

    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, closeOnEscape, onClose]);

  // Handle overlay click
  const handleOverlayClick = (e: React.MouseEvent) => {
    if (closeOnOverlay && e.target === e.currentTarget) {
      onClose();
    }
  };

  // Focus management
  useEffect(() => {
    if (isOpen && modalRef.current) {
      const focusableElements = modalRef.current.querySelectorAll(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
      );
      const firstElement = focusableElements[0] as HTMLElement;
      if (firstElement) {
        firstElement.focus();
      }
    }
  }, [isOpen]);

  // Prevent body scroll when modal is open
  useEffect(() => {
    if (isOpen) {
      document.body.style.overflow = 'hidden';
    } else {
      document.body.style.overflow = 'unset';
    }

    return () => {
      document.body.style.overflow = 'unset';
    };
  }, [isOpen]);

  if (!isOpen) return null;

  const modalContent = (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 dark:bg-black/80 backdrop-blur-sm animate-in fade-in duration-200"
      onClick={handleOverlayClick}
    >
      <div
        ref={modalRef}
        className={cn(
          'relative w-full bg-white/95 dark:bg-gray-900/95 backdrop-blur-xl rounded-2xl shadow-2xl max-h-[90vh] flex flex-col border border-gray-200/50 dark:border-gray-700/50 animate-in zoom-in-95 duration-200',
          sizeClasses[size],
          className
        )}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Gradient Overlay */}
        <div className="absolute inset-0 bg-gradient-to-br from-brand-500/5 via-blue-500/5 to-purple-500/5 dark:from-brand-400/10 dark:via-blue-400/10 dark:to-purple-400/10 rounded-2xl pointer-events-none" />

        {/* Header */}
        {(title || showCloseButton) && (
          <div className="relative flex items-center justify-between p-6 border-b border-gray-200 dark:border-gray-700 bg-gradient-to-r from-gray-50 to-white dark:from-gray-800 dark:to-gray-800 rounded-t-2xl">
            <div>
              {title && (
                <h2 className="text-xl font-bold bg-gradient-to-r from-gray-900 via-gray-800 to-gray-900 dark:from-white dark:via-gray-100 dark:to-white bg-clip-text text-transparent">
                  {title}
                </h2>
              )}
              {description && (
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
                  {description}
                </p>
              )}
            </div>
            {showCloseButton && (
              <Button
                variant="ghost"
                size="icon"
                onClick={onClose}
                className="text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-lg"
              >
                <X className="h-5 w-5" />
              </Button>
            )}
          </div>
        )}

        {/* Content */}
        <div className="relative flex-1 overflow-y-auto p-6">
          {children}
        </div>

        {/* Footer */}
        {footer && (
          <div className="relative border-t border-gray-200 dark:border-gray-700 p-6 bg-gradient-to-r from-gray-50 to-white dark:from-gray-900 dark:to-gray-800 rounded-b-2xl">
            {footer}
          </div>
        )}
      </div>
    </div>
  );

  // Use portal to render modal at document body level
  return typeof window !== 'undefined'
    ? createPortal(modalContent, document.body)
    : null;
}

// Confirmation Modal Component
export interface ConfirmModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  variant?: 'danger' | 'warning' | 'info';
  loading?: boolean;
}

export function ConfirmModal({
  isOpen,
  onClose,
  onConfirm,
  title,
  message,
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  variant = 'danger',
  loading = false,
}: ConfirmModalProps) {
  const handleConfirm = () => {
    onConfirm();
  };

  const confirmVariant = variant === 'danger' ? 'destructive' : 'default';

  // Icon and color based on variant
  const variantConfig = {
    danger: {
      icon: AlertTriangle,
      iconBg: 'bg-gradient-to-br from-error-100 to-red-100 dark:from-error-900/40 dark:to-red-900/40',
      iconColor: 'text-error-600 dark:text-error-400',
      borderColor: 'border-error-200 dark:border-error-800',
    },
    warning: {
      icon: AlertCircle,
      iconBg: 'bg-gradient-to-br from-warning-100 to-amber-100 dark:from-warning-900/40 dark:to-amber-900/40',
      iconColor: 'text-warning-600 dark:text-warning-400',
      borderColor: 'border-warning-200 dark:border-warning-800',
    },
    info: {
      icon: Info,
      iconBg: 'bg-gradient-to-br from-brand-100 to-blue-100 dark:from-brand-900/40 dark:to-blue-900/40',
      iconColor: 'text-brand-600 dark:text-brand-400',
      borderColor: 'border-brand-200 dark:border-brand-800',
    },
  };

  const config = variantConfig[variant];
  const Icon = config.icon;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
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
          {title}
        </h3>

        {/* Message */}
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-6">
          {message}
        </p>

        {/* Actions */}
        <div className="flex gap-3">
          <Button
            variant="outline"
            onClick={onClose}
            disabled={loading}
            className="flex-1 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700"
          >
            {cancelText}
          </Button>
          <Button
            variant={confirmVariant}
            onClick={handleConfirm}
            loading={loading}
            className="flex-1"
          >
            {confirmText}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// Success Modal Component
export interface SuccessModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  message: string;
  buttonText?: string;
}

export function SuccessModal({
  isOpen,
  onClose,
  title,
  message,
  buttonText = 'Got it',
}: SuccessModalProps) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="sm"
      showCloseButton={false}
    >
      <div className="text-center">
        {/* Success Icon */}
        <div className="mx-auto flex items-center justify-center w-16 h-16 rounded-full bg-gradient-to-br from-success-100 to-green-100 dark:from-success-900/40 dark:to-green-900/40 mb-4">
          <CheckCircle2 className="h-8 w-8 text-success-600 dark:text-success-400" />
        </div>

        {/* Title */}
        <h3 className="text-lg font-bold text-gray-900 dark:text-white mb-2">
          {title}
        </h3>

        {/* Message */}
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-6">
          {message}
        </p>

        {/* Action */}
        <Button
          onClick={onClose}
          className="w-full bg-gradient-to-r from-success-600 to-green-600 hover:from-success-700 hover:to-green-700 text-white"
        >
          {buttonText}
        </Button>
      </div>
    </Modal>
  );
}

// Alert Modal Component (for errors)
export interface AlertModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  message: string;
  buttonText?: string;
}

export function AlertModal({
  isOpen,
  onClose,
  title,
  message,
  buttonText = 'Close',
}: AlertModalProps) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="sm"
      showCloseButton={false}
    >
      <div className="text-center">
        {/* Error Icon */}
        <div className="mx-auto flex items-center justify-center w-16 h-16 rounded-full bg-gradient-to-br from-error-100 to-red-100 dark:from-error-900/40 dark:to-red-900/40 mb-4">
          <AlertCircle className="h-8 w-8 text-error-600 dark:text-error-400" />
        </div>

        {/* Title */}
        <h3 className="text-lg font-bold text-gray-900 dark:text-white mb-2">
          {title}
        </h3>

        {/* Message */}
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-6 whitespace-pre-line">
          {message}
        </p>

        {/* Action */}
        <Button
          onClick={onClose}
          variant="destructive"
          className="w-full"
        >
          {buttonText}
        </Button>
      </div>
    </Modal>
  );
}

export default Modal;