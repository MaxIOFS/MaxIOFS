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
                  <p className="text-sm font-semibold text-green-600 dark:text-green-400">{t('beta')}</p>
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
            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [CRITICAL] AES-256-CTR → AES-256-GCM Authenticated Encryption
              </h3>
              <p className="text-sm text-muted-foreground">
                Replaced AES-256-CTR with AES-256-GCM for all object encryption. CTR mode provides no authentication
                tag — a malicious actor with write access to the data directory could silently corrupt or tamper with
                ciphertext without detection. GCM adds a 128-bit authentication tag per 64 KB chunk; any tampered chunk
                is detected and rejected on read. Objects written in the legacy CTR format are decrypted transparently
                on first access (backward-compatible). New writes always use GCM.
              </p>
            </div>

            <div className="border-l-4 border-red-600 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [CRITICAL] Cluster CA Private Key No Longer Transmitted on Node Join
              </h3>
              <p className="text-sm text-muted-foreground">
                Previously, the cluster CA private key was sent in plaintext over the network when a node joined the
                cluster — anyone intercepting the join request could impersonate any node indefinitely. Fixed with a
                proper CSR flow: the joining node generates its own key pair locally, sends only a Certificate Signing
                Request (CSR) to the leader, and receives back a signed certificate. The CA private key never leaves
                the leader node.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [HIGH] 6 Cluster Admin Handlers Missing Authorization Check
              </h3>
              <p className="text-sm text-muted-foreground">
                Six cluster management endpoints (node removal, replication rule CRUD, cluster token retrieval) were
                missing the <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">isGlobalAdmin</code> check,
                allowing any authenticated user to perform privileged cluster operations. All six handlers now enforce
                global admin authorization before processing the request.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [HIGH] SSRF Protection on Webhooks, HTTP Logging & Replication
              </h3>
              <p className="text-sm text-muted-foreground">
                Webhook URLs, HTTP log targets, and replication endpoint URLs were forwarded without validation,
                allowing Server-Side Request Forgery (SSRF) attacks to scan internal networks, reach cloud metadata
                endpoints (e.g. <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">169.254.169.254</code>),
                or exfiltrate credentials. All three entry points now enforce an allowlist-based SSRF guard that
                blocks private, loopback, link-local, and reserved IP ranges.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [HIGH] HMAC Nonce: time.Now().UnixNano() → crypto/rand
              </h3>
              <p className="text-sm text-muted-foreground">
                The HMAC-SHA256 nonce used for inter-node authentication was generated with{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">time.Now().UnixNano()</code>,
                which is predictable — an attacker observing a few requests could predict future nonces and replay
                signed messages. Replaced with <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">crypto/rand</code> (128-bit),
                making nonces cryptographically unpredictable.
              </p>
            </div>

            <div className="border-l-4 border-yellow-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                [MEDIUM] Auth Cookies: Secure + SameSite=Strict; OAuth CSRF; CORS Allowlist
              </h3>
              <p className="text-sm text-muted-foreground">
                Auth cookies now set <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Secure</code> and{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">SameSite=Strict</code> to prevent session
                hijacking over HTTP and CSRF via cross-site requests. OAuth2 login now validates the{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">state</code> parameter against a
                signed cookie to prevent CSRF in the OAuth flow. The CORS configuration was tightened from a wildcard
                allowlist to explicit trusted origins only.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-foreground mb-1">
                Frontend Bundle −45% via React.lazy Code Splitting
              </h3>
              <p className="text-sm text-muted-foreground">
                The React application was previously bundled as a single monolithic chunk (1003 kB). All page
                components are now loaded lazily with{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">React.lazy</code> +{' '}
                <code className="font-mono text-xs bg-gray-100 dark:bg-gray-700 px-1 rounded">Suspense</code>, reducing the initial
                bundle to ~550 kB (−45%). Users only download the code for the pages they visit, improving first
                load time especially on slower connections.
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
