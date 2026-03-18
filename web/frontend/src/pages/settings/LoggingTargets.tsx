import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Plus,
  Trash2,
  Edit2,
  Zap,
  Server,
  Globe,
  Shield,
  CheckCircle,
  XCircle,
  Loader2,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Modal, ConfirmModal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import type { LoggingTarget, LoggingTargetType } from '@/types';

const emptyTarget: Partial<LoggingTarget> = {
  name: '',
  type: 'syslog',
  enabled: true,
  protocol: 'tcp',
  host: '',
  port: 514,
  tag: 'maxiofs',
  format: 'rfc5424',
  tls_enabled: false,
  tls_cert: '',
  tls_key: '',
  tls_ca: '',
  tls_skip_verify: false,
  filter_level: 'info',
  auth_token: '',
  url: '',
  batch_size: 100,
  flush_interval: 10,
};

export default function LoggingTargets() {
  const { t } = useTranslation('settings');
  const queryClient = useQueryClient();
  const [showModal, setShowModal] = useState(false);
  const [editingTarget, setEditingTarget] = useState<Partial<LoggingTarget> | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<LoggingTarget | null>(null);
  const [testResult, setTestResult] = useState<{ id: string; success: boolean; message: string } | null>(null);
  const [formErrors, setFormErrors] = useState<string | null>(null);

  // Fetch targets
  const { data, isLoading } = useQuery({
    queryKey: ['logging-targets'],
    queryFn: () => APIClient.listLoggingTargets(),
  });

  const targets = data?.targets ?? [];

  // Create mutation
  const createMutation = useMutation({
    mutationFn: (target: Partial<LoggingTarget>) => APIClient.createLoggingTarget(target),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logging-targets'] });
      setShowModal(false);
      setEditingTarget(null);
      setFormErrors(null);
    },
    onError: (error: any) => {
      setFormErrors(error.response?.data?.error || t('failedToCreateTarget'));
    },
  });

  // Update mutation
  const updateMutation = useMutation({
    mutationFn: ({ id, target }: { id: string; target: Partial<LoggingTarget> }) =>
      APIClient.updateLoggingTarget(id, target),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logging-targets'] });
      setShowModal(false);
      setEditingTarget(null);
      setFormErrors(null);
    },
    onError: (error: any) => {
      setFormErrors(error.response?.data?.error || t('failedToUpdateTarget'));
    },
  });

  // Delete mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => APIClient.deleteLoggingTarget(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['logging-targets'] });
      setDeleteTarget(null);
    },
  });

  // Test mutation
  const testMutation = useMutation({
    mutationFn: (id: string) => APIClient.testLoggingTarget(id),
    onSuccess: (_data, id) => {
      setTestResult({ id, success: true, message: t('testMessageSent') });
      setTimeout(() => setTestResult(null), 4000);
    },
    onError: (error: any, id) => {
      setTestResult({ id, success: false, message: error.response?.data?.error || t('testFailed') });
      setTimeout(() => setTestResult(null), 6000);
    },
  });

  // Test config (unsaved) mutation
  const testConfigMutation = useMutation({
    mutationFn: (target: Partial<LoggingTarget>) => APIClient.testLoggingTargetConfig(target),
    onSuccess: () => {
      setFormErrors(null);
      setTestResult({ id: 'modal', success: true, message: t('connectionTestPassed') });
      setTimeout(() => setTestResult(null), 4000);
    },
    onError: (error: any) => {
      setTestResult({ id: 'modal', success: false, message: error.response?.data?.error || t('connectionTestFailed') });
      setTimeout(() => setTestResult(null), 6000);
    },
  });

  const handleOpenCreate = () => {
    setEditingTarget({ ...emptyTarget });
    setFormErrors(null);
    setShowModal(true);
  };

  const handleOpenEdit = (target: LoggingTarget) => {
    setEditingTarget({ ...target });
    setFormErrors(null);
    setShowModal(true);
  };

  const handleSave = () => {
    if (!editingTarget) return;
    if (editingTarget.id) {
      updateMutation.mutate({ id: editingTarget.id, target: editingTarget });
    } else {
      createMutation.mutate(editingTarget);
    }
  };

  const handleTestConfig = () => {
    if (!editingTarget) return;
    testConfigMutation.mutate(editingTarget);
  };

  const updateField = <K extends keyof LoggingTarget>(field: K, value: LoggingTarget[K]) => {
    setEditingTarget(prev => prev ? { ...prev, [field]: value } : null);
  };

  const isSaving = createMutation.isPending || updateMutation.isPending;

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-5 w-5 animate-spin text-gray-400" />
        <span className="ml-2 text-sm text-gray-500">{t('loadingTargets')}</span>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h4 className="text-sm font-semibold text-foreground">{t('loggingTargetsTitle')}</h4>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('loggingTargetsDesc')}
          </p>
        </div>
        <Button onClick={handleOpenCreate}>
          <Plus className="h-4 w-4" />
          {t('addTarget')}
        </Button>
      </div>

      {/* Targets List */}
      {targets.length === 0 ? (
        <div className="text-center py-8 border border-dashed border-border rounded-lg">
          <Server className="h-8 w-8 mx-auto text-muted-foreground mb-2" />
          <p className="text-sm text-muted-foreground">{t('noTargetsConfigured')}</p>
          <p className="text-xs text-muted-foreground mt-1">
            {t('noTargetsConfiguredHint')}
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {targets.map(target => (
            <div
              key={target.id}
              className={`flex items-center justify-between p-3 rounded-lg border transition-colors ${
                target.enabled
                  ? 'border-green-200 dark:border-green-800/50 bg-green-50/50 dark:bg-green-900/10'
                  : 'border-border bg-gray-50/50 dark:bg-gray-800/50'
              }`}
            >
              <div className="flex items-center gap-3 min-w-0">
                {/* Icon */}
                <div className={`flex-shrink-0 p-2 rounded-lg ${
                  target.type === 'syslog'
                    ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400'
                    : 'bg-green-100 dark:bg-green-900/30 text-green-600 dark:text-green-400'
                }`}>
                  {target.type === 'syslog' ? <Server className="h-4 w-4" /> : <Globe className="h-4 w-4" />}
                </div>

                {/* Info */}
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-foreground truncate">
                      {target.name}
                    </span>
                    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium ${
                      target.enabled
                        ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
                        : 'bg-gray-100 dark:bg-gray-700 text-muted-foreground'
                    }`}>
                      {target.enabled ? t('targetActive') : t('targetDisabled')}
                    </span>
                    {target.tls_enabled && (
                      <span title="TLS Enabled"><Shield className="h-3.5 w-3.5 text-blue-500" /></span>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {target.type === 'syslog'
                      ? `${target.protocol?.toUpperCase()}://${target.host}:${target.port} • ${target.format?.toUpperCase()} • Level ≥ ${target.filter_level}`
                      : `${target.url} • Level ≥ ${target.filter_level}`}
                  </p>
                </div>
              </div>

              {/* Actions */}
              <div className="flex items-center gap-1 flex-shrink-0 ml-3">
                {/* Test result badge */}
                {testResult && testResult.id === target.id && (
                  <span className={`inline-flex items-center gap-1 px-2 py-1 rounded text-xs ${
                    testResult.success
                      ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
                      : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400'
                  }`}>
                    {testResult.success ? <CheckCircle className="h-3 w-3" /> : <XCircle className="h-3 w-3" />}
                    {testResult.message}
                  </span>
                )}

                <button
                  onClick={() => testMutation.mutate(target.id)}
                  disabled={testMutation.isPending}
                  className="p-1.5 text-gray-400 hover:text-yellow-600 dark:hover:text-yellow-400 transition-colors disabled:opacity-50"
                  title={t('testConnection')}
                >
                  {testMutation.isPending && testMutation.variables === target.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Zap className="h-4 w-4" />
                  )}
                </button>

                <button
                  onClick={() => handleOpenEdit(target)}
                  className="p-1.5 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"
                  title={t('editTarget')}
                >
                  <Edit2 className="h-4 w-4" />
                </button>

                <button
                  onClick={() => setDeleteTarget(target)}
                  className="p-1.5 text-gray-400 hover:text-red-600 dark:hover:text-red-400 transition-colors"
                  title={t('deleteTarget')}
                >
                  <Trash2 className="h-4 w-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Create/Edit Modal */}
      <Modal
        isOpen={showModal}
        onClose={() => { setShowModal(false); setEditingTarget(null); setFormErrors(null); }}
        title={editingTarget?.id ? t('editLoggingTarget') : t('addLoggingTarget')}
        size="lg"
      >
        {editingTarget && (
          <div className="space-y-5">
            {/* Error Banner */}
            {formErrors && (
              <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">
                {formErrors}
              </div>
            )}

            {/* Test Result Banner in Modal */}
            {testResult && testResult.id === 'modal' && (
              <div className={`p-3 rounded-lg text-sm border ${
                testResult.success
                  ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800 text-green-700 dark:text-green-300'
                  : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300'
              }`}>
                {testResult.success ? <CheckCircle className="h-4 w-4 inline mr-1" /> : <XCircle className="h-4 w-4 inline mr-1" />}
                {testResult.message}
              </div>
            )}

            {/* Basic Info */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-medium text-foreground mb-1">{t('nameLabel')}</label>
                <input
                  type="text"
                  value={editingTarget.name ?? ''}
                  onChange={e => updateField('name', e.target.value)}
                  placeholder="e.g. SIEM Production"
                  className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-foreground mb-1">{t('typeLabel')}</label>
                <select
                  value={editingTarget.type ?? 'syslog'}
                  onChange={e => {
                    const type = e.target.value as LoggingTargetType;
                    updateField('type', type);
                    if (type === 'syslog') {
                      updateField('port', 514);
                      updateField('protocol', 'tcp');
                    } else {
                      updateField('port', 443);
                    }
                  }}
                  className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                >
                  <option value="syslog">Syslog</option>
                  <option value="http">HTTP Endpoint</option>
                </select>
              </div>
            </div>

            {/* Enabled + Filter Level */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-xs font-medium text-foreground mb-1">{t('statusLabel')}</label>
                <div className="flex gap-2">
                  <button
                    onClick={() => updateField('enabled', true)}
                    className={`flex-1 px-3 py-2 text-sm font-medium rounded-lg transition-all ${
                      editingTarget.enabled
                        ? 'bg-green-600 text-white'
                        : 'bg-gray-100 dark:bg-gray-700 text-foreground'
                    }`}
                  >
                    {t('enabled')}
                  </button>
                  <button
                    onClick={() => updateField('enabled', false)}
                    className={`flex-1 px-3 py-2 text-sm font-medium rounded-lg transition-all ${
                      !editingTarget.enabled
                        ? 'bg-gray-600 text-white'
                        : 'bg-gray-100 dark:bg-gray-700 text-foreground'
                    }`}
                  >
                    {t('disabled')}
                  </button>
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-foreground mb-1">{t('minLogLevel')}</label>
                <select
                  value={editingTarget.filter_level ?? 'info'}
                  onChange={e => updateField('filter_level', e.target.value)}
                  className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                >
                  <option value="debug">Debug</option>
                  <option value="info">Info</option>
                  <option value="warn">Warning</option>
                  <option value="error">Error</option>
                </select>
              </div>
            </div>

            {/* Syslog-specific fields */}
            {editingTarget.type === 'syslog' && (
              <>
                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('protocolLabel')}</label>
                    <select
                      value={editingTarget.protocol ?? 'tcp'}
                      onChange={e => updateField('protocol', e.target.value)}
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    >
                      <option value="tcp">TCP</option>
                      <option value="udp">UDP</option>
                      <option value="tcp+tls">TCP + TLS</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('hostLabel')}</label>
                    <input
                      type="text"
                      value={editingTarget.host ?? ''}
                      onChange={e => updateField('host', e.target.value)}
                      placeholder="syslog.example.com"
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('portLabel')}</label>
                    <input
                      type="number"
                      value={editingTarget.port ?? 514}
                      onChange={e => updateField('port', parseInt(e.target.value) || 514)}
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('tagLabel')}</label>
                    <input
                      type="text"
                      value={editingTarget.tag ?? 'maxiofs'}
                      onChange={e => updateField('tag', e.target.value)}
                      placeholder="maxiofs"
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('formatLabel')}</label>
                    <select
                      value={editingTarget.format ?? 'rfc5424'}
                      onChange={e => updateField('format', e.target.value)}
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    >
                      <option value="rfc3164">RFC 3164 (BSD)</option>
                      <option value="rfc5424">RFC 5424 (Modern)</option>
                    </select>
                  </div>
                </div>

                {/* TLS options for tcp+tls */}
                {editingTarget.protocol === 'tcp+tls' && (
                  <div className="border border-blue-200 dark:border-blue-800/50 rounded-lg p-4 bg-blue-50/50 dark:bg-blue-900/10">
                    <div className="flex items-center gap-2 mb-3">
                      <Shield className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                      <span className="text-xs font-semibold text-blue-700 dark:text-blue-300">{t('tlsConfiguration')}</span>
                    </div>
                    <div className="space-y-3">
                      <div>
                        <label className="block text-xs font-medium text-foreground mb-1">{t('caCert')}</label>
                        <textarea
                          value={editingTarget.tls_ca ?? ''}
                          onChange={e => updateField('tls_ca', e.target.value)}
                          placeholder="-----BEGIN CERTIFICATE-----"
                          rows={3}
                          className="w-full px-3 py-2 text-xs font-mono border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                        />
                      </div>
                      <div className="grid grid-cols-2 gap-3">
                        <div>
                          <label className="block text-xs font-medium text-foreground mb-1">{t('clientCert')}</label>
                          <textarea
                            value={editingTarget.tls_cert ?? ''}
                            onChange={e => updateField('tls_cert', e.target.value)}
                            placeholder="Optional: for mTLS"
                            rows={2}
                            className="w-full px-3 py-2 text-xs font-mono border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                          />
                        </div>
                        <div>
                          <label className="block text-xs font-medium text-foreground mb-1">{t('clientKey')}</label>
                          <textarea
                            value={editingTarget.tls_key ?? ''}
                            onChange={e => updateField('tls_key', e.target.value)}
                            placeholder="Optional: for mTLS"
                            rows={2}
                            className="w-full px-3 py-2 text-xs font-mono border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                          />
                        </div>
                      </div>
                      <label className="flex items-center gap-2 text-xs text-muted-foreground">
                        <input
                          type="checkbox"
                          checked={editingTarget.tls_skip_verify ?? false}
                          onChange={e => updateField('tls_skip_verify', e.target.checked)}
                          className="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                        />
                        {t('skipTlsVerify')}
                      </label>
                    </div>
                  </div>
                )}
              </>
            )}

            {/* HTTP-specific fields */}
            {editingTarget.type === 'http' && (
              <>
                <div>
                  <label className="block text-xs font-medium text-foreground mb-1">{t('endpointUrlLabel')}</label>
                  <input
                    type="text"
                    value={editingTarget.url ?? ''}
                    onChange={e => updateField('url', e.target.value)}
                    placeholder="https://logs.example.com/_bulk"
                    className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                  />
                </div>
                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('authTokenLabel')}</label>
                    <input
                      type="password"
                      value={editingTarget.auth_token ?? ''}
                      onChange={e => updateField('auth_token', e.target.value)}
                      placeholder="Bearer token (optional)"
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('batchSizeLabel')}</label>
                    <input
                      type="number"
                      value={editingTarget.batch_size ?? 100}
                      onChange={e => updateField('batch_size', parseInt(e.target.value) || 100)}
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-foreground mb-1">{t('flushIntervalLabel')}</label>
                    <input
                      type="number"
                      value={editingTarget.flush_interval ?? 10}
                      onChange={e => updateField('flush_interval', parseInt(e.target.value) || 10)}
                      className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-card text-foreground focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    />
                  </div>
                </div>
              </>
            )}

            {/* Footer Buttons */}
            <div className="flex items-center justify-between pt-4 border-t border-border">
              <Button
                variant="outline"
                onClick={handleTestConfig}
                disabled={testConfigMutation.isPending}
                className="text-yellow-700 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800 hover:bg-yellow-50 dark:hover:bg-yellow-900/20"
              >
                {testConfigMutation.isPending ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Zap className="h-4 w-4" />
                )}
                {t('testConnection')}
              </Button>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  onClick={() => { setShowModal(false); setEditingTarget(null); setFormErrors(null); }}
                >
                  {t('cancel')}
                </Button>
                <Button
                  onClick={handleSave}
                  disabled={isSaving}
                >
                  {isSaving && <Loader2 className="h-4 w-4 animate-spin" />}
                  {editingTarget.id ? t('update') : t('create')}
                </Button>
              </div>
            </div>
          </div>
        )}
      </Modal>

      {/* Delete Confirm */}
      <ConfirmModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
        title={t('deleteLoggingTarget')}
        message={t('deleteLoggingTargetMsg', { name: deleteTarget?.name })}
        confirmText={t('deleteConfirm')}
        variant="danger"
        loading={deleteMutation.isPending}
      />
    </div>
  );
}
