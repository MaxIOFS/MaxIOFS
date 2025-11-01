import React from 'react';
import { Card } from '@/components/ui/Card';
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
  Layers
} from 'lucide-react';

export default function AboutPage() {
  const version = '0.3.0-beta';
  const buildDate = new Date().toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });

  // Get base path from window (injected by backend based on public_console_url)
  const basePath = ((window as any).BASE_PATH || '/').replace(/\/$/, '');

  return (
    <div className="space-y-6">
      {/* Header with Logo */}
      <div className="flex flex-col items-center justify-center text-center space-y-4">
        <div className="flex justify-center px-12 py-8 bg-gradient-to-br from-gray-50 to-gray-100 dark:from-gray-800 dark:to-gray-900 rounded-2xl">
          <img
            src={`${basePath}/assets/img/logo.png`}
            alt="MaxIOFS Logo"
            className="h-auto object-contain dark:opacity-90 dark:brightness-0 dark:invert"
            style={{ width: '22rem', maxHeight: '200px' }}
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
                  web interface. Features full multi-tenancy support, BadgerDB-powered metadata storage, and
                  comprehensive S3 API compatibility with 40+ operations including bulk operations, object lock,
                  and advanced bucket management.
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
                  with complete multi-tenant support, high-performance metadata storage using BadgerDB,
                  and production-ready security features.
                </p>
              </div>

              <div className="space-y-3 pt-4 border-t border-gray-200 dark:border-gray-700">
                <a
                  href="mailto:aluisco2005@gmail.com"
                  className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Mail className="h-4 w-4" />
                  <span>aluisco2005@gmail.com</span>
                </a>
                <a
                  href="https://github.com/aluisco/MaxIOFS"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center space-x-3 text-sm text-gray-600 dark:text-gray-400 hover:text-brand-600 dark:hover:text-brand-400 transition-colors"
                >
                  <Github className="h-4 w-4" />
                  <span>github.com/aluisco/MaxIOFS</span>
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
              description="Complete tenant isolation with configurable quotas, cascading deletes, deletion validation, and global admin cross-tenant visibility"
            />
            <FeatureCard
              icon={Lock}
              title="Security"
              description="JWT authentication, AWS Signature v2/v4, bcrypt password hashing, rate limiting, account lockout, and granular access control"
            />
            <FeatureCard
              icon={Zap}
              title="High Performance"
              description="BadgerDB v4 metadata store with transaction retry logic, metadata-first deletion, and stress-tested with 7000+ objects using MinIO Warp"
            />
            <FeatureCard
              icon={Package}
              title="Single Binary"
              description="Packaged as a single executable with embedded Next.js frontend, no external dependencies, and easy deployment"
            />
            <FeatureCard
              icon={Code}
              title="Modern UI"
              description="React 19 + TypeScript interface with dark mode support, real-time metrics, responsive design, and comprehensive management features"
            />
            <FeatureCard
              icon={FileJson}
              title="Advanced S3 Features"
              description="Object Lock (WORM), Bucket Versioning, Bucket Policies (JSON), CORS, Lifecycle rules, Object Tagging, and Object ACLs"
            />
            <FeatureCard
              icon={RefreshCw}
              title="Bulk Operations"
              description="DeleteObjects API (up to 1000 objects), sequential processing to avoid conflicts, complete metadata consistency"
            />
            <FeatureCard
              icon={Layers}
              title="Dual Storage"
              description="BadgerDB v4 for high-performance object metadata, SQLite for authentication, filesystem for object storage with atomic operations"
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
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  Go 1.21+
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  Gorilla Mux (Routing)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  BadgerDB v4 (Object Metadata)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  SQLite (Authentication)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  Filesystem Storage Backend
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
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
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  React 19 + TypeScript
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  Vite (Build Tool)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  TailwindCSS 3
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  TanStack Query (React Query)
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  React Router v6
                </li>
                <li className="flex items-center">
                  <span className="w-2 h-2 bg-brand-600 rounded-full mr-3"></span>
                  SweetAlert2 (Notifications)
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
            New Features in v0.3.0-beta (Beta Release)
          </h2>
          <div className="space-y-4">
            <div className="border-l-4 border-green-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                🎉 Beta Achievement - 97% S3 Compatibility
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                MaxIOFS has achieved Beta status with 97% S3 compatibility (95/98 tests passed). All core S3 operations
                fully tested and validated with AWS CLI. Zero critical bugs in core functionality.
              </p>
            </div>

            <div className="border-l-4 border-blue-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Bucket Tagging Visual UI
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Complete visual tag manager with key-value pairs interface. Add, edit, and delete tags without XML editing.
                Console API integration with automatic XML generation for S3 compatibility. Real-time updates with user-friendly UI.
              </p>
            </div>

            <div className="border-l-4 border-purple-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                CORS Visual Editor
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Dual-mode interface (Visual + XML) for CORS configuration. Visual rule builder with forms for origins, methods,
                headers, and expiration. No XML knowledge required for basic configurations. Multiple CORS rules support.
              </p>
            </div>

            <div className="border-l-4 border-orange-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Comprehensive S3 Testing Complete
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                All bucket operations (10/10), object operations (10/10), and multipart uploads (6/6) tested at 100%.
                Validated with AWS CLI: 50MB @ ~126 MiB/s, 100MB @ ~105 MiB/s. Batch operations, versioning, and
                lifecycle policies all working correctly.
              </p>
            </div>

            <div className="border-l-4 border-yellow-500 pl-4">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-white mb-1">
                Production-Ready Features
              </h3>
              <p className="text-sm text-gray-600 dark:text-gray-400">
                Complete bucket policy implementation with UTF-8 BOM handling. Object versioning with delete markers.
                Range requests, batch delete, and all bucket configurations (Tags, CORS, Policy, Lifecycle) validated.
                Suitable for testing, development, and staging environments.
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
        <div className="flex items-center justify-center w-8 h-8 rounded-lg bg-brand-100 dark:bg-brand-900/30">
          <Icon className="h-4 w-4 text-brand-600 dark:text-brand-400" />
        </div>
        <h3 className="font-semibold text-gray-900 dark:text-white">{title}</h3>
      </div>
      <p className="text-sm text-gray-600 dark:text-gray-400 leading-relaxed">
        {description}
      </p>
    </div>
  );
}
