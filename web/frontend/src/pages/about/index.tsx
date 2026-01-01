import React from 'react';
import { Card } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import { useQuery } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import type { ServerConfig } from '@/types';
import {
  Code,
  Github,
  Mail,
  Globe,
  Shield,
  Zap,
  Database,
  Lock,
  Package,
  Award,
  Heart,
  Send,
  FileJson,
  RefreshCw,
  Layers,
  Network,
  Copy,
  BarChart3
} from 'lucide-react';

export default function AboutPage() {
  const { data: config, isLoading } = useQuery<ServerConfig>({
    queryKey: ['serverConfig'],
    queryFn: APIClient.getServerConfig,
  });

  // Get base path from window (injected by backend based on public_console_url)
  const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');

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
        <div className="flex justify-center px-12 py-8 bg-gradient-to-br from-gray-50 to-gray-100 dark:from-gray-800 dark:to-gray-900 rounded-2xl">
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
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white">MaxIOFS</h2>
                <p className="text-sm text-gray-500 dark:text-gray-400">Version {version}</p>
              </div>
            </div>

            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">
                  Description
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-400 leading-relaxed">
                  MaxIOFS is a high-performance S3-compatible object storage system built with Go and React.
                  Designed to be simple, portable, and deployable as a single binary with an embedded modern
                  web interface. Features full multi-tenancy support, multi-node cluster with HA replication,
                  BadgerDB-powered metadata storage, and comprehensive S3 API compatibility with 40+ operations
                  including bulk operations, object lock, and advanced bucket management.
                </p>
              </div>

              <div className="grid grid-cols-2 gap-4 pt-4 border-t border-gray-200 dark:border-gray-700">
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">Version</p>
                  <p className="text-sm font-semibold text-gray-900 dark:text-white">{version}</p>
                </div>
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">Build Date</p>
                  <p className="text-sm font-semibold text-gray-900 dark:text-white">{buildDate}</p>
                </div>
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">License</p>
                  <p className="text-sm font-semibold text-gray-900 dark:text-white">MIT</p>
                </div>
                <div>
                  <p className="text-xs text-gray-500 dark:text-gray-400">Status</p>
                  <p className="text-sm font-semibold text-green-600 dark:text-green-400">Beta</p>
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
                <h2 className="text-xl font-bold text-gray-900 dark:text-white">Aluisco Ricardo</h2>
                <p className="text-sm text-gray-500 dark:text-gray-400">Lead Developer</p>
              </div>
            </div>

            <div className="space-y-4">
              <div>
                <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3">
                  Main Developer
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-400 leading-relaxed">
                  Project entirely developed by Aluisco Ricardo. MaxIOFS was born as a solution
                  to provide enterprise-grade S3-compatible object storage in a simple and efficient way,
                  with complete multi-tenant support, multi-node cluster for high availability,
                  high-performance metadata storage using BadgerDB, and production-ready security features.
                </p>
              </div>

              <div className="space-y-3 pt-4 border-t border-gray-200 dark:border-gray-700">
                <a
                  href="mailto:aluisco@maxiofs.com"
                  className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Mail className="h-4 w-4" />
                  <span>aluisco@maxiofs.com</span>
                </a>
                <a
                  href="https://github.com/MaxioFS/MaxioFS"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Github className="h-4 w-4" />
                  <span>github.com/MaxioFS/MaxioFS</span>
                </a>
                <a
                href="https://t.me/aluisco"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-[#0088cc] transition-colors"
                >
                <Send className="h-4 w-4 text-[#0088cc] rotate-45" />
                <span>t.me/aluisco</span>
                </a>
                <a
                  href="https://maxiofs.com"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
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
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-6">
            Key Features
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            <FeatureCard
              icon={Database}
              title="S3 Compatible"
              description="Full S3 API implementation with 40+ operations including multipart uploads, presigned URLs, and bulk operations (up to 1000 objects)"
            />
            <FeatureCard
              icon={Shield}
              title="Multi-Tenant"
              description="Complete tenant isolation with configurable quotas, cascading deletes, and global admin cross-tenant visibility"
            />
            <FeatureCard
              icon={Lock}
              title="Security & Encryption"
              description="AES-256-CTR encryption at rest, 2FA authentication, AWS Signature v2/v4, audit logging (20+ events), compliance-ready (GDPR, SOC 2, HIPAA)"
            />
            <FeatureCard
              icon={Zap}
              title="High Performance"
              description="BadgerDB v4 metadata store with transaction retry logic, stress-tested with 7000+ objects using MinIO Warp"
            />
            <FeatureCard
              icon={Package}
              title="Single Binary"
              description="Packaged as a single executable with embedded React frontend, no external dependencies, easy deployment"
            />
            <FeatureCard
              icon={Code}
              title="Modern UI"
              description="React 19 + TypeScript with dark mode, real-time metrics, responsive design, and comprehensive management features"
            />
            <FeatureCard
              icon={FileJson}
              title="Advanced S3 Features"
              description="Object Lock (WORM), Versioning, Bucket Policies (JSON), CORS, Lifecycle rules, Object Tagging, and ACLs"
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
              icon={RefreshCw}
              title="Bulk Operations"
              description="DeleteObjects API (up to 1000 objects), sequential processing to avoid conflicts, complete metadata consistency"
            />
            <FeatureCard
              icon={Layers}
              title="Dual Storage"
              description="BadgerDB v4 for object metadata, SQLite for authentication/audit/cluster, filesystem for objects with atomic operations"
            />
            <FeatureCard
              icon={BarChart3}
              title="Monitoring & Observability"
              description="Prometheus metrics endpoint, Grafana dashboards, performance SLOs, real-time latency tracking (p50/p95/p99), and alerting"
            />
          </div>
        </div>
      </Card>

      {/* Technology Stack */}
      <Card>
        <div className="p-6">
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-6">
            Technology Stack
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div>
              <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center">
                <Code className="h-4 w-4 mr-2" />
                Backend
              </h3>
              <ul className="space-y-2 text-sm text-gray-600 dark:text-gray-400">
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
                  BadgerDB v4 (Object Metadata)
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
              <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center">
                <Globe className="h-4 w-4 mr-2" />
                Frontend
              </h3>
              <ul className="space-y-2 text-sm text-gray-600 dark:text-gray-400">
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
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-6">
            New Features in v0.6.2-beta
          </h2>
          <div className="space-y-4">
            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Console API Documentation Fixed (GitHub Issue #2)
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Corrected all Console API endpoint documentation from /api/ to /api/v1/ prefix. Added GET /api/v1/ root endpoint that
                returns API information, available endpoints, and server version in JSON format. Updated docs/API.md, docs/CLUSTER.md,
                and docs/MULTI_TENANCY.md with accurate routes and examples.
              </p>
            </div>

            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                MIT LICENSE Added (GitHub Issue #3)
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Added MIT License file to repository root. Resolves missing LICENSE file referenced in README.md with complete
                copyright notice (2024-2026) and standard MIT License terms.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                S3 Authentication Test Suite - Complete Coverage
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Added 13 comprehensive test functions with 80+ test cases for S3 authentication. Improved auth module coverage from
                30.2% to 47.1% (+56% relative improvement). Tests cover AWS Signature V4/V2, JWT Bearer tokens, timestamp validation,
                S3 action extraction, ARN generation, and complete authentication flow. Fixed critical bugs in SigV4 authorization parsing,
                timestamp UTC timezone handling, and ARN trailing slash preservation.
              </p>
            </div>

            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                S3 API Test Suite Expansion - 42 New Tests
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Improved S3 API test coverage from 30.9% to 45.7% (+48% relative improvement). Added tests for advanced S3 features
                (multipart uploads, bucket/object ACLs, Object Lock, versioning, tagging), AWS chunked encoding (0% to 100% coverage),
                and error cases for HeadObject, DeleteObject, and PutObject operations. All 42 tests passing with complete XML/HTTP
                validation.
              </p>
            </div>

            <div className="border-l-4 border-purple-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Server Integration Test Suite - Lifecycle Tests
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Added 4 comprehensive server lifecycle integration tests. Improved server package coverage from 12.7% to 18.3% (+44%
                relative improvement). Tests cover server initialization with manager validation, version information handling, graceful
                shutdown with timeout, and start/stop cycle resilience with resource cleanup.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Frontend Dependencies Upgrade - Major Performance Gains
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Migrated Tailwind CSS from v3 to v4 with new Oxide engine (10x faster build times). Upgraded Vitest from v3 to v4 with
                59% faster test execution (21.74s to 9.00s). Updated lucide-react icon library and autoprefixer. All 64 frontend tests
                passing with zero breaking changes. Updated build requirements to Node.js 24+ and Go 1.25+ for latest security patches.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                SweetAlert2 Removed - Custom Modal System
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Completely removed sweetalert2 dependency (reduced bundle size by ~65KB). Created custom modal components using existing
                Modal UI component with better Tailwind CSS integration, consistent design system, and improved dark mode support. Migrated
                all pages using confirmations/alerts to new modal system.
              </p>
            </div>

            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Metrics Dashboard Complete Redesign
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Reorganized metrics page from 3 to 5 specialized tabs: Overview, System, Storage, API & Requests, and Performance. Added
                time range selector for historical data filtering (Real-time, 1H, 6H, 24H, 7D, 30D, 1Y). All charts now show temporal
                evolution with MetricLineChart component. Eliminated duplicate information with unique content per tab. Improved visual
                consistency with standardized MetricCard components across all metrics pages.
              </p>
            </div>

            <div className="border-l-4 border-red-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                CRITICAL FIX: Debian Package Configuration Preservation
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Fixed severe bug where Debian package upgrades could overwrite /etc/maxiofs/config.yaml, causing permanent data loss of
                all encrypted objects. Package upgrades now preserve existing config.yaml completely untouched while updating
                config.example.yaml with latest template. Smart logic creates config.yaml only on first installation. Prevents encryption
                key loss that would make all encrypted objects permanently inaccessible.
              </p>
            </div>

            <div className="border-l-4 border-purple-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Docker Infrastructure - Complete Rewrite
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Modernized docker-compose.yaml with 74% reduction (1040 to 285 lines). Added Docker profiles for conditional service
                startup: monitoring (Prometheus + Grafana) and cluster (3-node HA). Created organized docker/ directory with externalized
                configurations. Unified Grafana dashboard with 14 panels in 3 sections, auto-provisioning, and 14 performance alert rules.
                New Makefile commands (docker-monitoring, docker-cluster, docker-cluster-monitoring). Comprehensive Docker documentation
                in English.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Documentation & UI Corrections
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Fixed incorrect "Next.js" references to "React" throughout README, CHANGELOG, CLI help, package descriptions, and frontend
                locales. Corrected Tailwind v4 modal backdrop opacity syntax across 5 files (9 modal backdrops). Removed unused Next.js
                server code (118 lines). Fixed .env.example by removing obsolete Next.js environment variables. All tests passing (64
                frontend, 531 backend with 100% success rate).
              </p>
            </div>
          </div>
        </div>
      </Card>

      {/* Footer */}
      <Card>
        <div className="p-6">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2 text-sm text-gray-600 dark:text-gray-400">
              <Heart className="h-4 w-4 text-red-500" />
              <span>Developed with passion by Aluisco Ricardo</span>
            </div>
            <div className="flex items-center space-x-2 text-sm text-gray-500 dark:text-gray-400">
              <Award className="h-4 w-4" />
              <span>© 2024-2025 MaxIOFS</span>
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
          <Icon className="h-4 w-4 text-gray-600 dark:text-gray-400" />
        </div>
        <h3 className="font-semibold text-gray-900 dark:text-white">{title}</h3>
      </div>
      <p className="text-sm text-gray-600 dark:text-gray-400 leading-relaxed">
        {description}
      </p>
    </div>
  );
}
