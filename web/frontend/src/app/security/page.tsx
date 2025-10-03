'use client';

import React from 'react';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  Shield,
  Lock,
  Key,
  AlertTriangle,
  CheckCircle,
  Settings,
  Users,
  Database,
  Eye,
  EyeOff,
  Clock,
  FileText
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

export default function SecurityPage() {
  const { data: securityStatusData, isLoading } = useQuery({
    queryKey: ['securityStatus'],
    queryFn: APIClient.getSecurityStatus,
  });

  // Use real data from backend with fallback
  const securityStatus = securityStatusData?.data || {
    encryption: {
      enabled: false,
      algorithm: 'N/A',
      keyRotation: false,
      lastRotation: new Date().toISOString()
    },
    objectLock: {
      enabled: false,
      bucketsWithLock: 0,
      totalLockedObjects: 0,
      complianceMode: 0,
      governanceMode: 0
    },
    authentication: {
      requireAuth: true,
      mfaEnabled: false,
      activeUsers: 0,
      activeSessions: 0,
      failedLogins24h: 0
    },
    policies: {
      totalPolicies: 0,
      bucketPolicies: 0,
      userPolicies: 0,
      lastUpdate: new Date().toISOString()
    },
    audit: {
      enabled: false,
      logRetention: 90,
      totalEvents: 0,
      eventsToday: 0
    }
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  const displayData = securityStatus;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Security Overview</h1>
          <p className="text-muted-foreground">
            Monitor and manage security configurations
          </p>
        </div>
        <Button className="gap-2">
          <Settings className="h-4 w-4" />
          Security Settings
        </Button>
      </div>


      {/* Security Status */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Security Status</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Card className="border-green-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Encryption</CardTitle>
              {displayData?.encryption?.enabled ? (
                <CheckCircle className="h-4 w-4 text-green-600" />
              ) : (
                <AlertTriangle className="h-4 w-4 text-red-600" />
              )}
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-green-600">
                {displayData?.encryption?.enabled ? 'Enabled' : 'Disabled'}
              </div>
              <p className="text-xs text-muted-foreground">
                {displayData?.encryption?.algorithm || 'N/A'}
              </p>
            </CardContent>
          </Card>

          <Card className="border-blue-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Object Lock</CardTitle>
              {displayData?.objectLock?.enabled ? (
                <CheckCircle className="h-4 w-4 text-blue-600" />
              ) : (
                <AlertTriangle className="h-4 w-4 text-red-600" />
              )}
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-blue-600">
                {displayData?.objectLock?.enabled ? 'Active' : 'Inactive'}
              </div>
              <p className="text-xs text-muted-foreground">
                {displayData?.objectLock?.bucketsWithLock || 0} buckets protected
              </p>
            </CardContent>
          </Card>

          <Card className="border-purple-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Authentication</CardTitle>
              {displayData?.authentication?.requireAuth ? (
                <CheckCircle className="h-4 w-4 text-purple-600" />
              ) : (
                <AlertTriangle className="h-4 w-4 text-red-600" />
              )}
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-purple-600">
                {displayData?.authentication?.requireAuth ? 'Required' : 'Optional'}
              </div>
              <p className="text-xs text-muted-foreground">
                {displayData?.authentication?.activeUsers || 0} active users
              </p>
            </CardContent>
          </Card>

          <Card className="border-orange-200">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Audit Logging</CardTitle>
              {displayData?.audit?.enabled ? (
                <CheckCircle className="h-4 w-4 text-orange-600" />
              ) : (
                <AlertTriangle className="h-4 w-4 text-red-600" />
              )}
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-orange-600">
                {displayData?.audit?.enabled ? 'Enabled' : 'Disabled'}
              </div>
              <p className="text-xs text-muted-foreground">
                {displayData?.audit?.eventsToday || 0} events today
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Encryption Details */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Encryption Management</h2>
        <div className="grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Key className="h-5 w-5" />
                Server-Side Encryption
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Status</span>
                <span className="flex items-center gap-2 text-green-600">
                  <CheckCircle className="h-4 w-4" />
                  Enabled
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Algorithm</span>
                <span className="text-sm">{displayData?.encryption?.algorithm || 'N/A'}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Key Rotation</span>
                <span className="flex items-center gap-2 text-green-600">
                  <CheckCircle className="h-4 w-4" />
                  {displayData?.encryption?.keyRotation ? 'Enabled' : 'Disabled'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Last Rotation</span>
                <span className="text-sm text-muted-foreground">
                  {displayData?.encryption?.lastRotation ? formatDate(displayData.encryption.lastRotation) : 'N/A'}
                </span>
              </div>
              <Button className="w-full gap-2">
                <Settings className="h-4 w-4" />
                Manage Encryption
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Lock className="h-5 w-5" />
                Object Lock Statistics
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Protected Buckets</span>
                <span className="text-sm font-bold">{displayData?.objectLock?.bucketsWithLock || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Locked Objects</span>
                <span className="text-sm font-bold">{(displayData?.objectLock?.totalLockedObjects || 0).toLocaleString()}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Compliance Mode</span>
                <span className="text-sm text-red-600 font-medium">{displayData?.objectLock?.complianceMode || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Governance Mode</span>
                <span className="text-sm text-blue-600 font-medium">{displayData?.objectLock?.governanceMode || 0}</span>
              </div>
              <Button className="w-full gap-2">
                <Lock className="h-4 w-4" />
                Manage Object Lock
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Access Control */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Access Control</h2>
        <div className="grid gap-4 md:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Users className="h-5 w-5" />
                Authentication
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Active Users</span>
                <span className="text-sm font-bold">{displayData?.authentication?.activeUsers || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Active Sessions</span>
                <span className="text-sm font-bold">{displayData?.authentication?.activeSessions || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Failed Logins (24h)</span>
                <span className="text-sm text-orange-600 font-medium">{displayData?.authentication?.failedLogins24h || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">MFA Status</span>
                <span className="flex items-center gap-2 text-red-600">
                  <AlertTriangle className="h-4 w-4" />
                  Disabled
                </span>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Shield className="h-5 w-5" />
                Policies
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Total Policies</span>
                <span className="text-sm font-bold">{displayData?.policies?.totalPolicies || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Bucket Policies</span>
                <span className="text-sm font-bold">{displayData?.policies?.bucketPolicies || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">User Policies</span>
                <span className="text-sm font-bold">{displayData?.policies?.userPolicies || 0}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Last Update</span>
                <span className="text-sm text-muted-foreground">
                  {displayData?.policies?.lastUpdate ? formatDate(displayData.policies.lastUpdate) : 'N/A'}
                </span>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <FileText className="h-5 w-5" />
                Audit Log
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Status</span>
                <span className={`flex items-center gap-2 ${displayData?.audit?.enabled ? 'text-green-600' : 'text-red-600'}`}>
                  {displayData?.audit?.enabled ? <CheckCircle className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
                  {displayData?.audit?.enabled ? 'Active' : 'Inactive'}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Retention</span>
                <span className="text-sm font-bold">{displayData?.audit?.logRetention || 0} days</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Total Events</span>
                <span className="text-sm font-bold">{(displayData?.audit?.totalEvents || 0).toLocaleString()}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">Events Today</span>
                <span className="text-sm text-blue-600 font-medium">{displayData?.audit?.eventsToday || 0}</span>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>

      {/* Quick Actions */}
      <div>
        <h2 className="text-xl font-semibold mb-4">Quick Actions</h2>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <Button variant="outline" className="h-20 flex-col gap-2">
            <Key className="h-6 w-6" />
            <span>Encryption Settings</span>
          </Button>
          <Button variant="outline" className="h-20 flex-col gap-2">
            <Lock className="h-6 w-6" />
            <span>Object Lock Config</span>
          </Button>
          <Button variant="outline" className="h-20 flex-col gap-2">
            <Users className="h-6 w-6" />
            <span>User Permissions</span>
          </Button>
          <Button variant="outline" className="h-20 flex-col gap-2">
            <FileText className="h-6 w-6" />
            <span>Audit Logs</span>
          </Button>
        </div>
      </div>
    </div>
  );
}