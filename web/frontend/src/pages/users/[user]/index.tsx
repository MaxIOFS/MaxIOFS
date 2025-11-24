import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import SweetAlert from '@/lib/sweetalert';
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
  Settings
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
import { AccessKey, EditUserForm } from '@/types';
import Setup2FAModal, { BackupCodesModal } from '@/components/Setup2FAModal';
import { ConfirmModal } from '@/components/ui/Modal';
import { EmptyState } from '@/components/ui/EmptyState';

export default function UserDetailsPage() {
  const { user } = useParams<{ user: string }>();
  const navigate = useNavigate();
  const userId = user as string;
  
  // Debug: Log to verify this component is rendering
  console.log('UserDetailsPage rendering with userId:', userId);
  
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

  // Fetch user data
  const { data: userData, isLoading: userLoading } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => APIClient.getUser(userId),
    enabled: !!userId, // Only fetch when userId is available
  });

  // Fetch access keys
  const { data: accessKeys, isLoading: keysLoading } = useQuery({
    queryKey: ['accessKeys', userId],
    queryFn: () => APIClient.getUserAccessKeys(userId),
    enabled: !!userId, // Only fetch when userId is available
  });

  // Fetch tenants for assignment
  const { data: tenants } = useQuery({
    queryKey: ['tenants'],
    queryFn: APIClient.getTenants,
  });

  // Fetch current user to check if they're an admin
  const { data: currentUser } = useQuery({
    queryKey: ['currentUser'],
    queryFn: APIClient.getCurrentUser,
  });

  // Fetch all users from the same tenant to check if this is the last admin
  const { data: allUsers } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
  });

  // Fetch 2FA status for this user
  const { data: twoFactorStatus, isLoading: is2FALoading, refetch: refetch2FAStatus } = useQuery({
    queryKey: ['twoFactorStatus', userId],
    queryFn: () => APIClient.get2FAStatus(userId),
    enabled: !!userId,
  });

  // Update user mutation
  const updateUserMutation = useMutation({
    mutationFn: (data: EditUserForm) => APIClient.updateUser(userId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['user', userId] });
      setIsEditUserModalOpen(false);
      SweetAlert.toast('success', 'User updated successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
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
      SweetAlert.toast('success', 'Access key created successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  // Delete access key mutation
  const deleteAccessKeyMutation = useMutation({
    mutationFn: (keyId: string) => APIClient.deleteAccessKey(userId, keyId),
    onSuccess: async (_, keyId) => {
      SweetAlert.close();

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

      SweetAlert.toast('success', 'Access key deleted successfully');
    },
    onError: (error) => {
      SweetAlert.close();
      SweetAlert.apiError(error);
    },
  });

  // Change password mutation
  const changePasswordMutation = useMutation({
    mutationFn: (data: { currentPassword: string; newPassword: string }) =>
      APIClient.changePassword(userId, data.currentPassword, data.newPassword),
    onSuccess: () => {
      setIsChangePasswordOpen(false);
      setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
      SweetAlert.toast('success', 'Password changed successfully');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
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

  // Check if user is editing their own profile
  const isEditingSelf = currentUser?.id === userId;

  // Check if current user is admin (global or tenant admin)
  const isCurrentUserAdmin = currentUser?.roles?.includes('admin');

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
      SweetAlert.error(
        'Cannot Remove Admin Role',
        'Every tenant must have at least one admin. This is the last admin in the tenant.'
      );
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
      const result = await SweetAlert.fire({
        icon: 'warning',
        title: 'Delete access key?',
        html: `<p>You are about to delete the access key <strong>"${keyDescription}"</strong></p>
               <p class="text-red-600 mt-2">This action cannot be undone</p>`,
        showCancelButton: true,
        confirmButtonText: 'Yes, delete',
        cancelButtonText: 'Cancel',
        confirmButtonColor: '#dc2626',
      });

      if (result.isConfirmed) {
        SweetAlert.loading('Deleting access key...', `Deleting "${keyDescription}"`);
        deleteAccessKeyMutation.mutate(keyId);
      }
    } catch (error) {
      SweetAlert.apiError(error);
    }
  };

  const copyToClipboard = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      SweetAlert.toast('success', 'Copied to clipboard');
    } catch (err) {
      SweetAlert.toast('error', err.message);
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
    SweetAlert.toast('success', 'CSV downloaded successfully');
  };

  const handleChangePassword = () => {
    // Admin changing another user's password doesn't need current password
    const isAdminChangingOtherUser = isCurrentUserAdmin && !isEditingSelf;
    
    if (!isAdminChangingOtherUser && !passwordForm.currentPassword) {
      SweetAlert.toast('error', 'Current password is required');
      return;
    }

    if (!passwordForm.newPassword || !passwordForm.confirmPassword) {
      SweetAlert.toast('error', 'New password fields are required');
      return;
    }

    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      SweetAlert.toast('error', 'New passwords do not match');
      return;
    }

    if (passwordForm.newPassword.length < 6) {
      SweetAlert.toast('error', 'Password must be at least 6 characters');
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
      await SweetAlert.success(
        '2FA Disabled',
        'Two-factor authentication has been disabled for this user.'
      );
      setShowDisable2FAModal(false);
      refetch2FAStatus();
    } catch (error: any) {
      await SweetAlert.apiError(error);
    } finally {
      setDisabling2FA(false);
    }
  };

  const handleRegenerateBackupCodes = async () => {
    try {
      const result = await SweetAlert.confirm(
        'Regenerate Backup Codes',
        'This will invalidate all existing backup codes and generate new ones. Continue?',
        undefined,
        {
          confirmButtonText: 'Regenerate',
          icon: 'warning'
        }
      );

      if (result.isConfirmed) {
        SweetAlert.loading('Regenerating...', 'Generating new backup codes');
        const data = await APIClient.regenerateBackupCodes();
        SweetAlert.close();

        setBackupCodes(data.backup_codes);
        setShowBackupCodesModal(true);

        await SweetAlert.success(
          'Backup Codes Regenerated',
          'Your new backup codes are ready. Please save them in a secure location.'
        );
      }
    } catch (error: any) {
      SweetAlert.close();
      await SweetAlert.apiError(error);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border border-green-200 dark:border-green-800';
      case 'inactive':
        return 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border border-gray-200 dark:border-gray-700';
      case 'suspended':
        return 'bg-red-100 dark:bg-red-900/30 text-red-800 dark:text-red-400 border border-red-200 dark:border-red-800';
      default:
        return 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border border-gray-200 dark:border-gray-700';
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
        <h3 className="text-lg font-semibold">User not found</h3>
        <p className="text-muted-foreground">The requested user does not exist.</p>
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
            className="gap-2 bg-white dark:bg-gray-800 hover:bg-gray-50 dark:hover:bg-gray-700 border-gray-200 dark:border-gray-700"
          >
            <ArrowLeft className="h-4 w-4" />
            Back to Users
          </Button>
        </div>
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">{userData.username}</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
              User details and configuration
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              onClick={() => setIsChangePasswordOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Key className="h-4 w-4" />
              Change Password
            </Button>
            <Button
              onClick={() => setIsEditUserModalOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Edit className="h-4 w-4" />
              Edit User
            </Button>
            <Button
              onClick={() => setIsCreateKeyModalOpen(true)}
              variant="outline"
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              Create Access Key
            </Button>
          </div>
        </div>
      </div>

      {/* User Info Cards */}
      <div className="grid gap-4 md:grid-cols-3">
        {/* Status Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Status</p>
              <div className="mt-2">
                <span className={`inline-flex items-center px-2.5 py-1 rounded-full text-sm font-medium ${getStatusColor(userData.status)}`}>
                  {userData.status === 'active' ? <CheckCircle className="h-4 w-4 mr-1" /> : <XCircle className="h-4 w-4 mr-1" />}
                  {userData.status.charAt(0).toUpperCase() + userData.status.slice(1)}
                </span>
              </div>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-brand-50 dark:bg-brand-900/30">
              <UserIcon className="h-7 w-7 text-brand-600 dark:text-brand-400" />
            </div>
          </div>
        </div>

        {/* Email Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Email</p>
              <h3 className="text-lg font-bold text-gray-900 dark:text-white break-all">{userData.email || 'Not provided'}</h3>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
              <Mail className="h-7 w-7 text-blue-600 dark:text-blue-400" />
            </div>
          </div>
        </div>

        {/* Roles Card */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
          <div className="flex items-center justify-between">
            <div className="flex-1">
              <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Roles</p>
              <div className="flex flex-wrap gap-1 mt-2">
                {userData.roles && userData.roles.length > 0 ? (
                  userData.roles.map((role: string) => (
                    <span
                      key={role}
                      className="inline-flex items-center px-2 py-1 rounded-md text-xs font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-400 border border-blue-200 dark:border-blue-800"
                    >
                      {role}
                    </span>
                  ))
                ) : (
                  <span className="text-xs text-gray-500 dark:text-gray-400">No roles assigned</span>
                )}
              </div>
            </div>
            <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
              <Shield className="h-7 w-7 text-purple-600 dark:text-purple-400" />
            </div>
          </div>
        </div>
      </div>

      {/* Security & Preferences Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Two-Factor Authentication Section */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <KeyRound className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              <div>
                <h3 className="text-lg font-semibold text-gray-900 dark:text-white">
                  Two-Factor Authentication (2FA)
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-400 mt-1">
                  Add an extra layer of security by requiring a verification code from an authenticator app
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
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Status:</span>
                    {twoFactorStatus?.enabled ? (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-300">
                        <CheckCircle className="h-3 w-3 mr-1" />
                        Enabled
                      </span>
                    ) : (
                      <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-300">
                        Disabled
                      </span>
                    )}
                  </div>
                  {twoFactorStatus?.enabled && twoFactorStatus?.setup_at && (
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      Enabled on {new Date(twoFactorStatus.setup_at * 1000).toLocaleDateString()}
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
                          Regenerate Backup Codes
                        </Button>
                      )}
                      {(isCurrentUser || isGlobalAdmin) && (
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setShowDisable2FAModal(true)}
                        >
                          Disable 2FA
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
                          <KeyRound className="h-4 w-4 mr-2" />
                          Enable 2FA
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
                        <strong className="font-medium">Recommended:</strong> Enable 2FA to protect your account from unauthorized access.
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
                        This user has 2FA enabled. Only global administrators can disable it.
                      </p>
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
        </div>

        {/* User Preferences - Only visible for current user */}
        {isCurrentUser && (
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
            <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-base font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                <Settings className="h-4 w-4 text-gray-600 dark:text-gray-400" />
                Preferences
              </h3>
            </div>

            <div className="p-6">
              <UserPreferences />
            </div>
          </div>
        )}
      </div>

      {/* Password Management - Modal */}
      <Modal
        isOpen={isChangePasswordOpen}
        onClose={() => {
          setIsChangePasswordOpen(false);
          setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' });
        }}
        title="Change Password"
      >
        <div className="space-y-4">
          {/* Only show current password field when user is changing their own password */}
          {isEditingSelf && (
            <div>
              <label className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 block">Current Password</label>
              <Input
                type="password"
                placeholder="Enter current password"
                value={passwordForm.currentPassword}
                onChange={(e) => setPasswordForm({ ...passwordForm, currentPassword: e.target.value })}
                className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
              />
            </div>
          )}
          {!isEditingSelf && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3 mb-4">
              <p className="text-sm text-blue-800 dark:text-blue-300">
                As an admin, you can reset this user's password without knowing their current password.
              </p>
            </div>
          )}
          <div>
            <label className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 block">New Password</label>
            <Input
              type="password"
              placeholder="Enter new password (min 6 characters)"
              value={passwordForm.newPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, newPassword: e.target.value })}
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>
          <div>
            <label className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2 block">Confirm New Password</label>
            <Input
              type="password"
              placeholder="Confirm new password"
              value={passwordForm.confirmPassword}
              onChange={(e) => setPasswordForm({ ...passwordForm, confirmPassword: e.target.value })}
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
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
              Cancel
            </Button>
            <Button
              onClick={handleChangePassword}
              disabled={changePasswordMutation.isPending}
              variant="outline"
            >
              {changePasswordMutation.isPending ? 'Changing...' : 'Change Password'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Access Keys */}
      <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
        <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
            <Key className="h-5 w-5 text-gray-600 dark:text-gray-400" />
            Access Keys ({accessKeys?.length || 0})
          </h3>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Manage S3-compatible access credentials</p>
        </div>
        <div className="p-6">
          {keysLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loading size="md" />
            </div>
          ) : !accessKeys || accessKeys.length === 0 ? (
            <EmptyState
              icon={Key}
              title="No access keys"
              description="Create an access key to allow programmatic access to this user's resources."
              actionLabel="Create Access Key"
              onAction={() => setIsCreateKeyModalOpen(true)}
              showAction={true}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Access Key ID</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Last Time Used</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {accessKeys.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Key className="h-4 w-4 text-muted-foreground" />
                        <code className="text-sm bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-white px-2 py-1 rounded">{key.id}</code>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => copyToClipboard(key.id)}
                          title="Copy Access Key"
                        >
                          <Copy className="h-3 w-3" />
                        </Button>
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium border ${
                        key.status === 'active'
                          ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400 border-green-200 dark:border-green-800'
                          : 'bg-gray-100 dark:bg-gray-900/30 text-gray-800 dark:text-gray-400 border-gray-200 dark:border-gray-700'
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
                          ? 'Never'
                          : formatDate(key.lastUsed)}
                      </div>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDeleteAccessKey(key.id, key.id)}
                          title="Delete access key"
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
        title="Edit User"
      >
        <form onSubmit={handleEditUser} className="space-y-4">
          <div>
            <label htmlFor="email" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Email
            </label>
            <Input
              id="email"
              type="email"
              value={editForm.email}
              onChange={(e) => setEditForm(prev => ({ ...prev, email: e.target.value }))}
              placeholder="user@example.com"
              className="bg-white dark:bg-gray-900 text-gray-900 dark:text-white border-gray-200 dark:border-gray-700"
            />
          </div>

          <div>
            <label htmlFor="tenant" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Tenant (Optional)
            </label>
            <select
              id="tenant"
              value={editForm.tenantId || ''}
              onChange={(e) => setEditForm(prev => ({ ...prev, tenantId: e.target.value || undefined }))}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="">No Tenant (Global User)</option>
              {tenants?.map((tenant) => (
                <option key={tenant.id} value={tenant.id}>
                  {tenant.displayName} ({tenant.name})
                </option>
              ))}
            </select>
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Global users can access all buckets. Tenant users are limited to their tenant's buckets.
            </p>
          </div>

          <div>
            <label htmlFor="status" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Status
            </label>
            <select
              id="status"
              value={editForm.status}
              onChange={(e) => setEditForm(prev => ({ ...prev, status: e.target.value as any }))}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500"
            >
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
              <option value="suspended">Suspended</option>
            </select>
          </div>

          <div>
            <label htmlFor="roles" className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Role
            </label>
            <select
              id="roles"
              value={editForm.roles[0] || 'user'}
              onChange={(e) => setEditForm(prev => ({
                ...prev,
                roles: [e.target.value]
              }))}
              disabled={!isCurrentUserAdmin || (isCurrentUserAdmin && isEditingSelf)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md focus:outline-none focus:ring-2 focus:ring-brand-500 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <option value="admin">Admin - Full access to manage the system</option>
              <option value="user">User - Standard user with normal access</option>
              <option value="readonly">Read Only - Can only view, cannot modify</option>
              <option value="guest">Guest - Limited access</option>
            </select>
            {!isCurrentUserAdmin && (
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Only administrators can change user roles.
              </p>
            )}
            {isCurrentUserAdmin && isEditingSelf && (
              <p className="text-xs text-yellow-600 dark:text-yellow-400 mt-1">
                You cannot change your own role.
              </p>
            )}
            {isCurrentUserAdmin && !isEditingSelf && (
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Select the role for this user.
              </p>
            )}
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsEditUserModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="outline"
              disabled={updateUserMutation.isPending}
            >
              {updateUserMutation.isPending ? 'Saving...' : 'Save Changes'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Create Access Key Modal */}
      <Modal
        isOpen={isCreateKeyModalOpen}
        onClose={() => setIsCreateKeyModalOpen(false)}
        title="Create New Access Key"
      >
        <form onSubmit={handleCreateAccessKey} className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md p-3">
            <p className="text-sm text-blue-800 dark:text-blue-300">
              <strong>ℹ️ Information:</strong> An access key and secret key pair will be automatically generated for this user.
            </p>
          </div>

          <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-md p-3">
            <p className="text-sm text-yellow-800 dark:text-yellow-300">
              <strong>⚠️ Important:</strong> The secret key will only be displayed once after creation.
              Make sure to copy and store it in a safe place.
            </p>
          </div>

          <div className="flex justify-end space-x-2 pt-4">
            <Button
              type="button"
              variant="outline"
              onClick={() => setIsCreateKeyModalOpen(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="outline"
              disabled={createAccessKeyMutation.isPending}
            >
              {createAccessKeyMutation.isPending ? 'Creating...' : 'Create Access Key'}
            </Button>
          </div>
        </form>
      </Modal>

      {/* Created Key Modal */}
      {createdKey && (
        <Modal
          isOpen={!!createdKey}
          onClose={() => setCreatedKey(null)}
          title="Access Key Created"
        >
          <div className="space-y-4">
            <div className="bg-green-50 dark:bg-green-900/30 border border-green-200 dark:border-green-800 rounded-md p-3">
              <p className="text-sm text-green-800 dark:text-green-300">
                <strong>✅ Access Key created successfully!</strong>
              </p>
            </div>

            <div className="space-y-3">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Access Key ID:</label>
                <div className="flex items-center gap-2">
                  <code className="bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-white px-3 py-2 rounded text-sm flex-1">
                    {createdKey.accessKey}
                  </code>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => copyToClipboard(createdKey.accessKey)}
                    title="Copy to clipboard"
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>

              {createdKey.secretKey && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Secret Access Key:</label>
                  <div className="flex items-center gap-2">
                    <code className="bg-gray-100 dark:bg-gray-800 text-gray-900 dark:text-white px-3 py-2 rounded text-sm flex-1">
                      {createdKey.secretKey}
                    </code>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => copyToClipboard(createdKey.secretKey!)}
                      title="Copy to clipboard"
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              )}
            </div>

            <div className="bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-md p-3">
              <p className="text-sm text-red-800 dark:text-red-300">
                <strong>⚠️ Important:</strong> This is the only time the secret key will be displayed.
                Copy and store it in a safe place before closing this window.
              </p>
            </div>

            <div className="flex justify-between items-center">
              <Button
                variant="outline"
                onClick={() => downloadAsCSV(createdKey)}
                className="gap-2"
              >
                <Download className="h-4 w-4" />
                Download CSV
              </Button>
              <Button 
                onClick={() => setCreatedKey(null)}
                variant="outline"
              >
                Got it
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
        title="Disable Two-Factor Authentication"
        message={isCurrentUser
          ? "Are you sure you want to disable 2FA? This will make your account less secure."
          : "Are you sure you want to disable 2FA for this user? This will make their account less secure."}
        confirmText="Disable 2FA"
        cancelText="Cancel"
        variant="danger"
        loading={disabling2FA}
      />
    </div>
  );
}
