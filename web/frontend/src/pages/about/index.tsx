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
                  <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg>
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
                  Go 1.26+
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Gorilla Mux (Routing)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-gray-600 dark:bg-gray-400 rounded-full mr-3"></span>
                  Pebble v2.1 (Object Metadata, crash-safe WAL)
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
                  Vite 8 (Build Tool)
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

            <div className="border-l-4 border-blue-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                New Storage Layout
              </h3>
              <p className="text-sm text-muted-foreground">
                An object is now one file named by a digest of its key, and buckets sit at the
                storage root. No directory is built out of the parts of a key, so a key can be
                anything S3 allows: <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">2026</code>{' '}
                and <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">2026/</code>{' '}
                are two objects, as are two keys that differ only in case. An existing
                installation is migrated on the first start of this release.
              </p>
            </div>

            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Two Ways Objects Could Be Destroyed
              </h3>
              <p className="text-sm text-muted-foreground">
                Creating a folder whose name matched an existing object replaced that object, and
                listings kept reporting its original size while a download returned nothing. On
                Windows and macOS two keys differing only in case shared one file, so the second
                upload overwrote the first and a download returned the other key's bytes. The
                layout makes both impossible.
              </p>
            </div>

            <div className="border-l-4 border-purple-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Folders Are Not Invented
              </h3>
              <p className="text-sm text-muted-foreground">
                Uploading{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">logs/2026/app.log</code>{' '}
                used to write an object for every level above it. Those folders showed up in a
                version listing as objects nobody created. Only the key you upload is stored, and
                the hierarchy is derived from the keys, the way S3 does it.
              </p>
            </div>

            <div className="border-l-4 border-amber-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Deletions Finish
              </h3>
              <p className="text-sm text-muted-foreground">
                Deleting a bucket or cleaning up a multipart upload used to drop the record even
                when the files could not be removed, leaving objects on disk that nothing could
                reach. The record now outlives the files, and a removal that could not finish is
                completed at the next start.
              </p>
            </div>

            <div className="border-l-4 border-emerald-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Interrupted Writes Are Undone or Confirmed, Not Left in Between
              </h3>
              <p className="text-sm text-muted-foreground">
                Killing the process between storing an object's bytes and committing its index
                entry — an overwrite, a multipart completion, a part replacement — used to leave
                a GET announcing one size and delivering another. At the next start, before the
                server serves anything, each interrupted write is either undone or confirmed
                against the index. Object, version and part commits now wait for that index entry
                to be durable before the previous copy is discarded.
              </p>
            </div>

            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                A Failed Multipart Completion No Longer Keeps the New Bytes
              </h3>
              <p className="text-sm text-muted-foreground">
                If combining the parts of a multipart upload succeeded but a later step failed —
                out of disk space, a device error — the object that was being replaced is
                restored instead of being left overwritten with no matching metadata. Replacing
                an already-uploaded part is protected the same way: a failed part upload can no
                longer destroy the part it was replacing.
              </p>
            </div>

            <div className="border-l-4 border-purple-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">maxiofs reconcile</code>{' '}
                Repairs, Not Just Restores
              </h3>
              <p className="text-sm text-muted-foreground">
                Beyond rebuilding index entries that are missing entirely, reconcile now corrects
                an entry left describing bytes an interrupted overwrite already replaced, reading
                the object's true size and ETag from its own sidecar.
              </p>
            </div>

            <p className="text-sm text-muted-foreground pt-2">
              Earlier releases are listed in the{' '}
              <a
                href="https://github.com/MaxioFS/MaxioFS/blob/main/CHANGELOG.md"
                target="_blank"
                rel="noopener noreferrer"
                className="text-brand-600 dark:text-brand-400 hover:underline"
              >
                CHANGELOG
              </a>.
            </p>

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
