import React from 'react';
import { useRouter } from 'next/router';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  ArrowLeft,
  Shield,
  Clock,
  Tag,
  Lock,
  Globe,
  FileText,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { bucketsApi } from '@/lib/api';

export default function BucketSettingsPage() {
  const router = useRouter();
  const { bucket } = router.query;
  const bucketName = bucket as string;

  const { data: bucketData, isLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => bucketsApi.getBucket(bucketName),
  });

  if (isLoading) {
    return <Loading />;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => router.push(`/buckets/${bucketName}`)}
          >
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <h1 className="text-2xl font-bold">{bucketName}</h1>
            <p className="text-sm text-gray-500">Bucket Settings</p>
          </div>
        </div>
      </div>

      <div className="grid gap-6">
        {/* Versioning */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-5 w-5" />
              Versioning
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Version Control</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.versioning ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                <Button variant="outline">
                  {bucketData?.versioning ? 'Suspend' : 'Enable'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Object Lock */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Lock className="h-5 w-5" />
              Object Lock
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Object Lock Status</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.object_lock ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                {bucketData?.object_lock && (
                  <Button variant="outline">Configure</Button>
                )}
              </div>
              {bucketData?.object_lock && (
                <div className="rounded-lg border p-4 space-y-2">
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Mode:</span>
                    <span className="text-sm font-medium">
                      {bucketData.retention_mode || 'Not Set'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Retention:</span>
                    <span className="text-sm font-medium">
                      {bucketData.retention_days
                        ? `${bucketData.retention_days} days`
                        : 'Not Set'}
                    </span>
                  </div>
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Bucket Policy */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Shield className="h-5 w-5" />
              Bucket Policy
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Access Policy</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.policy ? 'Custom Policy' : 'No Policy Set'}
                  </p>
                </div>
                <Button variant="outline">
                  {bucketData?.policy ? 'Edit Policy' : 'Add Policy'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Tags */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Tag className="h-5 w-5" />
              Tags
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Bucket Tags</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.tags && Object.keys(bucketData.tags).length > 0
                      ? `${Object.keys(bucketData.tags).length} tags`
                      : 'No tags'}
                  </p>
                </div>
                <Button variant="outline">Manage Tags</Button>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* CORS */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Globe className="h-5 w-5" />
              CORS Configuration
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Cross-Origin Resource Sharing</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.cors ? 'Configured' : 'Not Configured'}
                  </p>
                </div>
                <Button variant="outline">
                  {bucketData?.cors ? 'Edit CORS' : 'Add CORS'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Lifecycle */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FileText className="h-5 w-5" />
              Lifecycle Rules
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Object Lifecycle Management</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.lifecycle ? 'Active Rules' : 'No Rules'}
                  </p>
                </div>
                <Button variant="outline">
                  {bucketData?.lifecycle ? 'Manage Rules' : 'Add Rule'}
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
