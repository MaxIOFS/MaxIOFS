'use client';

import React from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Database, FolderOpen, Users, Activity, HardDrive, TrendingUp } from 'lucide-react';
import { formatBytes } from '@/lib/utils';

// Mock data for demonstration
const mockStats = {
  totalBuckets: 12,
  totalObjects: 1584,
  totalSize: 256 * 1024 * 1024 * 1024, // 256 GB
  activeUsers: 8,
  requestsToday: 15420,
  uptime: '99.9%',
};

const mockRecentActivity = [
  {
    id: 1,
    action: 'Bucket Created',
    user: 'admin',
    resource: 'my-images-bucket',
    timestamp: '2 minutes ago',
    type: 'create',
  },
  {
    id: 2,
    action: 'Object Uploaded',
    user: 'john.doe',
    resource: 'documents/report.pdf',
    timestamp: '15 minutes ago',
    type: 'upload',
  },
  {
    id: 3,
    action: 'User Created',
    user: 'admin',
    resource: 'jane.smith',
    timestamp: '1 hour ago',
    type: 'user',
  },
  {
    id: 4,
    action: 'Object Downloaded',
    user: 'alice.wilson',
    resource: 'images/photo.jpg',
    timestamp: '2 hours ago',
    type: 'download',
  },
];

export default function Dashboard() {
  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          <p className="text-gray-600">Welcome to MaxIOFS Object Storage Console</p>
        </div>
        <div className="flex space-x-3">
          <Button variant="outline">
            <Database className="h-4 w-4 mr-2" />
            Create Bucket
          </Button>
          <Button>
            <FolderOpen className="h-4 w-4 mr-2" />
            Upload Object
          </Button>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Buckets</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.totalBuckets}</div>
            <p className="text-xs text-muted-foreground">
              +2 from last month
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Objects</CardTitle>
            <FolderOpen className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.totalObjects.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">
              +425 from last week
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Storage Used</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatBytes(mockStats.totalSize)}</div>
            <p className="text-xs text-muted-foreground">
              +12GB from last week
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Users</CardTitle>
            <Users className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.activeUsers}</div>
            <p className="text-xs text-muted-foreground">
              +1 from last month
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Requests Today</CardTitle>
            <Activity className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.requestsToday.toLocaleString()}</div>
            <p className="text-xs text-muted-foreground">
              +8% from yesterday
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">System Uptime</CardTitle>
            <TrendingUp className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{mockStats.uptime}</div>
            <p className="text-xs text-muted-foreground">
              Last 30 days
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Recent Activity */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Recent Activity</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {mockRecentActivity.map((activity) => (
                <div key={activity.id} className="flex items-center space-x-3">
                  <div className={`w-2 h-2 rounded-full ${
                    activity.type === 'create' ? 'bg-green-500' :
                    activity.type === 'upload' ? 'bg-blue-500' :
                    activity.type === 'download' ? 'bg-purple-500' :
                    'bg-orange-500'
                  }`} />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-gray-900 truncate">
                      {activity.action}
                    </p>
                    <p className="text-sm text-gray-600 truncate">
                      {activity.resource} by {activity.user}
                    </p>
                  </div>
                  <div className="text-sm text-gray-500">
                    {activity.timestamp}
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Quick Actions</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              <Button className="w-full justify-start" variant="outline">
                <Database className="h-4 w-4 mr-2" />
                Create New Bucket
              </Button>
              <Button className="w-full justify-start" variant="outline">
                <FolderOpen className="h-4 w-4 mr-2" />
                Upload Files
              </Button>
              <Button className="w-full justify-start" variant="outline">
                <Users className="h-4 w-4 mr-2" />
                Manage Users
              </Button>
              <Button className="w-full justify-start" variant="outline">
                <Activity className="h-4 w-4 mr-2" />
                View Metrics
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}