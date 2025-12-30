import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
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
  Bell,
  Settings,
  Plus,
  Trash2,
  Edit,
  AlertCircle,
  CheckCircle,
  XCircle,
  RefreshCw,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import ModalManager from '@/lib/modals';
import { useAuth } from '@/hooks/useAuth';
import type { NotificationConfiguration, NotificationRule, ReplicationRule, CreateReplicationRuleRequest } from '@/types';

// Tab types
type TabId = 'general' | 'security' | 'lifecycle' | 'notifications' | 'replication';

interface TabInfo {
  id: TabId;
  label: string;
  icon: React.ComponentType<any>;
  description: string;
}

const tabs: TabInfo[] = [
  {
    id: 'general',
    label: 'General',
    icon: Settings,
    description: 'Versioning, encryption, and bucket tags',
  },
  {
    id: 'security',
    label: 'Security & Access',
    icon: Shield,
    description: 'Bucket policy, ACL, and CORS configuration',
  },
  {
    id: 'lifecycle',
    label: 'Lifecycle',
    icon: Clock,
    description: 'Lifecycle rules and automatic deletion policies',
  },
  {
    id: 'notifications',
    label: 'Notifications',
    icon: Bell,
    description: 'Event notifications and webhooks',
  },
  {
    id: 'replication',
    label: 'Replication',
    icon: RefreshCw,
    description: 'Cross-bucket and cross-region replication rules',
  },
];

