'use client';

import React from 'react';
import Link from 'next/link';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  Shield,
  Lock,
  Users,
  AlertTriangle,
  CheckCircle,
  UserX,
  Activity
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
          <h1 className="text-3xl font-bold tracking-tight">Security Overview</h1>
          <p className="text-muted-foreground">
            Monitor authentication and user access
          </p>
        </div>
      </div>

      {/* Security Status */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Security Status</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card className="border-green-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Authentication</CardTitle>
              <CheckCircle className="h-4 w-4 text-green-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-green-600">
                Enabled
              </div>
              <p className="text-xs text-muted-foreground">
                JWT-based authentication
              </p>
            </CardContent>
          </Card>

          <Card className="border-blue-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Active Users</CardTitle>
              <Users className="h-4 w-4 text-blue-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-blue-600">
                {activeUsers}
              </div>
              <p className="text-xs text-muted-foreground">
                {totalUsers} total users
              </p>
            </CardContent>
          </Card>

          <Card className="border-orange-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Locked Accounts</CardTitle>
              {lockedUsers.length > 0 ? (
                <AlertTriangle className="h-4 w-4 text-orange-600" />
              ) : (
                <CheckCircle className="h-4 w-4 text-green-600" />
              )}
            </CardHeader>
            <CardContent>
              <div className={`text-2xl font-bold ${lockedUsers.length > 0 ? 'text-orange-600' : 'text-green-600'}`}>
                {lockedUsers.length}
              </div>
              <p className="text-xs text-muted-foreground">
                Due to failed logins
              </p>
            </CardContent>
          </Card>

          <Card className="border-purple-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Rate Limiting</CardTitle>
              <Shield className="h-4 w-4 text-purple-600" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-purple-600">
                Active
              </div>
              <p className="text-xs text-muted-foreground">
                5 attempts per minute
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* User Statistics */}
      <div>
        <h2 className="text-xl font-semibold mb-4">User Statistics</h2>
        <div className="grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Users className="h-5 w-5" />
                User Status
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Total Users</span>
                <span className="text-sm font-bold">{totalUsers}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Active Users</span>
                <span className="flex items-center gap-2 text-green-600 font-medium">
                  <CheckCircle className="h-4 w-4" />
                  {activeUsers}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Inactive Users</span>
                <span className="flex items-center gap-2 text-gray-600 font-medium">
                  <UserX className="h-4 w-4" />
                  {inactiveUsers}
                </span>
              </div>
              <div className="pt-2 border-t">
                <Link
                  href="/users"
                  className="text-sm text-blue-600 hover:text-blue-700 font-medium"
                >
                  Manage Users →
                </Link>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Lock className="h-5 w-5" />
                Account Security
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Locked Accounts</span>
                <span className={`text-sm font-bold ${lockedUsers.length > 0 ? 'text-orange-600' : 'text-green-600'}`}>
                  {lockedUsers.length}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Lockout Duration</span>
                <span className="text-sm text-gray-600">15 minutes</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Max Failed Attempts</span>
                <span className="text-sm text-gray-600">5 attempts</span>
              </div>
              {lockedUsers.length > 0 && (
                <div className="pt-2 border-t">
                  <Link
                    href="/users"
                    className="text-sm text-orange-600 hover:text-orange-700 font-medium"
                  >
                    View Locked Users →
                  </Link>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Active Security Features */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Active Security Features</h2>
        <Card>
          <CardContent className="pt-6">
            <div className="space-y-4">
              <div className="flex items-center gap-3">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <div>
                  <p className="font-medium">JWT Authentication</p>
                  <p className="text-sm text-muted-foreground">Token-based authentication for all API requests</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <div>
                  <p className="font-medium">Rate Limiting</p>
                  <p className="text-sm text-muted-foreground">IP-based rate limiting (5 login attempts per minute)</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <div>
                  <p className="font-medium">Account Lockout</p>
                  <p className="text-sm text-muted-foreground">Automatic 15-minute lockout after 5 failed login attempts</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <div>
                  <p className="font-medium">Role-Based Access Control (RBAC)</p>
                  <p className="text-sm text-muted-foreground">Global Admin and Tenant Admin roles with granular permissions</p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <CheckCircle className="h-5 w-5 text-green-600" />
                <div>
                  <p className="font-medium">Multi-Tenancy</p>
                  <p className="text-sm text-muted-foreground">Isolated tenant environments with separate access control</p>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
