import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Shield,
  HardDrive,
  Activity,
  Server,
  FileText,
  Save,
  RotateCcw,
  CheckCircle,
  AlertCircle,
  Info,
  FileCode,
  Mail,
  SendHorizonal,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Loading } from '@/components/ui/Loading';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import LoggingTargets from './LoggingTargets';
import type { Setting, SettingCategory } from '@/types';

// Category metadata
const categoryInfo: Record<SettingCategory, { icon: React.ComponentType<any>; title: string; description: string }> = {
  security: {
    icon: Shield,
    title: 'Security',
    description: 'Authentication, password policies, and rate limiting settings',
  },
  audit: {
    icon: FileText,
    title: 'Audit',
    description: 'Audit logging and retention configuration',
  },
  storage: {
    icon: HardDrive,
    title: 'Storage',
    description: 'Default storage behavior and versioning settings',
  },
  metrics: {
    icon: Activity,
    title: 'Metrics',
    description: 'Prometheus metrics and collection settings',
  },
  logging: {
    icon: FileCode,
    title: 'Logging',
    description: 'Structured logging, syslog, and HTTP endpoint configuration',
  },
  system: {
    icon: Server,
    title: 'System',
    description: 'System-wide settings, maintenance mode, and disk alert thresholds',
  },
  email: {
    icon: Mail,
    title: 'Email',
    description: 'SMTP configuration for disk space alerts and system notifications',
  },
};

