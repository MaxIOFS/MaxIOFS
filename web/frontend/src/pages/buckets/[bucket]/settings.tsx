import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { Modal } from '@/components/ui/Modal';
import { ObjectLockConfigModal } from '@/components/ObjectLockConfigModal';
import {
  ArrowLeft,
  Shield,
  Clock,
  Tag,
  Lock,
  Globe,
  FileText,
  Users,
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
  const [noncurrentDays, setNoncurrentDays] = useState<number>(30);
  const [deleteExpiredMarkers, setDeleteExpiredMarkers] = useState<boolean>(true);
  const [policyTab, setPolicyTab] = useState<'editor' | 'templates'>('editor');
  const [isTagsModalOpen, setIsTagsModalOpen] = useState(false);
  const [tags, setTags] = useState<Record<string, string>>({});
  const [newTagKey, setNewTagKey] = useState('');
  const [newTagValue, setNewTagValue] = useState('');
  const [currentPolicy, setCurrentPolicy] = useState<any>(null);
  const [policyStatementCount, setPolicyStatementCount] = useState<number>(0);

  // ACL state
  const [isACLModalOpen, setIsACLModalOpen] = useState(false);
  const [selectedCannedACL, setSelectedCannedACL] = useState<string>('private');
  const [currentACL, setCurrentACL] = useState<string>('private');
  const [aclViewMode, setAclViewMode] = useState<'simple' | 'advanced'>('simple');

  // Object Lock state
  const [isObjectLockModalOpen, setIsObjectLockModalOpen] = useState(false);

  // CORS rules state
  interface CORSRule {
    id: string;
    allowedOrigins: string[];
    allowedMethods: string[];
    allowedHeaders: string[];
    exposeHeaders: string[];
    maxAgeSeconds: number;
  }
  const [corsRules, setCorsRules] = useState<CORSRule[]>([]);
  const [editingCorsRule, setEditingCorsRule] = useState<CORSRule | null>(null);
  const [newOrigin, setNewOrigin] = useState('');
  const [newAllowedHeader, setNewAllowedHeader] = useState('');
  const [newExposeHeader, setNewExposeHeader] = useState('');
  const [corsViewMode, setCorsViewMode] = useState<'visual' | 'xml'>('visual');

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
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  // Helper to check if versioning is enabled
  const isVersioningEnabled = bucketData?.versioning?.Status === 'Enabled';

  // Policy mutations
  const savePolicyMutation = useMutation({
    mutationFn: (policy: string) => APIClient.putBucketPolicy(bucketName, policy),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsPolicyModalOpen(false);
      loadCurrentPolicy(); // Reload policy after save
      SweetAlert.toast('success', 'Bucket policy updated successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const deletePolicyMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketPolicy(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      loadCurrentPolicy(); // Reload policy after delete
      SweetAlert.toast('success', 'Bucket policy deleted successfully');
    },
    onError: (error: Error) => {
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
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteCORSMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketCORS(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'CORS configuration deleted successfully');
    },
    onError: (error: Error) => {
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
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteLifecycleMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketLifecycle(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Lifecycle rules deleted successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  // Tags mutations
  const saveTagsMutation = useMutation({
    mutationFn: (tagging: string) => APIClient.putBucketTagging(bucketName, tagging),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsTagsModalOpen(false);
      SweetAlert.toast('success', 'Bucket tags updated successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  const deleteTagsMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketTagging(bucketName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      SweetAlert.toast('success', 'Bucket tags deleted successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });

  // ACL mutations
  const saveACLMutation = useMutation({
    mutationFn: (cannedACL: string) => APIClient.putBucketACL(bucketName, '', cannedACL),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName] });
      setIsACLModalOpen(false);
      loadCurrentACL(); // Reload ACL after save
      SweetAlert.toast('success', 'Bucket ACL updated successfully');
    },
    onError: (error: Error) => {
      SweetAlert.apiError(error);
    },
  });


  // Load current ACL
  const loadCurrentACL = async () => {
    try {
      const response = await APIClient.getBucketACL(bucketName);
      console.log('ACL Response:', response); // Debug

      // First, check if the backend sent the canned_acl field directly
      const acl = response.data || response;

      if (acl.canned_acl || acl.CannedACL) {
        // Backend provided the canned ACL directly - use it!
        const cannedACL = acl.canned_acl || acl.CannedACL;
        console.log('Using canned_acl from backend:', cannedACL);
        setCurrentACL(cannedACL);
      } else {
        // Fallback: detect from grants
        console.log('Detecting ACL from grants');
        const grants = acl.Grant || acl.grants || [];
        const detectedACL = detectCannedACL(grants);
        setCurrentACL(detectedACL);
      }
    } catch (error) {
      console.error('Error loading bucket ACL:', error);
      setCurrentACL('private');
    }
  };

  // Load current Policy
  const loadCurrentPolicy = async () => {
    try {
      const response = await APIClient.getBucketPolicy(bucketName);
      console.log('Policy Response:', response); // Debug

      const policy = response.data || response;

      if (policy && policy.Statement) {
        setCurrentPolicy(policy);
        setPolicyStatementCount(policy.Statement.length);
      } else {
        setCurrentPolicy(null);
        setPolicyStatementCount(0);
      }
    } catch (error: any) {
      // Policy not found or error - this is normal if no policy is set
      console.log('No policy set or error loading policy:', error?.response?.status);
      setCurrentPolicy(null);
      setPolicyStatementCount(0);
    }
  };


  // Load current ACL and Policy on component mount
  useEffect(() => {
    loadCurrentACL();
    loadCurrentPolicy();
  }, [bucketName]);

  // Helper function to detect canned ACL from grants
  const detectCannedACL = (grants: any[]): string => {
    const hasAllUsersRead = grants.some((g: any) =>
      (g.Grantee?.URI?.includes('AllUsers') || g.Grantee?.uri?.includes('AllUsers')) &&
      (g.Permission === 'READ' || g.permission === 'READ')
    );

    const hasAllUsersWrite = grants.some((g: any) =>
      (g.Grantee?.URI?.includes('AllUsers') || g.Grantee?.uri?.includes('AllUsers')) &&
      (g.Permission === 'WRITE' || g.permission === 'WRITE')
    );

    const hasAuthenticatedUsersRead = grants.some((g: any) =>
      (g.Grantee?.URI?.includes('AuthenticatedUsers') || g.Grantee?.uri?.includes('AuthenticatedUsers')) &&
      (g.Permission === 'READ' || g.permission === 'READ')
    );

    if (hasAllUsersRead && hasAllUsersWrite) {
      return 'public-read-write';
    } else if (hasAllUsersRead) {
      return 'public-read';
    } else if (hasAuthenticatedUsersRead) {
      return 'authenticated-read';
    } else {
      return 'private';
    }
  };

  // Handlers
  const handleToggleVersioning = () => {
    const newState = !isVersioningEnabled;
    SweetAlert.confirm(
      `${newState ? 'Enable' : 'Suspend'} versioning?`,
      `This will ${newState ? 'enable' : 'suspend'} object versioning for this bucket.`,
      () => toggleVersioningMutation.mutate(newState)
    );
  };

  const handleEditPolicy = async () => {
    try {
      const response = await APIClient.getBucketPolicy(bucketName);
      // The response has format: { Policy: "JSON string" }
      let policyJson;
      if (response && response.Policy) {
        // Parse the Policy string to get the actual policy object
        policyJson = typeof response.Policy === 'string'
          ? JSON.parse(response.Policy)
          : response.Policy;
      } else {
        policyJson = response;
      }
      setPolicyText(JSON.stringify(policyJson, null, 2));
      setPolicyTab('editor');
      setIsPolicyModalOpen(true);
    } catch (error) {
      // No policy set, start with empty
      setPolicyText('');
      setPolicyTab('templates');
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
      const corsXml = await APIClient.getBucketCORS(bucketName);
      setCorsText(corsXml);

      // Parse XML to extract CORS rules
      const parser = new DOMParser();
      const xmlDoc = parser.parseFromString(corsXml, 'text/xml');
      const ruleElements = xmlDoc.getElementsByTagName('CORSRule');

      const parsedRules: CORSRule[] = [];
      for (let i = 0; i < ruleElements.length; i++) {
        const ruleEl = ruleElements[i];
        const rule: CORSRule = {
          id: ruleEl.getElementsByTagName('ID')[0]?.textContent || `rule-${i + 1}`,
          allowedOrigins: Array.from(ruleEl.getElementsByTagName('AllowedOrigin')).map(el => el.textContent || ''),
          allowedMethods: Array.from(ruleEl.getElementsByTagName('AllowedMethod')).map(el => el.textContent || ''),
          allowedHeaders: Array.from(ruleEl.getElementsByTagName('AllowedHeader')).map(el => el.textContent || ''),
          exposeHeaders: Array.from(ruleEl.getElementsByTagName('ExposeHeader')).map(el => el.textContent || ''),
          maxAgeSeconds: parseInt(ruleEl.getElementsByTagName('MaxAgeSeconds')[0]?.textContent || '0'),
        };
        parsedRules.push(rule);
      }

      setCorsRules(parsedRules);
      setIsCORSModalOpen(true);
    } catch (error) {
      setCorsText('');
      setCorsRules([]);
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

  const handleAddCorsRule = () => {
    const newRule: CORSRule = {
      id: `rule-${corsRules.length + 1}`,
      allowedOrigins: [],
      allowedMethods: [],
      allowedHeaders: [],
      exposeHeaders: [],
      maxAgeSeconds: 3600,
    };
    setEditingCorsRule(newRule);
  };

  const handleSaveCorsRule = () => {
    if (!editingCorsRule) return;

    if (editingCorsRule.allowedOrigins.length === 0) {
      SweetAlert.toast('error', 'At least one allowed origin is required');
      return;
    }
    if (editingCorsRule.allowedMethods.length === 0) {
      SweetAlert.toast('error', 'At least one allowed method is required');
      return;
    }

    const existingIndex = corsRules.findIndex(r => r.id === editingCorsRule.id);
    if (existingIndex >= 0) {
      const updated = [...corsRules];
      updated[existingIndex] = editingCorsRule;
      setCorsRules(updated);
    } else {
      setCorsRules([...corsRules, editingCorsRule]);
    }
    setEditingCorsRule(null);
  };

  const handleDeleteCorsRule = (id: string) => {
    setCorsRules(corsRules.filter(r => r.id !== id));
  };

  const handleSaveAllCorsRules = () => {
    // Generate XML from rules
    let xml = '<?xml version="1.0" encoding="UTF-8"?>\n<CORSConfiguration>\n';

    corsRules.forEach(rule => {
      xml += '  <CORSRule>\n';
      if (rule.id) xml += `    <ID>${rule.id}</ID>\n`;
      rule.allowedOrigins.forEach(origin => {
        xml += `    <AllowedOrigin>${origin}</AllowedOrigin>\n`;
      });
      rule.allowedMethods.forEach(method => {
        xml += `    <AllowedMethod>${method}</AllowedMethod>\n`;
      });
      rule.allowedHeaders.forEach(header => {
        xml += `    <AllowedHeader>${header}</AllowedHeader>\n`;
      });
      rule.exposeHeaders.forEach(header => {
        xml += `    <ExposeHeader>${header}</ExposeHeader>\n`;
      });
      if (rule.maxAgeSeconds > 0) {
        xml += `    <MaxAgeSeconds>${rule.maxAgeSeconds}</MaxAgeSeconds>\n`;
      }
      xml += '  </CORSRule>\n';
    });

    xml += '</CORSConfiguration>';
    saveCORSMutation.mutate(xml);
  };

  const toggleCorsMethod = (method: string) => {
    if (!editingCorsRule) return;
    const methods = editingCorsRule.allowedMethods.includes(method)
      ? editingCorsRule.allowedMethods.filter(m => m !== method)
      : [...editingCorsRule.allowedMethods, method];
    setEditingCorsRule({ ...editingCorsRule, allowedMethods: methods });
  };

  const addOriginToRule = () => {
    if (!editingCorsRule || !newOrigin.trim()) return;
    setEditingCorsRule({
      ...editingCorsRule,
      allowedOrigins: [...editingCorsRule.allowedOrigins, newOrigin.trim()]
    });
    setNewOrigin('');
  };

  const removeOriginFromRule = (origin: string) => {
    if (!editingCorsRule) return;
    setEditingCorsRule({
      ...editingCorsRule,
      allowedOrigins: editingCorsRule.allowedOrigins.filter(o => o !== origin)
    });
  };

  const addAllowedHeaderToRule = () => {
    if (!editingCorsRule || !newAllowedHeader.trim()) return;
    setEditingCorsRule({
      ...editingCorsRule,
      allowedHeaders: [...editingCorsRule.allowedHeaders, newAllowedHeader.trim()]
    });
    setNewAllowedHeader('');
  };

  const removeAllowedHeaderFromRule = (header: string) => {
    if (!editingCorsRule) return;
    setEditingCorsRule({
      ...editingCorsRule,
      allowedHeaders: editingCorsRule.allowedHeaders.filter(h => h !== header)
    });
  };

  const addExposeHeaderToRule = () => {
    if (!editingCorsRule || !newExposeHeader.trim()) return;
    setEditingCorsRule({
      ...editingCorsRule,
      exposeHeaders: [...editingCorsRule.exposeHeaders, newExposeHeader.trim()]
    });
    setNewExposeHeader('');
  };

  const removeExposeHeaderFromRule = (header: string) => {
    if (!editingCorsRule) return;
    setEditingCorsRule({
      ...editingCorsRule,
      exposeHeaders: editingCorsRule.exposeHeaders.filter(h => h !== header)
    });
  };

  const handleEditLifecycle = () => {
    // Use bucketData.lifecycle directly instead of making another API call
    const lifecycle = bucketData?.lifecycle;

    // Reset to defaults first
    setNoncurrentDays(30);
    setDeleteExpiredMarkers(true);

    // Parse lifecycle to extract values
    if (lifecycle && lifecycle.Rules && lifecycle.Rules.length > 0) {
      const rule = lifecycle.Rules[0];

      // Extract NoncurrentDays
      if (rule.NoncurrentVersionExpiration?.NoncurrentDays) {
        setNoncurrentDays(rule.NoncurrentVersionExpiration.NoncurrentDays);
      }

      // Extract ExpiredObjectDeleteMarker
      if (rule.Expiration?.ExpiredObjectDeleteMarker !== undefined) {
        setDeleteExpiredMarkers(rule.Expiration.ExpiredObjectDeleteMarker);
      }
    }

    setLifecycleText(lifecycle ? JSON.stringify(lifecycle, null, 2) : '');
    setIsLifecycleModalOpen(true);
  };

  const handleDeleteLifecycle = () => {
    SweetAlert.confirm(
      'Delete lifecycle rules?',
      'This will remove all lifecycle management rules for this bucket.',
      () => deleteLifecycleMutation.mutate()
    );
  };

  // Tags handlers
  const handleManageTags = async () => {
    try {
      const response = await APIClient.getBucketTagging(bucketName);
      // Parse XML response to get tags
      const parser = new DOMParser();
      const xmlDoc = parser.parseFromString(response, 'text/xml');
      const tagElements = xmlDoc.getElementsByTagName('Tag');
      const loadedTags: Record<string, string> = {};
      for (let i = 0; i < tagElements.length; i++) {
        const key = tagElements[i].getElementsByTagName('Key')[0]?.textContent || '';
        const value = tagElements[i].getElementsByTagName('Value')[0]?.textContent || '';
        if (key) {
          loadedTags[key] = value;
        }
      }
      setTags(loadedTags);
      setIsTagsModalOpen(true);
    } catch (error) {
      // No tags set, start with empty
      setTags({});
      setIsTagsModalOpen(true);
    }
  };

  const handleAddTag = () => {
    if (newTagKey && newTagValue) {
      setTags({ ...tags, [newTagKey]: newTagValue });
      setNewTagKey('');
      setNewTagValue('');
    }
  };

  const handleRemoveTag = (key: string) => {
    const newTags = { ...tags };
    delete newTags[key];
    setTags(newTags);
  };

  const handleSaveTags = () => {
    if (Object.keys(tags).length === 0) {
      // Delete all tags
      deleteTagsMutation.mutate();
    } else {
      // Build XML
      let xml = '<Tagging><TagSet>';
      Object.entries(tags).forEach(([key, value]) => {
        xml += `<Tag><Key>${key}</Key><Value>${value}</Value></Tag>`;
      });
      xml += '</TagSet></Tagging>';
      saveTagsMutation.mutate(xml);
    }
  };

  const handleDeleteAllTags = () => {
    SweetAlert.confirm(
      'Delete all bucket tags?',
      'This will remove all tags from this bucket.',
      () => deleteTagsMutation.mutate()
    );
  };

  // ACL handlers
  const handleManageACL = async () => {
    // Use the current loaded ACL
    setSelectedCannedACL(currentACL);
    setIsACLModalOpen(true);
  };

  const handleSaveACL = () => {
    saveACLMutation.mutate(selectedCannedACL);
  };

  // Policy Templates
  const policyTemplates = {
    publicRead: {
      name: 'Public Read Access',
      description: 'Allow anonymous read access to all objects',
      policy: {
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Principal: '*',
            Action: 's3:GetObject',
            Resource: `arn:aws:s3:::${bucketName}/*`,
          },
        ],
      },
    },
    publicReadWrite: {
      name: 'Public Read/Write Access',
      description: 'Allow anonymous read and write access',
      policy: {
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Principal: '*',
            Action: ['s3:GetObject', 's3:PutObject', 's3:DeleteObject'],
            Resource: `arn:aws:s3:::${bucketName}/*`,
          },
        ],
      },
    },
    listOnly: {
      name: 'Public List Access',
      description: 'Allow listing bucket contents only',
      policy: {
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Principal: '*',
            Action: 's3:ListBucket',
            Resource: `arn:aws:s3:::${bucketName}`,
          },
        ],
      },
    },
    fullPublic: {
      name: 'Full Public Access',
      description: 'Allow all operations to everyone',
      policy: {
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Principal: '*',
            Action: 's3:*',
            Resource: [
              `arn:aws:s3:::${bucketName}`,
              `arn:aws:s3:::${bucketName}/*`,
            ],
          },
        ],
      },
    },
  };

  const handleUseTemplate = (templateKey: keyof typeof policyTemplates) => {
    const template = policyTemplates[templateKey];
    setPolicyText(JSON.stringify(template.policy, null, 2));
    setPolicyTab('editor');
  };

  const handleSavePolicy = () => {
    try {
      // Validate JSON
      JSON.parse(policyText);
      savePolicyMutation.mutate(policyText);
    } catch (error) {
      SweetAlert.error('Invalid JSON', 'Please enter a valid JSON policy document');
    }
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
                    {isVersioningEnabled ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                <Button
                  variant="outline"
                  onClick={handleToggleVersioning}
                  disabled={toggleVersioningMutation.isPending}
                >
                  {isVersioningEnabled ? 'Suspend' : 'Enable'}
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
                  <Button variant="outline" onClick={() => setIsObjectLockModalOpen(true)}>
                    Configure
                  </Button>
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
                <div className="flex-1">
                  <p className="font-medium">Access Policy</p>
                  <p className="text-sm text-gray-500">
                    Define fine-grained permissions using JSON policy documents
                  </p>
                  <div className="mt-2">
                    {currentPolicy ? (
                      <div className="flex items-center gap-2">
                        <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
                          ✓ Policy Active
                        </span>
                        <span className="text-xs text-gray-600 dark:text-gray-400">
                          {policyStatementCount} statement{policyStatementCount !== 1 ? 's' : ''}
                        </span>
                      </div>
                    ) : (
                      <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400">
                        No Policy Set
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleEditPolicy}>
                    {currentPolicy ? 'Edit Policy' : 'Add Policy'}
                  </Button>
                  {currentPolicy && (
                    <Button variant="destructive" size="sm" onClick={handleDeletePolicy}>
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Bucket ACL */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Users className="h-5 w-5" />
              Access Control List (ACL)
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex-1">
                  <p className="font-medium">Bucket Permissions</p>
                  <p className="text-sm text-gray-500">
                    Control who can access this bucket
                  </p>
                  <div className="mt-2">
                    <span className="text-xs font-medium text-gray-600 dark:text-gray-400">Current ACL: </span>
                    <span className={`inline-flex items-center px-2 py-1 rounded text-xs font-medium ${
                      currentACL === 'private' ? 'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200' :
                      currentACL === 'public-read' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200' :
                      currentACL === 'public-read-write' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200' :
                      currentACL === 'authenticated-read' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' :
                      'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200'
                    }`}>
                      {currentACL === 'private' && '🔒 Private'}
                      {currentACL === 'public-read' && '👁️ Public Read'}
                      {currentACL === 'public-read-write' && '⚠️ Public Read/Write'}
                      {currentACL === 'authenticated-read' && '🔐 Authenticated Read'}
                    </span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleManageACL}>
                    Manage ACL
                  </Button>
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
                <div className="flex gap-2">
                  <Button variant="outline" onClick={handleManageTags}>
                    Manage Tags
                  </Button>
                  {bucketData?.tags && Object.keys(bucketData.tags).length > 0 && (
                    <Button variant="destructive" size="sm" onClick={handleDeleteAllTags}>
                      Delete All
                    </Button>
                  )}
                </div>
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
        title="Bucket Policy"
        size="xl"
      >
        <div className="space-y-4">
          {/* Tabs */}
          <div className="border-b border-gray-200 dark:border-gray-700">
            <nav className="-mb-px flex space-x-8">
              <button
                onClick={() => setPolicyTab('editor')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  policyTab === 'editor'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
                }`}
              >
                Policy Editor
              </button>
              <button
                onClick={() => setPolicyTab('templates')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  policyTab === 'templates'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300 dark:text-gray-400 dark:hover:text-gray-300'
                }`}
              >
                Templates
              </button>
            </nav>
          </div>

          {/* Editor Tab */}
          {policyTab === 'editor' && (
            <div className="space-y-4">
              <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
                <p className="text-sm text-blue-800 dark:text-blue-200">
                  <strong>Tip:</strong> You can use templates as a starting point, then customize them in the editor.
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Policy JSON
                </label>
                <textarea
                  value={policyText}
                  onChange={(e) => setPolicyText(e.target.value)}
                  rows={18}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md font-mono text-sm"
                  placeholder='{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::bucket/*"}]}'
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  Enter a valid S3 bucket policy in JSON format. The policy will be validated before saving.
                </p>
              </div>
            </div>
          )}

          {/* Templates Tab */}
          {policyTab === 'templates' && (
            <div className="space-y-4">
              <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
                <p className="text-sm text-yellow-800 dark:text-yellow-200">
                  <strong>Warning:</strong> These templates grant public access. Use carefully and only when needed.
                </p>
              </div>
              <div className="space-y-3">
                {Object.entries(policyTemplates).map(([key, template]) => (
                  <div
                    key={key}
                    className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors"
                  >
                    <div className="flex items-start justify-between">
                      <div className="flex-1">
                        <h4 className="text-sm font-semibold text-gray-900 dark:text-white">
                          {template.name}
                        </h4>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          {template.description}
                        </p>
                      </div>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleUseTemplate(key as keyof typeof policyTemplates)}
                      >
                        Use Template
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="outline" onClick={() => setIsPolicyModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSavePolicy}
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
        onClose={() => {
          setIsCORSModalOpen(false);
          setEditingCorsRule(null);
        }}
        title="CORS Configuration"
        size="xl"
      >
        <div className="space-y-4">
          {/* View Mode Toggle */}
          <div className="flex gap-2 border-b border-gray-200 dark:border-gray-700">
            <button
              onClick={() => setCorsViewMode('visual')}
              className={`px-4 py-2 font-medium text-sm ${
                corsViewMode === 'visual'
                  ? 'text-blue-600 border-b-2 border-blue-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              Visual Editor
            </button>
            <button
              onClick={() => setCorsViewMode('xml')}
              className={`px-4 py-2 font-medium text-sm ${
                corsViewMode === 'xml'
                  ? 'text-blue-600 border-b-2 border-blue-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              XML Editor
            </button>
          </div>

          {/* Visual Mode */}
          {corsViewMode === 'visual' && !editingCorsRule && (
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <h3 className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  CORS Rules ({corsRules.length})
                </h3>
                <Button variant="default" size="sm" onClick={handleAddCorsRule}>
                  Add Rule
                </Button>
              </div>

              {corsRules.length === 0 ? (
                <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                  No CORS rules configured. Click "Add Rule" to create one.
                </div>
              ) : (
                <div className="space-y-3">
                  {corsRules.map((rule, index) => (
                    <div
                      key={rule.id}
                      className="border border-gray-200 dark:border-gray-700 rounded-lg p-4 bg-gray-50 dark:bg-gray-800"
                    >
                      <div className="flex justify-between items-start mb-2">
                        <div className="font-medium text-sm text-gray-900 dark:text-white">
                          Rule {index + 1}: {rule.id}
                        </div>
                        <div className="flex gap-2">
                          <button
                            onClick={() => setEditingCorsRule(rule)}
                            className="text-blue-600 hover:text-blue-700 text-sm"
                          >
                            Edit
                          </button>
                          <button
                            onClick={() => handleDeleteCorsRule(rule.id)}
                            className="text-red-600 hover:text-red-700 text-sm"
                          >
                            Delete
                          </button>
                        </div>
                      </div>
                      <div className="grid grid-cols-2 gap-2 text-xs">
                        <div>
                          <span className="font-medium text-gray-600 dark:text-gray-400">Origins:</span>{' '}
                          {rule.allowedOrigins.join(', ')}
                        </div>
                        <div>
                          <span className="font-medium text-gray-600 dark:text-gray-400">Methods:</span>{' '}
                          {rule.allowedMethods.join(', ')}
                        </div>
                        {rule.allowedHeaders.length > 0 && (
                          <div>
                            <span className="font-medium text-gray-600 dark:text-gray-400">Allowed Headers:</span>{' '}
                            {rule.allowedHeaders.join(', ')}
                          </div>
                        )}
                        {rule.exposeHeaders.length > 0 && (
                          <div>
                            <span className="font-medium text-gray-600 dark:text-gray-400">Expose Headers:</span>{' '}
                            {rule.exposeHeaders.join(', ')}
                          </div>
                        )}
                        {rule.maxAgeSeconds > 0 && (
                          <div>
                            <span className="font-medium text-gray-600 dark:text-gray-400">Max Age:</span>{' '}
                            {rule.maxAgeSeconds}s
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
                <Button variant="outline" onClick={() => setIsCORSModalOpen(false)}>
                  Cancel
                </Button>
                <Button
                  onClick={handleSaveAllCorsRules}
                  disabled={saveCORSMutation.isPending || corsRules.length === 0}
                >
                  {saveCORSMutation.isPending ? 'Saving...' : 'Save Configuration'}
                </Button>
              </div>
            </div>
          )}

          {/* Edit Rule Form */}
          {corsViewMode === 'visual' && editingCorsRule && (
            <div className="space-y-4">
              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
                <h3 className="text-sm font-medium text-blue-900 dark:text-blue-100 mb-2">
                  {corsRules.find(r => r.id === editingCorsRule.id) ? 'Edit' : 'Add'} CORS Rule
                </h3>
              </div>

              {/* Rule ID */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Rule ID
                </label>
                <input
                  type="text"
                  value={editingCorsRule.id}
                  onChange={(e) => setEditingCorsRule({ ...editingCorsRule, id: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                  placeholder="e.g., rule-1"
                />
              </div>

              {/* Allowed Origins */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Allowed Origins <span className="text-red-500">*</span>
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newOrigin}
                    onChange={(e) => setNewOrigin(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addOriginToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                    placeholder="e.g., https://example.com or *"
                  />
                  <Button onClick={addOriginToRule} disabled={!newOrigin.trim()}>
                    Add
                  </Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  {editingCorsRule.allowedOrigins.map(origin => (
                    <span
                      key={origin}
                      className="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded text-sm"
                    >
                      {origin}
                      <button onClick={() => removeOriginFromRule(origin)} className="hover:text-red-600">
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              </div>

              {/* Allowed Methods */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Allowed Methods <span className="text-red-500">*</span>
                </label>
                <div className="flex flex-wrap gap-2">
                  {['GET', 'PUT', 'POST', 'DELETE', 'HEAD'].map(method => (
                    <label key={method} className="inline-flex items-center">
                      <input
                        type="checkbox"
                        checked={editingCorsRule.allowedMethods.includes(method)}
                        onChange={() => toggleCorsMethod(method)}
                        className="mr-2"
                      />
                      <span className="text-sm text-gray-700 dark:text-gray-300">{method}</span>
                    </label>
                  ))}
                </div>
              </div>

              {/* Allowed Headers */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Allowed Headers (Optional)
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newAllowedHeader}
                    onChange={(e) => setNewAllowedHeader(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addAllowedHeaderToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                    placeholder="e.g., Authorization, Content-Type, or *"
                  />
                  <Button onClick={addAllowedHeaderToRule} disabled={!newAllowedHeader.trim()}>
                    Add
                  </Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  {editingCorsRule.allowedHeaders.map(header => (
                    <span
                      key={header}
                      className="inline-flex items-center gap-1 px-2 py-1 bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 rounded text-sm"
                    >
                      {header}
                      <button onClick={() => removeAllowedHeaderFromRule(header)} className="hover:text-red-600">
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              </div>

              {/* Expose Headers */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Expose Headers (Optional)
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newExposeHeader}
                    onChange={(e) => setNewExposeHeader(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addExposeHeaderToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                    placeholder="e.g., ETag, x-amz-request-id"
                  />
                  <Button onClick={addExposeHeaderToRule} disabled={!newExposeHeader.trim()}>
                    Add
                  </Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  {editingCorsRule.exposeHeaders.map(header => (
                    <span
                      key={header}
                      className="inline-flex items-center gap-1 px-2 py-1 bg-purple-100 dark:bg-purple-900 text-purple-800 dark:text-purple-200 rounded text-sm"
                    >
                      {header}
                      <button onClick={() => removeExposeHeaderFromRule(header)} className="hover:text-red-600">
                        ×
                      </button>
                    </span>
                  ))}
                </div>
              </div>

              {/* Max Age Seconds */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Max Age (seconds)
                </label>
                <input
                  type="number"
                  value={editingCorsRule.maxAgeSeconds}
                  onChange={(e) => setEditingCorsRule({ ...editingCorsRule, maxAgeSeconds: parseInt(e.target.value) || 0 })}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
                  placeholder="3600"
                  min="0"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                  How long browsers can cache preflight results (0 to disable)
                </p>
              </div>

              <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
                <Button variant="outline" onClick={() => setEditingCorsRule(null)}>
                  Cancel
                </Button>
                <Button onClick={handleSaveCorsRule}>
                  Save Rule
                </Button>
              </div>
            </div>
          )}

          {/* XML Mode */}
          {corsViewMode === 'xml' && (
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
          )}
        </div>
      </Modal>

      {/* Lifecycle Modal */}
      <Modal
        isOpen={isLifecycleModalOpen}
        onClose={() => setIsLifecycleModalOpen(false)}
        title="Lifecycle Rules"
        size="lg"
      >
        <div className="space-y-6">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              Lifecycle policies automatically delete old versions and expired delete markers to save storage space.
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Delete Noncurrent Versions After (days)
            </label>
            <input
              type="number"
              min="1"
              max="3650"
              value={noncurrentDays}
              onChange={(e) => setNoncurrentDays(parseInt(e.target.value) || 30)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Automatically delete versions that are no longer the latest after this many days
            </p>
          </div>

          <div className="flex items-start gap-2">
            <input
              type="checkbox"
              id="delete-markers"
              checked={deleteExpiredMarkers}
              onChange={(e) => setDeleteExpiredMarkers(e.target.checked)}
              className="mt-1 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
            />
            <div>
              <label htmlFor="delete-markers" className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Delete Expired Delete Markers
              </label>
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                Automatically remove delete markers when they are the only version remaining
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t">
            <Button variant="outline" onClick={() => setIsLifecycleModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                // Generate XML from form values
                const xml = `<LifecycleConfiguration>
  <Rule>
    <ID>delete-old-versions</ID>
    <Status>Enabled</Status>
    <Prefix></Prefix>
    <NoncurrentVersionExpiration>
      <NoncurrentDays>${noncurrentDays}</NoncurrentDays>
    </NoncurrentVersionExpiration>${deleteExpiredMarkers ? '\n    <Expiration>\n      <ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker>\n    </Expiration>' : ''}
  </Rule>
</LifecycleConfiguration>`;
                saveLifecycleMutation.mutate(xml);
              }}
              disabled={saveLifecycleMutation.isPending}
            >
              {saveLifecycleMutation.isPending ? 'Saving...' : 'Save Rules'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Tags Modal */}
      <Modal
        isOpen={isTagsModalOpen}
        onClose={() => setIsTagsModalOpen(false)}
        title="Manage Bucket Tags"
        size="lg"
      >
        <div className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              Tags are key-value pairs that help you organize and categorize your buckets.
            </p>
          </div>

          {/* Existing Tags */}
          {Object.keys(tags).length > 0 && (
            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Current Tags
              </label>
              <div className="space-y-2">
                {Object.entries(tags).map(([key, value]) => (
                  <div
                    key={key}
                    className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-md"
                  >
                    <div>
                      <span className="font-medium text-sm text-gray-900 dark:text-white">
                        {key}
                      </span>
                      <span className="text-gray-500 dark:text-gray-400 mx-2">:</span>
                      <span className="text-sm text-gray-700 dark:text-gray-300">
                        {value}
                      </span>
                    </div>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() => handleRemoveTag(key)}
                    >
                      Remove
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Add New Tag */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Add New Tag
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                placeholder="Key"
                value={newTagKey}
                onChange={(e) => setNewTagKey(e.target.value)}
                className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <input
                type="text"
                placeholder="Value"
                value={newTagValue}
                onChange={(e) => setNewTagValue(e.target.value)}
                className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <Button onClick={handleAddTag} disabled={!newTagKey || !newTagValue}>
                Add
              </Button>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="outline" onClick={() => setIsTagsModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveTags}
              disabled={saveTagsMutation.isPending}
            >
              {saveTagsMutation.isPending ? 'Saving...' : 'Save Tags'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* ACL Modal */}
      <Modal
        isOpen={isACLModalOpen}
        onClose={() => setIsACLModalOpen(false)}
        title="Bucket Access Control List (ACL)"
        size="lg"
      >
        <div className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              <strong>ACL (Access Control List)</strong> defines who can access your bucket and what permissions they have.
              Choose a canned ACL for simple permission management.
            </p>
          </div>

          {/* Canned ACL Selection */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Select Permission Level
            </label>

            <div className="space-y-2">
              {/* Private */}
              <label className="flex items-start gap-3 p-3 border border-gray-200 dark:border-gray-700 rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="private"
                  checked={selectedCannedACL === 'private'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-gray-900 dark:text-white">Private</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Owner gets FULL_CONTROL. No one else has access rights.
                  </div>
                </div>
              </label>

              {/* Public Read */}
              <label className="flex items-start gap-3 p-3 border border-gray-200 dark:border-gray-700 rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="public-read"
                  checked={selectedCannedACL === 'public-read'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-gray-900 dark:text-white">Public Read</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Owner gets FULL_CONTROL. Anyone (including anonymous users) can READ objects.
                  </div>
                </div>
              </label>

              {/* Public Read Write */}
              <label className="flex items-start gap-3 p-3 border border-gray-200 dark:border-gray-700 rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="public-read-write"
                  checked={selectedCannedACL === 'public-read-write'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-gray-900 dark:text-white">Public Read/Write</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Owner gets FULL_CONTROL. Anyone can READ and WRITE objects. <strong className="text-yellow-600">Use with caution!</strong>
                  </div>
                </div>
              </label>

              {/* Authenticated Read */}
              <label className="flex items-start gap-3 p-3 border border-gray-200 dark:border-gray-700 rounded-lg cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="authenticated-read"
                  checked={selectedCannedACL === 'authenticated-read'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-gray-900 dark:text-white">Authenticated Read</div>
                  <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Owner gets FULL_CONTROL. Any authenticated AWS user can READ objects.
                  </div>
                </div>
              </label>
            </div>
          </div>

          {/* Warning for public access */}
          {(selectedCannedACL === 'public-read' || selectedCannedACL === 'public-read-write') && (
            <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
              <p className="text-sm text-yellow-800 dark:text-yellow-200">
                <strong>Warning:</strong> This ACL grants public access. Anyone on the internet can access your bucket.
              </p>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="outline" onClick={() => setIsACLModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveACL}
              disabled={saveACLMutation.isPending}
            >
              {saveACLMutation.isPending ? 'Saving...' : 'Save ACL'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Object Lock Configuration Modal */}
      <ObjectLockConfigModal
        isOpen={isObjectLockModalOpen}
        onClose={() => setIsObjectLockModalOpen(false)}
        bucketName={bucketName}
        currentMode={bucketData?.objectLock?.rule?.defaultRetention?.mode || 'GOVERNANCE'}
        currentDays={bucketData?.objectLock?.rule?.defaultRetention?.days}
        currentYears={bucketData?.objectLock?.rule?.defaultRetention?.years}
      />
    </div>
  );
}