export default function BucketSettingsPage() {
  const { bucket, tenantId } = useParams<{ bucket: string; tenantId?: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();
  const bucketName = bucket as string;
  const bucketPath = tenantId ? `/buckets/${tenantId}/${bucketName}` : `/buckets/${bucketName}`;

  // Active tab state
  const [activeTab, setActiveTab] = useState<TabId>('general');

  // Check if user is global admin (no tenantId) accessing a tenant bucket
  // Global admins should only have read-only access to tenant buckets
  const isGlobalAdminInTenantBucket = user && !user.tenantId && !!tenantId;

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

  // Notification state
  const [isNotificationModalOpen, setIsNotificationModalOpen] = useState(false);
  const [editingNotificationRule, setEditingNotificationRule] = useState<NotificationRule | null>(null);
  const [notificationRuleForm, setNotificationRuleForm] = useState<Partial<NotificationRule>>({
    id: '',
    enabled: true,
    webhookUrl: '',
    events: [],
    filterPrefix: '',
    filterSuffix: '',
    customHeaders: {},
  });

  // Replication state
  const [isReplicationModalOpen, setIsReplicationModalOpen] = useState(false);
  const [editingReplicationRule, setEditingReplicationRule] = useState<ReplicationRule | null>(null);
  const [replicationRuleForm, setReplicationRuleForm] = useState<Partial<CreateReplicationRuleRequest>>({
    destination_endpoint: '',
    destination_bucket: '',
    destination_access_key: '',
    destination_secret_key: '',
    destination_region: '',
    prefix: '',
    enabled: true,
    priority: 1,
    mode: 'realtime',
    schedule_interval: 60,
    conflict_resolution: 'last_write_wins',
    replicate_deletes: true,
    replicate_metadata: true,
  });

  const { data: bucketData, isLoading } = useQuery({
    queryKey: ['bucket', bucketName, tenantId],
    queryFn: () => APIClient.getBucket(bucketName, tenantId || undefined),
  });

  // Versioning mutation
  const toggleVersioningMutation = useMutation({
    mutationFn: (enabled: boolean) => APIClient.putBucketVersioning(bucketName, enabled, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', 'Versioning updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Helper to check if versioning is enabled
  const isVersioningEnabled = bucketData?.versioning?.Status === 'Enabled';

  // Policy mutations
  const savePolicyMutation = useMutation({
    mutationFn: (policy: string) => APIClient.putBucketPolicy(bucketName, policy, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      setIsPolicyModalOpen(false);
      loadCurrentPolicy(); // Reload policy after save
      ModalManager.toast('success', 'Bucket policy updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deletePolicyMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketPolicy(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      loadCurrentPolicy(); // Reload policy after delete
      ModalManager.toast('success', 'Bucket policy deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // CORS mutations
  const saveCORSMutation = useMutation({
    mutationFn: (cors: string) => APIClient.putBucketCORS(bucketName, cors, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      setIsCORSModalOpen(false);
      ModalManager.toast('success', 'CORS configuration updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteCORSMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketCORS(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', 'CORS configuration deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Lifecycle mutations
  const saveLifecycleMutation = useMutation({
    mutationFn: (lifecycle: string) => APIClient.putBucketLifecycle(bucketName, lifecycle, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      setIsLifecycleModalOpen(false);
      ModalManager.toast('success', 'Lifecycle rules updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteLifecycleMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketLifecycle(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', 'Lifecycle rules deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Tags mutations
  const saveTagsMutation = useMutation({
    mutationFn: (tagging: string) => APIClient.putBucketTagging(bucketName, tagging, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      setIsTagsModalOpen(false);
      ModalManager.toast('success', 'Bucket tags updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteTagsMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketTagging(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', 'Bucket tags deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Notification query
  const { data: notificationData, refetch: refetchNotifications } = useQuery({
    queryKey: ['bucket-notification', bucketName, tenantId],
    queryFn: () => APIClient.getBucketNotification(bucketName, tenantId),
    enabled: activeTab === 'notifications',
  });

  // Replication query
  const { data: replicationRules, refetch: refetchReplicationRules } = useQuery({
    queryKey: ['bucket-replication-rules', bucketName],
    queryFn: () => APIClient.listReplicationRules(bucketName),
    enabled: activeTab === 'replication',
  });

  // Notification mutations
  const saveNotificationMutation = useMutation({
    mutationFn: (config: NotificationConfiguration) =>
      APIClient.putBucketNotification(bucketName, config, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-notification', bucketName, tenantId] });
      refetchNotifications();
      ModalManager.toast('success', 'Notification configuration updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteNotificationMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketNotification(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-notification', bucketName, tenantId] });
      refetchNotifications();
      ModalManager.toast('success', 'Notification configuration deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Replication mutations
  const createReplicationRuleMutation = useMutation({
    mutationFn: (request: CreateReplicationRuleRequest) =>
      APIClient.createReplicationRule(bucketName, request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-replication-rules', bucketName] });
      refetchReplicationRules();
      setIsReplicationModalOpen(false);
      setReplicationRuleForm({
        destination_endpoint: '',
        destination_bucket: '',
        destination_access_key: '',
        destination_secret_key: '',
        destination_region: '',
        prefix: '',
        enabled: true,
        priority: 1,
        mode: 'realtime',
        schedule_interval: 60,
        conflict_resolution: 'last_write_wins',
        replicate_deletes: true,
        replicate_metadata: true,
      });
      ModalManager.toast('success', 'Replication rule created successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const updateReplicationRuleMutation = useMutation({
    mutationFn: ({ ruleId, request }: { ruleId: string; request: CreateReplicationRuleRequest }) =>
      APIClient.updateReplicationRule(bucketName, ruleId, request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-replication-rules', bucketName] });
      refetchReplicationRules();
      setIsReplicationModalOpen(false);
      setEditingReplicationRule(null);
      ModalManager.toast('success', 'Replication rule updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteReplicationRuleMutation = useMutation({
    mutationFn: (ruleId: string) => APIClient.deleteReplicationRule(bucketName, ruleId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-replication-rules', bucketName] });
      refetchReplicationRules();
      ModalManager.toast('success', 'Replication rule deleted successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const triggerReplicationSyncMutation = useMutation({
    mutationFn: (ruleId: string) => APIClient.triggerReplicationSync(bucketName, ruleId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['bucket-replication-rules', bucketName] });
      ModalManager.toast('success', `Replication sync triggered! ${data.queued_count} object(s) queued for replication.`);
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // ACL mutations
  const saveACLMutation = useMutation({
    mutationFn: (cannedACL: string) => APIClient.putBucketACL(bucketName, '', cannedACL, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      setIsACLModalOpen(false);
      loadCurrentACL(); // Reload ACL after save
      ModalManager.toast('success', 'Bucket ACL updated successfully');
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });


  // Load current ACL
  const loadCurrentACL = async () => {
    try {
      const response = await APIClient.getBucketACL(bucketName, tenantId);
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
      const response = await APIClient.getBucketPolicy(bucketName, tenantId);
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
  }, [bucketName, tenantId]);

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
    ModalManager.confirm(
      `${newState ? 'Enable' : 'Suspend'} versioning?`,
      `This will ${newState ? 'enable' : 'suspend'} object versioning for this bucket.`,
      () => toggleVersioningMutation.mutate(newState)
    );
  };

  const handleEditPolicy = async () => {
    try {
      const response = await APIClient.getBucketPolicy(bucketName, tenantId);
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
    ModalManager.confirm(
      'Delete bucket policy?',
      'This will remove all custom access policies for this bucket.',
      () => deletePolicyMutation.mutate()
    );
  };

  const handleEditCORS = async () => {
    try {
      const corsXml = await APIClient.getBucketCORS(bucketName, tenantId);
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
    ModalManager.confirm(
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
      ModalManager.toast('error', 'At least one allowed origin is required');
      return;
    }
    if (editingCorsRule.allowedMethods.length === 0) {
      ModalManager.toast('error', 'At least one allowed method is required');
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
    ModalManager.confirm(
      'Delete lifecycle rules?',
      'This will remove all lifecycle management rules for this bucket.',
      () => deleteLifecycleMutation.mutate()
    );
  };

  // Tags handlers
  const handleManageTags = async () => {
    try {
      const response = await APIClient.getBucketTagging(bucketName, tenantId);
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
    ModalManager.confirm(
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

  // Notification handlers
  const handleAddNotificationRule = () => {
    setEditingNotificationRule(null);
    setNotificationRuleForm({
      id: `rule-${Date.now()}`,
      enabled: true,
      webhookUrl: '',
      events: [],
      filterPrefix: '',
      filterSuffix: '',
      customHeaders: {},
    });
    setIsNotificationModalOpen(true);
  };

  const handleEditNotificationRule = (rule: NotificationRule) => {
    setEditingNotificationRule(rule);
    setNotificationRuleForm(rule);
    setIsNotificationModalOpen(true);
  };

  const handleDeleteNotificationRule = (ruleId: string) => {
    const currentConfig = notificationData as NotificationConfiguration | null;
    if (!currentConfig) return;

    ModalManager.confirm(
      'Delete notification rule?',
      'This will remove this notification rule from the bucket.',
      () => {
        const updatedRules = currentConfig.rules.filter((r) => r.id !== ruleId);
        const updatedConfig: NotificationConfiguration = {
          ...currentConfig,
          rules: updatedRules,
        };
        saveNotificationMutation.mutate(updatedConfig);
      }
    );
  };

  const handleToggleNotificationRule = (ruleId: string) => {
    const currentConfig = notificationData as NotificationConfiguration | null;
    if (!currentConfig) return;

    const updatedRules = currentConfig.rules.map((r) =>
      r.id === ruleId ? { ...r, enabled: !r.enabled } : r
    );
    const updatedConfig: NotificationConfiguration = {
      ...currentConfig,
      rules: updatedRules,
    };
    saveNotificationMutation.mutate(updatedConfig);
  };

  const handleSaveNotificationRule = () => {
    if (!notificationRuleForm.webhookUrl || !notificationRuleForm.events || notificationRuleForm.events.length === 0) {
      ModalManager.toast('error', 'Please provide webhook URL and select at least one event');
      return;
    }

    const currentConfig = notificationData as NotificationConfiguration | null;
    let updatedRules: NotificationRule[];

    if (editingNotificationRule) {
      // Update existing rule
      updatedRules = currentConfig
        ? currentConfig.rules.map((r) =>
            r.id === editingNotificationRule.id ? (notificationRuleForm as NotificationRule) : r
          )
        : [notificationRuleForm as NotificationRule];
    } else {
      // Add new rule
      updatedRules = currentConfig
        ? [...currentConfig.rules, notificationRuleForm as NotificationRule]
        : [notificationRuleForm as NotificationRule];
    }

    const updatedConfig: NotificationConfiguration = {
      bucketName: bucketName,
      tenantId: tenantId || '',
      rules: updatedRules,
      updatedAt: new Date().toISOString(),
      updatedBy: user?.username || '',
    };

    saveNotificationMutation.mutate(updatedConfig);
    setIsNotificationModalOpen(false);
  };

  const handleDeleteAllNotifications = () => {
    ModalManager.confirm(
      'Delete all notification rules?',
      'This will remove all notification rules from this bucket.',
      () => deleteNotificationMutation.mutate()
    );
  };

  const handleToggleEvent = (event: string) => {
    const currentEvents = notificationRuleForm.events || [];
    const updatedEvents = currentEvents.includes(event)
      ? currentEvents.filter((e) => e !== event)
      : [...currentEvents, event];
    setNotificationRuleForm({ ...notificationRuleForm, events: updatedEvents });
  };

  // Replication handlers
  const handleAddReplicationRule = () => {
    setEditingReplicationRule(null);
    setReplicationRuleForm({
      destination_endpoint: '',
      destination_bucket: '',
      destination_access_key: '',
      destination_secret_key: '',
      destination_region: '',
      prefix: '',
      enabled: true,
      priority: 1,
      mode: 'realtime',
      schedule_interval: 60,
      conflict_resolution: 'last_write_wins',
      replicate_deletes: true,
      replicate_metadata: true,
    });
    setIsReplicationModalOpen(true);
  };

  const handleEditReplicationRule = (rule: ReplicationRule) => {
    setEditingReplicationRule(rule);
    setReplicationRuleForm({
      destination_endpoint: rule.destination_endpoint,
      destination_bucket: rule.destination_bucket,
      destination_access_key: rule.destination_access_key,
      destination_secret_key: rule.destination_secret_key,
      destination_region: rule.destination_region,
      prefix: rule.prefix,
      enabled: rule.enabled,
      priority: rule.priority,
      mode: rule.mode,
      schedule_interval: rule.schedule_interval,
      conflict_resolution: rule.conflict_resolution,
      replicate_deletes: rule.replicate_deletes,
      replicate_metadata: rule.replicate_metadata,
    });
    setIsReplicationModalOpen(true);
  };

  const handleSaveReplicationRule = () => {
    if (!replicationRuleForm.destination_endpoint) {
      ModalManager.toast('error', 'Please provide a destination endpoint URL');
      return;
    }
    if (!replicationRuleForm.destination_bucket) {
      ModalManager.toast('error', 'Please provide a destination bucket');
      return;
    }
    if (!replicationRuleForm.destination_access_key || !replicationRuleForm.destination_secret_key) {
      ModalManager.toast('error', 'Please provide access key and secret key');
      return;
    }

    const request: CreateReplicationRuleRequest = {
      destination_endpoint: replicationRuleForm.destination_endpoint || '',
      destination_bucket: replicationRuleForm.destination_bucket || '',
      destination_access_key: replicationRuleForm.destination_access_key || '',
      destination_secret_key: replicationRuleForm.destination_secret_key || '',
      destination_region: replicationRuleForm.destination_region,
      prefix: replicationRuleForm.prefix,
      enabled: replicationRuleForm.enabled || true,
      priority: replicationRuleForm.priority || 1,
      mode: replicationRuleForm.mode || 'realtime',
      schedule_interval: replicationRuleForm.schedule_interval,
      conflict_resolution: replicationRuleForm.conflict_resolution || 'last_write_wins',
      replicate_deletes: replicationRuleForm.replicate_deletes !== undefined ? replicationRuleForm.replicate_deletes : true,
      replicate_metadata: replicationRuleForm.replicate_metadata !== undefined ? replicationRuleForm.replicate_metadata : true,
    };

    if (editingReplicationRule) {
      updateReplicationRuleMutation.mutate({ ruleId: editingReplicationRule.id, request });
    } else {
      createReplicationRuleMutation.mutate(request);
    }
  };

  const handleDeleteReplicationRule = (ruleId: string) => {
    ModalManager.confirm(
      'Delete replication rule?',
      'This will remove this replication rule. Objects will no longer be replicated to the destination.',
      () => deleteReplicationRuleMutation.mutate(ruleId)
    );
  };

  const handleToggleReplicationRule = (rule: ReplicationRule) => {
    const request: CreateReplicationRuleRequest = {
      destination_endpoint: rule.destination_endpoint,
      destination_bucket: rule.destination_bucket,
      destination_access_key: rule.destination_access_key,
      destination_secret_key: rule.destination_secret_key,
      destination_region: rule.destination_region,
      prefix: rule.prefix,
      enabled: !rule.enabled,
      priority: rule.priority,
      mode: rule.mode,
      schedule_interval: rule.schedule_interval,
      conflict_resolution: rule.conflict_resolution,
      replicate_deletes: rule.replicate_deletes,
      replicate_metadata: rule.replicate_metadata,
    };
    updateReplicationRuleMutation.mutate({ ruleId: rule.id, request });
  };

  const handleTriggerReplicationSync = (ruleId: string) => {
    ModalManager.confirm(
      'Trigger manual sync?',
      'This will queue all objects in the source bucket for replication to the destination.',
      () => triggerReplicationSyncMutation.mutate(ruleId)
    );
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
      ModalManager.error('Invalid JSON', 'Please enter a valid JSON policy document');
    }
  };

  if (isLoading) {
    return <Loading />;
  }

  // Get current tab info
  const currentTab = tabs.find(t => t.id === activeTab) || tabs[0];

  return (
    <div className="space-y-6 p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => navigate(bucketPath)}
            className="hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 transition-all duration-200"
          >
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <h1 className="text-3xl font-bold bg-gradient-to-r from-gray-900 via-gray-800 to-gray-900 dark:from-white dark:via-gray-100 dark:to-white bg-clip-text text-transparent">{bucketName}</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Bucket Settings</p>
          </div>
        </div>
      </div>

      {/* Tabs Container */}
      <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-md">
        <div className="p-6">
          {/* Tabs Navigation */}
          <div className="bg-gradient-to-r from-white to-gray-50 dark:from-gray-800 dark:to-gray-800/50 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm p-1 mb-6">
            <div className="flex space-x-2">
              {tabs.map((tab) => {
                const Icon = tab.icon;
                return (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={`flex-1 flex items-center justify-center space-x-2 px-4 py-3 font-medium text-sm rounded-lg transition-all duration-200 ${
                      activeTab === tab.id
                        ? 'bg-gradient-to-r from-brand-600 to-brand-700 text-white shadow-md'
                        : 'text-gray-600 dark:text-gray-400 hover:bg-gradient-to-r hover:from-brand-50 hover:to-blue-50 dark:hover:from-brand-900/30 dark:hover:to-blue-900/30 hover:text-brand-700 dark:hover:text-brand-300'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{tab.label}</span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Tab Description */}
          <div className="mb-6 pb-6 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-1">
              {currentTab.label}
            </h3>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {currentTab.description}
            </p>
          </div>

          {/* Tab Content */}
          <div className="space-y-6">
            {/* GENERAL TAB */}
            {activeTab === 'general' && (
              <>
                {/* Versioning */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Clock className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              Versioning
            </h3>
          </div>
          <div>
            <div className="p-6">
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
                  disabled={isGlobalAdminInTenantBucket || toggleVersioningMutation.isPending}
                  title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                >
                  {isVersioningEnabled ? 'Suspend' : 'Enable'}
                </Button>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Object Lock */}
        <div className="bg-gradient-to-br from-yellow-50 to-amber-50/30 dark:from-yellow-950/20 dark:to-amber-950/10 rounded-lg border border-yellow-200 dark:border-yellow-800/50 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-yellow-200 dark:border-yellow-800/50">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Lock className="h-5 w-5 text-yellow-600 dark:text-yellow-500" />
              Object Lock
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Object Lock Status</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.objectLock?.objectLockEnabled ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                {bucketData?.objectLock?.objectLockEnabled && (
                  <Button
                    variant="outline"
                    onClick={() => setIsObjectLockModalOpen(true)}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View' : 'Configure'}
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
          </div>
        </div>
        </div>
              </>
            )}

            {/* SECURITY TAB */}
            {activeTab === 'security' && (
              <>
        {/* Bucket Policy */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Shield className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              Bucket Policy
            </h3>
          </div>
          <div>
            <div className="p-6">
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
                  <Button
                    variant="outline"
                    onClick={handleEditPolicy}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View Policy' : (currentPolicy ? 'Edit Policy' : 'Add Policy')}
                  </Button>
                  {currentPolicy && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeletePolicy}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Bucket ACL */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Users className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              Access Control List (ACL)
            </h3>
          </div>
          <div>
            <div className="p-6">
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
                  <Button
                    variant="outline"
                    onClick={handleManageACL}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View ACL' : 'Manage ACL'}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Tags */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Tag className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              Tags
            </h3>
          </div>
          <div>
            <div className="p-6">
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
                  <Button
                    variant="outline"
                    onClick={handleManageTags}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View Tags' : 'Manage Tags'}
                  </Button>
                  {bucketData?.tags && Object.keys(bucketData.tags).length > 0 && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteAllTags}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                    >
                      Delete All
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* CORS */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <Globe className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              CORS Configuration
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Cross-Origin Resource Sharing</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.cors ? 'Configured' : 'Not Configured'}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleEditCORS}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View CORS' : (bucketData?.cors ? 'Edit CORS' : 'Add CORS')}
                  </Button>
                  {bucketData?.cors && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteCORS}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>
              </>
            )}

            {/* LIFECYCLE TAB */}
            {activeTab === 'lifecycle' && (
              <>
        {/* Lifecycle */}
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
              <FileText className="h-5 w-5 text-gray-600 dark:text-gray-400" />
              Lifecycle Rules
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">Object Lifecycle Management</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.lifecycle ? 'Active Rules' : 'No Rules'}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleEditLifecycle}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? 'View Rules' : (bucketData?.lifecycle ? 'Manage Rules' : 'Add Rule')}
                  </Button>
                  {bucketData?.lifecycle && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteLifecycle}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? "Global admins cannot modify tenant bucket settings" : undefined}
                    >
                      Delete
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>
              </>
            )}

            {/* NOTIFICATIONS TAB */}
            {activeTab === 'notifications' && (
              <>
                <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                    <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                      <Bell className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                      Event Notifications
                    </h3>
                  </div>
                  <div>
                    <div className="p-6">
                    <div className="space-y-4">
                      <div className="flex items-center justify-between">
                        <div>
                          <p className="font-medium">Webhook Notifications</p>
                          <p className="text-sm text-gray-500 dark:text-gray-400">
                            {notificationData?.rules?.length > 0
                              ? `${notificationData.rules.length} active rule${notificationData.rules.length > 1 ? 's' : ''}`
                              : 'No notification rules configured'}
                          </p>
                        </div>
                        <div className="flex gap-2">
                          <Button
                            onClick={handleAddNotificationRule}
                            disabled={isGlobalAdminInTenantBucket}
                            title={
                              isGlobalAdminInTenantBucket
                                ? 'Global admins cannot modify tenant bucket settings'
                                : undefined
                            }
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            Add Rule
                          </Button>
                          {notificationData?.rules?.length > 0 && (
                            <Button
                              variant="destructive"
                              size="sm"
                              onClick={handleDeleteAllNotifications}
                              disabled={isGlobalAdminInTenantBucket}
                              title={
                                isGlobalAdminInTenantBucket
                                  ? 'Global admins cannot modify tenant bucket settings'
                                  : undefined
                              }
                            >
                              Delete All
                            </Button>
                          )}
                        </div>
                      </div>

                      {/* Notification Rules List */}
                      {notificationData?.rules && notificationData.rules.length > 0 ? (
                        <div className="space-y-3">
                          {notificationData.rules.map((rule: NotificationRule) => (
                            <div
                              key={rule.id}
                              className="p-4 border border-gray-200 dark:border-gray-700 rounded-lg bg-gray-50 dark:bg-gray-800"
                            >
                              <div className="flex items-start justify-between">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-2">
                                    {rule.enabled ? (
                                      <CheckCircle className="h-5 w-5 text-green-500" />
                                    ) : (
                                      <XCircle className="h-5 w-5 text-gray-400" />
                                    )}
                                    <span className="font-medium text-gray-900 dark:text-white">
                                      {rule.id}
                                    </span>
                                    <span
                                      className={`text-xs px-2 py-1 rounded ${
                                        rule.enabled
                                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                                          : 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
                                      }`}
                                    >
                                      {rule.enabled ? 'Enabled' : 'Disabled'}
                                    </span>
                                  </div>

                                  <div className="space-y-1 text-sm">
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Webhook: </span>
                                      <span className="text-gray-900 dark:text-white font-mono text-xs">
                                        {rule.webhookUrl}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Events: </span>
                                      <span className="text-gray-900 dark:text-white">
                                        {rule.events.join(', ')}
                                      </span>
                                    </div>
                                    {(rule.filterPrefix || rule.filterSuffix) && (
                                      <div>
                                        <span className="text-gray-500 dark:text-gray-400">Filters: </span>
                                        {rule.filterPrefix && (
                                          <span className="text-gray-900 dark:text-white">
                                            Prefix: {rule.filterPrefix}
                                          </span>
                                        )}
                                        {rule.filterPrefix && rule.filterSuffix && (
                                          <span className="text-gray-500 dark:text-gray-400"> | </span>
                                        )}
                                        {rule.filterSuffix && (
                                          <span className="text-gray-900 dark:text-white">
                                            Suffix: {rule.filterSuffix}
                                          </span>
                                        )}
                                      </div>
                                    )}
                                  </div>
                                </div>

                                <div className="flex items-center gap-2 ml-4">
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleToggleNotificationRule(rule.id)}
                                    disabled={isGlobalAdminInTenantBucket}
                                    title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                                  >
                                    {rule.enabled ? 'Disable' : 'Enable'}
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleEditNotificationRule(rule)}
                                    disabled={isGlobalAdminInTenantBucket}
                                  >
                                    <Edit className="h-4 w-4" />
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="destructive"
                                    onClick={() => handleDeleteNotificationRule(rule.id)}
                                    disabled={isGlobalAdminInTenantBucket}
                                  >
                                    <Trash2 className="h-4 w-4" />
                                  </Button>
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-center py-12 border-2 border-dashed border-gray-200 dark:border-gray-700 rounded-lg">
                          <Bell className="h-12 w-12 text-gray-400 mx-auto mb-4" />
                          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">
                            No Notification Rules
                          </h3>
                          <p className="text-sm text-gray-500 dark:text-gray-400 max-w-md mx-auto mb-4">
                            Configure webhook notifications to receive real-time events when objects are created,
                            modified, or deleted in this bucket.
                          </p>
                          <Button
                            onClick={handleAddNotificationRule}
                            disabled={isGlobalAdminInTenantBucket}
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            Add First Rule
                          </Button>
                        </div>
                      )}

                      {/* Info Box */}
                      <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                        <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
                        <div className="text-sm text-blue-800 dark:text-blue-300">
                          <p className="font-medium mb-1">About Bucket Notifications</p>
                          <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                            <li>Webhook notifications are sent as HTTP POST requests</li>
                            <li>Events follow AWS S3 notification format</li>
                            <li>Failed webhooks are retried up to 3 times</li>
                            <li>Use prefix/suffix filters to limit which objects trigger notifications</li>
                          </ul>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                </div>
              </>
            )}

            {/* REPLICATION TAB */}
            {activeTab === 'replication' && (
              <>
                <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-gray-200 dark:border-gray-700">
                    <h3 className="text-lg font-semibold text-gray-900 dark:text-white flex items-center gap-2">
                      <RefreshCw className="h-5 w-5 text-gray-600 dark:text-gray-400" />
                      Bucket Replication
                    </h3>
                  </div>
                  <div>
                    <div className="p-6">
                    <div className="space-y-4">
                      <div className="flex items-center justify-between">
                        <div>
                          <p className="font-medium">Replication Rules</p>
                          <p className="text-sm text-gray-500 dark:text-gray-400">
                            {replicationRules && replicationRules.length > 0
                              ? `${replicationRules.length} rule${replicationRules.length > 1 ? 's' : ''} configured`
                              : 'No replication rules configured'}
                          </p>
                        </div>
                        <div className="flex gap-2">
                          <Button
                            onClick={handleAddReplicationRule}
                            disabled={isGlobalAdminInTenantBucket}
                            title={
                              isGlobalAdminInTenantBucket
                                ? 'Global admins cannot modify tenant bucket settings'
                                : undefined
                            }
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            Add Rule
                          </Button>
                        </div>
                      </div>

                      {/* Replication Rules List */}
                      {replicationRules && replicationRules.length > 0 ? (
                        <div className="space-y-3">
                          {replicationRules.map((rule: ReplicationRule) => (
                            <div
                              key={rule.id}
                              className="p-4 border border-gray-200 dark:border-gray-700 rounded-lg bg-gray-50 dark:bg-gray-800"
                            >
                              <div className="flex items-start justify-between">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-2">
                                    {rule.enabled ? (
                                      <CheckCircle className="h-5 w-5 text-green-500" />
                                    ) : (
                                      <XCircle className="h-5 w-5 text-gray-400" />
                                    )}
                                    <span className="font-medium text-gray-900 dark:text-white">
                                      Rule {rule.id}
                                    </span>
                                    <span
                                      className={`text-xs px-2 py-1 rounded ${
                                        rule.enabled
                                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                                          : 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
                                      }`}
                                    >
                                      {rule.enabled ? 'Enabled' : 'Disabled'}
                                    </span>
                                    <span className="text-xs px-2 py-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">
                                      {rule.mode}
                                    </span>
                                    <span className="text-xs px-2 py-1 rounded bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                                      Priority: {rule.priority}
                                    </span>
                                  </div>

                                  <div className="space-y-1 text-sm">
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Source: </span>
                                      <span className="text-gray-900 dark:text-white font-mono text-xs">
                                        {rule.source_bucket}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Destination Endpoint: </span>
                                      <span className="text-gray-900 dark:text-white font-mono text-xs">
                                        {rule.destination_endpoint}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Destination Bucket: </span>
                                      <span className="text-gray-900 dark:text-white font-mono text-xs">
                                        {rule.destination_bucket}
                                        {rule.destination_region && ` [${rule.destination_region}]`}
                                      </span>
                                    </div>
                                    {rule.schedule_interval && rule.mode === 'scheduled' && (
                                      <div>
                                        <span className="text-gray-500 dark:text-gray-400">Schedule: </span>
                                        <span className="text-gray-900 dark:text-white">
                                          Every {rule.schedule_interval} minutes
                                        </span>
                                      </div>
                                    )}
                                    {rule.prefix && (
                                      <div>
                                        <span className="text-gray-500 dark:text-gray-400">Prefix Filter: </span>
                                        <span className="text-gray-900 dark:text-white font-mono text-xs">
                                          {rule.prefix}
                                        </span>
                                      </div>
                                    )}
                                    <div>
                                      <span className="text-gray-500 dark:text-gray-400">Conflict Resolution: </span>
                                      <span className="text-gray-900 dark:text-white">
                                        {rule.conflict_resolution}
                                      </span>
                                    </div>
                                    <div className="flex gap-4">
                                      <span className={rule.replicate_deletes ? 'text-green-600 dark:text-green-400' : 'text-gray-400'}>
                                        {rule.replicate_deletes ? '✓' : '✗'} Replicate Deletes
                                      </span>
                                      <span className={rule.replicate_metadata ? 'text-green-600 dark:text-green-400' : 'text-gray-400'}>
                                        {rule.replicate_metadata ? '✓' : '✗'} Replicate Metadata
                                      </span>
                                    </div>
                                  </div>
                                </div>

                                <div className="flex items-center gap-2 ml-4">
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleToggleReplicationRule(rule)}
                                    disabled={isGlobalAdminInTenantBucket}
                                    title={rule.enabled ? 'Disable rule' : 'Enable rule'}
                                  >
                                    {rule.enabled ? 'Disable' : 'Enable'}
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleTriggerReplicationSync(rule.id)}
                                    disabled={isGlobalAdminInTenantBucket || !rule.enabled}
                                    title="Trigger manual sync"
                                  >
                                    <RefreshCw className="h-4 w-4 mr-1" />
                                    Sync Now
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleEditReplicationRule(rule)}
                                    disabled={isGlobalAdminInTenantBucket}
                                  >
                                    <Edit className="h-4 w-4" />
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="destructive"
                                    onClick={() => handleDeleteReplicationRule(rule.id)}
                                    disabled={isGlobalAdminInTenantBucket}
                                  >
                                    <Trash2 className="h-4 w-4" />
                                  </Button>
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-center py-12 border-2 border-dashed border-gray-200 dark:border-gray-700 rounded-lg">
                          <RefreshCw className="h-12 w-12 text-gray-400 mx-auto mb-4" />
                          <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-2">
                            No Replication Rules
                          </h3>
                          <p className="text-sm text-gray-500 dark:text-gray-400 max-w-md mx-auto mb-4">
                            Configure replication rules to automatically copy objects to another bucket.
                            Support for cross-bucket, cross-region, and cross-tenant replication.
                          </p>
                          <Button
                            onClick={handleAddReplicationRule}
                            disabled={isGlobalAdminInTenantBucket}
                          >
                            <Plus className="h-4 w-4 mr-2" />
                            Add First Rule
                          </Button>
                        </div>
                      )}

                      {/* Info Box */}
                      <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                        <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
                        <div className="text-sm text-blue-800 dark:text-blue-300">
                          <p className="font-medium mb-1">About Bucket Replication</p>
                          <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                            <li>Realtime mode replicates objects immediately after upload</li>
                            <li>Scheduled mode processes replication in batches at intervals</li>
                            <li>Batch mode processes large volumes of objects efficiently</li>
                            <li>Use prefix filters to replicate only specific object paths</li>
                            <li>Higher priority rules are processed first</li>
                          </ul>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                </div>
              </>
            )}
          </div>
        </div>
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
              disabled={isGlobalAdminInTenantBucket || savePolicyMutation.isPending || !policyText.trim()}
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
                  disabled={isGlobalAdminInTenantBucket || saveCORSMutation.isPending || corsRules.length === 0}
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
                  disabled={isGlobalAdminInTenantBucket || saveCORSMutation.isPending || !corsText.trim()}
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
              disabled={isGlobalAdminInTenantBucket || saveLifecycleMutation.isPending}
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
              disabled={isGlobalAdminInTenantBucket || saveTagsMutation.isPending}
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
              disabled={isGlobalAdminInTenantBucket || saveACLMutation.isPending}
            >
              {saveACLMutation.isPending ? 'Saving...' : 'Save ACL'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Notification Rule Modal */}
      <Modal
        isOpen={isNotificationModalOpen}
        onClose={() => setIsNotificationModalOpen(false)}
        title={editingNotificationRule ? 'Edit Notification Rule' : 'Add Notification Rule'}
        size="xl"
      >
        <div className="space-y-4">
          {/* Rule ID */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Rule ID
            </label>
            <input
              type="text"
              value={notificationRuleForm.id || ''}
              onChange={(e) =>
                setNotificationRuleForm({ ...notificationRuleForm, id: e.target.value })
              }
              placeholder="rule-1"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              disabled={!!editingNotificationRule}
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Unique identifier for this notification rule
            </p>
          </div>

          {/* Webhook URL */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Webhook URL <span className="text-red-500">*</span>
            </label>
            <input
              type="url"
              value={notificationRuleForm.webhookUrl || ''}
              onChange={(e) =>
                setNotificationRuleForm({ ...notificationRuleForm, webhookUrl: e.target.value })
              }
              placeholder="https://example.com/webhook"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              HTTP or HTTPS endpoint that will receive event notifications
            </p>
          </div>

          {/* Event Types */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Event Types <span className="text-red-500">*</span>
            </label>
            <div className="grid grid-cols-2 gap-2">
              {[
                { value: 's3:ObjectCreated:*', label: 'All Object Created Events' },
                { value: 's3:ObjectCreated:Put', label: 'Object Created (Put)' },
                { value: 's3:ObjectCreated:Post', label: 'Object Created (Post)' },
                { value: 's3:ObjectCreated:Copy', label: 'Object Created (Copy)' },
                { value: 's3:ObjectCreated:CompleteMultipartUpload', label: 'Multipart Upload Complete' },
                { value: 's3:ObjectRemoved:*', label: 'All Object Removed Events' },
                { value: 's3:ObjectRemoved:Delete', label: 'Object Deleted' },
                { value: 's3:ObjectRestored:Post', label: 'Object Restored' },
              ].map((eventType) => (
                <label
                  key={eventType.value}
                  className="flex items-start gap-2 p-2 border border-gray-200 dark:border-gray-700 rounded cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-800"
                >
                  <input
                    type="checkbox"
                    checked={notificationRuleForm.events?.includes(eventType.value) || false}
                    onChange={() => handleToggleEvent(eventType.value)}
                    className="mt-1"
                  />
                  <div className="text-sm">
                    <div className="font-medium text-gray-900 dark:text-white">
                      {eventType.label}
                    </div>
                    <div className="text-xs text-gray-500 dark:text-gray-400 font-mono">
                      {eventType.value}
                    </div>
                  </div>
                </label>
              ))}
            </div>
          </div>

          {/* Filters */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Prefix Filter (Optional)
              </label>
              <input
                type="text"
                value={notificationRuleForm.filterPrefix || ''}
                onChange={(e) =>
                  setNotificationRuleForm({ ...notificationRuleForm, filterPrefix: e.target.value })
                }
                placeholder="images/"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Only trigger for objects with this prefix
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Suffix Filter (Optional)
              </label>
              <input
                type="text"
                value={notificationRuleForm.filterSuffix || ''}
                onChange={(e) =>
                  setNotificationRuleForm({ ...notificationRuleForm, filterSuffix: e.target.value })
                }
                placeholder=".jpg"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Only trigger for objects with this suffix
              </p>
            </div>
          </div>

          {/* Enabled Toggle */}
          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="notificationEnabled"
              checked={notificationRuleForm.enabled || false}
              onChange={(e) =>
                setNotificationRuleForm({ ...notificationRuleForm, enabled: e.target.checked })
              }
              className="h-4 w-4"
            />
            <label htmlFor="notificationEnabled" className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Enable this rule
            </label>
          </div>

          {/* Info */}
          <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
            <div className="text-sm text-blue-800 dark:text-blue-300">
              <p className="font-medium mb-1">Webhook Format</p>
              <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                <li>Events are sent as HTTP POST requests with JSON payload</li>
                <li>Payload follows AWS S3 event notification format</li>
                <li>Failed deliveries are retried up to 3 times</li>
                <li>Timeout is set to 10 seconds per request</li>
              </ul>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="outline" onClick={() => setIsNotificationModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveNotificationRule}
              disabled={
                isGlobalAdminInTenantBucket ||
                saveNotificationMutation.isPending ||
                !notificationRuleForm.webhookUrl ||
                !notificationRuleForm.events ||
                notificationRuleForm.events.length === 0
              }
            >
              {saveNotificationMutation.isPending
                ? 'Saving...'
                : editingNotificationRule
                ? 'Update Rule'
                : 'Add Rule'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Replication Rule Modal */}
      <Modal
        isOpen={isReplicationModalOpen}
        onClose={() => setIsReplicationModalOpen(false)}
        title={editingReplicationRule ? 'Edit Replication Rule' : 'Add Replication Rule'}
        size="xl"
      >
        <div className="space-y-4">
          {/* Destination S3 Endpoint */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Destination S3 Endpoint <span className="text-red-500">*</span>
            </label>
            <input
              type="url"
              value={replicationRuleForm.destination_endpoint || ''}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, destination_endpoint: e.target.value })
              }
              placeholder="https://s3.amazonaws.com or http://localhost:8080"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              S3-compatible endpoint URL (AWS S3, MinIO, or another MaxIOFS instance)
            </p>
          </div>

          {/* Destination Bucket */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Destination Bucket <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={replicationRuleForm.destination_bucket || ''}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, destination_bucket: e.target.value })
              }
              placeholder="my-destination-bucket"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              The destination bucket name where objects will be replicated
            </p>
          </div>

          {/* Access Credentials */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Access Key <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={replicationRuleForm.destination_access_key || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_access_key: e.target.value })
                }
                placeholder="AKIAIOSFODNN7EXAMPLE"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                S3 access key for destination
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Secret Key <span className="text-red-500">*</span>
              </label>
              <input
                type="password"
                value={replicationRuleForm.destination_secret_key || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_secret_key: e.target.value })
                }
                placeholder="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                S3 secret key for destination
              </p>
            </div>
          </div>

          {/* Region and Prefix */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Destination Region (Optional)
              </label>
              <input
                type="text"
                value={replicationRuleForm.destination_region || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_region: e.target.value })
                }
                placeholder="us-east-1"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                S3 region (e.g., us-east-1, eu-west-1)
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Prefix Filter (Optional)
              </label>
              <input
                type="text"
                value={replicationRuleForm.prefix || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, prefix: e.target.value })
                }
                placeholder="images/"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm font-mono"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Only replicate objects with this prefix
              </p>
            </div>
          </div>

          {/* Mode, Schedule and Priority */}
          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Replication Mode <span className="text-red-500">*</span>
              </label>
              <select
                value={replicationRuleForm.mode || 'realtime'}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, mode: e.target.value as 'realtime' | 'scheduled' | 'batch' })
                }
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              >
                <option value="realtime">Realtime</option>
                <option value="scheduled">Scheduled</option>
                <option value="batch">Batch</option>
              </select>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Replication frequency
              </p>
            </div>

            {replicationRuleForm.mode === 'scheduled' && (
              <div className="space-y-2">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                  Interval (minutes) <span className="text-red-500">*</span>
                </label>
                <input
                  type="number"
                  min="1"
                  value={replicationRuleForm.schedule_interval || 60}
                  onChange={(e) =>
                    setReplicationRuleForm({ ...replicationRuleForm, schedule_interval: parseInt(e.target.value) || 60 })
                  }
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
                />
                <p className="text-xs text-gray-500 dark:text-gray-400">
                  Run every N minutes
                </p>
              </div>
            )}

            <div className="space-y-2">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
                Priority
              </label>
              <input
                type="number"
                min="1"
                max="100"
                value={replicationRuleForm.priority || 1}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, priority: parseInt(e.target.value) || 1 })
                }
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
              />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Higher priority rules are processed first
              </p>
            </div>
          </div>

          {/* Conflict Resolution */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Conflict Resolution <span className="text-red-500">*</span>
            </label>
            <select
              value={replicationRuleForm.conflict_resolution || 'last_write_wins'}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, conflict_resolution: e.target.value as 'last_write_wins' | 'version_based' | 'primary_wins' })
              }
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-white rounded-md text-sm"
            >
              <option value="last_write_wins">Last Write Wins - Most recent version prevails</option>
              <option value="version_based">Version Based - Use version numbers</option>
              <option value="primary_wins">Primary Wins - Source always prevails</option>
            </select>
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Strategy for resolving conflicts when objects exist in both locations
            </p>
          </div>

          {/* Options */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300">
              Replication Options
            </label>
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="replicateDeletes"
                  checked={replicationRuleForm.replicate_deletes !== undefined ? replicationRuleForm.replicate_deletes : true}
                  onChange={(e) =>
                    setReplicationRuleForm({ ...replicationRuleForm, replicate_deletes: e.target.checked })
                  }
                  className="h-4 w-4"
                />
                <label htmlFor="replicateDeletes" className="text-sm text-gray-700 dark:text-gray-300">
                  Replicate delete operations to destination
                </label>
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="replicateMetadata"
                  checked={replicationRuleForm.replicate_metadata !== undefined ? replicationRuleForm.replicate_metadata : true}
                  onChange={(e) =>
                    setReplicationRuleForm({ ...replicationRuleForm, replicate_metadata: e.target.checked })
                  }
                  className="h-4 w-4"
                />
                <label htmlFor="replicateMetadata" className="text-sm text-gray-700 dark:text-gray-300">
                  Replicate object metadata (tags, ACLs, etc.)
                </label>
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="enabled"
                  checked={replicationRuleForm.enabled || false}
                  onChange={(e) =>
                    setReplicationRuleForm({ ...replicationRuleForm, enabled: e.target.checked })
                  }
                  className="h-4 w-4"
                />
                <label htmlFor="enabled" className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Enable this replication rule
                </label>
              </div>
            </div>
          </div>

          {/* Info */}
          <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
            <div className="text-sm text-blue-800 dark:text-blue-300">
              <p className="font-medium mb-1">Replication Requirements</p>
              <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                <li>Destination bucket must exist and be accessible</li>
                <li>Versioning is recommended for both source and destination</li>
                <li>Ensure appropriate permissions for cross-tenant replication</li>
                <li>Rules can be temporarily disabled without deleting them</li>
              </ul>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-gray-200 dark:border-gray-700">
            <Button variant="outline" onClick={() => setIsReplicationModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveReplicationRule}
              disabled={
                isGlobalAdminInTenantBucket ||
                createReplicationRuleMutation.isPending ||
                updateReplicationRuleMutation.isPending ||
                !replicationRuleForm.destination_bucket
              }
            >
              {createReplicationRuleMutation.isPending || updateReplicationRuleMutation.isPending
                ? 'Saving...'
                : editingReplicationRule
                ? 'Update Rule'
                : 'Add Rule'}
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
