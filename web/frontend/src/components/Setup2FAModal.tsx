import { useState, useEffect } from 'react';
import { Modal } from './ui/Modal';
import { Button } from './ui/Button';
import { KeyRound, Download, Copy, Check } from 'lucide-react';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import { getErrorMessage } from '@/lib/utils';

interface Setup2FAModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: (backupCodes: string[]) => void;
}

export function Setup2FAModal({ isOpen, onClose, onSuccess }: Setup2FAModalProps) {
  const [step, setStep] = useState<'loading' | 'scan' | 'verify'>('loading');
  const [secret, setSecret] = useState('');
  const [qrCode, setQrCode] = useState('');
  const [verificationCode, setVerificationCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [secretCopied, setSecretCopied] = useState(false);

  useEffect(() => {
    if (isOpen) {
      setupTwoFactor();
    }
  }, [isOpen]);

  const setupTwoFactor = async () => {
    setStep('loading');
    setError(null);

    try {
      const data = await APIClient.setup2FA();
      setSecret(data.secret);
      setQrCode(data.qr_code);
      setStep('scan');
    } catch (err: unknown) {
      console.error('Setup 2FA error:', err);
      setError(getErrorMessage(err, 'Failed to setup 2FA'));
      setStep('scan');
    }
  };

  const handleVerifyAndEnable = async () => {
    if (!verificationCode || verificationCode.length !== 6) {
      setError('Please enter a valid 6-digit code');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const data = await APIClient.enable2FA(verificationCode, secret);

      await ModalManager.success(
        '2FA Enabled Successfully',
        'Your account is now protected with two-factor authentication. Please save your backup codes securely.'
      );

      onSuccess(data.backup_codes);
      onClose();
    } catch (err: unknown) {
      console.error('Enable 2FA error:', err);
      setError(getErrorMessage(err, 'Invalid verification code. Please try again.'));
    } finally {
      setLoading(false);
    }
  };

  const copySecret = () => {
    navigator.clipboard.writeText(secret);
    setSecretCopied(true);
    setTimeout(() => setSecretCopied(false), 2000);
  };

  const handleClose = () => {
    setStep('loading');
    setSecret('');
    setQrCode('');
    setVerificationCode('');
    setError(null);
    setSecretCopied(false);
    onClose();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Enable Two-Factor Authentication"
      size="lg"
      closeOnOverlay={false}
    >
      <div className="space-y-6">
        {step === 'loading' && (
          <div className="flex flex-col items-center justify-center py-12">
            <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600 dark:border-blue-400 mb-4"></div>
            <p className="text-gray-600 dark:text-gray-400">Setting up 2FA...</p>
          </div>
        )}

        {step === 'scan' && (
          <div className="space-y-6">
            {/* Step 1: Scan QR Code */}
            <div>
              <div className="flex items-center mb-4">
                <div className="flex items-center justify-center w-8 h-8 rounded-full bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-bold mr-3">
                  1
                </div>
                <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                  Scan QR Code
                </h3>
              </div>

              <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-6 space-y-4">
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  Scan this QR code with your authenticator app (Google Authenticator, Microsoft Authenticator, Authy, etc.)
                </p>

                {qrCode && (
                  <div className="flex justify-center p-4 bg-white dark:bg-gray-800 rounded-lg">
                    <img
                      src={`data:image/png;base64,${qrCode}`}
                      alt="2FA QR Code"
                      className="w-48 h-48 3xl:w-56 3xl:h-56 4xl:w-64 4xl:h-64"
                    />
                  </div>
                )}

                <div className="pt-2">
                  <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                    Can't scan? Enter this code manually:
                  </p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 px-3 py-2 bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded text-sm font-mono text-gray-900 dark:text-white break-all">
                      {secret}
                    </code>
                    <Button
                      variant="outline"
                      size="icon"
                      onClick={copySecret}
                      className="shrink-0"
                    >
                      {secretCopied ? (
                        <Check className="w-4 h-4 text-green-600" />
                      ) : (
                        <Copy className="w-4 h-4" />
                      )}
                    </Button>
                  </div>
                </div>
              </div>
            </div>

            {/* Step 2: Verify Code */}
            <div>
              <div className="flex items-center mb-4">
                <div className="flex items-center justify-center w-8 h-8 rounded-full bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400 font-bold mr-3">
                  2
                </div>
                <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                  Verify Code
                </h3>
              </div>

              <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-6 space-y-4">
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  Enter the 6-digit code from your authenticator app to verify the setup
                </p>

                {error && (
                  <div className="rounded-lg bg-red-50 dark:bg-red-900/20 p-3 border-l-4 border-red-500">
                    <div className="text-sm text-red-800 dark:text-red-200">{error}</div>
                  </div>
                )}

                <div>
                  <input
                    type="text"
                    inputMode="numeric"
                    maxLength={6}
                    value={verificationCode}
                    onChange={(e) => {
                      const value = e.target.value.replace(/\D/g, '');
                      setVerificationCode(value);
                      setError(null);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && verificationCode.length === 6) {
                        handleVerifyAndEnable();
                      }
                    }}
                    placeholder="000000"
                    disabled={loading}
                    className="w-full px-4 py-3 text-center text-2xl font-mono tracking-widest border-2 border-gray-300 dark:border-gray-600 rounded-lg focus:border-blue-600 dark:focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:focus:ring-blue-400 bg-white dark:bg-gray-800 text-gray-900 dark:text-white disabled:opacity-50 disabled:cursor-not-allowed"
                  />
                </div>

                <div className="flex gap-3 pt-2">
                  <Button
                    variant="outline"
                    onClick={handleClose}
                    disabled={loading}
                    className="flex-1"
                  >
                    Cancel
                  </Button>
                  <Button
                    onClick={handleVerifyAndEnable}
                    disabled={loading || verificationCode.length !== 6}
                    loading={loading}
                    className="flex-1"
                  >
                    <KeyRound className="w-4 h-4 mr-2" />
                    Enable 2FA
                  </Button>
                </div>
              </div>
            </div>

            {/* Warning */}
            <div className="bg-yellow-50 dark:bg-yellow-900/20 border-l-4 border-yellow-500 p-4 rounded">
              <div className="flex">
                <div className="flex-shrink-0">
                  <svg className="h-5 w-5 text-yellow-600 dark:text-yellow-400" viewBox="0 0 20 20" fill="currentColor">
                    <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                  </svg>
                </div>
                <div className="ml-3">
                  <p className="text-sm text-yellow-800 dark:text-yellow-200">
                    <strong className="font-medium">Important:</strong> After enabling 2FA, you will receive backup codes. Save them securely - you'll need them to access your account if you lose your authenticator device.
                  </p>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

// Backup Codes Modal
interface BackupCodesModalProps {
  isOpen: boolean;
  onClose: () => void;
  backupCodes: string[];
}

export function BackupCodesModal({ isOpen, onClose, backupCodes }: BackupCodesModalProps) {
  const [copied, setCopied] = useState(false);

  const copyAllCodes = () => {
    navigator.clipboard.writeText(backupCodes.join('\n'));
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const downloadCodes = () => {
    const blob = new Blob([backupCodes.join('\n')], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `maxiofs-backup-codes-${Date.now()}.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Backup Codes"
      size="md"
      closeOnOverlay={false}
    >
      <div className="space-y-6">
        <div className="bg-yellow-50 dark:bg-yellow-900/20 border-l-4 border-yellow-500 p-4 rounded">
          <div className="flex">
            <div className="flex-shrink-0">
              <svg className="h-5 w-5 text-yellow-600 dark:text-yellow-400" viewBox="0 0 20 20" fill="currentColor">
                <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
              </svg>
            </div>
            <div className="ml-3">
              <p className="text-sm text-yellow-800 dark:text-yellow-200">
                <strong className="font-medium">Save these codes now!</strong> Each code can only be used once. Store them in a secure location.
              </p>
            </div>
          </div>
        </div>

        <div className="bg-gray-50 dark:bg-gray-900 rounded-lg p-4">
          <div className="grid grid-cols-2 gap-3">
            {backupCodes.map((code, index) => (
              <div
                key={index}
                className="px-3 py-2 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded font-mono text-sm text-center text-gray-900 dark:text-white"
              >
                {code}
              </div>
            ))}
          </div>
        </div>

        <div className="flex gap-3">
          <Button
            variant="outline"
            onClick={copyAllCodes}
            className="flex-1"
          >
            {copied ? (
              <>
                <Check className="w-4 h-4 mr-2" />
                Copied!
              </>
            ) : (
              <>
                <Copy className="w-4 h-4 mr-2" />
                Copy All
              </>
            )}
          </Button>
          <Button
            variant="outline"
            onClick={downloadCodes}
            className="flex-1"
          >
            <Download className="w-4 h-4 mr-2" />
            Download
          </Button>
        </div>

        <Button
          onClick={onClose}
          className="w-full"
        >
          I've Saved My Codes
        </Button>
      </div>
    </Modal>
  );
}

export default Setup2FAModal;
