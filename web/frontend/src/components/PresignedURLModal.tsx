import React, { useState } from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Copy as CopyIcon, Link as LinkIcon } from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

interface PresignedURLModalProps {
  isOpen: boolean;
  onClose: () => void;
  bucketName: string;
  objectKey: string;
  tenantId?: string;
}

export function PresignedURLModal({
  isOpen,
  onClose,
  bucketName,
  objectKey,
  tenantId,
}: PresignedURLModalProps) {
  const [expiresIn, setExpiresIn] = useState<string>('3600'); // 1 hour default
  const [method, setMethod] = useState<string>('GET');
  const [generatedURL, setGeneratedURL] = useState<string>('');
  const [expiresAt, setExpiresAt] = useState<string>('');

  const generateMutation = useMutation({
    mutationFn: () =>
      APIClient.generatePresignedURL({
        bucket: bucketName,
        key: objectKey,
        tenantId,
        expiresIn: parseInt(expiresIn),
        method,
      }),
    onSuccess: (data) => {
      setGeneratedURL(data.url);
      setExpiresAt(data.expiresAt);
      SweetAlert.toast('success', 'Presigned URL generated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const handleGenerate = () => {
    generateMutation.mutate();
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(generatedURL);
    SweetAlert.toast('success', 'URL copied to clipboard');
  };

  const handleClose = () => {
    // Don't reset the generated URL so user can reopen and copy it
    // Only reset when user generates a new one
    onClose();
  };

  const handleReset = () => {
    setGeneratedURL('');
    setExpiresAt('');
    setExpiresIn('3600');
    setMethod('GET');
  };

  const formatExpirationTime = () => {
    const seconds = parseInt(expiresIn);
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    if (hours > 0) {
      return `${hours} hour${hours > 1 ? 's' : ''}${minutes > 0 ? ` ${minutes} min` : ''}`;
    }
    return `${minutes} minute${minutes > 1 ? 's' : ''}`;
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title="Generate Presigned URL">
      <div className="space-y-4">
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
          <p className="text-sm text-blue-800 dark:text-blue-200">
            <strong>🔗 About Presigned URLs:</strong> Generate a temporary, signed URL that allows anyone with the link
            to access this object without authentication. The URL expires automatically after the specified time.
          </p>
        </div>

        {!generatedURL ? (
          <>
            <div>
              <label className="block text-sm font-medium mb-2">Object</label>
              <Input value={objectKey} readOnly className="bg-gray-50 dark:bg-gray-900" />
            </div>

            <div>
              <label className="block text-sm font-medium mb-2">HTTP Method</label>
              <select
                value={method}
                onChange={(e) => setMethod(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-white"
              >
                <option value="GET">GET (Download)</option>
                <option value="PUT">PUT (Upload)</option>
                <option value="HEAD">HEAD (Metadata)</option>
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Select the HTTP method the presigned URL will allow
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium mb-2">Expires In</label>
              <select
                value={expiresIn}
                onChange={(e) => setExpiresIn(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-white"
              >
                <option value="300">5 minutes</option>
                <option value="900">15 minutes</option>
                <option value="1800">30 minutes</option>
                <option value="3600">1 hour</option>
                <option value="10800">3 hours</option>
                <option value="21600">6 hours</option>
                <option value="43200">12 hours</option>
                <option value="86400">24 hours</option>
                <option value="604800">7 days (maximum)</option>
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                The URL will expire in {formatExpirationTime()}
              </p>
            </div>

            <div className="flex justify-end gap-2 pt-4 border-t">
              <Button variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button
                onClick={handleGenerate}
                disabled={generateMutation.isPending}
                className="gap-2"
              >
                {generateMutation.isPending ? (
                  'Generating...'
                ) : (
                  <>
                    <LinkIcon className="h-4 w-4" />
                    Generate URL
                  </>
                )}
              </Button>
            </div>
          </>
        ) : (
          <>
            <div>
              <label className="block text-sm font-medium mb-2">Generated Presigned URL</label>
              <div className="bg-gray-50 dark:bg-gray-900 p-3 rounded border border-gray-200 dark:border-gray-700">
                <code className="text-xs break-all text-gray-900 dark:text-white">{generatedURL}</code>
              </div>
            </div>

            <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-4">
              <div className="text-sm text-yellow-800 dark:text-yellow-200">
                <p className="font-medium mb-1">⏰ Expires At:</p>
                <p>{new Date(expiresAt).toLocaleString()}</p>
                <p className="mt-2 text-xs">
                  This URL will be valid for <strong>{formatExpirationTime()}</strong>
                </p>
              </div>
            </div>

            <div className="bg-orange-50 dark:bg-orange-900/30 border border-orange-200 dark:border-orange-800 rounded-lg p-4">
              <p className="text-sm text-orange-800 dark:text-orange-200">
                <strong>⚠️ Security Notice:</strong> Anyone with this URL can {method === 'GET' ? 'download' : method === 'PUT' ? 'upload to' : 'access'} this object
                until it expires. Do not share this URL publicly unless intended.
              </p>
            </div>

            <div className="flex justify-between pt-4 border-t">
              <Button variant="outline" onClick={handleReset} className="gap-2">
                <LinkIcon className="h-4 w-4" />
                Generate New URL
              </Button>
              <div className="flex gap-2">
                <Button variant="outline" onClick={handleClose}>
                  Close
                </Button>
                <Button onClick={handleCopy} className="gap-2">
                  <CopyIcon className="h-4 w-4" />
                  Copy URL
                </Button>
              </div>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}
