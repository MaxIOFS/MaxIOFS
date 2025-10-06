'use client';

import React from 'react';
import { useParams, useRouter } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import {
  ArrowLeft,
  Shield,
  Lock,
  Key,
  Database,
  AlertTriangle,
  Trash2,
  Info,
  Clock,
  Tag
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

export default function BucketSettingsPage() {
  const params = useParams();
  const router = useRouter();
  const bucketName = params.bucket as string;
  const queryClient = useQueryClient();

  const { data: bucketData, isLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => APIClient.getBucket(bucketName),
  });

  const deleteBucketMutation = useMutation({
    mutationFn: () => APIClient.deleteBucket(bucketName),
    onSuccess: () => {
      SweetAlert.toast('success', `Bucket "${bucketName}" deleted successfully`);
      router.push('/buckets');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  const handleDeleteBucket = async () => {
    const result = await SweetAlert.fire({
      icon: 'warning',
      title: 'Delete Bucket?',
      html: `
        <div class="text-left space-y-2">
          <p>Are you sure you want to delete bucket <strong>${bucketName}</strong>?</p>
          <p class="text-sm text-red-600">⚠️ This action cannot be undone and will delete all objects in the bucket.</p>
        </div>
      `,
      showCancelButton: true,
      confirmButtonText: 'Yes, delete it',
      cancelButtonText: 'Cancel',
      confirmButtonColor: '#dc2626',
    });

    if (result.isConfirmed) {
      SweetAlert.loading('Deleting bucket...', `Removing "${bucketName}"`);
      deleteBucketMutation.mutate();
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" text="Loading bucket settings..." />
      </div>
    );
  }

  const bucket = bucketData;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => router.push(`/buckets/${bucketName}`)}
            className="gap-2"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Bucket Settings</h1>
            <p className="text-muted-foreground">{bucketName}</p>
          </div>
        </div>
      </div>

      {/* General Information */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Database className="h-5 w-5" />
            General Information
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Bucket Name</label>
              <p className="text-sm font-mono bg-gray-50 p-2 rounded">{bucket?.name}</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Region</label>
              <p className="text-sm bg-gray-50 p-2 rounded">{bucket?.region || 'us-east-1'}</p>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Created</label>
              <p className="text-sm bg-gray-50 p-2 rounded">{bucket?.creation_date ? new Date(bucket.creation_date).toLocaleString() : 'N/A'}</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Owner</label>
              <p className="text-sm bg-gray-50 p-2 rounded">
                {bucket?.owner_type ? `${bucket.owner_type}: ${bucket.owner_id}` : 'Global'}
              </p>
            </div>
          </div>

          {bucket?.is_public && (
            <div className="bg-yellow-50 border border-yellow-200 rounded-md p-3 flex items-start gap-2">
              <AlertTriangle className="h-5 w-5 text-yellow-600 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-yellow-800">
                <strong>Public Bucket:</strong> This bucket is publicly accessible
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Versioning */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Clock className="h-5 w-5" />
            Versioning
          </CardTitle>
        </CardHeader>
        <CardContent>
          {bucket?.versioning?.Status === 'Enabled' ? (
            <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2">
              <div className="h-2 w-2 rounded-full bg-green-500"></div>
              <span className="text-sm text-green-800">Versioning is <strong>Enabled</strong></span>
            </div>
          ) : (
            <div className="bg-gray-50 border border-gray-200 rounded-md p-3 flex items-center gap-2">
              <div className="h-2 w-2 rounded-full bg-gray-400"></div>
              <span className="text-sm text-gray-600">Versioning is <strong>Disabled</strong></span>
            </div>
          )}
          <p className="text-xs text-muted-foreground mt-2">
            Object versioning cannot be changed after bucket creation.
          </p>
        </CardContent>
      </Card>

      {/* Object Lock & WORM */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Lock className="h-5 w-5" />
            Object Lock & WORM
          </CardTitle>
        </CardHeader>
        <CardContent>
          {bucket?.objectLock?.objectLockEnabled ? (
            <div className="space-y-3">
              <div className="bg-blue-50 border border-blue-200 rounded-md p-3">
                <div className="flex items-center gap-2 mb-2">
                  <Lock className="h-4 w-4 text-blue-600" />
                  <span className="text-sm font-semibold text-blue-800">Object Lock Enabled</span>
                </div>

                {bucket.objectLock.rule?.defaultRetention && (
                  <div className="mt-3 space-y-2 text-sm text-blue-800">
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-xs font-medium text-blue-600 mb-1">Retention Mode</label>
                        <p className="font-mono bg-white p-2 rounded border border-blue-200">
                          {bucket.objectLock.rule.defaultRetention.mode}
                        </p>
                      </div>
                      <div>
                        <label className="block text-xs font-medium text-blue-600 mb-1">Retention Period</label>
                        <p className="font-mono bg-white p-2 rounded border border-blue-200">
                          {bucket.objectLock.rule.defaultRetention.days ?
                            `${bucket.objectLock.rule.defaultRetention.days} days` :
                            bucket.objectLock.rule.defaultRetention.years ?
                            `${bucket.objectLock.rule.defaultRetention.years} years` :
                            'Not set'}
                        </p>
                      </div>
                    </div>

                    {bucket.objectLock.rule.defaultRetention.mode === 'COMPLIANCE' && (
                      <div className="bg-red-50 border border-red-200 rounded-md p-2 mt-3">
                        <p className="text-xs text-red-700">
                          <strong>⚠️ COMPLIANCE Mode:</strong> Objects cannot be deleted or modified by anyone until retention expires.
                        </p>
                      </div>
                    )}

                    {bucket.objectLock.rule.defaultRetention.mode === 'GOVERNANCE' && (
                      <div className="bg-yellow-50 border border-yellow-200 rounded-md p-2 mt-3">
                        <p className="text-xs text-yellow-700">
                          <strong>GOVERNANCE Mode:</strong> Users with special permissions can bypass retention.
                        </p>
                      </div>
                    )}
                  </div>
                )}
              </div>

              <p className="text-xs text-muted-foreground">
                Object Lock settings cannot be changed after bucket creation.
              </p>
            </div>
          ) : (
            <div className="bg-gray-50 border border-gray-200 rounded-md p-3 flex items-center gap-2">
              <Info className="h-4 w-4 text-gray-500" />
              <span className="text-sm text-gray-600">Object Lock is not enabled for this bucket</span>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Encryption */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Key className="h-5 w-5" />
            Encryption
          </CardTitle>
        </CardHeader>
        <CardContent>
          {bucket?.encryption ? (
            <div className="bg-green-50 border border-green-200 rounded-md p-3">
              <div className="flex items-center gap-2">
                <Shield className="h-4 w-4 text-green-600" />
                <span className="text-sm text-green-800">
                  <strong>Server-side encryption:</strong> {bucket.encryption.type}
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-2 ml-6">
                All objects are encrypted at rest using {bucket.encryption.type === 'AES256' ? 'AES-256-GCM' : bucket.encryption.type}
              </p>
            </div>
          ) : (
            <div className="bg-gray-50 border border-gray-200 rounded-md p-3 flex items-center gap-2">
              <Info className="h-4 w-4 text-gray-500" />
              <span className="text-sm text-gray-600">Server-side encryption is not enabled</span>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Public Access Control */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            Public Access Control
          </CardTitle>
        </CardHeader>
        <CardContent>
          {bucket?.publicAccessBlock ? (
            <div className="space-y-2">
              <div className="grid grid-cols-2 gap-3">
                <div className={`p-2 rounded-md border ${bucket.publicAccessBlock.blockPublicAcls ? 'bg-green-50 border-green-200' : 'bg-red-50 border-red-200'}`}>
                  <p className="text-xs font-medium">Block Public ACLs</p>
                  <p className={`text-sm font-semibold ${bucket.publicAccessBlock.blockPublicAcls ? 'text-green-700' : 'text-red-700'}`}>
                    {bucket.publicAccessBlock.blockPublicAcls ? '✓ Enabled' : '✗ Disabled'}
                  </p>
                </div>

                <div className={`p-2 rounded-md border ${bucket.publicAccessBlock.ignorePublicAcls ? 'bg-green-50 border-green-200' : 'bg-red-50 border-red-200'}`}>
                  <p className="text-xs font-medium">Ignore Public ACLs</p>
                  <p className={`text-sm font-semibold ${bucket.publicAccessBlock.ignorePublicAcls ? 'text-green-700' : 'text-red-700'}`}>
                    {bucket.publicAccessBlock.ignorePublicAcls ? '✓ Enabled' : '✗ Disabled'}
                  </p>
                </div>

                <div className={`p-2 rounded-md border ${bucket.publicAccessBlock.blockPublicPolicy ? 'bg-green-50 border-green-200' : 'bg-red-50 border-red-200'}`}>
                  <p className="text-xs font-medium">Block Public Policy</p>
                  <p className={`text-sm font-semibold ${bucket.publicAccessBlock.blockPublicPolicy ? 'text-green-700' : 'text-red-700'}`}>
                    {bucket.publicAccessBlock.blockPublicPolicy ? '✓ Enabled' : '✗ Disabled'}
                  </p>
                </div>

                <div className={`p-2 rounded-md border ${bucket.publicAccessBlock.restrictPublicBuckets ? 'bg-green-50 border-green-200' : 'bg-red-50 border-red-200'}`}>
                  <p className="text-xs font-medium">Restrict Public Buckets</p>
                  <p className={`text-sm font-semibold ${bucket.publicAccessBlock.restrictPublicBuckets ? 'text-green-700' : 'text-red-700'}`}>
                    {bucket.publicAccessBlock.restrictPublicBuckets ? '✓ Enabled' : '✗ Disabled'}
                  </p>
                </div>
              </div>
            </div>
          ) : (
            <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-start gap-2">
              <AlertTriangle className="h-5 w-5 text-red-600 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-red-800">
                <strong>Warning:</strong> No public access controls are configured for this bucket
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Tags */}
      {bucket?.tags && Object.keys(bucket.tags).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Tag className="h-5 w-5" />
              Tags
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(bucket.tags).map(([key, value]) => (
                <div key={key} className="flex items-center gap-2 bg-gray-50 p-2 rounded">
                  <span className="text-xs font-medium text-gray-600">{key}:</span>
                  <span className="text-xs font-mono bg-white px-2 py-1 rounded border">{value}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Danger Zone */}
      <Card className="border-red-200">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-red-600">
            <AlertTriangle className="h-5 w-5" />
            Danger Zone
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between p-4 border border-red-200 rounded-md bg-red-50">
            <div>
              <h3 className="font-semibold text-red-600">Delete Bucket</h3>
              <p className="text-sm text-muted-foreground">
                Permanently delete this bucket and all its contents. This action cannot be undone.
              </p>
              {bucket?.objectLock?.objectLockEnabled && (
                <p className="text-sm text-red-600 mt-1">
                  ⚠️ This bucket has Object Lock enabled. Objects under retention cannot be deleted.
                </p>
              )}
            </div>
            <Button
              variant="destructive"
              onClick={handleDeleteBucket}
              disabled={deleteBucketMutation.isPending}
              className="gap-2"
            >
              <Trash2 className="h-4 w-4" />
              {deleteBucketMutation.isPending ? 'Deleting...' : 'Delete Bucket'}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
