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
            New Features in v0.7.0-beta
          </h2>
          <div className="space-y-4">
            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Bucket Inventory System - Automated Report Generation
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Complete S3-compatible inventory system with automated periodic report generation (daily/weekly schedules). Supports CSV and
                JSON formats with 12 configurable fields including size, ETag, storage class, and encryption status. Includes REST API
                endpoints for configuration management, frontend UI with inventory tab in bucket settings, and cluster migration integration.
                11 comprehensive tests with 100% coverage ensure reliability.
              </p>
            </div>

            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Database Migration System - Schema Versioning Framework
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Comprehensive database schema versioning with automatic migration execution on startup. Features 8 historical migrations
                (v0.1.0 to v0.6.2), version tracking with schema_version table, and transaction-based migrations for data integrity.
                Supports incremental migrations to specific versions and prevents accidental downgrades. 18 comprehensive tests validate
                all migration scenarios with 100% pass rate.
              </p>
            </div>

            <div className="border-l-4 border-purple-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Performance Profiling & Benchmarking Infrastructure
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Production-ready performance monitoring with 36 Go benchmarks covering storage operations (12 tests), encryption (13 tests),
                and authentication. CI/CD integration executes benchmarks in nightly builds with results published to S3. Cross-platform
                Makefile targets (make bench, make bench-profile) support local profiling. Added pprof endpoints (/debug/pprof/*) for live
                production profiling with admin-only access.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Metrics Test Suite Expansion - 102 New Test Functions
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Dramatically improved monitoring test coverage from 25.8% to 36.2% (+10.4 points, +40.3% improvement). Added 102 test
                functions including 28 system metrics tests (CPU, memory, disk, requests), 17 collector tests (metrics collection, health
                checks), 29 SQLite history tests, and 28 BadgerDB history tests. 45 tests currently active with 57 pending database
                refactoring for test isolation.
              </p>
            </div>

            <div className="border-l-4 border-red-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                CI/CD Improvements - RPM Package Generation
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Enhanced build pipeline with RPM package generation for RHEL/CentOS/Fedora distributions. Docker-based builds using Rocky
                Linux 9 produce both AMD64 (x86_64) and ARM64 (aarch64) packages. Fixed permission issues in artifact preparation with sudo
                commands for Docker-created directories. Automated benchmark execution integrated into nightly builds with results uploaded
                to S3.
              </p>
            </div>

            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                AWS-Compatible Access Key Format
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Access keys now follow AWS standard format with AKIA prefix for access key IDs (20 characters total) and 40-character
                base64-encoded secret keys. New functions generateAccessKeyID() and generateSecretAccessKey() ensure compatibility with AWS
                tools and SDKs. Fully backward compatible with existing access keys - only new keys use the updated format.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Dependencies Cleanup & Code Quality
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Removed unused github.com/mattn/go-sqlite3 dependency identified by go mod tidy, reducing build complexity. Deleted
                .env.example file that wasn't loaded by the application (no godotenv integration), preventing developer confusion about
                configuration. Application continues to support environment variables via viper's AutomaticEnv() and command-line flags.
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
              <span>© 2024-2026 MaxIOFS</span>
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