export default function SettingsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { isGlobalAdmin, user: currentUser } = useCurrentUser();

  const [activeCategory, setActiveCategory] = useState<SettingCategory>('security');
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [testEmailStatus, setTestEmailStatus] = useState<'idle' | 'sending' | 'success' | 'error'>('idle');
  const [testEmailMessage, setTestEmailMessage] = useState<string>('');

  // Only global admins can access settings
  useEffect(() => {
    if (currentUser && !isGlobalAdmin) {
      navigate('/');
    }
  }, [currentUser, isGlobalAdmin, navigate]);

  // Fetch all settings
  const { data: settings, isLoading } = useQuery<Setting[]>({
    queryKey: ['settings'],
    queryFn: () => APIClient.listSettings(),
    enabled: isGlobalAdmin,
  });

  // Group settings by category
  const settingsByCategory = React.useMemo(() => {
    if (!settings) return {};
    return settings.reduce((acc, setting) => {
      if (!acc[setting.category]) {
        acc[setting.category] = [];
      }
      acc[setting.category].push(setting);
      return acc;
    }, {} as Record<SettingCategory, Setting[]>);
  }, [settings]);

  // Update mutation
  const updateMutation = useMutation({
    mutationFn: (updates: Record<string, string>) => APIClient.bulkUpdateSettings(updates),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      queryClient.invalidateQueries({ queryKey: ['serverConfig'] });
      setEditedValues({});
      setHasChanges(false);
      setSaveSuccess(true);
      setSaveError(null);
      setTimeout(() => setSaveSuccess(false), 3000);
    },
    onError: (error: any) => {
      setSaveError(error.response?.data?.error || 'Failed to save settings');
      setTimeout(() => setSaveError(null), 5000);
    },
  });

  // Handle value change
  const handleValueChange = (key: string, value: string, originalValue: string) => {
    const newEditedValues = { ...editedValues };

    if (value === originalValue) {
      // If value matches original, remove from edited values
      delete newEditedValues[key];
    } else {
      newEditedValues[key] = value;
    }

    setEditedValues(newEditedValues);
    setHasChanges(Object.keys(newEditedValues).length > 0);
  };

  // Handle save
  const handleSave = () => {
    if (Object.keys(editedValues).length === 0) return;
    updateMutation.mutate(editedValues);
  };

  // Handle reset
  const handleReset = () => {
    setEditedValues({});
    setHasChanges(false);
    setSaveError(null);
  };

  // Handle test email
  const handleTestEmail = async () => {
    setTestEmailStatus('sending');
    setTestEmailMessage('');
    try {
      const result = await APIClient.testEmail();
      setTestEmailStatus('success');
      setTestEmailMessage(result.message);
    } catch (err: any) {
      setTestEmailStatus('error');
      setTestEmailMessage(err.response?.data?.error || 'Failed to send test email');
    }
    setTimeout(() => setTestEmailStatus('idle'), 5000);
  };

  // Get current value (edited or original)
  const getCurrentValue = (setting: Setting): string => {
    return editedValues[setting.key] !== undefined ? editedValues[setting.key] : setting.value;
  };

  // Format setting label from key
  const formatLabel = (key: string): string => {
    return key
      .split('.')
      .pop()
      ?.replace(/_/g, ' ')
      .replace(/\b\w/g, l => l.toUpperCase()) || key;
  };

  // Get options for select fields based on key
  const getSelectOptions = (key: string): string[] | null => {
    if (key === 'logging.format') return ['json', 'text'];
    if (key === 'logging.level' || key === 'logging.frontend_level') return ['debug', 'info', 'warn', 'error'];
    if (key === 'logging.syslog_protocol') return ['tcp', 'udp'];
    return null;
  };

  // Get status indicator for boolean settings
  const getStatusBadge = (value: string, type: string) => {
    if (type !== 'bool') return null;

    const isEnabled = value === 'true' || value === '1';
    return (
      <span className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
        isEnabled
          ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300'
          : 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-300'
      }`}>
        {isEnabled ? '● Enabled' : '○ Disabled'}
      </span>
    );
  };

  // Format value display for read-only fields
  const formatValueDisplay = (setting: Setting): string => {
    const value = getCurrentValue(setting);

    if (setting.type === 'bool') {
      return value === 'true' || value === '1' ? 'Enabled' : 'Disabled';
    }

    if (setting.type === 'int') {
      // Add units based on key
      if (setting.key.includes('timeout') || setting.key.includes('duration')) {
        const seconds = parseInt(value);
        if (seconds >= 86400) return `${Math.floor(seconds / 86400)} days`;
        if (seconds >= 3600) return `${Math.floor(seconds / 3600)} hours`;
        if (seconds >= 60) return `${Math.floor(seconds / 60)} minutes`;
        return `${seconds} seconds`;
      }
      if (setting.key.includes('size') && setting.key.includes('mb')) {
        return `${value} MB`;
      }
      if (setting.key.includes('days')) {
        return `${value} days`;
      }
      if (setting.key.includes('per_minute')) {
        return `${value} per minute`;
      }
      if (setting.key.includes('per_second')) {
        return `${value} per second`;
      }
    }

    return value;
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (!settings) {
    return (
      <div className="flex items-center justify-center h-64">
        <p className="text-gray-500 dark:text-gray-400">No settings available</p>
      </div>
    );
  }

  const currentSettings = settingsByCategory[activeCategory] || [];
  const tabs = (Object.keys(categoryInfo) as SettingCategory[]).map(cat => ({
    id: cat,
    label: categoryInfo[cat].title,
    icon: categoryInfo[cat].icon,
  }));

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-4">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">System Settings</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Configure MaxIOFS runtime settings stored in database
          </p>
        </div>

        {/* Save/Reset Buttons */}
        {hasChanges && (
          <div className="flex gap-2">
            <button
              onClick={handleReset}
              disabled={updateMutation.isPending}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md hover:bg-gray-50 dark:hover:bg-gray-700 disabled:opacity-50 transition-colors flex items-center gap-2"
            >
              <RotateCcw className="h-4 w-4" />
              Reset
            </button>
            <button
              onClick={handleSave}
              disabled={updateMutation.isPending}
              className="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-md hover:bg-blue-700 disabled:opacity-50 flex items-center gap-2 transition-colors"
            >
              <Save className="h-4 w-4" />
              {updateMutation.isPending ? 'Saving...' : `Save ${Object.keys(editedValues).length} Change${Object.keys(editedValues).length !== 1 ? 's' : ''}`}
            </button>
          </div>
        )}
      </div>

      {/* Success/Error Messages */}
      {saveSuccess && (
        <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
            <div>
              <p className="text-sm font-medium text-green-900 dark:text-green-300">Settings saved successfully</p>
              <p className="text-sm text-green-700 dark:text-green-400 mt-1">Changes have been applied and are now active</p>
            </div>
          </div>
        </div>
      )}

      {saveError && (
        <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <AlertCircle className="h-5 w-5 text-red-600 dark:text-red-400 mt-0.5 flex-shrink-0" />
            <div>
              <p className="text-sm font-medium text-red-900 dark:text-red-300">Error saving settings</p>
              <p className="text-sm text-red-700 dark:text-red-400 mt-1">{saveError}</p>
            </div>
          </div>
        </div>
      )}

      {/* Info Banner */}
      <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
        <div className="flex items-start gap-3">
          <Info className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
          <div>
            <p className="text-sm font-medium text-blue-900 dark:text-blue-300">Database-Backed Configuration</p>
            <p className="text-sm text-blue-700 dark:text-blue-400 mt-1">
              These settings are stored in SQLite and take effect immediately. Static infrastructure settings (ports, paths, TLS) remain in config.yaml.
            </p>
          </div>
        </div>
      </div>

      {/* Tabs and Content */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
        <div className="p-6">
          {/* Tabs Navigation - Same style as Metrics */}
          <div className="flex space-x-1 bg-gray-100 dark:bg-gray-900 rounded-lg p-1 mb-6">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              const categorySettings = settingsByCategory[tab.id] || [];
              const hasEditsInCategory = categorySettings.some(s => editedValues[s.key] !== undefined);

              return (
                <button
                  key={tab.id}
                  onClick={() => setActiveCategory(tab.id as SettingCategory)}
                  className={`flex-1 flex items-center justify-center space-x-2 px-4 py-3 font-medium text-sm rounded-md transition-all duration-200 relative ${
                    activeCategory === tab.id
                      ? 'bg-white dark:bg-gray-800 text-brand-600 dark:text-brand-400 shadow-sm'
                      : 'text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200'
                  }`}
                >
                  <Icon className="h-4 w-4" />
                  <span>{tab.label}</span>
                  {hasEditsInCategory && (
                    <span className="absolute top-2 right-2 h-2 w-2 bg-yellow-500 rounded-full"></span>
                  )}
                </button>
              );
            })}
          </div>

          {/* Category Description */}
          <div className="mb-6 pb-6 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-1">
              {categoryInfo[activeCategory].title} Settings
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {categoryInfo[activeCategory].description}
            </p>
          </div>

          {/* Test Email button — shown only in email category */}
          {activeCategory === 'email' && (
            <div className="mb-6 pb-6 border-b border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between flex-wrap gap-4">
                <div>
                  <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300">Test SMTP Connection</h4>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                    Send a test email to your account address to verify the configuration
                  </p>
                </div>
                <button
                  onClick={handleTestEmail}
                  disabled={testEmailStatus === 'sending'}
                  className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 disabled:opacity-50 transition-colors"
                >
                  <SendHorizonal className="h-4 w-4" />
                  {testEmailStatus === 'sending' ? 'Sending…' : 'Send Test Email'}
                </button>
              </div>
              {testEmailStatus === 'success' && (
                <div className="mt-3 flex items-center gap-2 text-sm text-green-700 dark:text-green-400">
                  <CheckCircle className="h-4 w-4 flex-shrink-0" />
                  {testEmailMessage}
                </div>
              )}
              {testEmailStatus === 'error' && (
                <div className="mt-3 flex items-center gap-2 text-sm text-red-700 dark:text-red-400">
                  <AlertCircle className="h-4 w-4 flex-shrink-0" />
                  {testEmailMessage}
                </div>
              )}
            </div>
          )}

          {/* Settings List */}
          {currentSettings.length === 0 ? (
            <div className="text-center py-12">
              <Server className="h-12 w-12 text-gray-400 mx-auto mb-3" />
              <p className="text-gray-500 dark:text-gray-400">No settings in this category</p>
            </div>
          ) : activeCategory === 'logging' ? (
            // Special rendering for logging settings with clear grouping
            <div className="space-y-8">
              {/* Backend Logging Group */}
              {(() => {
                const backendSettings = currentSettings.filter(s =>
                  ['logging.format', 'logging.level', 'logging.include_caller'].includes(s.key)
                );
                if (backendSettings.length === 0) return null;
                return (
                  <div className="border-l-4 border-blue-500 pl-4">
                    <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3">Backend Logs</h4>
                    <div className="space-y-4">
                      {backendSettings.map((setting) => renderSetting(setting))}
                    </div>
                  </div>
                );
              })()}

              {/* External Logging Targets */}
              <div className="border-l-4 border-indigo-500 pl-4">
                <LoggingTargets />
              </div>

              {/* Frontend Logging Group */}
              {(() => {
                const frontendSettings = currentSettings.filter(s => s.key.startsWith('logging.frontend_'));
                if (frontendSettings.length === 0) return null;
                const enabled = editedValues['logging.frontend_enabled'] ?? frontendSettings.find(s => s.key === 'logging.frontend_enabled')?.value === 'true';
                return (
                  <div className={`border-l-4 ${enabled ? 'border-orange-500' : 'border-gray-300'} pl-4`}>
                    <h4 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-1">Frontend Logs</h4>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">Collect browser errors and warnings</p>
                    <div className="space-y-4">
                      {frontendSettings.map((setting) => renderSetting(setting))}
                    </div>
                  </div>
                );
              })()}
            </div>
          ) : (
            <div className="space-y-6">
              {currentSettings.map((setting) => renderSetting(setting))}
            </div>
          )}
        </div>
      </div>

      {/* Footer Info */}
      <div className="text-sm text-gray-500 dark:text-gray-400 text-center space-y-1">
        <p>Settings are stored in SQLite database • Changes take effect immediately</p>
        <p className="text-xs">Total settings: {settings.length} • Editable: {settings.filter(s => s.editable).length} • Read-only: {settings.filter(s => !s.editable).length}</p>
      </div>
    </div>
  );

  function renderSetting(setting: Setting) {
    const currentValue = getCurrentValue(setting);
    const isEdited = editedValues[setting.key] !== undefined;

    return (
      <div
        key={setting.key}
        className={`pb-6 border-b border-gray-100 dark:border-gray-700 last:border-0 last:pb-0 transition-all ${
          isEdited ? 'bg-yellow-50 dark:bg-yellow-900/10 -mx-6 px-6 py-4 rounded-lg' : ''
        }`}
      >
                    {/* Setting Header */}
                    <div className="flex items-start justify-between gap-4 mb-3">
                      <div className="flex-1">
                        <div className="flex items-center gap-2 mb-1">
                          <label className="text-sm font-semibold text-gray-900 dark:text-white">
                            {formatLabel(setting.key)}
                          </label>
                          {getStatusBadge(currentValue, setting.type)}
                          {isEdited && (
                            <span className="text-xs font-medium text-yellow-600 dark:text-yellow-400 bg-yellow-100 dark:bg-yellow-900/30 px-2 py-0.5 rounded">
                              Modified
                            </span>
                          )}
                        </div>
                        <p className="text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
                          {setting.description}
                        </p>
                      </div>

                      <div className="flex flex-col items-end gap-1 flex-shrink-0">
                        <span className={`px-2 py-1 text-xs font-medium rounded ${
                          setting.editable
                            ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
                            : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400'
                        }`}>
                          {setting.type}
                        </span>
                        {!setting.editable && (
                          <span className="text-xs text-gray-400 dark:text-gray-500">
                            Read-only
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Setting Control */}
                    {setting.editable ? (
                      <div className="mt-3">
                        {setting.type === 'bool' ? (
                          <div className="flex items-center gap-3">
                            <button
                              onClick={() => handleValueChange(setting.key, 'true', setting.value)}
                              className={`px-6 py-2.5 text-sm font-medium rounded-lg transition-all ${
                                currentValue === 'true'
                                  ? 'bg-green-600 text-white shadow-sm'
                                  : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                              }`}
                            >
                              <CheckCircle className="h-4 w-4 inline-block mr-2" />
                              Enabled
                            </button>
                            <button
                              onClick={() => handleValueChange(setting.key, 'false', setting.value)}
                              className={`px-6 py-2.5 text-sm font-medium rounded-lg transition-all ${
                                currentValue === 'false'
                                  ? 'bg-gray-600 text-white shadow-sm'
                                  : 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'
                              }`}
                            >
                              <span className="h-4 w-4 inline-block mr-2">○</span>
                              Disabled
                            </button>
                          </div>
                        ) : getSelectOptions(setting.key) ? (
                          <select
                            value={currentValue}
                            onChange={(e) => handleValueChange(setting.key, e.target.value, setting.value)}
                            className="w-full max-w-xs px-4 py-2.5 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-900 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-colors"
                          >
                            {getSelectOptions(setting.key)!.map(option => (
                              <option key={option} value={option}>
                                {option}
                              </option>
                            ))}
                          </select>
                        ) : setting.type === 'int' ? (
                          <div className="flex items-center gap-2">
                            <input
                              type="number"
                              value={currentValue}
                              onChange={(e) => handleValueChange(setting.key, e.target.value, setting.value)}
                              className="w-full max-w-xs px-4 py-2.5 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-900 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-colors"
                            />
                            <span className="text-sm text-gray-500 dark:text-gray-400 whitespace-nowrap">
                              Current: {formatValueDisplay(setting)}
                            </span>
                          </div>
                        ) : (
                          <input
                            type={setting.key === 'email.smtp_password' ? 'password' : 'text'}
                            value={currentValue}
                            onChange={(e) => handleValueChange(setting.key, e.target.value, setting.value)}
                            className="w-full max-w-md px-4 py-2.5 text-sm border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-900 text-gray-900 dark:text-white focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-colors"
                          />
                        )}
                      </div>
                    ) : (
                      <div className="mt-3">
                        <div className="px-4 py-2.5 bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg text-sm text-gray-700 dark:text-gray-300 max-w-md font-medium">
                          {formatValueDisplay(setting)}
                        </div>
                      </div>
                    )}

        {/* Setting Metadata */}
        <div className="mt-3 flex items-center gap-4 text-xs text-gray-400 dark:text-gray-500">
          <span>Key: <code className="text-gray-600 dark:text-gray-400 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded">{setting.key}</code></span>
          {isEdited && (
            <span className="text-yellow-600 dark:text-yellow-400">
              Original: {formatValueDisplay({ ...setting, value: setting.value } as Setting)}
            </span>
          )}
        </div>
      </div>
    );
  }
}
