import React, { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import {
  ArrowLeft,
  Shield,
  Clock,
  Tag,
  Lock,
  Globe,
  FileText,
  AlertCircle,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import SweetAlert from '@/lib/sweetalert';

export default function BucketSettingsPage() {
  const { bucket, tenantId } = useParams<{ bucket: string; tenantId?: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const bucketName = bucket as string;
  const bucketPath = tenantId ? `/buckets/${tenantId}/${bucketName}` : `/buckets/${bucketName}`;

  // Modal states
  const [isPolicyModalOpen, setIsPolicyModalOpen] = useState(false);
  const [isCORSModalOpen, setIsCORSModalOpen] = useState(false);
  const [isLifecycleModalOpen, setIsLifecycleModalOpen] = useState(false);
  const [policyText, setPolicyText] = useState('');
  const [corsText, setCorsText] = useState('');
  const [lifecycleText, setLifecycleText] = useState('');

  const { data: bucketData, isLoading } = useQuery({
    queryKey: ['bucket', bucketName],
    queryFn: () => APIClient.getBucket(bucketName),
  });

  // Versioning mutation
  const toggleVersioningMutation = useMutation({
    mutationFn: (enabled: boolean) => APIClient.putBucketVersioning(bucketName, enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Versioning updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // Policy mutations
  const savePolicyMutation = useMutation({
    mutationFn: (policy: string) => APIClient.putBucketPolicy(bucketName, policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsPolicyModalOpen(false);
      SweetAlert.toast('success', 'Bucket policy updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deletePolicyMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketPolicy(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Bucket policy deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // CORS mutations
  const saveCORSMutation = useMutation({
    mutationFn: (cors: string) => APIClient.putBucketCORS(bucketName, cors),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsCORSModalOpen(false);
      SweetAlert.toast('success', 'CORS configuration updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteCORSMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketCORS(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'CORS configuration deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // Lifecycle mutations
  const saveLifecycleMutation = useMutation({
    mutationFn: (lifecycle: string) => APIClient.putBucketLifecycle(bucketName, lifecycle),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsLifecycleModalOpen(false);
      SweetAlert.toast('success', 'Lifecycle rules updated successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteLifecycleMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketLifecycle(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Lifecycle rules deleted successfully');
    },
    onError: (error: any) => {
      SweetAlert.apiError(error);
    },
  });

  // Handlers
  const handleToggleVersioning = () => {
    const newState = !bucketData?.versioning;
    SweetAlert.confirm(
      `${newState ? 'Enable' : 'Suspend'} versioning?`,
      `This will ${newState ? 'enable' : 'suspend'} object versioning for this bucket.`,
      () => toggleVersioningMutation.mutate(newState)
    );
  };

  const handleEditPolicy = async () => {
    try {
      const policy = await APIClient.getBucketPolicy(bucketName);
      setPolicyText(typeof policy === 'string' ? policy : JSON.stringify(policy, null, 2));
      setIsPolicyModalOpen(true);
    } catch (error) {
      setPolicyText('');
      setIsPolicyModalOpen(true);
    }
  };

  const handleDeletePolicy = () => {
    SweetAlert.confirm(
      'Delete bucket policy?',
      'This will remove all custom access policies for this bucket.',
      () => deletePolicyMutation.mutate()
    );
  };

  const handleEditCORS = async () => {
    try {
      const cors = await APIClient.getBucketCORS(bucketName);
      setCorsText(typeof cors === 'string' ? cors : JSON.stringify(cors, null, 2));
      setIsCORSModalOpen(true);
    } catch (error) {
      setCorsText('');
      setIsCORSModalOpen(true);
    }
  };

  const handleDeleteCORS = () => {
    SweetAlert.confirm(
      'Delete CORS configuration?',
      'This will remove all CORS rules for this bucket.',
      () => deleteCORSMutation.mutate()
    );
  };

  const handleEditLifecycle = async () => {
    try {
      const lifecycle = await APIClient.getBucketLifecycle(bucketName);
      setLifecycleText(typeof lifecycle === 'string' ? lifecycle : JSON.stringify(lifecycle, null, 2));
      setIsLifecycleModalOpen(true);
    } catch (error) {
      setLifecycleText('');
      setIsLifecycleModalOpen(true);
    }
  };

  const handleDeleteLifecycle = () => {
    SweetAlert.confirm(
      'Delete lifecycle rules?',
      'This will remove all lifecycle management rules for this bucket.',
      () => deleteLifecycleMutation.mutate()
    );
  };

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
            onClick={() => navigate(bucketPath)}
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
                <Button
                  variant="outline"
                  onClick={handleToggleVersioning}
                  disabled={toggleVersioningMutation.isPending}
                >
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
                    {bucketData?.objectLock?.objectLockEnabled ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                {bucketData?.objectLock?.objectLockEnabled && (
                  <Button variant="outline">Configure</Button>
                )}
              </div>
              {bucketData?.objectLock?.objectLockEnabled && bucketData?.objectLock?.rule && (
                <div className="rounded-lg border p-4 space-y-2">
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Mode:</span>
                    <span className="text-sm font-medium">
                      {bucketData.objectLock.rule.defaultRetention?.mode || 'Not Set'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">Retention:</span>
                    <span className="text-sm font-medium">
                      {bucketData.objectLock.rule.defaultRetention?.days
                        ? `${bucketData.objectLock.rule.defaultRetention.days} days`
                        : bucketData.objectLock.rule.defaultRetention?.years
                        ? `${bucketData.objectLock.rule.defaultRetention.years} years`
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
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleEditPolicy}>
                    {bucketData?.policy ? 'Edit Policy' : 'Add Policy'}
                  </Button>
                  {bucketData?.policy && (
                    <Button variant="destructive" size="sm" onClick={handleDeletePolicy}>
                      Delete
                    </Button>
                  )}
                </div>
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
                <Button
                  variant="outline"
                  onClick={() => SweetAlert.info('Not Implemented', 'Bucket tagging is not yet implemented in the backend')}
                >
                  Manage Tags
                </Button>
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
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleEditCORS}>
                    {bucketData?.cors ? 'Edit CORS' : 'Add CORS'}
                  </Button>
                  {bucketData?.cors && (
                    <Button variant="destructive" size="sm" onClick={handleDeleteCORS}>
                      Delete
                    </Button>
                  )}
                </div>
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
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleEditLifecycle}>
                    {bucketData?.lifecycle ? 'Manage Rules' : 'Add Rule'}
                  </Button>
                  {bucketData?.lifecycle && (
                    <Button variant="destructive" size="sm" onClick={handleDeleteLifecycle}>
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Policy Modal */}
      <Modal
        isOpen={isPolicyModalOpen}
        onClose={() => setIsPolicyModalOpen(false)}
        title="Edit Bucket Policy"
        size="lg"
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Policy JSON
            </label>
            <textarea
              value={policyText}
              onChange={(e) => setPolicyText(e.target.value)}
              rows={15}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md font-mono text-sm"
              placeholder='{"Version":"2012-10-17","Statement":[...]}'
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Enter a valid S3 bucket policy in JSON format
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setIsPolicyModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => savePolicyMutation.mutate(policyText)}
              disabled={savePolicyMutation.isPending || !policyText.trim()}
            >
              {savePolicyMutation.isPending ? 'Saving...' : 'Save Policy'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* CORS Modal */}
      <Modal
        isOpen={isCORSModalOpen}
        onClose={() => setIsCORSModalOpen(false)}
        title="Edit CORS Configuration"
        size="lg"
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              CORS Configuration XML
            </label>
            <textarea
              value={corsText}
              onChange={(e) => setCorsText(e.target.value)}
              rows={15}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md font-mono text-sm"
              placeholder='<CORSConfiguration><CORSRule>...</CORSRule></CORSConfiguration>'
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Enter valid CORS configuration in XML format
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setIsCORSModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => saveCORSMutation.mutate(corsText)}
              disabled={saveCORSMutation.isPending || !corsText.trim()}
            >
              {saveCORSMutation.isPending ? 'Saving...' : 'Save CORS'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Lifecycle Modal */}
      <Modal
        isOpen={isLifecycleModalOpen}
        onClose={() => setIsLifecycleModalOpen(false)}
        title="Edit Lifecycle Rules"
        size="lg"
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Lifecycle Configuration XML
            </label>
            <textarea
              value={lifecycleText}
              onChange={(e) => setLifecycleText(e.target.value)}
              rows={15}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md font-mono text-sm"
              placeholder='<LifecycleConfiguration><Rule>...</Rule></LifecycleConfiguration>'
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Enter valid lifecycle configuration in XML format
            </p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setIsLifecycleModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => saveLifecycleMutation.mutate(lifecycleText)}
              disabled={saveLifecycleMutation.isPending || !lifecycleText.trim()}
            >
              {saveLifecycleMutation.isPending ? 'Saving...' : 'Save Rules'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
