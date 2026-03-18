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

            {/* UI Redesign */}
            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Complete Frontend Redesign
              </h3>
              <p className="text-sm text-muted-foreground">
                New floating layout with sidebar and content inset from the browser edge. Collapsible sidebar
                (icon-only or full) persisted to localStorage. New light mode theme: white cards on slate-200
                background replacing the previous gray-on-gray scheme. All hardcoded Tailwind gray pairs replaced
                with semantic CSS tokens — every page and component is now fully theme-aware. Compact S3-style
                table rows, standardized page headers, and transparent logo background in light mode.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Folder Upload in Bucket Browser
              </h3>
              <p className="text-sm text-muted-foreground">
                The bucket browser now supports uploading entire folder trees, preserving the full relative path
                as the S3 key prefix — just like AWS S3. Drag and drop a folder (uses{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">webkitGetAsEntry()</code>,
                works in all browsers, no confirmation dialogs) or use the Browse Folder button via the File
                System Access API (Chrome/Edge). Upload modal redesigned with Files/Folder tabs, styled drag zone,
                and collapsible file preview — the modal no longer grows downward after selection.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: POST Presigned URLs (HTML Form Upload)
              </h3>
              <p className="text-sm text-muted-foreground">
                Browsers can now upload files directly to a bucket via HTML{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">&lt;form enctype="multipart/form-data"&gt;</code>{' '}
                using S3-compatible POST policy signatures (V4 and V2). The server validates policy expiration,
                HMAC signature, bucket/key/content-type conditions, starts-with prefix conditions, and
                content-length-range constraints. Supports{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">success_action_redirect</code>,{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">success_action_status</code>,
                and <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">x-amz-meta-*</code> form fields.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: Bucket Notifications Dispatched as Webhooks
              </h3>
              <p className="text-sm text-muted-foreground">
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutBucketNotification</code> and{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">GetBucketNotification</code> were previously
                no-ops. Notification configs are now persisted and evaluated after every mutating object operation
                (PutObject, DeleteObject, CopyObject, CompleteMultipartUpload). SNS/SQS/Lambda ARN values are
                treated as webhook HTTP endpoints (same approach as MinIO). SSRF protection blocks internal network
                targets.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: Lifecycle Expiration and AbortIncompleteMultipartUpload Now Executed
              </h3>
              <p className="text-sm text-muted-foreground">
                Lifecycle rules with{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Expiration.Days</code> /
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Expiration.Date</code> and{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">AbortIncompleteMultipartUpload</code>{' '}
                were parsed and stored but the background worker never evaluated them. Both are now fully executed:
                expired objects are deleted (or a delete marker created on versioned buckets), and stale multipart
                uploads past{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">DaysAfterInitiation</code> are aborted.
                Also, per-bucket CORS rules are now enforced on actual requests (previously stored but ignored).
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                S3: Multipart ETag Now Spec-Compliant
              </h3>
              <p className="text-sm text-muted-foreground">
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">CompleteMultipartUpload</code> previously
                used only the MD5 of the concatenated part ETag strings. The AWS S3 spec requires{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">hex(MD5(raw_MD5_part1 ‖ … ‖ raw_MD5_partN))-N</code>{' '}
                using the raw binary digests. Now spec-compliant, enabling clients like{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">aws s3 sync --checksum</code> to correctly
                verify uploaded multipart objects.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Veeam B&R Full Compatibility
              </h3>
              <p className="text-sm text-muted-foreground">
                Multiple Veeam-specific fixes: <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">HEAD /</code>{' '}
                now returns 200 (Veeam verifies the endpoint before any operation);
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">x-amz-bucket-region</code> header added
                to HeadBucket and GetBucketLocation; Object Lock default retention made optional (Veeam sets
                per-object retention and rejects buckets with a pre-set default); HeadObject and PutObjectRetention
                with <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">?versionId</code> returned 404 — now
                resolved; SOSAPI <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">capacity.xml</code> reported
                0 bytes for tenants without quota — now falls back to real disk usage.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Object Lock: Critical Correctness Fixes
              </h3>
              <p className="text-sm text-muted-foreground">
                Enabling Object Lock on a bucket now automatically enables versioning (AWS S3 requirement).
                Legal hold and retention metadata is now stored at per-version keys, not only the latest-version
                key — previously it was possible to delete a locked version because the lock was stored in the
                wrong place.{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">PutObjectLockConfiguration</code> with no
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">&lt;Rule&gt;</code> element now clears the
                bucket-level default retention rule (previously returned 400 MalformedXML).
              </p>
            </div>

            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [CRITICAL / HIGH] Security: SSRF, Open Redirect, Webhook URL Validation
              </h3>
              <p className="text-sm text-muted-foreground">
                BUG-25 (CRITICAL): webhook notification delivery used{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">http.DefaultClient</code> with no
                restrictions — an attacker with bucket-owner access could reach internal services (e.g.
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">169.254.169.254</code>). Fixed with a
                custom SSRF-blocking dialer that rejects private, loopback, and link-local ranges. BUG-26 (HIGH):
                webhook ARN values were never validated as HTTP URLs — now enforced. BUG-27 (MEDIUM): open redirect
                via <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">success_action_redirect</code> allowed
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">javascript:</code> and{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">data:</code> URLs — now restricted to
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">http(s)://</code> only.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Real-Time Throughput Metrics Fixed
              </h3>
              <p className="text-sm text-muted-foreground">
                The dashboard throughput cards (requests/s, bytes/s, objects/s) always showed zero because
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">RecordThroughput()</code> was defined
                but never called. Fixed by invoking it inside{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">TracingMiddleware</code> after every request,
                tracking upload bytes, response bytes written, and per-operation object counters.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Auth Session Fixes: Refresh Token After 2FA and OAuth Login
              </h3>
              <p className="text-sm text-muted-foreground">
                Sessions expired after 15 minutes following a 2FA or OAuth/SSO login because the refresh token
                returned by the server was silently discarded by the frontend. Both paths now correctly store the
                refresh token, giving sessions the full expiry lifetime. Also fixed: audit log export now fetches
                all pages (previously only the visible page), stats cards show accurate totals, and CSV timestamps
                no longer split across columns due to locale comma formatting.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Cluster: Deduplicated Bucket List for Replicated Buckets
              </h3>
              <p className="text-sm text-muted-foreground">
                When a bucket was replicated across cluster nodes, both the S3 API and the web console listed
                it once per node. Users could mistake replicas for duplicates and delete them.{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">BucketAggregator.ListAllBuckets</code>{' '}
                now deduplicates by <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">(TenantID, Name)</code>,
                returning one canonical entry per logical bucket, preferring the local node.
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
