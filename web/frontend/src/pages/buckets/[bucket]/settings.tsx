import React, { useState, useEffect } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
  AlertTriangle,
  CheckCircle,
  XCircle,
  RefreshCw,
  Package,
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import ModalManager from '@/lib/modals';
import { getErrorStatus } from '@/lib/utils';
import { useAuth } from '@/hooks/useAuth';
import type { NotificationConfiguration, NotificationRule, ReplicationRule, CreateReplicationRuleRequest } from '@/types';

// Tab types
type TabId = 'general' | 'security' | 'lifecycle' | 'notifications' | 'replication' | 'inventory' | 'website';

interface TabInfo {
  id: TabId;
  label: string;
  icon: React.ComponentType<any>;
  description: string;
}

export default function BucketSettingsPage() {
  const { t } = useTranslation('bucketSettings');
  const { bucket } = useParams<{ bucket: string }>();
  const location = useLocation();
  const tenantId = (location.state as any)?.tenantId || undefined;
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuth();

  const tabs: TabInfo[] = [
    {
      id: 'general',
      label: t('tabs.general.label'),
      icon: Settings,
      description: t('tabs.general.description'),
    },
    {
      id: 'security',
      label: t('tabs.security.label'),
      icon: Shield,
      description: t('tabs.security.description'),
    },
    {
      id: 'lifecycle',
      label: t('tabs.lifecycle.label'),
      icon: Clock,
      description: t('tabs.lifecycle.description'),
    },
    {
      id: 'notifications',
      label: t('tabs.notifications.label'),
      icon: Bell,
      description: t('tabs.notifications.description'),
    },
    {
      id: 'replication',
      label: t('tabs.replication.label'),
      icon: RefreshCw,
      description: t('tabs.replication.description'),
    },
    {
      id: 'inventory',
      label: t('tabs.inventory.label'),
      icon: Package,
      description: t('tabs.inventory.description'),
    },
    {
      id: 'website',
      label: t('tabs.website.label'),
      icon: Globe,
      description: t('tabs.website.description'),
    },
  ];
  const bucketName = bucket as string;
  const bucketPath = `/buckets/${bucketName}`;

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

  // Inventory state — local edits only; form is derived from server data
  const [localInventoryEdits, setLocalInventoryEdits] = useState<any>(null);

  const { data: bucketData, isLoading } = useQuery({
    queryKey: ['bucket', bucketName, tenantId],
    queryFn: () => APIClient.getBucket(bucketName, tenantId),
  });

  // Versioning mutation
  const toggleVersioningMutation = useMutation({
    mutationFn: (enabled: boolean) => APIClient.putBucketVersioning(bucketName, enabled, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', t('versioning.updatedSuccess'));
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
      ModalManager.toast('success', t('policy.savedSuccess'));
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
      ModalManager.toast('success', t('policy.deletedSuccess'));
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
      ModalManager.toast('success', t('cors.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteCORSMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketCORS(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', t('cors.deletedSuccess'));
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
      ModalManager.toast('success', t('lifecycle.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteLifecycleMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketLifecycle(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', t('lifecycle.deletedSuccess'));
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
      ModalManager.toast('success', t('tags.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteTagsMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketTagging(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket', bucketName, tenantId] });
      ModalManager.toast('success', t('tags.deletedSuccess'));
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
      ModalManager.toast('success', t('notifications.savedSuccess'));
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
      ModalManager.toast('success', t('notifications.deletedSuccess'));
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
      ModalManager.toast('success', t('replication.createdSuccess'));
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
      ModalManager.toast('success', t('replication.updatedSuccess'));
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
      ModalManager.toast('success', t('replication.deletedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const triggerReplicationSyncMutation = useMutation({
    mutationFn: (ruleId: string) => APIClient.triggerReplicationSync(bucketName, ruleId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['bucket-replication-rules', bucketName] });
      ModalManager.toast('success', t('replication.syncTriggered', { count: data.queued_count }));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Inventory query — only fetch when the inventory tab is active
  const { data: inventoryConfig, refetch: refetchInventory } = useQuery({
    queryKey: ['bucket-inventory', bucketName, tenantId],
    queryFn: () => APIClient.getBucketInventory(bucketName, tenantId),
    retry: false,
    staleTime: 0,
    refetchOnMount: 'always',
    enabled: activeTab === 'inventory',
  });

  const { data: inventoryReports } = useQuery({
    queryKey: ['bucket-inventory-reports', bucketName, tenantId],
    queryFn: () => APIClient.listBucketInventoryReports(bucketName, 50, 0, tenantId),
    enabled: activeTab === 'inventory',
  });

  // Inventory mutations
  const saveInventoryMutation = useMutation({
    mutationFn: (config: any) => APIClient.putBucketInventory(bucketName, config, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-inventory', bucketName, tenantId] });
      refetchInventory();
      ModalManager.toast('success', t('inventory.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteInventoryMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketInventory(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-inventory', bucketName, tenantId] });
      refetchInventory();
      ModalManager.toast('success', t('inventory.deletedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  // Website state
  const [websiteEnabled, setWebsiteEnabled] = useState<boolean>(false);
  const [websiteIndexDoc, setWebsiteIndexDoc] = useState<string>('index.html');
  const [websiteErrorDoc, setWebsiteErrorDoc] = useState<string>('');

  // Server config (para saber si website_hostname está configurado)
  const { data: serverConfig } = useQuery({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
    enabled: activeTab === 'website',
  });
  const websiteHostnameConfigured = !!serverConfig?.server?.websiteHostname;
  const websiteHostname = serverConfig?.server?.websiteHostname ?? '';

  // Website query
  const { data: websiteConfig, refetch: refetchWebsite } = useQuery({
    queryKey: ['bucket-website', bucketName, tenantId],
    queryFn: () => APIClient.getBucketWebsite(bucketName, tenantId),
    enabled: activeTab === 'website',
  });

  // Sync website form when query loads
  useEffect(() => {
    if (websiteConfig) {
      setWebsiteEnabled(true);
      setWebsiteIndexDoc(websiteConfig.indexDocument || 'index.html');
      setWebsiteErrorDoc(websiteConfig.errorDocument || '');
    } else if (websiteConfig === null) {
      setWebsiteEnabled(false);
      setWebsiteIndexDoc('index.html');
      setWebsiteErrorDoc('');
    }
  }, [websiteConfig]);

  // Website mutations
  const saveWebsiteMutation = useMutation({
    mutationFn: (config: { indexDocument: string; errorDocument?: string }) =>
      APIClient.putBucketWebsite(bucketName, config, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-website', bucketName, tenantId] });
      refetchWebsite();
      ModalManager.toast('success', t('website.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });

  const deleteWebsiteMutation = useMutation({
    mutationFn: () => APIClient.deleteBucketWebsite(bucketName, tenantId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-website', bucketName, tenantId] });
      refetchWebsite();
      ModalManager.toast('success', t('website.deletedSuccess'));
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
      ModalManager.toast('success', t('acl.savedSuccess'));
    },
    onError: (error: Error) => {
      ModalManager.apiError(error);
    },
  });


  // Load current ACL
  const loadCurrentACL = async () => {
    try {
      const response = await APIClient.getBucketACL(bucketName, tenantId);

      // First, check if the backend sent the canned_acl field directly
      const acl = response.data || response;

      if (acl.canned_acl || acl.CannedACL) {
        // Backend provided the canned ACL directly - use it!
        const cannedACL = acl.canned_acl || acl.CannedACL;
        setCurrentACL(cannedACL);
      } else {
        // Fallback: detect from grants
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

      const policy = response.data || response;

      if (policy && policy.Statement) {
        setCurrentPolicy(policy);
        setPolicyStatementCount(policy.Statement.length);
      } else {
        setCurrentPolicy(null);
        setPolicyStatementCount(0);
      }
    } catch (error: unknown) {
      // Policy not found or error - this is normal if no policy is set
      setCurrentPolicy(null);
      setPolicyStatementCount(0);
    }
  };


  // Load current ACL and Policy on component mount
  useEffect(() => {
    loadCurrentACL();
    loadCurrentPolicy();
  }, [bucketName, tenantId]);

  // Refetch inventory config every time the user opens the inventory tab
  useEffect(() => {
    if (activeTab === 'inventory') {
      refetchInventory();
    }
  }, [activeTab]);

  // When server data changes (save/delete/refetch), discard local edits so the form
  // reflects the fresh server state immediately on the next render.
  useEffect(() => {
    setLocalInventoryEdits(null);
  }, [inventoryConfig]);

  // Derived form: prefer local edits (user is mid-edit), otherwise derive from server data.
  // This eliminates the useState+useEffect timing issue: the form always reflects the
  // current inventoryConfig on the SAME render in which the query resolves.
  const inventoryFormDefaults = {
    enabled: false,
    frequency: 'daily',
    format: 'csv',
    destination_bucket: '',
    destination_prefix: '',
    included_fields: ['bucket_name', 'object_key', 'size', 'last_modified', 'etag'],
    schedule_time: '00:00',
  };
  const inventoryForm = localInventoryEdits ?? (inventoryConfig
    ? {
        enabled: inventoryConfig.enabled,
        frequency: inventoryConfig.frequency || 'daily',
        format: inventoryConfig.format || 'csv',
        destination_bucket: inventoryConfig.destination_bucket || '',
        destination_prefix: inventoryConfig.destination_prefix || '',
        included_fields: inventoryConfig.included_fields || inventoryFormDefaults.included_fields,
        schedule_time: inventoryConfig.schedule_time || '00:00',
      }
    : inventoryFormDefaults);
  const setInventoryForm = (newForm: any) => setLocalInventoryEdits(newForm);

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
  const isObjectLockEnabled = bucketData?.objectLock?.objectLockEnabled === true;

  const handleToggleVersioning = () => {
    const newState = !isVersioningEnabled;
    ModalManager.confirm(
      newState ? t('versioning.confirmEnableTitle') : t('versioning.confirmSuspendTitle'),
      newState ? t('versioning.confirmEnableMsg') : t('versioning.confirmSuspendMsg'),
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
      t('policy.confirmDeleteTitle'),
      t('policy.confirmDeleteMsg'),
      () => deletePolicyMutation.mutate()
    );
  };

  const handleEditCORS = async () => {
    try {
      const corsXml = await APIClient.getBucketCORS(bucketName, tenantId);
      const xmlStr = typeof corsXml === 'string' ? corsXml : '';
      setCorsText(xmlStr);

      // Parse XML to extract CORS rules
      const parser = new DOMParser();
      const xmlDoc = parser.parseFromString(xmlStr, 'text/xml');
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
      t('cors.confirmDeleteTitle'),
      t('cors.confirmDeleteMsg'),
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
      ModalManager.toast('error', t('cors.originRequired'));
      return;
    }
    if (editingCorsRule.allowedMethods.length === 0) {
      ModalManager.toast('error', t('cors.methodRequired'));
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
      t('lifecycle.confirmDeleteTitle'),
      t('lifecycle.confirmDeleteMsg'),
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
      t('tags.confirmDeleteAllTitle'),
      t('tags.confirmDeleteAllMsg'),
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
      t('notifications.confirmDeleteRuleTitle'),
      t('notifications.confirmDeleteRuleMsg'),
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
      ModalManager.toast('error', t('notifications.validationError'));
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
      t('notifications.confirmDeleteAllTitle'),
      t('notifications.confirmDeleteAllMsg'),
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
      ModalManager.toast('error', t('replication.validationEndpoint'));
      return;
    }
    if (!replicationRuleForm.destination_bucket) {
      ModalManager.toast('error', t('replication.validationBucket'));
      return;
    }
    if (!replicationRuleForm.destination_access_key || !replicationRuleForm.destination_secret_key) {
      ModalManager.toast('error', t('replication.validationCredentials'));
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
      t('replication.confirmDeleteTitle'),
      t('replication.confirmDeleteMsg'),
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
      t('replication.confirmTriggerTitle'),
      t('replication.confirmTriggerMsg'),
      () => triggerReplicationSyncMutation.mutate(ruleId)
    );
  };

  // Policy Templates
  const policyTemplates = {
    publicRead: {
      name: t('policy.templates.publicRead.name'),
      description: t('policy.templates.publicRead.description'),
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
      name: t('policy.templates.publicReadWrite.name'),
      description: t('policy.templates.publicReadWrite.description'),
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
      name: t('policy.templates.listOnly.name'),
      description: t('policy.templates.listOnly.description'),
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
      name: t('policy.templates.fullPublic.name'),
      description: t('policy.templates.fullPublic.description'),
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
      ModalManager.error(t('policy.invalidJson'), t('policy.invalidJsonMsg'));
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
            className="hover:bg-secondary transition-all duration-200"
          >
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <h1 className="text-2xl font-bold text-foreground">{bucketName}</h1>
            <p className="text-sm text-muted-foreground mt-1">{t('pageSubtitle')}</p>
          </div>
        </div>
      </div>

      {/* Tabs Container */}
      <div className="bg-card rounded-xl border border-border shadow-md">
        <div className="p-6">
          {/* Tabs Navigation */}
          <div className="bg-gradient-to-r from-white to-gray-50 dark:from-gray-800 dark:to-gray-800/50 rounded-xl border border-border shadow-sm p-1 mb-6">
            <div className="flex space-x-2">
              {tabs.map((tab) => {
                const Icon = tab.icon;
                return (
                  <button
                    key={tab.id}
                    onClick={() => setActiveTab(tab.id)}
                    className={`flex-1 flex items-center justify-center space-x-2 px-4 py-3 font-medium text-sm rounded-lg transition-all duration-200 ${
                      activeTab === tab.id
                        ? 'bg-brand-600 text-white'
                        : 'text-muted-foreground hover:bg-secondary hover:text-brand-700 dark:hover:text-brand-300'
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
          <div className="mb-6 pb-6 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground mb-1">
              {currentTab.label}
            </h3>
            <p className="text-sm text-muted-foreground">
              {currentTab.description}
            </p>
          </div>

          {/* Tab Content */}
          <div className="space-y-6">
            {/* GENERAL TAB */}
            {activeTab === 'general' && (
              <>
                {/* Versioning */}
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Clock className="h-5 w-5 text-muted-foreground" />
              {t('versioning.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{t('versioning.versionControl')}</p>
                  <p className="text-sm text-gray-500">
                    {isVersioningEnabled ? t('versioning.enabled') : t('versioning.disabled')}
                  </p>
                </div>
                <Button
                  variant="outline"
                  onClick={handleToggleVersioning}
                  disabled={isGlobalAdminInTenantBucket || toggleVersioningMutation.isPending || (isObjectLockEnabled && isVersioningEnabled)}
                  title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : (isObjectLockEnabled && isVersioningEnabled) ? t('versioning.wormLocked') : undefined}
                >
                  {isVersioningEnabled ? t('versioning.suspend') : t('versioning.enable')}
                </Button>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Object Lock */}
        <div className="bg-gradient-to-br from-yellow-50 to-amber-50/30 dark:from-yellow-950/20 dark:to-amber-950/10 rounded-lg border border-yellow-200 dark:border-yellow-800/50 shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-yellow-200 dark:border-yellow-800/50">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Lock className="h-5 w-5 text-yellow-600 dark:text-yellow-500" />
              {t('objectLock.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{t('objectLock.status')}</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.objectLock?.objectLockEnabled ? t('objectLock.enabled') : t('objectLock.disabled')}
                  </p>
                </div>
                {bucketData?.objectLock?.objectLockEnabled && (
                  <Button
                    variant="outline"
                    onClick={() => setIsObjectLockModalOpen(true)}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('objectLock.view') : t('objectLock.configure')}
                  </Button>
                )}
              </div>
              {bucketData?.objectLock?.objectLockEnabled && bucketData?.objectLock?.rule && (
                <div className="rounded-lg border p-4 space-y-2">
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">{t('objectLock.mode')}</span>
                    <span className="text-sm font-medium">
                      {bucketData.objectLock.rule.defaultRetention?.mode || t('objectLock.notSet')}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-sm text-gray-600">{t('objectLock.retention')}</span>
                    <span className="text-sm font-medium">
                      {bucketData.objectLock.rule.defaultRetention?.days
                        ? t('objectLock.days', { days: bucketData.objectLock.rule.defaultRetention.days })
                        : bucketData.objectLock.rule.defaultRetention?.years
                        ? t('objectLock.years', { years: bucketData.objectLock.rule.defaultRetention.years })
                        : t('objectLock.notSet')}
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
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Shield className="h-5 w-5 text-muted-foreground" />
              {t('policy.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex-1">
                  <p className="font-medium">{t('policy.sectionTitle')}</p>
                  <p className="text-sm text-gray-500">
                    {t('policy.description')}
                  </p>
                  <div className="mt-2">
                    {currentPolicy ? (
                      <div className="flex items-center gap-2">
                        <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200">
                          {t('policy.policyActive')}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          {t('policy.statements', { count: policyStatementCount })}
                        </span>
                      </div>
                    ) : (
                      <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-secondary text-muted-foreground">
                        {t('policy.noPolicySet')}
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleEditPolicy}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('policy.viewPolicy') : (currentPolicy ? t('policy.editPolicy') : t('policy.addPolicy'))}
                  </Button>
                  {currentPolicy && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeletePolicy}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                    >
                      {t('policy.delete')}
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Bucket ACL */}
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Users className="h-5 w-5 text-muted-foreground" />
              {t('acl.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex-1">
                  <p className="font-medium">{t('acl.sectionTitle')}</p>
                  <p className="text-sm text-gray-500">
                    {t('acl.description')}
                  </p>
                  <div className="mt-2">
                    <span className="text-xs font-medium text-muted-foreground">{t('acl.currentAcl')}</span>
                    <span className={`inline-flex items-center px-2 py-1 rounded text-xs font-medium ${
                      currentACL === 'private' ? 'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200' :
                      currentACL === 'public-read' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200' :
                      currentACL === 'public-read-write' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200' :
                      currentACL === 'authenticated-read' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' :
                      'bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200'
                    }`}>
                      {currentACL === 'private' && `🔒 ${t('acl.private')}`}
                      {currentACL === 'public-read' && `👁️ ${t('acl.publicRead')}`}
                      {currentACL === 'public-read-write' && `⚠️ ${t('acl.publicReadWrite')}`}
                      {currentACL === 'authenticated-read' && `🔐 ${t('acl.authenticatedRead')}`}
                    </span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleManageACL}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('acl.viewAcl') : t('acl.manageAcl')}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* Tags */}
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Tag className="h-5 w-5 text-muted-foreground" />
              {t('tags.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{t('tags.sectionTitle')}</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.tags && Object.keys(bucketData.tags).length > 0
                      ? t('tags.tagCount', { count: Object.keys(bucketData.tags).length })
                      : t('tags.noTags')}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleManageTags}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('tags.viewTags') : t('tags.manageTags')}
                  </Button>
                  {bucketData?.tags && Object.keys(bucketData.tags).length > 0 && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteAllTags}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                    >
                      {t('tags.deleteAll')}
                    </Button>
                  )}
                </div>
              </div>
            </div>
          </div>
        </div>
        </div>

        {/* CORS */}
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <Globe className="h-5 w-5 text-muted-foreground" />
              {t('cors.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{t('cors.sectionTitle')}</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.cors ? t('cors.configured') : t('cors.notConfigured')}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleEditCORS}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('cors.viewCors') : (bucketData?.cors ? t('cors.editCors') : t('cors.addCors'))}
                  </Button>
                  {bucketData?.cors && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteCORS}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                    >
                      {t('cors.delete')}
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
        <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
          <div className="px-6 py-4 border-b border-border">
            <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
              <FileText className="h-5 w-5 text-muted-foreground" />
              {t('lifecycle.title')}
            </h3>
          </div>
          <div>
            <div className="p-6">
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-medium">{t('lifecycle.sectionTitle')}</p>
                  <p className="text-sm text-gray-500">
                    {bucketData?.lifecycle ? t('lifecycle.activeRules') : t('lifecycle.noRules')}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    onClick={handleEditLifecycle}
                    disabled={isGlobalAdminInTenantBucket}
                    title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                  >
                    {isGlobalAdminInTenantBucket ? t('lifecycle.viewRules') : (bucketData?.lifecycle ? t('lifecycle.manageRules') : t('lifecycle.addRule'))}
                  </Button>
                  {bucketData?.lifecycle && (
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={handleDeleteLifecycle}
                      disabled={isGlobalAdminInTenantBucket}
                      title={isGlobalAdminInTenantBucket ? t('globalAdminReadOnly') : undefined}
                    >
                      {t('lifecycle.delete')}
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
                <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-border">
                    <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                      <Bell className="h-5 w-5 text-muted-foreground" />
                      {t('notifications.title')}
                    </h3>
                  </div>
                  <div>
                    <div className="p-6">
                    <div className="space-y-4">
                      <div className="flex items-center justify-between">
                        <div>
                          <p className="font-medium">{t('notifications.sectionTitle')}</p>
                          <p className="text-sm text-muted-foreground">
                            {notificationData?.rules?.length > 0
                              ? t('notifications.rulesCount', { count: notificationData.rules.length })
                              : t('notifications.noRulesConfigured')}
                          </p>
                        </div>
                        <div className="flex gap-2">
                          <Button
                            onClick={handleAddNotificationRule}
                            disabled={isGlobalAdminInTenantBucket}
                            title={
                              isGlobalAdminInTenantBucket
                                ? t('globalAdminReadOnly')
                                : undefined
                            }
                          >
                            <Plus className="h-4 w-4" />
                            {t('notifications.addRule')}
                          </Button>
                          {notificationData?.rules?.length > 0 && (
                            <Button
                              variant="destructive"
                              size="sm"
                              onClick={handleDeleteAllNotifications}
                              disabled={isGlobalAdminInTenantBucket}
                              title={
                                isGlobalAdminInTenantBucket
                                  ? t('globalAdminReadOnly')
                                  : undefined
                              }
                            >
                              {t('notifications.deleteAll')}
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
                              className="p-4 border border-border rounded-lg bg-gray-50 dark:bg-gray-800"
                            >
                              <div className="flex items-start justify-between">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-2">
                                    {rule.enabled ? (
                                      <CheckCircle className="h-5 w-5 text-green-500" />
                                    ) : (
                                      <XCircle className="h-5 w-5 text-gray-400" />
                                    )}
                                    <span className="font-medium text-foreground">
                                      {rule.id}
                                    </span>
                                    <span
                                      className={`text-xs px-2 py-1 rounded ${
                                        rule.enabled
                                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                                          : 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
                                      }`}
                                    >
                                      {rule.enabled ? t('notifications.enabled') : t('notifications.disabled')}
                                    </span>
                                  </div>

                                  <div className="space-y-1 text-sm">
                                    <div>
                                      <span className="text-muted-foreground">{t('notifications.webhook')}</span>
                                      <span className="text-foreground font-mono text-xs">
                                        {rule.webhookUrl}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-muted-foreground">{t('notifications.eventsLabel')}</span>
                                      <span className="text-foreground">
                                        {rule.events.join(', ')}
                                      </span>
                                    </div>
                                    {(rule.filterPrefix || rule.filterSuffix) && (
                                      <div>
                                        <span className="text-muted-foreground">{t('notifications.filters')}</span>
                                        {rule.filterPrefix && (
                                          <span className="text-foreground">
                                            {t('notifications.prefix', { value: rule.filterPrefix })}
                                          </span>
                                        )}
                                        {rule.filterPrefix && rule.filterSuffix && (
                                          <span className="text-muted-foreground"> | </span>
                                        )}
                                        {rule.filterSuffix && (
                                          <span className="text-foreground">
                                            {t('notifications.suffix', { value: rule.filterSuffix })}
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
                                    title={rule.enabled ? t('notifications.disableTitle') : t('notifications.enableTitle')}
                                  >
                                    {rule.enabled ? t('notifications.disable') : t('notifications.enable')}
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
                        <div className="text-center py-12 border-2 border-dashed border-border rounded-lg">
                          <Bell className="h-12 w-12 text-gray-400 mx-auto mb-4" />
                          <h3 className="text-lg font-semibold text-foreground mb-2">
                            {t('notifications.noRulesTitle')}
                          </h3>
                          <p className="text-sm text-muted-foreground max-w-md mx-auto mb-4">
                            {t('notifications.noRulesDesc')}
                          </p>
                          <Button
                            onClick={handleAddNotificationRule}
                            disabled={isGlobalAdminInTenantBucket}
                          >
                            <Plus className="h-4 w-4" />
                            {t('notifications.addFirstRule')}
                          </Button>
                        </div>
                      )}

                      {/* Info Box */}
                      <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                        <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
                        <div className="text-sm text-blue-800 dark:text-blue-300">
                          <p className="font-medium mb-1">{t('notifications.infoTitle')}</p>
                          <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                            <li>{t('notifications.infoItem1')}</li>
                            <li>{t('notifications.infoItem2')}</li>
                            <li>{t('notifications.infoItem3')}</li>
                            <li>{t('notifications.infoItem4')}</li>
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
                <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-border">
                    <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                      <RefreshCw className="h-5 w-5 text-muted-foreground" />
                      {t('replication.title')}
                    </h3>
                  </div>
                  <div>
                    <div className="p-6">
                    <div className="space-y-4">
                      <div className="flex items-center justify-between">
                        <div>
                          <p className="font-medium">{t('replication.sectionTitle')}</p>
                          <p className="text-sm text-muted-foreground">
                            {replicationRules && replicationRules.length > 0
                              ? t('replication.rulesCount', { count: replicationRules.length })
                              : t('replication.noRulesConfigured')}
                          </p>
                        </div>
                        <div className="flex gap-2">
                          <Button
                            onClick={handleAddReplicationRule}
                            disabled={isGlobalAdminInTenantBucket}
                            title={
                              isGlobalAdminInTenantBucket
                                ? t('globalAdminReadOnly')
                                : undefined
                            }
                          >
                            <Plus className="h-4 w-4" />
                            {t('replication.addRule')}
                          </Button>
                        </div>
                      </div>

                      {/* Replication Rules List */}
                      {replicationRules && replicationRules.length > 0 ? (
                        <div className="space-y-3">
                          {replicationRules.map((rule: ReplicationRule) => (
                            <div
                              key={rule.id}
                              className="p-4 border border-border rounded-lg bg-gray-50 dark:bg-gray-800"
                            >
                              <div className="flex items-start justify-between">
                                <div className="flex-1">
                                  <div className="flex items-center gap-2 mb-2">
                                    {rule.enabled ? (
                                      <CheckCircle className="h-5 w-5 text-green-500" />
                                    ) : (
                                      <XCircle className="h-5 w-5 text-gray-400" />
                                    )}
                                    <span className="font-medium text-foreground">
                                      {t('replication.ruleLabel', { id: rule.id })}
                                    </span>
                                    <span
                                      className={`text-xs px-2 py-1 rounded ${
                                        rule.enabled
                                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                                          : 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200'
                                      }`}
                                    >
                                      {rule.enabled ? t('replication.enabled') : t('replication.disabled')}
                                    </span>
                                    <span className="text-xs px-2 py-1 rounded bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200">
                                      {rule.mode}
                                    </span>
                                    <span className="text-xs px-2 py-1 rounded bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                                      {t('replication.priority', { value: rule.priority })}
                                    </span>
                                  </div>

                                  <div className="space-y-1 text-sm">
                                    <div>
                                      <span className="text-muted-foreground">{t('replication.source')}</span>
                                      <span className="text-foreground font-mono text-xs">
                                        {rule.source_bucket}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-muted-foreground">{t('replication.destinationEndpoint')}</span>
                                      <span className="text-foreground font-mono text-xs">
                                        {rule.destination_endpoint}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="text-muted-foreground">{t('replication.destinationBucket')}</span>
                                      <span className="text-foreground font-mono text-xs">
                                        {rule.destination_bucket}
                                        {rule.destination_region && ` [${rule.destination_region}]`}
                                      </span>
                                    </div>
                                    {rule.schedule_interval && rule.mode === 'scheduled' && (
                                      <div>
                                        <span className="text-muted-foreground">{t('replication.schedule')}</span>
                                        <span className="text-foreground">
                                          {t('replication.everyMinutes', { minutes: rule.schedule_interval })}
                                        </span>
                                      </div>
                                    )}
                                    {rule.prefix && (
                                      <div>
                                        <span className="text-muted-foreground">{t('replication.prefixFilter')}</span>
                                        <span className="text-foreground font-mono text-xs">
                                          {rule.prefix}
                                        </span>
                                      </div>
                                    )}
                                    <div>
                                      <span className="text-muted-foreground">{t('replication.conflictResolution')}</span>
                                      <span className="text-foreground">
                                        {rule.conflict_resolution}
                                      </span>
                                    </div>
                                    <div className="flex gap-4">
                                      <span className={rule.replicate_deletes ? 'text-green-600 dark:text-green-400' : 'text-gray-400'}>
                                        {rule.replicate_deletes ? '✓' : '✗'} {t('replication.replicateDeletes')}
                                      </span>
                                      <span className={rule.replicate_metadata ? 'text-green-600 dark:text-green-400' : 'text-gray-400'}>
                                        {rule.replicate_metadata ? '✓' : '✗'} {t('replication.replicateMetadata')}
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
                                    title={rule.enabled ? t('replication.disableRule') : t('replication.enableRule')}
                                  >
                                    {rule.enabled ? t('replication.disable') : t('replication.enable')}
                                  </Button>
                                  <Button
                                    size="sm"
                                    variant="outline"
                                    onClick={() => handleTriggerReplicationSync(rule.id)}
                                    disabled={isGlobalAdminInTenantBucket || !rule.enabled}
                                    title={t('replication.triggerSyncTitle')}
                                  >
                                    <RefreshCw className="h-4 w-4 mr-1" />
                                    {t('replication.syncNow')}
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
                        <div className="text-center py-12 border-2 border-dashed border-border rounded-lg">
                          <RefreshCw className="h-12 w-12 text-gray-400 mx-auto mb-4" />
                          <h3 className="text-lg font-semibold text-foreground mb-2">
                            {t('replication.noRulesTitle')}
                          </h3>
                          <p className="text-sm text-muted-foreground max-w-md mx-auto mb-4">
                            {t('replication.noRulesDesc')}
                          </p>
                          <Button
                            onClick={handleAddReplicationRule}
                            disabled={isGlobalAdminInTenantBucket}
                          >
                            <Plus className="h-4 w-4" />
                            {t('replication.addFirstRule')}
                          </Button>
                        </div>
                      )}

                      {/* Info Box */}
                      <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                        <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
                        <div className="text-sm text-blue-800 dark:text-blue-300">
                          <p className="font-medium mb-1">{t('replication.infoTitle')}</p>
                          <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                            <li>{t('replication.infoItem1')}</li>
                            <li>{t('replication.infoItem2')}</li>
                            <li>{t('replication.infoItem3')}</li>
                            <li>{t('replication.infoItem4')}</li>
                            <li>{t('replication.infoItem5')}</li>
                          </ul>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
                </div>
              </>
            )}

            {/* INVENTORY TAB */}
            {activeTab === 'inventory' && (
              <>
                <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-border">
                    <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                      <Package className="h-5 w-5 text-muted-foreground" />
                      {t('inventory.title')}
                    </h3>
                  </div>
                  <div className="p-6">
                    <div className="space-y-6">
                      {/* Configuration Form */}
                      <div className="space-y-4">
                        <div className="flex items-center justify-between">
                          <label className="flex items-center gap-2">
                            <input
                              type="checkbox"
                              checked={inventoryForm.enabled}
                              onChange={(e) => setInventoryForm({ ...inventoryForm, enabled: e.target.checked })}
                              className="rounded border-border text-blue-600 focus:ring-blue-500"
                              disabled={isGlobalAdminInTenantBucket}
                            />
                            <span className="font-medium text-foreground">
                              {t('inventory.enableLabel')}
                            </span>
                          </label>
                        </div>

                        {inventoryForm.enabled && (
                          <>
                            <div className="grid grid-cols-2 gap-4">
                              <div>
                                <label className="block text-sm font-medium text-foreground mb-1">
                                  {t('inventory.frequencyLabel')}
                                </label>
                                <select
                                  value={inventoryForm.frequency}
                                  onChange={(e) => setInventoryForm({ ...inventoryForm, frequency: e.target.value })}
                                  className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground"
                                  disabled={isGlobalAdminInTenantBucket}
                                >
                                  <option value="daily">{t('inventory.frequencyDaily')}</option>
                                  <option value="weekly">{t('inventory.frequencyWeekly')}</option>
                                </select>
                              </div>

                              <div>
                                <label className="block text-sm font-medium text-foreground mb-1">
                                  {t('inventory.formatLabel')}
                                </label>
                                <select
                                  value={inventoryForm.format}
                                  onChange={(e) => setInventoryForm({ ...inventoryForm, format: e.target.value })}
                                  className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground"
                                  disabled={isGlobalAdminInTenantBucket}
                                >
                                  <option value="csv">CSV</option>
                                  <option value="json">JSON</option>
                                </select>
                              </div>
                            </div>

                            <div>
                              <label className="block text-sm font-medium text-foreground mb-1">
                                {t('inventory.destBucketLabel')}
                              </label>
                              <input
                                type="text"
                                value={inventoryForm.destination_bucket}
                                onChange={(e) => setInventoryForm({ ...inventoryForm, destination_bucket: e.target.value })}
                                placeholder={t('inventory.destBucketPlaceholder')}
                                className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground"
                                disabled={isGlobalAdminInTenantBucket}
                              />
                              <p className="mt-1 text-xs text-muted-foreground">
                                {t('inventory.destBucketHint')}
                              </p>
                            </div>

                            <div>
                              <label className="block text-sm font-medium text-foreground mb-1">
                                {t('inventory.destPrefixLabel')}
                              </label>
                              <input
                                type="text"
                                value={inventoryForm.destination_prefix}
                                onChange={(e) => setInventoryForm({ ...inventoryForm, destination_prefix: e.target.value })}
                                placeholder={t('inventory.destPrefixPlaceholder')}
                                className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground"
                                disabled={isGlobalAdminInTenantBucket}
                              />
                            </div>

                            <div className="grid grid-cols-2 gap-4">
                              <div>
                                <label className="block text-sm font-medium text-foreground mb-1">
                                  {t('inventory.scheduleTimeLabel')}
                                </label>
                                <input
                                  type="time"
                                  value={inventoryForm.schedule_time}
                                  onChange={(e) => setInventoryForm({ ...inventoryForm, schedule_time: e.target.value })}
                                  className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground"
                                  disabled={isGlobalAdminInTenantBucket}
                                />
                                <p className="mt-1 text-xs text-muted-foreground">
                                  {t('inventory.scheduleTimeHint')}
                                </p>
                              </div>
                            </div>

                            <div>
                              <label className="block text-sm font-medium text-foreground mb-2">
                                {t('inventory.includedFieldsLabel')}
                              </label>
                              <div className="grid grid-cols-2 gap-2">
                                {[
                                  { value: 'bucket_name', label: t('inventory.fields.bucket_name') },
                                  { value: 'object_key', label: t('inventory.fields.object_key') },
                                  { value: 'version_id', label: t('inventory.fields.version_id') },
                                  { value: 'is_latest', label: t('inventory.fields.is_latest') },
                                  { value: 'size', label: t('inventory.fields.size') },
                                  { value: 'last_modified', label: t('inventory.fields.last_modified') },
                                  { value: 'etag', label: t('inventory.fields.etag') },
                                  { value: 'storage_class', label: t('inventory.fields.storage_class') },
                                  { value: 'is_multipart_uploaded', label: t('inventory.fields.is_multipart_uploaded') },
                                  { value: 'encryption_status', label: t('inventory.fields.encryption_status') },
                                  { value: 'replication_status', label: t('inventory.fields.replication_status') },
                                  { value: 'object_acl', label: t('inventory.fields.object_acl') },
                                ].map((field) => (
                                  <label key={field.value} className="flex items-center gap-2 text-sm">
                                    <input
                                      type="checkbox"
                                      checked={inventoryForm.included_fields.includes(field.value)}
                                      onChange={(e) => {
                                        if (e.target.checked) {
                                          setInventoryForm({
                                            ...inventoryForm,
                                            included_fields: [...inventoryForm.included_fields, field.value],
                                          });
                                        } else {
                                          setInventoryForm({
                                            ...inventoryForm,
                                            included_fields: inventoryForm.included_fields.filter((f) => f !== field.value),
                                          });
                                        }
                                      }}
                                      className="rounded border-border text-blue-600 focus:ring-blue-500"
                                      disabled={isGlobalAdminInTenantBucket}
                                    />
                                    <span className="text-foreground">{field.label}</span>
                                  </label>
                                ))}
                              </div>
                            </div>

                            <div className="flex gap-2">
                              <Button
                                onClick={() => saveInventoryMutation.mutate(inventoryForm)}
                                disabled={isGlobalAdminInTenantBucket || !inventoryForm.destination_bucket}
                                loading={saveInventoryMutation.isPending}
                              >
                                {t('inventory.saveConfiguration')}
                              </Button>
                              {inventoryConfig && (
                                <Button
                                  variant="destructive"
                                  onClick={() => {
                                    if (confirm(t('inventory.confirmDeleteMsg'))) {
                                      deleteInventoryMutation.mutate();
                                    }
                                  }}
                                  disabled={isGlobalAdminInTenantBucket}
                                  loading={deleteInventoryMutation.isPending}
                                >
                                  {t('inventory.deleteConfiguration')}
                                </Button>
                              )}
                            </div>
                          </>
                        )}
                      </div>

                      {/* Inventory Reports */}
                      {inventoryReports && inventoryReports.reports && inventoryReports.reports.length > 0 && (
                        <div className="border-t border-border pt-6">
                          <h4 className="font-semibold text-foreground mb-4">{t('inventory.recentReports')}</h4>
                          <div className="space-y-2">
                            {inventoryReports.reports.map((report: any) => (
                              <div
                                key={report.id}
                                className="flex items-center justify-between p-3 border border-border rounded-lg bg-gray-50 dark:bg-gray-800"
                              >
                                <div className="flex-1">
                                  <div className="flex items-center gap-2">
                                    <FileText className="h-4 w-4 text-gray-500" />
                                    <span className="font-mono text-sm text-foreground">
                                      {report.report_path}
                                    </span>
                                    <span
                                      className={`text-xs px-2 py-1 rounded ${
                                        report.status === 'completed'
                                          ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
                                          : report.status === 'failed'
                                          ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
                                          : 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
                                      }`}
                                    >
                                      {report.status}
                                    </span>
                                  </div>
                                  <div className="text-xs text-muted-foreground mt-1">
                                    {t('inventory.reportObjects', { count: report.object_count })} • {t('inventory.reportSize', { size: (report.total_size / 1024 / 1024).toFixed(2) })}
                                    {report.completed_at && ` • ${new Date(report.completed_at * 1000).toLocaleString()}`}
                                  </div>
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Info Box */}
                      <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                        <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5" />
                        <div className="text-sm text-blue-800 dark:text-blue-300">
                          <p className="font-medium mb-1">{t('inventory.infoTitle')}</p>
                          <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                            <li>{t('inventory.infoItem1')}</li>
                            <li>{t('inventory.infoItem2')}</li>
                            <li>{t('inventory.infoItem3')}</li>
                            <li>{t('inventory.infoItem4')}</li>
                            <li>{t('inventory.infoItem5')}</li>
                          </ul>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </>
            )}

            {/* WEBSITE TAB */}
            {activeTab === 'website' && (
              <>
                <div className="bg-card rounded-lg border border-border shadow-sm hover:shadow-md transition-shadow">
                  <div className="px-6 py-4 border-b border-border">
                    <h3 className="text-lg font-semibold text-foreground flex items-center gap-2">
                      <Globe className="h-5 w-5 text-muted-foreground" />
                      {t('website.title')}
                    </h3>
                  </div>
                  <div className="p-6 space-y-6">

                    {/* Banner: website_hostname no configurado en el servidor */}
                    {!websiteHostnameConfigured && (
                      <div className="bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-800 rounded-md p-3">
                        <div className="flex items-start gap-2">
                          <AlertTriangle className="h-5 w-5 text-amber-600 dark:text-amber-400 flex-shrink-0 mt-0.5" />
                          <div>
                            <p className="text-sm font-semibold text-amber-800 dark:text-amber-300">
                              {t('website.hostnameNotConfiguredTitle')}
                            </p>
                            <p
                              className="text-xs text-amber-700 dark:text-amber-400 mt-1"
                              dangerouslySetInnerHTML={{ __html: t('website.hostnameNotConfiguredHelp') }}
                            />
                          </div>
                        </div>
                      </div>
                    )}

                    {/* Toggle principal: activar / desactivar */}
                    <div className={`flex items-start justify-between p-4 rounded-lg border-2 transition-colors ${
                      websiteEnabled
                        ? 'border-blue-300 bg-blue-50 dark:border-blue-700 dark:bg-blue-900/20'
                        : 'border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-900/20'
                    }`}>
                      <div className="flex-1 pr-4">
                        <p className="font-medium text-foreground">
                          {t('website.toggleLabel')}
                        </p>
                        <p className="text-sm text-muted-foreground mt-0.5">
                          {websiteEnabled ? t('website.toggleActiveDesc') : t('website.toggleInactiveDesc')}
                        </p>
                      </div>
                      {/* Toggle switch */}
                      <button
                        type="button"
                        role="switch"
                        aria-checked={websiteEnabled}
                        onClick={() => !isGlobalAdminInTenantBucket && websiteHostnameConfigured && setWebsiteEnabled(!websiteEnabled)}
                        disabled={isGlobalAdminInTenantBucket || !websiteHostnameConfigured}
                        className={`relative inline-flex h-7 w-12 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed ${
                          websiteEnabled ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-600'
                        }`}
                      >
                        <span
                          className={`pointer-events-none inline-block h-6 w-6 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
                            websiteEnabled ? 'translate-x-5' : 'translate-x-0'
                          }`}
                        />
                      </button>
                    </div>

                    {/* Formulario de configuración — visible sólo cuando está activado */}
                    {websiteEnabled && (
                      <div className="space-y-4 pt-2">
                        <div>
                          <label className="block text-sm font-medium text-foreground mb-1">
                            {t('website.indexDocLabel')} <span className="text-red-500">*</span>
                          </label>
                          <input
                            type="text"
                            value={websiteIndexDoc}
                            onChange={(e) => setWebsiteIndexDoc(e.target.value)}
                            placeholder={t('website.indexDocPlaceholder')}
                            className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                            disabled={isGlobalAdminInTenantBucket}
                          />
                          <p className="mt-1 text-xs text-muted-foreground">
                            {t('website.indexDocHint')}
                          </p>
                        </div>

                        <div>
                          <label className="block text-sm font-medium text-foreground mb-1">
                            {t('website.errorDocLabel')}
                          </label>
                          <input
                            type="text"
                            value={websiteErrorDoc}
                            onChange={(e) => setWebsiteErrorDoc(e.target.value)}
                            placeholder={t('website.errorDocPlaceholder')}
                            className="w-full px-3 py-2 border border-border rounded-lg bg-white dark:bg-gray-700 text-foreground focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                            disabled={isGlobalAdminInTenantBucket}
                          />
                          <p className="mt-1 text-xs text-muted-foreground">
                            {t('website.errorDocHint')}
                          </p>
                        </div>

                        {/* Endpoint URL */}
                        <div>
                          <label className="block text-sm font-medium text-foreground mb-1">
                            {t('website.websiteUrlLabel')}
                          </label>
                          <div className="flex items-center px-3 py-2 bg-gray-50 dark:bg-gray-700 border border-border rounded-lg font-mono text-sm text-foreground select-all">
                            {websiteHostname ? `${bucketName}.${websiteHostname}` : t('website.notConfiguredYet')}
                          </div>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {t('website.websiteUrlHint')}
                          </p>
                        </div>
                      </div>
                    )}

                    {/* Botón guardar configuración */}
                    <div className="flex items-center gap-3 pt-2 border-t border-border">
                      <Button
                        onClick={() => {
                          if (websiteEnabled) {
                            const errDoc = websiteErrorDoc.trim();
                            saveWebsiteMutation.mutate({
                              indexDocument: websiteIndexDoc.trim(),
                              ...(errDoc ? { errorDocument: errDoc } : {}),
                            });
                          } else {
                            deleteWebsiteMutation.mutate();
                          }
                        }}
                        disabled={
                          isGlobalAdminInTenantBucket ||
                          !websiteHostnameConfigured ||
                          saveWebsiteMutation.isPending ||
                          deleteWebsiteMutation.isPending ||
                          (websiteEnabled && !websiteIndexDoc.trim())
                        }
                        loading={saveWebsiteMutation.isPending || deleteWebsiteMutation.isPending}
                      >
                        {t('website.saveConfiguration')}
                      </Button>
                      {websiteEnabled && !websiteIndexDoc.trim() && (
                        <p className="text-sm text-amber-600 dark:text-amber-400">
                          {t('website.indexDocRequired')}
                        </p>
                      )}
                    </div>

                    {/* Info Box */}
                    <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                      <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
                      <div className="text-sm text-blue-800 dark:text-blue-300">
                        <p className="font-medium mb-1">{t('website.infoTitle')}</p>
                        <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                          <li>{t('website.infoItem1')}</li>
                          <li>{t('website.infoItem2')}</li>
                          <li>{t('website.infoItem3')}</li>
                          <li>{t('website.infoItem4')}</li>
                          <li>{t('website.infoItem5')}</li>
                        </ul>
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
        title={t('policy.modalTitle')}
        size="xl"
      >
        <div className="space-y-4">
          {/* Tabs */}
          <div className="border-b border-border">
            <nav className="-mb-px flex space-x-8">
              <button
                onClick={() => setPolicyTab('editor')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  policyTab === 'editor'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-foreground hover:border-border'
                }`}
              >
                {t('policy.editorTab')}
              </button>
              <button
                onClick={() => setPolicyTab('templates')}
                className={`py-2 px-1 border-b-2 font-medium text-sm ${
                  policyTab === 'templates'
                    ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                    : 'border-transparent text-gray-500 hover:text-foreground hover:border-border'
                }`}
              >
                {t('policy.templatesTab')}
              </button>
            </nav>
          </div>

          {/* Editor Tab */}
          {policyTab === 'editor' && (
            <div className="space-y-4">
              <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
                <p className="text-sm text-blue-800 dark:text-blue-200">
                  <strong>Tip:</strong> {t('policy.editorTip')}
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-foreground mb-2">
                  {t('policy.policyJsonLabel')}
                </label>
                <textarea
                  value={policyText}
                  onChange={(e) => setPolicyText(e.target.value)}
                  rows={18}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md font-mono text-sm"
                  placeholder={t('policy.policyJsonPlaceholder')}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  {t('policy.policyJsonHint')}
                </p>
              </div>
            </div>
          )}

          {/* Templates Tab */}
          {policyTab === 'templates' && (
            <div className="space-y-4">
              <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
                <p className="text-sm text-yellow-800 dark:text-yellow-200">
                  <strong>Warning:</strong> {t('policy.templatesWarning')}
                </p>
              </div>
              <div className="space-y-3">
                {Object.entries(policyTemplates).map(([key, template]) => (
                  <div
                    key={key}
                    className="border border-border rounded-lg p-4 hover:bg-secondary transition-colors"
                  >
                    <div className="flex items-start justify-between">
                      <div className="flex-1">
                        <h4 className="text-sm font-semibold text-foreground">
                          {template.name}
                        </h4>
                        <p className="text-xs text-muted-foreground mt-1">
                          {template.description}
                        </p>
                      </div>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleUseTemplate(key as keyof typeof policyTemplates)}
                      >
                        {t('policy.useTemplate')}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button variant="outline" onClick={() => setIsPolicyModalOpen(false)}>
              {t('policy.cancel')}
            </Button>
            <Button
              onClick={handleSavePolicy}
              disabled={isGlobalAdminInTenantBucket || savePolicyMutation.isPending || !policyText.trim()}
            >
              {savePolicyMutation.isPending ? t('policy.saving') : t('policy.savePolicy')}
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
        title={t('cors.modalTitle')}
        size="xl"
      >
        <div className="space-y-4">
          {/* View Mode Toggle */}
          <div className="flex gap-2 border-b border-border">
            <button
              onClick={() => setCorsViewMode('visual')}
              className={`px-4 py-2 font-medium text-sm ${
                corsViewMode === 'visual'
                  ? 'text-blue-600 border-b-2 border-blue-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              {t('cors.visualEditor')}
            </button>
            <button
              onClick={() => setCorsViewMode('xml')}
              className={`px-4 py-2 font-medium text-sm ${
                corsViewMode === 'xml'
                  ? 'text-blue-600 border-b-2 border-blue-600'
                  : 'text-gray-500 hover:text-gray-700'
              }`}
            >
              {t('cors.xmlEditor')}
            </button>
          </div>

          {/* Visual Mode */}
          {corsViewMode === 'visual' && !editingCorsRule && (
            <div className="space-y-4">
              <div className="flex justify-between items-center">
                <h3 className="text-sm font-medium text-foreground">
                  {t('cors.rulesCount', { count: corsRules.length })}
                </h3>
                <Button variant="default" size="sm" onClick={handleAddCorsRule}>
                  {t('cors.addRule')}
                </Button>
              </div>

              {corsRules.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  {t('cors.noRules')}
                </div>
              ) : (
                <div className="space-y-3">
                  {corsRules.map((rule, index) => (
                    <div
                      key={rule.id}
                      className="border border-border rounded-lg p-4 bg-gray-50 dark:bg-gray-800"
                    >
                      <div className="flex justify-between items-start mb-2">
                        <div className="font-medium text-sm text-foreground">
                          {t('cors.ruleNumber', { number: index + 1, id: rule.id })}
                        </div>
                        <div className="flex gap-2">
                          <button
                            onClick={() => setEditingCorsRule(rule)}
                            className="text-blue-600 hover:text-blue-700 text-sm"
                          >
                            {t('cors.edit')}
                          </button>
                          <button
                            onClick={() => handleDeleteCorsRule(rule.id)}
                            className="text-red-600 hover:text-red-700 text-sm"
                          >
                            {t('cors.delete')}
                          </button>
                        </div>
                      </div>
                      <div className="grid grid-cols-2 gap-2 text-xs">
                        <div>
                          <span className="font-medium text-muted-foreground">{t('cors.origins')}</span>{' '}
                          {rule.allowedOrigins.join(', ')}
                        </div>
                        <div>
                          <span className="font-medium text-muted-foreground">{t('cors.methods')}</span>{' '}
                          {rule.allowedMethods.join(', ')}
                        </div>
                        {rule.allowedHeaders.length > 0 && (
                          <div>
                            <span className="font-medium text-muted-foreground">{t('cors.allowedHeaders')}</span>{' '}
                            {rule.allowedHeaders.join(', ')}
                          </div>
                        )}
                        {rule.exposeHeaders.length > 0 && (
                          <div>
                            <span className="font-medium text-muted-foreground">{t('cors.exposeHeaders')}</span>{' '}
                            {rule.exposeHeaders.join(', ')}
                          </div>
                        )}
                        {rule.maxAgeSeconds > 0 && (
                          <div>
                            <span className="font-medium text-muted-foreground">{t('cors.maxAge')}</span>{' '}
                            {t('cors.maxAgeSeconds', { seconds: rule.maxAgeSeconds })}
                          </div>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <div className="flex justify-end gap-2 pt-4 border-t border-border">
                <Button variant="outline" onClick={() => setIsCORSModalOpen(false)}>
                  {t('cors.cancel')}
                </Button>
                <Button
                  onClick={handleSaveAllCorsRules}
                  disabled={isGlobalAdminInTenantBucket || saveCORSMutation.isPending || corsRules.length === 0}
                >
                  {saveCORSMutation.isPending ? t('cors.saving') : t('cors.saveConfiguration')}
                </Button>
              </div>
            </div>
          )}

          {/* Edit Rule Form */}
          {corsViewMode === 'visual' && editingCorsRule && (
            <div className="space-y-4">
              <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
                <h3 className="text-sm font-medium text-blue-900 dark:text-blue-100 mb-2">
                  {corsRules.find(r => r.id === editingCorsRule.id) ? t('cors.editCorsRuleTitle') : t('cors.addCorsRuleTitle')}
                </h3>
              </div>

              {/* Rule ID */}
              <div>
                <label className="block text-sm font-medium text-foreground mb-1">
                  {t('cors.ruleId')}
                </label>
                <input
                  type="text"
                  value={editingCorsRule.id}
                  onChange={(e) => setEditingCorsRule({ ...editingCorsRule, id: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
                  placeholder={t('cors.ruleIdPlaceholder')}
                />
              </div>

              {/* Allowed Origins */}
              <div>
                <label className="block text-sm font-medium text-foreground mb-1">
                  {t('cors.allowedOriginsLabel')} <span className="text-red-500">*</span>
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newOrigin}
                    onChange={(e) => setNewOrigin(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addOriginToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
                    placeholder={t('cors.allowedOriginPlaceholder')}
                  />
                  <Button onClick={addOriginToRule} disabled={!newOrigin.trim()}>
                    {t('cors.addButton')}
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
                <label className="block text-sm font-medium text-foreground mb-2">
                  {t('cors.allowedMethodsLabel')} <span className="text-red-500">*</span>
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
                      <span className="text-sm text-foreground">{method}</span>
                    </label>
                  ))}
                </div>
              </div>

              {/* Allowed Headers */}
              <div>
                <label className="block text-sm font-medium text-foreground mb-1">
                  {t('cors.allowedHeadersLabel')}
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newAllowedHeader}
                    onChange={(e) => setNewAllowedHeader(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addAllowedHeaderToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
                    placeholder={t('cors.allowedHeaderPlaceholder')}
                  />
                  <Button onClick={addAllowedHeaderToRule} disabled={!newAllowedHeader.trim()}>
                    {t('cors.addButton')}
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
                <label className="block text-sm font-medium text-foreground mb-1">
                  {t('cors.exposeHeadersLabel')}
                </label>
                <div className="flex gap-2 mb-2">
                  <input
                    type="text"
                    value={newExposeHeader}
                    onChange={(e) => setNewExposeHeader(e.target.value)}
                    onKeyPress={(e) => e.key === 'Enter' && addExposeHeaderToRule()}
                    className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
                    placeholder={t('cors.exposeHeaderPlaceholder')}
                  />
                  <Button onClick={addExposeHeaderToRule} disabled={!newExposeHeader.trim()}>
                    {t('cors.addButton')}
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
                <label className="block text-sm font-medium text-foreground mb-1">
                  {t('cors.maxAgeLabel')}
                </label>
                <input
                  type="number"
                  value={editingCorsRule.maxAgeSeconds}
                  onChange={(e) => setEditingCorsRule({ ...editingCorsRule, maxAgeSeconds: parseInt(e.target.value) || 0 })}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
                  placeholder={t('cors.maxAgePlaceholder')}
                  min="0"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  {t('cors.maxAgeHint')}
                </p>
              </div>

              <div className="flex justify-end gap-2 pt-4 border-t border-border">
                <Button variant="outline" onClick={() => setEditingCorsRule(null)}>
                  {t('cors.cancel')}
                </Button>
                <Button onClick={handleSaveCorsRule}>
                  {t('cors.saveRule')}
                </Button>
              </div>
            </div>
          )}

          {/* XML Mode */}
          {corsViewMode === 'xml' && (
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-foreground mb-2">
                  {t('cors.corsXmlLabel')}
                </label>
                <textarea
                  value={corsText}
                  onChange={(e) => setCorsText(e.target.value)}
                  rows={15}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md font-mono text-sm"
                  placeholder={t('cors.corsXmlPlaceholder')}
                />
                <p className="text-xs text-muted-foreground mt-1">
                  {t('cors.corsXmlHint')}
                </p>
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => setIsCORSModalOpen(false)}>
                  {t('cors.cancel')}
                </Button>
                <Button
                  onClick={() => saveCORSMutation.mutate(corsText)}
                  disabled={isGlobalAdminInTenantBucket || saveCORSMutation.isPending || !corsText.trim()}
                >
                  {saveCORSMutation.isPending ? t('cors.saving') : t('cors.saveCors')}
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
        title={t('lifecycle.modalTitle')}
        size="lg"
      >
        <div className="space-y-6">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              {t('lifecycle.lifecycleInfo')}
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium text-foreground mb-2">
              {t('lifecycle.noncurrentDaysLabel')}
            </label>
            <input
              type="number"
              min="1"
              max="3650"
              value={noncurrentDays}
              onChange={(e) => setNoncurrentDays(parseInt(e.target.value) || 30)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md"
            />
            <p className="text-xs text-muted-foreground mt-1">
              {t('lifecycle.noncurrentDaysHint')}
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
              <label htmlFor="delete-markers" className="block text-sm font-medium text-foreground">
                {t('lifecycle.deleteMarkersLabel')}
              </label>
              <p className="text-xs text-muted-foreground mt-1">
                {t('lifecycle.deleteMarkersHint')}
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t">
            <Button variant="outline" onClick={() => setIsLifecycleModalOpen(false)}>
              {t('lifecycle.cancel')}
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
              {saveLifecycleMutation.isPending ? t('lifecycle.saving') : t('lifecycle.saveRules')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Tags Modal */}
      <Modal
        isOpen={isTagsModalOpen}
        onClose={() => setIsTagsModalOpen(false)}
        title={t('tags.modalTitle')}
        size="lg"
      >
        <div className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              {t('tags.tagsInfo')}
            </p>
          </div>

          {/* Existing Tags */}
          {Object.keys(tags).length > 0 && (
            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('tags.currentTags')}
              </label>
              <div className="space-y-2">
                {Object.entries(tags).map(([key, value]) => (
                  <div
                    key={key}
                    className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-md"
                  >
                    <div>
                      <span className="font-medium text-sm text-foreground">
                        {key}
                      </span>
                      <span className="text-muted-foreground mx-2">:</span>
                      <span className="text-sm text-foreground">
                        {value}
                      </span>
                    </div>
                    <Button
                      size="sm"
                      variant="destructive"
                      onClick={() => handleRemoveTag(key)}
                    >
                      {t('tags.remove')}
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Add New Tag */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-foreground">
              {t('tags.addNewTag')}
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                placeholder={t('tags.keyPlaceholder')}
                value={newTagKey}
                onChange={(e) => setNewTagKey(e.target.value)}
                className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <input
                type="text"
                placeholder={t('tags.valuePlaceholder')}
                value={newTagValue}
                onChange={(e) => setNewTagValue(e.target.value)}
                className="flex-1 px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <Button onClick={handleAddTag} disabled={!newTagKey || !newTagValue}>
                {t('tags.add')}
              </Button>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button variant="outline" onClick={() => setIsTagsModalOpen(false)}>
              {t('tags.cancel')}
            </Button>
            <Button
              onClick={handleSaveTags}
              disabled={isGlobalAdminInTenantBucket || saveTagsMutation.isPending}
            >
              {saveTagsMutation.isPending ? t('tags.saving') : t('tags.saveTags')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* ACL Modal */}
      <Modal
        isOpen={isACLModalOpen}
        onClose={() => setIsACLModalOpen(false)}
        title={t('acl.modalTitle')}
        size="lg"
      >
        <div className="space-y-4">
          <div className="bg-blue-50 dark:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-lg p-3">
            <p className="text-sm text-blue-800 dark:text-blue-200">
              {t('acl.aclInfo')}
            </p>
          </div>

          {/* Canned ACL Selection */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-foreground">
              {t('acl.selectLevel')}
            </label>

            <div className="space-y-2">
              {/* Private */}
              <label className="flex items-start gap-3 p-3 border border-border rounded-lg cursor-pointer hover:bg-secondary transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="private"
                  checked={selectedCannedACL === 'private'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-foreground">{t('acl.privateLabel')}</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    {t('acl.privateDesc')}
                  </div>
                </div>
              </label>

              {/* Public Read */}
              <label className="flex items-start gap-3 p-3 border border-border rounded-lg cursor-pointer hover:bg-secondary transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="public-read"
                  checked={selectedCannedACL === 'public-read'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-foreground">{t('acl.publicReadLabel')}</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    {t('acl.publicReadDesc')}
                  </div>
                </div>
              </label>

              {/* Public Read Write */}
              <label className="flex items-start gap-3 p-3 border border-border rounded-lg cursor-pointer hover:bg-secondary transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="public-read-write"
                  checked={selectedCannedACL === 'public-read-write'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-foreground">{t('acl.publicReadWriteLabel')}</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    {t('acl.publicReadWriteDesc')} <strong className="text-yellow-600">{t('acl.publicReadWriteCaution')}</strong>
                  </div>
                </div>
              </label>

              {/* Authenticated Read */}
              <label className="flex items-start gap-3 p-3 border border-border rounded-lg cursor-pointer hover:bg-secondary transition-colors">
                <input
                  type="radio"
                  name="acl"
                  value="authenticated-read"
                  checked={selectedCannedACL === 'authenticated-read'}
                  onChange={(e) => setSelectedCannedACL(e.target.value)}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm text-foreground">{t('acl.authenticatedReadLabel')}</div>
                  <div className="text-xs text-muted-foreground mt-1">
                    {t('acl.authenticatedReadDesc')}
                  </div>
                </div>
              </label>
            </div>
          </div>

          {/* Warning for public access */}
          {(selectedCannedACL === 'public-read' || selectedCannedACL === 'public-read-write') && (
            <div className="bg-yellow-50 dark:bg-yellow-900/30 border border-yellow-200 dark:border-yellow-800 rounded-lg p-3">
              <p className="text-sm text-yellow-800 dark:text-yellow-200">
                <strong>Warning:</strong> {t('acl.publicWarning')}
              </p>
            </div>
          )}

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button variant="outline" onClick={() => setIsACLModalOpen(false)}>
              {t('acl.cancel')}
            </Button>
            <Button
              onClick={handleSaveACL}
              disabled={isGlobalAdminInTenantBucket || saveACLMutation.isPending}
            >
              {saveACLMutation.isPending ? t('acl.saving') : t('acl.saveAcl')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Notification Rule Modal */}
      <Modal
        isOpen={isNotificationModalOpen}
        onClose={() => setIsNotificationModalOpen(false)}
        title={editingNotificationRule ? t('notifications.editModalTitle') : t('notifications.addModalTitle')}
        size="xl"
      >
        <div className="space-y-4">
          {/* Rule ID */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('notifications.ruleIdLabel')}
            </label>
            <input
              type="text"
              value={notificationRuleForm.id || ''}
              onChange={(e) =>
                setNotificationRuleForm({ ...notificationRuleForm, id: e.target.value })
              }
              placeholder={t('notifications.ruleIdPlaceholder')}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              disabled={!!editingNotificationRule}
            />
            <p className="text-xs text-muted-foreground">
              {t('notifications.ruleIdHint')}
            </p>
          </div>

          {/* Webhook URL */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('notifications.webhookUrlLabel')} <span className="text-red-500">*</span>
            </label>
            <input
              type="url"
              value={notificationRuleForm.webhookUrl || ''}
              onChange={(e) =>
                setNotificationRuleForm({ ...notificationRuleForm, webhookUrl: e.target.value })
              }
              placeholder={t('notifications.webhookUrlPlaceholder')}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">
              {t('notifications.webhookUrlHint')}
            </p>
          </div>

          {/* Event Types */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('notifications.eventTypesLabel')} <span className="text-red-500">*</span>
            </label>
            <div className="grid grid-cols-2 gap-2">
              {[
                { value: 's3:ObjectCreated:*', label: t('notifications.events.objectCreatedAll') },
                { value: 's3:ObjectCreated:Put', label: t('notifications.events.objectCreatedPut') },
                { value: 's3:ObjectCreated:Post', label: t('notifications.events.objectCreatedPost') },
                { value: 's3:ObjectCreated:Copy', label: t('notifications.events.objectCreatedCopy') },
                { value: 's3:ObjectCreated:CompleteMultipartUpload', label: t('notifications.events.multipartComplete') },
                { value: 's3:ObjectRemoved:*', label: t('notifications.events.objectRemovedAll') },
                { value: 's3:ObjectRemoved:Delete', label: t('notifications.events.objectDeleted') },
                { value: 's3:ObjectRestored:Post', label: t('notifications.events.objectRestored') },
              ].map((eventType) => (
                <label
                  key={eventType.value}
                  className="flex items-start gap-2 p-2 border border-border rounded cursor-pointer hover:bg-secondary"
                >
                  <input
                    type="checkbox"
                    checked={notificationRuleForm.events?.includes(eventType.value) || false}
                    onChange={() => handleToggleEvent(eventType.value)}
                    className="mt-1"
                  />
                  <div className="text-sm">
                    <div className="font-medium text-foreground">
                      {eventType.label}
                    </div>
                    <div className="text-xs text-muted-foreground font-mono">
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
              <label className="block text-sm font-medium text-foreground">
                {t('notifications.prefixFilterLabel')}
              </label>
              <input
                type="text"
                value={notificationRuleForm.filterPrefix || ''}
                onChange={(e) =>
                  setNotificationRuleForm({ ...notificationRuleForm, filterPrefix: e.target.value })
                }
                placeholder={t('notifications.prefixFilterPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <p className="text-xs text-muted-foreground">
                {t('notifications.prefixFilterHint')}
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('notifications.suffixFilterLabel')}
              </label>
              <input
                type="text"
                value={notificationRuleForm.filterSuffix || ''}
                onChange={(e) =>
                  setNotificationRuleForm({ ...notificationRuleForm, filterSuffix: e.target.value })
                }
                placeholder={t('notifications.suffixFilterPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <p className="text-xs text-muted-foreground">
                {t('notifications.suffixFilterHint')}
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
            <label htmlFor="notificationEnabled" className="text-sm font-medium text-foreground">
              {t('notifications.enableRuleLabel')}
            </label>
          </div>

          {/* Info */}
          <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
            <div className="text-sm text-blue-800 dark:text-blue-300">
              <p className="font-medium mb-1">{t('notifications.webhookFormatTitle')}</p>
              <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                <li>{t('notifications.webhookFormatItem1')}</li>
                <li>{t('notifications.webhookFormatItem2')}</li>
                <li>{t('notifications.webhookFormatItem3')}</li>
                <li>{t('notifications.webhookFormatItem4')}</li>
              </ul>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button variant="outline" onClick={() => setIsNotificationModalOpen(false)}>
              {t('notifications.cancel')}
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
                ? t('notifications.saving')
                : editingNotificationRule
                ? t('notifications.updateRule')
                : t('notifications.addRule')}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Replication Rule Modal */}
      <Modal
        isOpen={isReplicationModalOpen}
        onClose={() => setIsReplicationModalOpen(false)}
        title={editingReplicationRule ? t('replication.editModalTitle') : t('replication.addModalTitle')}
        size="xl"
      >
        <div className="space-y-4">
          {/* Destination S3 Endpoint */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('replication.endpointLabel')} <span className="text-red-500">*</span>
            </label>
            <input
              type="url"
              value={replicationRuleForm.destination_endpoint || ''}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, destination_endpoint: e.target.value })
              }
              placeholder={t('replication.endpointPlaceholder')}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">
              {t('replication.endpointHint')}
            </p>
          </div>

          {/* Destination Bucket */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('replication.destBucketLabel')} <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={replicationRuleForm.destination_bucket || ''}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, destination_bucket: e.target.value })
              }
              placeholder={t('replication.destBucketPlaceholder')}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">
              {t('replication.destBucketHint')}
            </p>
          </div>

          {/* Access Credentials */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.accessKeyLabel')} <span className="text-red-500">*</span>
              </label>
              <input
                type="text"
                value={replicationRuleForm.destination_access_key || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_access_key: e.target.value })
                }
                placeholder={t('replication.accessKeyPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
              />
              <p className="text-xs text-muted-foreground">
                {t('replication.accessKeyHint')}
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.secretKeyLabel')} <span className="text-red-500">*</span>
              </label>
              <input
                type="password"
                value={replicationRuleForm.destination_secret_key || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_secret_key: e.target.value })
                }
                placeholder={t('replication.secretKeyPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
              />
              <p className="text-xs text-muted-foreground">
                {t('replication.secretKeyHint')}
              </p>
            </div>
          </div>

          {/* Region and Prefix */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.regionLabel')}
              </label>
              <input
                type="text"
                value={replicationRuleForm.destination_region || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, destination_region: e.target.value })
                }
                placeholder={t('replication.regionPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <p className="text-xs text-muted-foreground">
                {t('replication.regionHint')}
              </p>
            </div>

            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.prefixLabel')}
              </label>
              <input
                type="text"
                value={replicationRuleForm.prefix || ''}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, prefix: e.target.value })
                }
                placeholder={t('replication.prefixPlaceholder')}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm font-mono"
              />
              <p className="text-xs text-muted-foreground">
                {t('replication.prefixHint')}
              </p>
            </div>
          </div>

          {/* Mode, Schedule and Priority */}
          <div className="grid grid-cols-3 gap-4">
            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.modeLabel')} <span className="text-red-500">*</span>
              </label>
              <select
                value={replicationRuleForm.mode || 'realtime'}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, mode: e.target.value as 'realtime' | 'scheduled' | 'batch' })
                }
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              >
                <option value="realtime">{t('replication.modeRealtime')}</option>
                <option value="scheduled">{t('replication.modeScheduled')}</option>
                <option value="batch">{t('replication.modeBatch')}</option>
              </select>
              <p className="text-xs text-muted-foreground">
                {t('replication.modeHint')}
              </p>
            </div>

            {replicationRuleForm.mode === 'scheduled' && (
              <div className="space-y-2">
                <label className="block text-sm font-medium text-foreground">
                  {t('replication.intervalLabel')} <span className="text-red-500">*</span>
                </label>
                <input
                  type="number"
                  min="1"
                  value={replicationRuleForm.schedule_interval || 60}
                  onChange={(e) =>
                    setReplicationRuleForm({ ...replicationRuleForm, schedule_interval: parseInt(e.target.value) || 60 })
                  }
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
                />
                <p className="text-xs text-muted-foreground">
                  {t('replication.intervalHint')}
                </p>
              </div>
            )}

            <div className="space-y-2">
              <label className="block text-sm font-medium text-foreground">
                {t('replication.priorityLabel')}
              </label>
              <input
                type="number"
                min="1"
                max="100"
                value={replicationRuleForm.priority || 1}
                onChange={(e) =>
                  setReplicationRuleForm({ ...replicationRuleForm, priority: parseInt(e.target.value) || 1 })
                }
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
              />
              <p className="text-xs text-muted-foreground">
                {t('replication.priorityHint')}
              </p>
            </div>
          </div>

          {/* Conflict Resolution */}
          <div className="space-y-2">
            <label className="block text-sm font-medium text-foreground">
              {t('replication.conflictLabel')} <span className="text-red-500">*</span>
            </label>
            <select
              value={replicationRuleForm.conflict_resolution || 'last_write_wins'}
              onChange={(e) =>
                setReplicationRuleForm({ ...replicationRuleForm, conflict_resolution: e.target.value as 'last_write_wins' | 'version_based' | 'primary_wins' })
              }
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-card text-foreground rounded-md text-sm"
            >
              <option value="last_write_wins">{t('replication.conflictLastWrite')}</option>
              <option value="version_based">{t('replication.conflictVersionBased')}</option>
              <option value="primary_wins">{t('replication.conflictPrimaryWins')}</option>
            </select>
            <p className="text-xs text-muted-foreground">
              {t('replication.conflictHint')}
            </p>
          </div>

          {/* Options */}
          <div className="space-y-3">
            <label className="block text-sm font-medium text-foreground">
              {t('replication.optionsLabel')}
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
                <label htmlFor="replicateDeletes" className="text-sm text-foreground">
                  {t('replication.replicateDeletesLabel')}
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
                <label htmlFor="replicateMetadata" className="text-sm text-foreground">
                  {t('replication.replicateMetadataLabel')}
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
                <label htmlFor="enabled" className="text-sm font-medium text-foreground">
                  {t('replication.enableRuleLabel')}
                </label>
              </div>
            </div>
          </div>

          {/* Info */}
          <div className="flex items-start gap-3 p-4 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
            <AlertCircle className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
            <div className="text-sm text-blue-800 dark:text-blue-300">
              <p className="font-medium mb-1">{t('replication.requirementsTitle')}</p>
              <ul className="list-disc list-inside space-y-1 text-blue-700 dark:text-blue-400">
                <li>{t('replication.requirementsItem1')}</li>
                <li>{t('replication.requirementsItem2')}</li>
                <li>{t('replication.requirementsItem3')}</li>
                <li>{t('replication.requirementsItem4')}</li>
              </ul>
            </div>
          </div>

          {/* Actions */}
          <div className="flex justify-end gap-2 pt-4 border-t border-border">
            <Button variant="outline" onClick={() => setIsReplicationModalOpen(false)}>
              {t('replication.cancel')}
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
                ? t('replication.saving')
                : editingReplicationRule
                ? t('replication.updateRule')
                : t('replication.addRule')}
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
