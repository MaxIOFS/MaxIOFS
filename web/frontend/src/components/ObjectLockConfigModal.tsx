import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
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
  tenantId?: string;
  currentMode: string; // GOVERNANCE or COMPLIANCE
  currentDays?: number;
  currentYears?: number;
}

export function ObjectLockConfigModal({
  isOpen,
  onClose,
  bucketName,
  tenantId,
  currentMode,
  currentDays,
  currentYears,
}: ObjectLockConfigModalProps) {
  const { t } = useTranslation('bucketSettings');

  // Calcular valores iniciales desde las props (se ejecuta una sola vez por montaje)
  const initialValue = currentYears ? currentYears.toString() : (currentDays?.toString() || '');
  const initialUnit: 'days' | 'years' = currentYears ? 'years' : 'days';

  const [retentionValue, setRetentionValue] = useState<string>(initialValue);
  const [retentionUnit, setRetentionUnit] = useState<'days' | 'years'>(initialUnit);
  const queryClient = useQueryClient();

  useEffect(() => {
    if (!isOpen) return;
    setRetentionValue(currentYears ? currentYears.toString() : (currentDays?.toString() || ''));
    setRetentionUnit(currentYears ? 'years' : 'days');
  }, [isOpen, currentDays, currentYears]);

  // Calcular días totales actuales
  const currentTotalDays = currentYears ? currentYears * 365 : (currentDays || 0);

  const updateMutation = useMutation({
    mutationFn: () =>
      APIClient.updateObjectLockConfiguration(bucketName, {
        mode: currentMode,
        [retentionUnit]: parseInt(retentionValue),
      }, tenantId),
    onSuccess: () => {
      ModalManager.toast('success', t('objectLock.configModal.successMsg'));
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      onClose();
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const handleUpdate = () => {
    const newValue = parseInt(retentionValue);
    const newTotalDays = retentionUnit === 'years' ? newValue * 365 : newValue;

    if (newTotalDays < currentTotalDays) {
      ModalManager.toast(
        'error',
        t('objectLock.configModal.errorDecrease', { current: currentTotalDays, requested: newTotalDays })
      );
      return;
    }

    if (newValue <= 0) {
      ModalManager.toast('error', t('objectLock.configModal.errorPositive'));
      return;
    }

    updateMutation.mutate();
  };

  const handleClose = () => {
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
    <Modal isOpen={isOpen} onClose={handleClose} title={t('objectLock.configModal.title')}>
      <div className="space-y-4">
        {/* Warning Banner */}
        <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-lg p-4">
          <div className="flex gap-3">
            <ShieldAlert className="h-5 w-5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
            <div className="text-sm text-amber-800 dark:text-amber-200">
              <p className="font-semibold mb-1">{t('objectLock.configModal.warningTitle')}</p>
              <ul className="list-disc list-inside space-y-1">
                <li dangerouslySetInnerHTML={{ __html: t('objectLock.configModal.warningIncrease') }} />
                <li dangerouslySetInnerHTML={{ __html: t('objectLock.configModal.warningModeImmutable', { mode: currentMode }) }} />
                <li>{t('objectLock.configModal.warningPermanent')}</li>
              </ul>
            </div>
          </div>
        </div>

        {/* Current Configuration */}
        <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
          <div className="flex gap-3">
            <Lock className="h-5 w-5 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5" />
            <div className="text-sm">
              <p className="font-semibold text-blue-900 dark:text-blue-100 mb-2">{t('objectLock.configModal.currentConfig')}</p>
              <div className="space-y-1 text-blue-800 dark:text-blue-200">
                <p>
                  <strong>{t('objectLock.configModal.currentMode')}</strong>{' '}
                  <span className="font-mono">{currentMode}</span>
                </p>
                <p>
                  <strong>{t('objectLock.configModal.currentRetention')}</strong>{' '}
                  {currentYears ? `${currentYears} ${currentYears > 1 ? t('objectLock.years', { years: currentYears }) : t('objectLock.years', { years: currentYears })}` : ''}
                  {currentDays ? `${currentDays} ${t('objectLock.days', { days: currentDays })}` : ''}
                  {' '}
                  <span className="text-xs">{t('objectLock.configModal.totalDays', { days: currentTotalDays })}</span>
                </p>
              </div>
            </div>
          </div>
        </div>

        {/* New Configuration Form */}
        <div>
          <label className="block text-sm font-medium mb-2">{t('objectLock.configModal.newRetentionLabel')}</label>
          <div className="flex gap-2">
            <Input
              type="number"
              min={retentionUnit === 'years' ? Math.ceil(currentTotalDays / 365) : currentTotalDays}
              value={retentionValue}
              onChange={(e) => setRetentionValue(e.target.value)}
              placeholder={t('objectLock.configModal.minimumPlaceholder', {
                min: retentionUnit === 'years' ? Math.ceil(currentTotalDays / 365) : currentTotalDays,
              })}
              className="flex-1"
            />
            <select
              value={retentionUnit}
              onChange={(e) => setRetentionUnit(e.target.value as 'days' | 'years')}
              className="px-4 py-2 border border-border rounded-lg bg-card text-foreground"
            >
              <option value="days">{t('objectLock.configModal.days')}</option>
              <option value="years">{t('objectLock.configModal.years')}</option>
            </select>
          </div>
          {retentionValue && (
            <p className="text-xs text-muted-foreground mt-2">
              <Info className="inline h-3 w-3 mr-1" />
              {t('objectLock.configModal.newTotal', { days: getNewTotalDays() })}
              {canIncrease() ? (
                <span className="text-green-600 dark:text-green-400 ml-2">
                  ✓ {t('objectLock.configModal.increaseOf', { days: getNewTotalDays() - currentTotalDays })}
                </span>
              ) : getNewTotalDays() === currentTotalDays ? (
                <span className="text-muted-foreground ml-2">{t('objectLock.configModal.noChange')}</span>
              ) : (
                <span className="text-red-600 dark:text-red-400 ml-2">{t('objectLock.configModal.cannotDecrease')}</span>
              )}
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex justify-end gap-3 pt-4 border-t border-border">
          <Button variant="outline" onClick={handleClose} disabled={updateMutation.isPending}>
            {t('objectLock.configModal.cancel')}
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
            {updateMutation.isPending
              ? t('objectLock.configModal.updating')
              : t('objectLock.configModal.updateRetention')}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
