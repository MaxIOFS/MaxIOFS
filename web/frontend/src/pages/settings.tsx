import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import {
  Server,
  Shield,
  HardDrive,
  Monitor,
  Info,
  CheckCircle,
  Package
} from 'lucide-react';

export default function SettingsPage() {
  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">System Settings</h1>
          <p className="text-muted-foreground">
            View current MaxIOFS configuration
          </p>
        </div>
      </div>

      {/* Info Banner */}
      <div className="bg-blue-50 border border-blue-200 rounded-md p-4">
        <div className="flex items-start gap-3">
          <Info className="h-5 w-5 text-blue-600 mt-0.5" />
          <div>
            <p className="text-sm font-medium text-blue-900">Read-Only Configuration</p>
            <p className="text-sm text-blue-700 mt-1">
              These settings are configured via command-line flags and configuration files.
              To modify them, please update your server configuration and restart MaxIOFS.
            </p>
          </div>
        </div>
      </div>

      {/* Server Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Server className="h-5 w-5" />
            Server Configuration
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">S3 API Port</label>
              <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm">
                8080
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Console API Port</label>
              <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm">
                8081
              </div>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Data Directory</label>
            <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm font-mono">
              ./data
            </div>
          </div>

          <div className="flex items-center gap-2 text-sm text-green-600">
            <CheckCircle className="h-4 w-4" />
            <span>Server running and accepting connections</span>
          </div>
        </CardContent>
      </Card>

      {/* Storage Backend */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <HardDrive className="h-5 w-5" />
            Storage Backend
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Storage Type</label>
            <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm">
              File System (Local)
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Storage Path</label>
            <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm font-mono">
              ./data
            </div>
          </div>

          <div className="flex items-center gap-2 text-sm text-green-600">
            <CheckCircle className="h-4 w-4" />
            <span>Storage backend operational</span>
          </div>
        </CardContent>
      </Card>

      {/* Security Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            Security Settings
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Authentication</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Enabled (JWT)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Rate Limiting</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Enabled
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Account Lockout</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Enabled (5 attempts / 15 min)
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">CORS</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Enabled
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Monitoring & Logging */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Monitor className="h-5 w-5" />
            Monitoring & Logging
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Metrics Collection</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Enabled
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Log Level</label>
              <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm">
                Debug
              </div>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Log Format</label>
            <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm">
              Structured (logrus)
            </div>
          </div>
        </CardContent>
      </Card>

      {/* System Information */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Package className="h-5 w-5" />
            System Information
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Version</label>
              <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm font-mono">
                1.1.0
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Build</label>
              <div className="px-3 py-2 bg-gray-50 border border-gray-200 rounded-md text-sm font-mono">
                Production
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">S3 API Compatibility</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ AWS S3 Compatible
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Multi-Tenancy</label>
              <div className="px-3 py-2 bg-green-50 border border-green-200 rounded-md text-sm text-green-700 font-medium">
                ✓ Supported
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
