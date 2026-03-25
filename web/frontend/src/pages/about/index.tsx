import React from 'react';
import { useTranslation } from 'react-i18next';
import { Card } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { useBasePath } from '@/hooks/useBasePath';
import type { ServerConfig } from '@/types';
import {
  Code,
  Github,
  Mail,
  Globe,
  Shield,
  Zap,
  Box,
  Lock,
  Package,
  Award,
  Heart,
  Send,
  FileJson,
  Layers,
  Network,
  Copy,
  BarChart3,
  KeyRound
} from 'lucide-react';

export default function AboutPage() {
  const { t } = useTranslation('about');
  const basePath = useBasePath();
  const { data: config, isLoading } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loading size="lg" />
      </div>
    );
  }

  const version = config?.version || 'unknown';
  const buildDate = config?.buildDate || 'unknown';

  return (
    <div className="space-y-6">
      {/* Header with Logo */}
      <div className="flex flex-col items-center justify-center text-center space-y-4">
        <div className="flex justify-center px-12 py-8 dark:bg-gradient-to-br dark:from-gray-800 dark:to-gray-900 dark:rounded-2xl">
          <img
            src={`${basePath}/assets/img/logo.png`}
            alt="MaxIOFS Logo"
            className="w-[22rem] max-h-[200px] 3xl:w-[28rem] 3xl:max-h-[250px] 4xl:w-[32rem] 4xl:max-h-[300px] h-auto object-contain dark:opacity-90 dark:brightness-0 dark:invert"
          />
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Project Info */}
        <Card>
          <div className="p-6">
            <div className="flex items-center space-x-3 mb-6">
              <div className="flex items-center justify-center w-12 h-12 rounded-lg bg-brand-600">
                <img
                  src={`${basePath}/assets/img/icon.png`}
                  alt="MaxIOFS"
                  className="w-8 h-8 rounded"
                />
              </div>
              <div>
                <h2 className="text-2xl font-bold text-foreground">MaxIOFS</h2>
                <p className="text-sm text-muted-foreground">{t('version')} {version}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-semibold text-foreground mb-2">
                  {t('description')}
                </h3>
                <p className="text-sm text-muted-foreground leading-relaxed">
                  {t('descriptionText')}
                </p>
              </div>

              <div className="grid grid-cols-2 gap-4 pt-4 border-t border-border">
                <div>
                  <p className="text-xs text-muted-foreground">{t('version')}</p>
                  <p className="text-sm font-semibold text-foreground">{version}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">{t('buildDate')}</p>
                  <p className="text-sm font-semibold text-foreground">{buildDate}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">{t('license')}</p>
                  <p className="text-sm font-semibold text-foreground">MIT</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">{t('status')}</p>
                  <p className="text-sm font-semibold text-green-600 dark:text-green-400">{t('stable')}</p>
                </div>
              </div>
            </div>
          </div>
        </Card>

        {/* Developer Info */}
        <Card>
          <div className="p-6">
            <div className="flex items-center space-x-3 mb-6">
              <div className="flex items-center justify-center w-12 h-12 rounded-full bg-gradient-to-br from-brand-600 to-brand-700 text-white text-xl font-bold">
                AR
              </div>
              <div>
                <h2 className="text-xl font-bold text-foreground">Aluisco Ricardo</h2>
                <p className="text-sm text-muted-foreground">{t('leadDeveloper')}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-semibold text-foreground mb-3">
                  {t('mainDeveloper')}
                </h3>
                <p className="text-sm text-muted-foreground leading-relaxed">
                  {t('mainDeveloperDescription')}
                </p>
              </div>

              <div className="space-y-3 pt-4 border-t border-border">
                <a
                  href="mailto:aluisco@maxiofs.com"
                  className="flex items-center space-x-3 text-sm text-muted-foreground hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Mail className="h-4 w-4" />
                  <span>aluisco@maxiofs.com</span>
                </a>
                <a
                  href="https://github.com/MaxioFS/MaxioFS"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center space-x-3 text-sm text-muted-foreground hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Github className="h-4 w-4" />
                  <span>github.com/MaxioFS/MaxioFS</span>
                </a>
                <a
                href="https://t.me/aluisco"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center space-x-3 text-sm text-muted-foreground hover:text-[#0088cc] transition-colors"
                >
                <Send className="h-4 w-4 text-[#0088cc] rotate-45" />
                <span>t.me/aluisco</span>
                </a>
                <a
                  href="https://maxiofs.com"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center space-x-3 text-sm text-muted-foreground hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Globe className="h-4 w-4" />
                  <span>maxiofs.com</span>
                </a>
              </div>
            </div>
          </div>
        </Card>
      </div>

      {/* Features Grid */}
      <Card>
        <div className="p-6">
          <h2 className="text-xl font-bold text-foreground mb-6">
            {t('keyFeatures')}
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            <FeatureCard
              icon={Box}
              title={t('featureS3Compatible')}
              description={t('featureS3CompatibleDesc')}
            />
            <FeatureCard
              icon={Shield}
              title={t('featureMultiTenant')}
              description={t('featureMultiTenantDesc')}
            />
            <FeatureCard
              icon={Lock}
              title={t('featureSecurity')}
              description={t('featureSecurityDesc')}
            />
            <FeatureCard
              icon={Zap}
              title={t('featurePerformance')}
              description={t('featurePerformanceDesc')}
            />
            <FeatureCard
              icon={Package}
              title={t('featureSingleBinary')}
              description={t('featureSingleBinaryDesc')}
            />
            <FeatureCard
              icon={Code}
              title={t('featureModernUI')}
              description={t('featureModernUIDesc')}
            />
            <FeatureCard
              icon={FileJson}
              title={t('featureAdvancedS3')}
              description={t('featureAdvancedS3Desc')}
            />
            <FeatureCard
              icon={Copy}
              title="Bucket Replication"
              description="S3-compatible replication to AWS S3, MinIO, or other MaxIOFS instances. Realtime, scheduled, and batch modes with retry logic"
            />
            <FeatureCard
              icon={Network}
              title="High Availability Cluster"
              description="Multi-node cluster with intelligent routing, HMAC-authenticated HA replication, health monitoring, and automatic failover"
            />
            <FeatureCard
              icon={Layers}
              title={t('featureDualStorage')}
              description={t('featureDualStorageDesc')}
            />
            <FeatureCard
              icon={BarChart3}
              title="Monitoring & Observability"
              description="Prometheus metrics endpoint, Grafana dashboards, performance SLOs, real-time latency tracking (p50/p95/p99), and alerting"
            />
            <FeatureCard
              icon={KeyRound}
              title="SSO & Identity Providers"
              description="OAuth2/OIDC SSO with Google and Microsoft presets, LDAP/AD integration, auto-provisioning via group mappings, multi-tenant provider routing by email domain"
            />
          </div>
        </div>
      </Card>

      {/* Technology Stack */}
      <Card>
        <div className="p-6">
          <h2 className="text-xl font-bold text-foreground mb-6">
            {t('technologyStack')}
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div>
              <h3 className="text-sm font-semibold text-foreground mb-3 flex items-center">
                <Code className="h-4 w-4 mr-2" />
                {t('backend')}
              </h3>
              <ul className="space-y-2 text-sm text-muted-foreground">
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Go 1.25+
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Gorilla Mux (Routing)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Pebble v1.1 (Object Metadata, crash-safe WAL)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  SQLite (Authentication, Audit & Cluster)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Filesystem Storage Backend
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Logrus (Structured Logging)
                </li>
              </ul>
            </div>
            <div>
              <h3 className="text-sm font-semibold text-foreground mb-3 flex items-center">
                <Globe className="h-4 w-4 mr-2" />
                {t('frontend')}
              </h3>
              <ul className="space-y-2 text-sm text-muted-foreground">
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  React 19 + TypeScript
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Vite 7 (Build Tool)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  TailwindCSS 4 (Oxide Engine)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  TanStack Query v5 (React Query)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  React Router v7
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Vitest 4 (Testing Framework)
                </li>
              </ul>
            </div>
          </div>
        </div>
      </Card>

      {/* Recent Improvements */}
      <Card>
        <div className="p-6">
          <h2 className="text-xl font-bold text-foreground mb-6">
            {t('newFeaturesTitle', { version })}
          </h2>
          <div className="space-y-4">

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                AWS S3-Style Actions Toolbar in Bucket Browser
              </h3>
              <p className="text-sm text-muted-foreground">
                Per-row action buttons replaced with a single <strong>Actions</strong> dropdown operating on
                checkbox-selected items — the same pattern used by AWS S3 console. Supports Copy S3 URI,
                Copy Object URL, Download, Download as ZIP (folders), Calculate Folder Size, Share,
                Presigned URL, View Versions, Legal Hold toggle, Rename, Edit Tags, and Delete.
                Actions are automatically disabled when the selection count or type does not match the operation.
              </p>
            </div>

            <div className="border-l-4 border-indigo-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Object Detail View
              </h3>
              <p className="text-sm text-muted-foreground">
                Clicking a file name now opens a full-page detail view with three tabs:{' '}
                <strong>Properties</strong> (S3 URI, ARN, URL, Key, size, ETag, content type, storage class,
                region, custom metadata — all with copy buttons), <strong>Permissions</strong> (ACL owner and
                grants, lazy-loaded), and <strong>Versions</strong> (version history with delete markers,
                lazy-loaded). The breadcrumb tracks the full path and each segment is a clickable navigation link.
              </p>
            </div>

            <div className="border-l-4 border-cyan-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Object Rename and Tags Editor
              </h3>
              <p className="text-sm text-muted-foreground">
                Objects can now be renamed directly from the bucket browser via the Actions menu. The server
                copies data, metadata, and tags to the new key, then deletes the original — blocked for objects
                under active COMPLIANCE retention or Legal Hold. S3 object tags can also be viewed and edited
                inline without leaving the bucket browser.
              </p>
            </div>

            <div className="border-l-4 border-teal-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Folder Download as ZIP and Folder Size Calculator
              </h3>
              <p className="text-sm text-muted-foreground">
                Entire folder trees can be downloaded as a ZIP archive directly from the Actions menu. Objects
                are stored without compression (ZIP Store mode) to minimize server CPU. Limits: 10,000 objects
                and 10 GB maximum; requests exceeding either receive a 400 error before streaming begins.
                The folder size calculator totals all object sizes and counts under a prefix without downloading.
              </p>
            </div>

            <div className="border-l-4 border-purple-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: GetObjectAttributes, Conditional PutObject, and Real Bucket Logging
              </h3>
              <p className="text-sm text-muted-foreground">
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">GetObjectAttributes</code> returns ETag,
                StorageClass, ObjectSize, and part count without downloading the body — required by AWS CLI v2,
                SDK v3, and Mountpoint S3.{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutObject</code> now supports{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">If-None-Match: *</code>{' '}
                for atomic "write only if not exists" semantics (Terraform state backends, distributed locks).
                Bucket access logging now delivers log entries asynchronously to the target bucket in standard AWS
                S3 access log format.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: Encryption Correctly Applied and SSE Headers Returned
              </h3>
              <p className="text-sm text-muted-foreground">
                Server-side encryption was silently skipped for buckets that had no explicit{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutBucketEncryption</code> call, even
                when the global encryption key was configured. Fixed: encryption now applies to all buckets by
                default. The <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">x-amz-server-side-encryption</code>{' '}
                response header is now returned on{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">GetObject</code>,{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">HeadObject</code>, and{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutObject</code>{' '}
                for encrypted objects.{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">GetBucketEncryption</code> /{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutBucketEncryption</code>{' '}
                now persist real configuration (previously always returned 404).
              </p>
            </div>

            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Security: SigV2 Fix, Bucket Policy Conditions, and PublicAccessBlock Enforcement
              </h3>
              <p className="text-sm text-muted-foreground">
                AWS Signature Version 2 used SHA256+hex instead of the required SHA1+base64 — all V2 presigned
                URLs and Authorization-header requests were rejected. Fixed in both code paths. Bucket policy{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Condition</code> blocks
                were silently skipped; full evaluator implemented (StringEquals/Like, IpAddress/CIDR, Bool, Arn,
                Numeric operators). A <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Principal: null</code>{' '}
                policy statement no longer grants access to everyone.{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PublicAccessBlock</code> flags
                (IgnorePublicAcls, RestrictPublicBuckets) are now consulted before ACL evaluation.
                The console port (8081) now sets security headers (CSP, X-Frame-Options, nosniff).
              </p>
            </div>

            <div className="border-l-4 border-yellow-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Security: DeleteBucket No Longer Bypasses Object Lock
              </h3>
              <p className="text-sm text-muted-foreground">
                Deleting a versioned bucket silently deleted all version files (stored under{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">/.versions/</code>) before checking
                emptiness, bypassing COMPLIANCE-mode Object Lock retention on every non-current version.
                Fixed by resolving versioned paths to real object metadata before classifying them as empty.
              </p>
            </div>

          </div>
        </div>
      </Card>

      {/* Footer */}
      <Card>
        <div className="p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2 text-sm text-muted-foreground">
              <Heart className="h-4 w-4 text-red-500" />
              <span>{t('developedWithPassion')}</span>
            </div>
            <div className="flex items-center space-x-2 text-sm text-muted-foreground">
              <Award className="h-4 w-4" />
              <span>{t('copyright')}</span>
            </div>
          </div>
        </div>
      </Card>
    </div>
  );
}

interface FeatureCardProps {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: string;
}

function FeatureCard({ icon: Icon, title, description }: FeatureCardProps) {
  return (
    <div className="flex flex-col">
      <div className="flex items-center space-x-3 mb-2">
        <div className="flex items-center justify-center w-8 h-8 rounded-lg bg-gray-100 dark:bg-gray-700/50">
          <Icon className="h-4 w-4 text-muted-foreground" />
        </div>
        <h3 className="font-semibold text-foreground">{title}</h3>
      </div>
      <p className="text-sm text-muted-foreground leading-relaxed">
        {description}
      </p>
    </div>
  );
}
