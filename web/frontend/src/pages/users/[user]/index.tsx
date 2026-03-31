import React, { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import ModalManager from '@/lib/modals';
import {
  ArrowLeft,
  User as UserIcon,
  Mail,
  Shield,
  Edit,
  CheckCircle,
  XCircle,
  Key,
  Plus,
  Trash2,
  Copy,
  Download,
  Calendar,
  KeyRound,
  Settings,
  UsersRound,
  UserMinus,
} from 'lucide-react';
import { UserPreferences } from '@/components/preferences/UserPreferences';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { AccessKey, EditUserForm, Group } from '@/types';
import Setup2FAModal, { BackupCodesModal } from '@/components/Setup2FAModal';
import { ConfirmModal } from '@/components/ui/Modal';
import { EmptyState } from '@/components/ui/EmptyState';
import { escapeHtml } from '@/lib/utils';

export default function UserDetailsPage() {
  const { t } = useTranslation('users');
  const { user } = useParams<{ user: string }>();
  const navigate = useNavigate();
  const userId = user as string;
  
  const [isEditUserModalOpen, setIsEditUserModalOpen] = useState(false);
  const [isCreateKeyModalOpen, setIsCreateKeyModalOpen] = useState(false);
  const [editForm, setEditForm] = useState<EditUserForm>({
    email: '',
    roles: [],
    status: 'active',
    tenantId: undefined,
  });
  const [newKeyName, setNewKeyName] = useState('');
  const [showSecretKeys, setShowSecretKeys] = useState<Record<string, boolean>>({});
  const [createdKey, setCreatedKey] = useState<AccessKey | null>(null);
  const [isChangePasswordOpen, setIsChangePasswordOpen] = useState(false);
  const [passwordForm, setPasswordForm] = useState({
    currentPassword: '',
    newPassword: '',
    confirmPassword: '',
  });
  const [showSetup2FAModal, setShowSetup2FAModal] = useState(false);
  const [showBackupCodesModal, setShowBackupCodesModal] = useState(false);
  const [showDisable2FAModal, setShowDisable2FAModal] = useState(false);
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [disabling2FA, setDisabling2FA] = useState(false);
  const queryClient = useQueryClient();

  // Fetch current user to check if they're an admin
  const { data: currentUser } = useQuery({
    queryKey: ['currentUser'],
    queryFn: APIClient.getCurrentUser,
  });

  // Check if user is editing their own profile
  const isEditingSelf = currentUser?.id === userId;

  // Fetch user data
  const { data: userData, isLoading: userLoading } = useQuery({
    queryKey: ['user', userId, isEditingSelf ? 'self' : 'admin'],
    queryFn: () => (isEditingSelf ? APIClient.getCurrentUser() : APIClient.getUser(userId)),
    enabled: !!userId, // Only fetch when userId is available
  });

  // Fetch access keys
  const { data: accessKeys, isLoading: keysLoading } = useQuery({
    queryKey: ['accessKeys', userId, isEditingSelf ? 'self' : 'admin'],
    queryFn: () => (isEditingSelf ? APIClient.getAccessKeys() : APIClient.getUserAccessKeys(userId)),
    enabled: !!userId, // Only fetch when userId is available
  });

  // Fetch tenants for assignment
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  // Fetch all users from the same tenant to check if this is the last admin (admins only)
  const isCurrentUserAdmin = currentUser?.roles?.includes('admin');
  const { data: allUsers } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    enabled: !!isCurrentUserAdmin,
  });

  // Fetch 2FA status for this user
  const { data: twoFactorStatus, isLoading: is2FALoading, refetch: refetch2FAStatus } = useQuery({
    queryKey: ['twoFactorStatus', userId],
    queryFn: () => APIClient.get2FAStatus(userId),
    enabled: !!userId,
  });

  // Fetch groups this user belongs to (admins only)
  const { data: userGroups = [] } = useQuery({
    queryKey: ['userGroups', userId],
    queryFn: () => APIClient.listUserGroups(userId),
    enabled: !!userId && !!isCurrentUserAdmin,
  });

  // Fetch all groups for the add-to-group dropdown, scoped to the user's tenant
  const [isAddToGroupOpen, setIsAddToGroupOpen] = useState(false);
  const [selectedGroupId, setSelectedGroupId] = useState('');
  const { data: allGroups = [] } = useQuery({
    queryKey: ['groups', userData?.tenantId, !userData?.tenantId],
    queryFn: () => APIClient.listGroups(userData?.tenantId, !userData?.tenantId),
    enabled: isAddToGroupOpen && !!userData,
  });

  const addToGroupMutation = useMutation({
    mutationFn: (groupId: string) => APIClient.addGroupMember(groupId, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userGroups', userId] });
      setIsAddToGroupOpen(false);
      setSelectedGroupId('');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const removeFromGroupMutation = useMutation({
    mutationFn: (groupId: string) => APIClient.removeGroupMember(groupId, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['userGroups', userId] });
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Exclude groups the user already belongs to
  const availableGroups = allGroups.filter(
    (g: Group) => !userGroups.some((ug: Group) => ug.id === g.id)
  );

  // Update user mutation
  const updateUserMutation = useMutation({
    mutationFn: (data: EditUserForm) => APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', userId] });
      setIsEditUserModalOpen(false);
      ModalManager.toast('success', t('userUpdatedSuccess'));
    },
    onError: (error) => {
      ModalManager.apiError(error);
    },
  });

  // Create access key mutation
  const createAccessKeyMutation = useMutation({
    mutationFn: () => APIClient.createAccessKey({ userId }),
    onSuccess: (response) => {
      // Refetch to update immediately
      queryClient.refetchQueries({ queryKey: ['accessKeys', userId] }); // Update user's keys
      queryClient.refetchQueries({ queryKey: ['accessKeys'] }); // Update global access keys list
      queryClient.refetchQueries({ queryKey: ['users'] }); // Update users list (shows key count)
      queryClient.refetchQueries({ queryKey: ['tenants'] }); // Update tenant access key count
      setCreatedKey(response);
      setIsCreateKeyModalOpen(false);
      setNewKeyName('');
      ModalManager.toast('success', t('accessKeyCreatedSuccess'));
    },
    onError: (error) => {
      ModalManager.apiError(error);
    },
  });

  // Delete access key mutation
  const deleteAccessKeyMutation = useMutation({
    mutationFn: (keyId: string) => APIClient.deleteAccessKey(userId, keyId),
    onSuccess: async (_, keyId) => {
      ModalManager.close();

      // Update cache immediately for this user's keys
      queryClient.setQueryData(['accessKeys', userId], (oldData: AccessKey[] | undefined) => {
        if (!oldData) return [];
        return oldData.filter(key => key.id !== keyId);
      });

      // Update cache for global access keys list
      queryClient.setQueryData(['accessKeys'], (oldData: AccessKey[] | undefined) => {
        if (!oldData) return [];
        return oldData.filter(key => key.id !== keyId);
      });

      // Refetch other queries to update counts immediately
      queryClient.refetchQueries({ queryKey: ['users'] });
      queryClient.refetchQueries({ queryKey: ['tenants'] });

      // Force refetch to ensure we have the latest data from server
      await Promise.all([
        queryClient.refetchQueries({ queryKey: ['accessKeys', userId] }),
        queryClient.refetchQueries({ queryKey: ['accessKeys'] }),
      ]);

      ModalManager.toast('success', t('accessKeyDeletedSuccess'));
    },
    onError: (error) => {
      ModalManager.close();
      ModalManager.apiError(error);
    },
  });

  // Change password mutation
  const changePasswordMutation = useMutation({
    mutationFn: (data: { currentPassword: string; newPassword: string }) =>
      APIClient.changePassword(userId, data.currentPassword, data.newPassword),
    onSuccess: () => {
      setIsChangePasswordOpen(false);
      setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
      ModalManager.toast('success', t('passwordChangedSuccess'));
    },
    onError: (error) => {
      ModalManager.apiError(error);
    },
  });

  // Initialize edit form when user data is loaded
  useEffect(() => {
    if (userData) {
      setEditForm({
        email: userData.email || '',
        roles: userData.roles || [],
        status: userData.status,
        tenantId: userData.tenantId || undefined,
      });
    }
  }, [userData]);

  // Check if this is the last admin in the tenant
  const isLastAdminInTenant = () => {
    if (!userData?.tenantId) return false; // Global users not affected

    // Get all users from the same tenant
    const tenantUsers = allUsers?.filter(u => u.tenantId === userData.tenantId) || [];

    // Count admins in this tenant
    const adminCount = tenantUsers.filter(u => u.roles?.includes('admin')).length;

    // Check if current user is admin and would be removed
    const isCurrentlyAdmin = userData.roles?.includes('admin');
    const willRemoveAdmin = isCurrentlyAdmin && !editForm.roles.includes('admin');

    return adminCount === 1 && willRemoveAdmin;
  };

  // Handlers
  const handleEditUser = (e: React.FormEvent) => {
    e.preventDefault();

    // Validate: Cannot remove last admin from tenant
    if (isLastAdminInTenant()) {
      ModalManager.error(t('cannotRemoveAdminRole'), t('lastAdminInTenant'));
      return;
    }

    updateUserMutation.mutate(editForm);
  };

  const handleCreateAccessKey = (e: React.FormEvent) => {
    e.preventDefault();
    createAccessKeyMutation.mutate();
  };

  const handleDeleteAccessKey = async (keyId: string, keyDescription: string) => {
    try {
      const result = await ModalManager.fire({
        icon: 'warning',
        title: t('deleteAccessKeyQuestion'),
        html: `<p>${t('aboutToDeleteKey')} <strong>"${escapeHtml(keyDescription)}"</strong></p>
               <p class="text-red-600 mt-2">${t('actionCannotBeUndone')}</p>`,
        showCancelButton: true,
        confirmButtonText: t('yesDelete'),
        cancelButtonText: t('cancel'),
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        ModalManager.loading(t('deletingAccessKey'), t('deletingAccessKeyMessage', { keyId: keyDescription }));
        deleteAccessKeyMutation.mutate(keyId);
      }
    } catch (error) {
      ModalManager.apiError(error);
    }
  };

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      ModalManager.toast('success', t('copiedToClipboard'));
    } catch (err) {
      ModalManager.toast('error', err.message);
    }
  };

  const downloadAsCSV = (key: AccessKey) => {
    const csvContent = `Access Key ID,Secret Access Key,Status,Created At\n${key.accessKey},${key.secretKey || 'N/A'},${key.status},${formatDate(key.createdAt)}`;
    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.setAttribute('href', url);
    link.setAttribute('download', `access-key-${key.accessKey}.csv`);
    link.style.visibility = 'hidden';
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    ModalManager.toast('success', t('csvDownloaded'));
  };

  const handleChangePassword = () => {
    // Admin changing another user's password doesn't need current password
    const isAdminChangingOtherUser = isCurrentUserAdmin && !isEditingSelf;
    
    if (!isAdminChangingOtherUser && !passwordForm.currentPassword) {
      ModalManager.toast('error', t('currentPasswordRequired'));
      return;
    }

    if (!passwordForm.newPassword || !passwordForm.confirmPassword) {
      ModalManager.toast('error', t('newPasswordRequired'));
      return;
    }

    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      ModalManager.toast('error', t('passwordsMismatch'));
      return;
    }

    if (passwordForm.newPassword.length < 6) {
      ModalManager.toast('error', t('passwordTooShort'));
      return;
    }

    changePasswordMutation.mutate({
      currentPassword: isAdminChangingOtherUser ? '' : passwordForm.currentPassword,
      newPassword: passwordForm.newPassword,
    });
  };

  // 2FA Handlers
  // Global admin = admin role AND no tenant assignment
  const isGlobalAdmin = currentUser?.roles?.includes('admin') && !currentUser?.tenantId;
  const isCurrentUser = currentUser?.id === userId;

  const handleSetup2FASuccess = (codes: string[]) => {
    setBackupCodes(codes);
    setShowBackupCodesModal(true);
    refetch2FAStatus();
  };

  const handleDisable2FA = async () => {
    setDisabling2FA(true);
    try {
      await APIClient.disable2FA(userId);
      await ModalManager.success(t('twoFADisabled'), t('twoFADisabledDesc'));
      setShowDisable2FAModal(false);
      refetch2FAStatus();
    } catch (error: unknown) {
      await ModalManager.apiError(error);
    } finally {
      setDisabling2FA(false);
    }
  };

  const handleRegenerateBackupCodes = async () => {
    try {
      const result = await ModalManager.confirm(
        t('regenerateBackupCodes'),
        t('regenerateCodesConfirm'),
        undefined,
        {
          confirmButtonText: t('regenerate'),
          icon: 'warning'
        }
      );

      if (result.isConfirmed) {
        ModalManager.loading(t('regenerating'), t('generatingCodes'));
        const data = await APIClient.regenerateBackupCodes();
        ModalManager.close();

        setBackupCodes(data.backup_codes);
        setShowBackupCodesModal(true);

        await ModalManager.success(t('codesRegenerated'), t('codesRegeneratedDesc'));
      }
    } catch (error: unknown) {
      ModalManager.close();
      await ModalManager.apiError(error);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border border-green-200 dark:border-green-800';
      case 'inactive':
        return 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border border-border';
      case 'suspended':
        return 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-400 border border-red-200 dark:border-red-800';
      default:
        return 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border border-border';
    }
  };

  const formatDate = (date: string | number) => {
    // If it's a number (Unix timestamp in seconds), convert to milliseconds
    const timestamp = typeof date === 'number' ? date * 1000 : new Date(date).getTime();
    return new Date(timestamp).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  // Check if userId exists
  if (!userId) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (userLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  if (!userData) {
    return (
      <div className="text-center py-8">
        <h3 className="text-lg font-semibold">{t('userNotFound')}</h3>
        <p className="text-sm text-muted-foreground mt-1">{t('userNotFoundDesc')}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4">
        <div>
          <Button
            variant="outline"
            size="default"
            onClick={() => navigate('/users')}
            className="gap-2 bg-card hover:bg-secondary border-border"
          >
            <ArrowLeft className="h-4 w-4" />
            {t('backToUsers')}
          </Button>
        </div>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-foreground">{userData.username}</h1>
            <p className="text-sm text-muted-foreground mt-1">
              {t('userDetailsConfig')}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              onClick={() => setIsChangePasswordOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Key className="h-4 w-4" />
              {t('changePassword')}
            </Button>
            <Button
              onClick={() => setIsEditUserModalOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Edit className="h-4 w-4" />
              {t('editUser')}
            </Button>
            <Button
              onClick={() => setIsCreateKeyModalOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              {t('createAccessKey')}
            </Button>
          </div>
        </div>
      </div>

      {/* User Info Cards */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Status Card */}
        <div className="bg-card rounded-lg border border-border shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-muted-foreground mb-1">{t('status')}</p>
              <div className="mt-2">
                <span className={`inline-flex items-center px-2.5 py-1 rounded-full text-sm font-medium ${getStatusColor(userData.status)}`}>
                  {userData.status === 'active' ? <CheckCircle className="h-4 w-4 mr-1" /> : <XCircle className="h-4 w-4 mr-1" />}
                  {t(userData.status as 'active' | 'inactive' | 'suspended')}
                </span>
              </div>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
              <UserIcon className="h-7 w-7 text-brand-600 dark:text-brand-400" />
            </div>
          </div>
        </div>

        {/* Email Card */}
        <div className="bg-card rounded-lg border border-border shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-muted-foreground mb-1">{t('email')}</p>
              <h3 className="text-lg font-bold text-foreground break-all">{userData.email || t('notProvided')}</h3>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-gray-100 dark:bg-gray-700/50">
              <Mail className="h-7 w-7 text-muted-foreground" />
            </div>
          </div>
        </div>

        {/* Roles Card */}
        <div className="bg-card rounded-lg border border-border shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-muted-foreground mb-1">{t('roles')}</p>
              <div className="flex flex-wrap gap-1 mt-2">
                {userData.roles && userData.roles.length > 0 ? (
                  userData.roles.map((role: string) => (
                    <span
                      key={role}
                      className="inline-flex items-center px-2 py-1 rounded-md text-xs font-medium bg-gray-100 dark:bg-gray-700/50 text-gray-800 dark:text-gray-300 border border-border"
                    >
                      {role}
                    </span>
                  ))
                ) : (
                  <span className="text-xs text-muted-foreground">{t('noRolesAssigned')}</span>
                )}
              </div>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-gray-100 dark:bg-gray-700/50">
              <Shield className="h-7 w-7 text-muted-foreground" />
            </div>
          </div>
        </div>
      </div>

      {/* Security & Preferences Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Two-Factor Authentication Section */}
        <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-border">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <KeyRound className="h-5 w-5 text-muted-foreground" />
              <div>
                <h3 className="text-lg font-semibold text-foreground">
                  {t('twoFactorTitle')}
                </h3>
                <p className="text-sm text-muted-foreground mt-1">
                  {t('twoFactorSubtitle')}
                </p>
              </div>
            </div>
          </div>
        </div>

        <div className="p-6">
          {is2FALoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="sm" />
            </div>
          ) : (
            <div className="space-y-4">
              <div className="flex items-center justify-between p-4 bg-gray-50 dark:bg-gray-900 rounded-lg">
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-foreground">{t('twoFactorStatusLabel')}</span>
                    {twoFactorStatus?.enabled ? (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300">
                        <CheckCircle className="h-3 w-3 mr-1" />
                        {t('enabled')}
                      </span>
                    ) : (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-300">
                        {t('disabled')}
                      </span>
                    )}
                  </div>
                  {twoFactorStatus?.enabled && twoFactorStatus?.setup_at && (
                    <p className="text-xs text-muted-foreground mt-1">
                      {t('enabledOn', { date: new Date(twoFactorStatus.setup_at * 1000).toLocaleDateString() })}
                    </p>
                  )}
                </div>

                <div className="flex gap-2">
                  {twoFactorStatus?.enabled ? (
                    <>
                      {isCurrentUser && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={handleRegenerateBackupCodes}
                        >
                          {t('regenerateBackupCodes')}
                        </Button>
                      )}
                      {(isCurrentUser || isGlobalAdmin) && (
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setShowDisable2FAModal(true)}
                        >
                          {t('disable2FA')}
                        </Button>
                      )}
                    </>
                  ) : (
                    <>
                      {isCurrentUser && (
                        <Button
                          size="sm"
                          onClick={() => setShowSetup2FAModal(true)}
                        >
                          <KeyRound className="h-4 w-4" />
                          {t('enable2FA')}
                        </Button>
                      )}
                    </>
                  )}
                </div>
              </div>

              {!twoFactorStatus?.enabled && isCurrentUser && (
                <div className="bg-yellow-50 dark:bg-yellow-900/20 border-l-4 border-yellow-500 p-3 rounded">
                  <div className="flex">
                    <div className="flex-shrink-0">
                      <svg className="h-4 w-4 text-yellow-600 dark:text-yellow-400" viewBox="0 0 20 20" fill="currentColor">
                        <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
                      </svg>
                    </div>
                    <div className="ml-3">
                      <p className="text-xs text-yellow-800 dark:text-yellow-200">
                        <strong className="font-medium">{t('recommended')}</strong> {t('recommendEnable2FA')}
                      </p>
                    </div>
                  </div>
                </div>
              )}

              {!isCurrentUser && !isGlobalAdmin && twoFactorStatus?.enabled && (
                <div className="bg-blue-50 dark:bg-blue-900/20 border-l-4 border-blue-500 p-3 rounded">
                  <div className="flex">
                    <div className="flex-shrink-0">
                      <Shield className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                    </div>
                    <div className="ml-3">
                      <p className="text-xs text-blue-800 dark:text-blue-200">
                        {t('only2FAGlobalAdmins')}
                      </p>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
        </div>

        {/* User Preferences */}
        <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-base font-semibold text-foreground flex items-center gap-2">
              <Settings className="h-4 w-4 text-muted-foreground" />
              {t('preferences')}
            </h3>
          </div>

          <div className="p-6">
            <UserPreferences disabled={!isCurrentUser} />
          </div>
        </div>

        {/* Groups — visible to admins only */}
        {isCurrentUserAdmin && (
          <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
            <div className="px-6 py-4 border-b border-border flex items-center justify-between">
              <h3 className="text-base font-semibold text-foreground flex items-center gap-2">
                <UsersRound className="h-4 w-4 text-muted-foreground" />
                {t('groupsTitle')}
              </h3>
              <Button size="sm" onClick={() => setIsAddToGroupOpen(true)} className="gap-2">
                <Plus className="h-4 w-4" />
                {t('addToGroup')}
              </Button>
            </div>

            <div className="p-6">
              {userGroups.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-4">
                  {t('notInAnyGroup')}
                </p>
              ) : (
                <div className="space-y-2">
                  {userGroups.map((group: Group) => (
                    <div
                      key={group.id}
                      className="flex items-center justify-between px-3 py-2 rounded-lg bg-secondary"
                    >
                      <div>
                        <p className="text-sm font-medium text-foreground">{group.displayName || group.name}</p>
                        {group.displayName && (
                          <p className="text-xs text-muted-foreground">{group.name}</p>
                        )}
                      </div>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          ModalManager.confirmDelete(group.displayName || group.name, t('groupMembership')).then((result) => {
                            if (result.isConfirmed) {
                              removeFromGroupMutation.mutate(group.id);
                            }
                          })
                        }
                        className="text-red-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20"
                      >
                        <UserMinus className="h-4 w-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Add to Group Modal */}
      <Modal
        isOpen={isAddToGroupOpen}
        onClose={() => { setIsAddToGroupOpen(false); setSelectedGroupId(''); }}
        title={t('addToGroupModalTitle')}
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-foreground mb-1">{t('selectGroupLabel')}</label>
            <select
              className="w-full rounded-md border border-input bg-background text-foreground px-3 py-2 text-sm"
              value={selectedGroupId}
              onChange={(e) => setSelectedGroupId(e.target.value)}
            >
              <option value="">{t('selectGroupPlaceholder')}</option>
              {availableGroups.map((g: Group) => (
                <option key={g.id} value={g.id}>
                  {g.displayName || g.name}
                  {g.memberCount !== undefined ? ` (${g.memberCount})` : ''}
                </option>
              ))}
            </select>
            {availableGroups.length === 0 && (
              <p className="text-xs text-muted-foreground mt-1">
                {t('alreadyInAllGroups')}
              </p>
            )}
          </div>
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => { setIsAddToGroupOpen(false); setSelectedGroupId(''); }}>
              {t('cancel')}
            </Button>
            <Button
              onClick={() => addToGroupMutation.mutate(selectedGroupId)}
              disabled={!selectedGroupId || addToGroupMutation.isPending}
            >
              {addToGroupMutation.isPending ? t('addingToGroup') : t('addToGroup')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Password Management - Modal */}
      <Modal
        isOpen={isChangePasswordOpen}
        onClose={() => {
          setIsChangePasswordOpen(false);
          setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
        }}
        title={t('changePassword')}
      >
        <div className="space-y-4">
          {/* Only show current password field when user is changing their own password */}
          {isEditingSelf && (
            <div>
              <label className="text-sm font-medium text-foreground mb-2 block">{t('currentPassword')}</label>
              <Input
                type="password"
                placeholder={t('enterCurrentPassword')}
                value={passwordForm.currentPassword}
                onChange={(e) => setPasswordForm({ ...passwordForm, currentPassword: e.target.value })}
                className="bg-card text-foreground border-border"
              />
            </div>
          )}
          {!isEditingSelf && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3 mb-4">
              <p className="text-sm text-blue-800 dark:text-blue-300">
                {t('adminResetPasswordInfo')}
              </p>
            </div>
          )}
          <div>
            <label className="text-sm font-medium text-foreground mb-2 block">{t('newPassword')}</label>
            <Input
              type="password"
              placeholder={t('enterNewPassword')}
              value={passwordForm.newPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, newPassword: e.target.value })}
              className="bg-card text-foreground border-border"
            />
          </div>
          <div>
            <label className="text-sm font-medium text-foreground mb-2 block">{t('confirmNewPassword')}</label>
            <Input
              type="password"
              placeholder={t('confirmNewPasswordPlaceholder')}
              value={passwordForm.confirmPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, confirmPassword: e.target.value })}
              className="bg-card text-foreground border-border"
            />
          </div>
          <div className="flex gap-2 justify-end mt-6">
            <Button
              variant="outline"
              onClick={() => {
                setIsChangePasswordOpen(false);
                setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
              }}
            >
              {t('cancel')}
            </Button>
            <Button
              onClick={handleChangePassword}
              disabled={changePasswordMutation.isPending}
              variant="outline"
            >
              {changePasswordMutation.isPending ? t('changing') : t('changePassword')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Access Keys */}
      <div className="bg-card rounded-xl border border-border shadow-md overflow-hidden">
        <div className="px-6 py-5 border-b border-border">
          <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
            <Key className="h-5 w-5 text-muted-foreground" />
            {t('accessKeysCount', { count: accessKeys?.length || 0 })}
          </h3>
          <p className="text-sm text-muted-foreground mt-1">{t('manageS3Creds')}</p>
        </div>
        <div className="overflow-x-auto">
          {keysLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : !accessKeys || accessKeys.length === 0 ? (
            <EmptyState
              icon={Key}
              title={t('noAccessKeysProfile')}
              description={t('createKeyDesc')}
              actionLabel={t('createAccessKey')}
              onAction={() => setIsCreateKeyModalOpen(true)}
              showAction={true}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('accessKeyId')}</TableHead>
                  <TableHead>{t('status')}</TableHead>
                  <TableHead>{t('created')}</TableHead>
                  <TableHead>{t('lastTimeUsed')}</TableHead>
                  <TableHead className="text-right">{t('actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {accessKeys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Key className="h-4 w-4 text-muted-foreground" />
                        <code className="text-sm bg-gray-100 dark:bg-gray-800 text-foreground px-2 py-1 rounded">{key.id}</code>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => copyToClipboard(key.id)}
                          title={t('copyAccessKey')}
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium border ${
                        key.status === 'active'
                          ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border-green-200 dark:border-green-800'
                          : 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border-border'
                      }`}>
                        {key.status}
                      </span>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                        {formatDate(key.createdAt)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1 text-sm text-muted-foreground">
                        <Calendar className="h-3 w-3" />
                          {!key.lastUsed || isNaN(new Date(key.lastUsed).getTime())
                          ? t('never')
                          : formatDate(key.lastUsed)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteAccessKey(key.id, key.id)}
                          title={t('deleteAccessKey')}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </div>

      {/* Edit User Modal */}
      <Modal
        isOpen={isEditUserModalOpen}
        onClose={() => setIsEditUserModalOpen(false)}
        title={t('editUser')}
      >
        <form onSubmit={handleEditUser} className="space-y-4">
          <div>
            <label htmlFor="email" className="block text-sm font-medium text-foreground mb-2">
              {t('email')}
            </label>
            <Input
              id="email"
              type="email"
              value={editForm.email}
              onChange={(e) => setEditForm(prev => ({ ...prev, email: e.target.value }))}
              placeholder="user@example.com"
              className="bg-card text-foreground border-border"
            />
          </div>

          <div>
            <label htmlFor="tenant" className="block text-sm font-medium text-foreground mb-2">
              {t('tenantOptional')}
            </label>
            <select
              id="tenant"
              value={editForm.tenantId || ''}
              onChange={(e) => setEditForm(prev => ({ ...prev, tenantId: e.target.value || undefined }))}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="">{t('noTenantGlobal')}</option>
              {tenants?.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.displayName} ({tenant.name})
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground mt-1">
              {t('globalUsersInfo')}
            </p>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium text-foreground mb-2">
              {t('status')}
            </label>
            <select
              id="status"
              value={editForm.status}
              onChange={(e) => setEditForm(prev => ({ ...prev, status: e.target.value as any }))}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="active">{t('active')}</option>
              <option value="inactive">{t('inactive')}</option>
              <option value="suspended">{t('suspended')}</option>
            </select>
          </div>

          <div>
            <label htmlFor="roles" className="block text-sm font-medium text-foreground mb-2">
              {t('role')}
            </label>
            <select
              id="roles"
              value={editForm.roles[0] || 'user'}
              onChange={(e) => setEditForm(prev => ({
                ...prev,
                roles: [e.target.value]
              }))}
              disabled={!isCurrentUserAdmin || (isCurrentUserAdmin && isEditingSelf)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <option value="admin">{t('adminRole')}</option>
              <option value="user">{t('userRole')}</option>
              <option value="readonly">{t('readonlyRole')}</option>
              <option value="guest">{t('guestRole')}</option>
            </select>
            {!isCurrentUserAdmin && (
              <p className="text-xs text-muted-foreground mt-1">
                {t('onlyAdminsChangeRoles')}
              </p>
            )}
            {isCurrentUserAdmin && isEditingSelf && (
              <p className="text-xs text-yellow-600 dark:text-yellow-400 mt-1">
                {t('cannotChangeOwnRole')}
              </p>
            )}
            {isCurrentUserAdmin && !isEditingSelf && (
              <p className="text-xs text-muted-foreground mt-1">
                {t('selectRole')}
              </p>
            )}
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsEditUserModalOpen(false)}
            >
              {t('cancel')}
            </Button>
            <Button
              type="submit"
              variant="outline"
              disabled={updateUserMutation.isPending}
            >
              {updateUserMutation.isPending ? t('saving') : t('saveChanges')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Access Key Modal */}
      <Modal
        isOpen={isCreateKeyModalOpen}
        onClose={() => setIsCreateKeyModalOpen(false)}
        title={t('createNewAccessKey')}
      >
        <form onSubmit={handleCreateAccessKey} className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md p-3">
            <p className="text-sm text-blue-800 dark:text-blue-300">
              {t('accessKeyInfoMsg')}
            </p>
          </div>

          <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-md p-3">
            <p className="text-sm text-yellow-800 dark:text-yellow-300">
              {t('secretKeyOnceMsg')}
            </p>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateKeyModalOpen(false)}
            >
              {t('cancel')}
            </Button>
            <Button
              type="submit"
              variant="outline"
              disabled={createAccessKeyMutation.isPending}
            >
              {createAccessKeyMutation.isPending ? t('creating') : t('createAccessKey')}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Created Key Modal */}
      {createdKey && (
        <Modal
          isOpen={!!createdKey}
          onClose={() => setCreatedKey(null)}
          title={t('accessKeyCreatedTitle')}
        >
          <div className="space-y-4">
            <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md p-3">
              <p className="text-sm text-green-800 dark:text-green-300">
                <strong>{t('accessKeyCreatedOK')}</strong>
              </p>
            </div>

            <div className="space-y-3">
              <div>
                <label className="block text-sm font-medium text-foreground mb-1">{t('accessKeyIdLabel')}</label>
                <div className="flex items-center gap-2">
                  <code className="bg-gray-100 dark:bg-gray-800 text-foreground px-3 py-2 rounded text-sm flex-1">
                    {createdKey.accessKey}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => copyToClipboard(createdKey.accessKey)}
                    title={t('copyToClipboard')}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              {createdKey.secretKey && (
                <div>
                  <label className="block text-sm font-medium text-foreground mb-1">{t('secretAccessKeyLabel')}</label>
                  <div className="flex items-center gap-2">
                    <code className="bg-gray-100 dark:bg-gray-800 text-foreground px-3 py-2 rounded text-sm flex-1">
                      {createdKey.secretKey}
                    </code>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => copyToClipboard(createdKey.secretKey!)}
                      title={t('copyToClipboard')}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              )}
            </div>

            <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-md p-3">
              <p className="text-sm text-red-800 dark:text-red-300">
                {t('secretKeyWarning')}
              </p>
            </div>

            <div className="flex justify-between items-center">
              <Button
                variant="outline"
                onClick={() => downloadAsCSV(createdKey)}
                className="gap-2"
              >
                <Download className="h-4 w-4" />
                {t('downloadCSV')}
              </Button>
              <Button
                onClick={() => setCreatedKey(null)}
                variant="outline"
              >
                {t('gotIt')}
              </Button>
            </div>
          </div>
        </Modal>
      )}
      {/* 2FA Modals */}
      <Setup2FAModal
        isOpen={showSetup2FAModal}
        onClose={() => setShowSetup2FAModal(false)}
        onSuccess={handleSetup2FASuccess}
      />

      <BackupCodesModal
        isOpen={showBackupCodesModal}
        onClose={() => setShowBackupCodesModal(false)}
        backupCodes={backupCodes}
      />

      <ConfirmModal
        isOpen={showDisable2FAModal}
        onClose={() => setShowDisable2FAModal(false)}
        onConfirm={handleDisable2FA}
        title={t('disable2FATitle')}
        message={isCurrentUser
          ? t('disable2FAConfirmSelf')
          : t('disable2FAConfirmOther')}
        confirmText={t('disable2FA')}
        cancelText={t('cancel')}
        variant="danger"
        loading={disabling2FA}
      />
    </div>
  );
}
