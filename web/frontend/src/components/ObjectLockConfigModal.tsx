import React, { useState } from 'react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Lock, ShieldAlert, Info } from 'lucide-react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import ModalManager from '@/lib/modals';

interface ObjectLockConfigModalProps {
  isOpen: boolean;
  onClose: () => void;
  bucketName: string;
  currentMode: string; // GOVERNANCE or COMPLIANCE
  currentDays?: number;
  currentYears?: number;
}

export function ObjectLockConfigModal({
  isOpen,
  onClose,
  bucketName,
  currentMode,
  currentDays,
  currentYears,
}: ObjectLockConfigModalProps) {
  // Calcular valores iniciales desde las props (se ejecuta una sola vez por montaje)
  const initialValue = currentYears ? currentYears.toString() : (currentDays?.toString() || '');
  const initialUnit: 'days' | 'years' = currentYears ? 'years' : 'days';

  const [retentionValue, setRetentionValue] = useState<string>(initialValue);
  const [retentionUnit, setRetentionUnit] = useState<'days' | 'years'>(initialUnit);
  const queryClient = useQueryClient();

  // Calcular días totales actuales
  const currentTotalDays = currentYears ? currentYears * 365 : (currentDays || 0);

  const updateMutation = useMutation({
    mutationFn: () =>
      APIClient.updateObjectLockConfiguration(bucketName, {
        mode: currentMode,
        [retentionUnit]: parseInt(retentionValue),
      }),
    onSuccess: () => {
      ModalManager.toast('success', 'Object Lock configuration updated successfully');
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      onClose();
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const handleUpdate = () => {
    const newValue = parseInt(retentionValue);
    const newTotalDays = retentionUnit === 'years' ? newValue * 365 : newValue;

    // Validar que solo se aumente
    if (newTotalDays < currentTotalDays) {
      ModalManager.toast(
        'error',
        `Retention period can only be increased (current: ${currentTotalDays} days, requested: ${newTotalDays} days)`
      );
      return;
    }

    // Validar valor positivo
    if (newValue <= 0) {
      ModalManager.toast('error', 'Retention period must be greater than 0');
      return;
    }

    updateMutation.mutate();
  };

  const handleClose = () => {
    // Reset form to initial values
    const resetValue = currentYears ? currentYears.toString() : (currentDays?.toString() || '');
    const resetUnit: 'days' | 'years' = currentYears ? 'years' : 'days';
    setRetentionValue(resetValue);
    setRetentionUnit(resetUnit);
    onClose();
  };

  const getNewTotalDays = () => {
    const value = parseInt(retentionValue) || 0;
    return retentionUnit === 'years' ? value * 365 : value;
  };

  const canIncrease = () => {
    const newTotalDays = getNewTotalDays();
    return newTotalDays > currentTotalDays;
  };

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title="Configure Object Lock Retention">
      <div className="space-y-4">
        {/* Warning Banner */}
        <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-lg p-4">
          <div className="flex gap-3">
            <ShieldAlert className="h-5 w-5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
            <div className="text-sm text-amber-800 dark:text-amber-200">
              <p className="font-semibold mb-1">⚠️ Security Restrictions:</p>
              <ul className="list-disc list-inside space-y-1">
                <li>Retention period can only be <strong>increased</strong>, never decreased</li>
                <li>
                  Mode is <strong>immutable</strong> (current: <span className="font-mono">{currentMode}</span>)
                </li>
                <li>Changes are permanent and cannot be reverted</li>
              </ul>
            </div>
          </div>
        </div>

        {/* Current Configuration */}
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
          <div className="flex gap-3">
            <Lock className="h-5 w-5 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
            <div className="text-sm">
              <p className="font-semibold text-blue-900 dark:text-blue-100 mb-2">Current Configuration:</p>
              <div className="space-y-1 text-blue-800 dark:text-blue-200">
                <p>
                  <strong>Mode:</strong> <span className="font-mono">{currentMode}</span>
                </p>
                <p>
                  <strong>Retention:</strong>{' '}
                  {currentYears ? `${currentYears} year${currentYears > 1 ? 's' : ''}` : ''}
                  {currentDays ? `${currentDays} day${currentDays > 1 ? 's' : ''}` : ''}
                  {' '}
                  <span className="text-xs">({currentTotalDays} total days)</span>
                </p>
              </div>
            </div>
          </div>
        </div>

        {/* New Configuration Form */}
        <div>
          <label className="block text-sm font-medium mb-2">New Retention Period</label>
          <div className="flex gap-2">
            <Input
              type="number"
              min={retentionUnit === 'years' ? Math.ceil(currentTotalDays / 365) : currentTotalDays}
              value={retentionValue}
              onChange={(e) => setRetentionValue(e.target.value)}
              placeholder={`Minimum ${retentionUnit === 'years' ? Math.ceil(currentTotalDays / 365) : currentTotalDays}`}
              className="flex-1"
            />
            <select
              value={retentionUnit}
              onChange={(e) => setRetentionUnit(e.target.value as 'days' | 'years')}
              className="px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100"
            >
              <option value="days">Days</option>
              <option value="years">Years</option>
            </select>
          </div>
          {retentionValue && (
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">
              <Info className="inline h-3 w-3 mr-1" />
              New total: {getNewTotalDays()} days
              {canIncrease() ? (
                <span className="text-green-600 dark:text-green-400 ml-2">
                  ✓ Increase of {getNewTotalDays() - currentTotalDays} days
                </span>
              ) : getNewTotalDays() === currentTotalDays ? (
                <span className="text-gray-600 dark:text-gray-400 ml-2">⚠️ No change</span>
              ) : (
                <span className="text-red-600 dark:text-red-400 ml-2">✗ Cannot decrease retention</span>
              )}
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex justify-end gap-3 pt-4 border-t border-gray-200 dark:border-gray-700">
          <Button variant="outline" onClick={handleClose} disabled={updateMutation.isPending}>
            Cancel
          </Button>
          <Button
            onClick={handleUpdate}
            disabled={
              updateMutation.isPending ||
              !retentionValue ||
              !canIncrease() ||
              parseInt(retentionValue) <= 0
            }
          >
            {updateMutation.isPending ? 'Updating...' : 'Update Retention'}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
