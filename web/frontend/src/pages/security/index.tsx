import React from 'react';
import { Link } from 'react-router-dom';
import { Loading } from '@/components/ui/Loading';
import {
  Shield,
  Lock,
  Users,
  AlertTriangle,
  CheckCircle,
  UserX,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

export default function SecurityPage() {
  const { data: users = [], isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: APIClient.getUsers,
    staleTime: 30000, // Consider data fresh for 30 seconds
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  // Calculate locked users directly from the users data
  const now = Math.floor(Date.now() / 1000);
  const lockedUsers = users.filter((u: any) => u.locked_until && u.locked_until > now);

  const activeUsers = users.filter((u: any) => u.status === 'active').length;
  const totalUsers = users.length;
  const inactiveUsers = users.filter((u: any) => u.status === 'inactive').length;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900 dark:text-white">Security Overview</h1>
          <p className="text-gray-500 dark:text-gray-400">
            Monitor authentication and user access
          </p>
        </div>
      </div>

      {/* Security Status */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Security Status</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          {/* Authentication */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
            <div className="flex items-center justify-between">
              <div className="flex-1">
                <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Authentication</p>
                <h3 className="text-3xl font-bold text-green-600 dark:text-green-400">Enabled</h3>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">JWT-based authentication</p>
              </div>
              <div className="flex items-center justify-center w-14 h-14 rounded-full bg-green-50 dark:bg-green-900/30">
                <CheckCircle className="h-7 w-7 text-green-600 dark:text-green-400" />
              </div>
            </div>
          </div>

          {/* Active Users */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
            <div className="flex items-center justify-between">
              <div className="flex-1">
                <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Active Users</p>
                <h3 className="text-3xl font-bold text-gray-900 dark:text-white">{activeUsers}</h3>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">{totalUsers} total users</p>
              </div>
              <div className="flex items-center justify-center w-14 h-14 rounded-full bg-blue-50 dark:bg-blue-900/30">
                <Users className="h-7 w-7 text-blue-600 dark:text-blue-400" />
              </div>
            </div>
          </div>

          {/* Locked Accounts */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
            <div className="flex items-center justify-between">
              <div className="flex-1">
                <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Locked Accounts</p>
                <h3 className={`text-3xl font-bold ${lockedUsers.length > 0 ? 'text-orange-600 dark:text-orange-400' : 'text-green-600 dark:text-green-400'}`}>
                  {lockedUsers.length}
                </h3>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">Due to failed logins</p>
              </div>
              <div className={`flex items-center justify-center w-14 h-14 rounded-full ${lockedUsers.length > 0 ? 'bg-orange-50 dark:bg-orange-900/30' : 'bg-green-50 dark:bg-green-900/30'}`}>
                {lockedUsers.length > 0 ? (
                  <AlertTriangle className="h-7 w-7 text-orange-600 dark:text-orange-400" />
                ) : (
                  <CheckCircle className="h-7 w-7 text-green-600 dark:text-green-400" />
                )}
              </div>
            </div>
          </div>

          {/* Rate Limiting */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6 hover:shadow-md transition-shadow">
            <div className="flex items-center justify-between">
              <div className="flex-1">
                <p className="text-sm font-medium text-gray-600 dark:text-gray-400 mb-1">Rate Limiting</p>
                <h3 className="text-3xl font-bold text-purple-600 dark:text-purple-400">Active</h3>
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-2">5 attempts per minute</p>
              </div>
              <div className="flex items-center justify-center w-14 h-14 rounded-full bg-purple-50 dark:bg-purple-900/30">
                <Shield className="h-7 w-7 text-purple-600 dark:text-purple-400" />
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* User Statistics */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">User Statistics</h2>
        <div className="grid gap-4 md:grid-cols-2">
          {/* User Status Card */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                <Users className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                User Status
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Total Users</span>
                <span className="text-sm font-bold text-gray-900 dark:text-white">{totalUsers}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Active Users</span>
                <span className="flex items-center gap-2 text-green-600 dark:text-green-400 font-medium">
                  <CheckCircle className="h-4 w-4" />
                  {activeUsers}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Inactive Users</span>
                <span className="flex items-center gap-2 text-gray-600 dark:text-gray-400 font-medium">
                  <UserX className="h-4 w-4" />
                  {inactiveUsers}
                </span>
              </div>
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <Link
                  to="/users"
                  className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:hover:text-brand-300 font-medium"
                >
                  Manage Users →
                </Link>
              </div>
            </div>
          </div>

          {/* Account Security Card */}
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm overflow-hidden">
            <div className="px-6 py-5 border-b border-gray-200 dark:border-gray-700">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                <Lock className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                Account Security
              </h3>
            </div>
            <div className="p-6 space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Locked Accounts</span>
                <span className={`text-sm font-bold ${lockedUsers.length > 0 ? 'text-orange-600 dark:text-orange-400' : 'text-green-600 dark:text-green-400'}`}>
                  {lockedUsers.length}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Lockout Duration</span>
                <span className="text-sm text-gray-600 dark:text-gray-400">15 minutes</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Max Failed Attempts</span>
                <span className="text-sm text-gray-600 dark:text-gray-400">5 attempts</span>
              </div>
              {lockedUsers.length > 0 && (
                <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                  <Link
                    to="/users"
                    className="text-sm text-orange-600 dark:text-orange-400 hover:text-orange-700 dark:hover:text-orange-300 font-medium"
                  >
                    View Locked Users →
                  </Link>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Active Security Features */}
      <div>
        <h2 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Active Security Features</h2>
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm p-6">
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              <div>
                <p className="font-medium text-gray-900 dark:text-white">JWT Authentication</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">Token-based authentication for all API requests</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              <div>
                <p className="font-medium text-gray-900 dark:text-white">Rate Limiting</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">IP-based rate limiting (5 login attempts per minute)</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              <div>
                <p className="font-medium text-gray-900 dark:text-white">Account Lockout</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">Automatic 15-minute lockout after 5 failed login attempts</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              <div>
                <p className="font-medium text-gray-900 dark:text-white">Role-Based Access Control (RBAC)</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">Global Admin and Tenant Admin roles with granular permissions</p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              <div>
                <p className="font-medium text-gray-900 dark:text-white">Multi-Tenancy</p>
                <p className="text-sm text-gray-500 dark:text-gray-400">Isolated tenant environments with separate access control</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
