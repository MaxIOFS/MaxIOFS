package server

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/api"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/maxiofs/maxiofs/internal/lifecycle"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/maxiofs/maxiofs/internal/middleware"
	"github.com/maxiofs/maxiofs/internal/notifications"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/logging"
	"github.com/maxiofs/maxiofs/internal/replication"
	"github.com/maxiofs/maxiofs/internal/settings"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Server represents the MaxIOFS server
type Server struct {
	config          *config.Config
	httpServer      *http.Server
	consoleServer   *http.Server
	storageBackend  storage.Backend
	metadataStore   metadata.Store
	bucketManager   bucket.Manager
	objectManager   object.Manager
	authManager     auth.Manager
	db              *sql.DB
	auditManager    *audit.Manager
	metricsManager      metrics.Manager
	settingsManager     *settings.Manager
	loggingManager      *logging.Manager
	shareManager        share.Manager
	notificationManager *notifications.Manager
	replicationManager      *replication.Manager
	clusterManager          *cluster.Manager
	clusterRouter           *cluster.Router
	clusterReplicationMgr   *cluster.ClusterReplicationManager
	bucketAggregator        *cluster.BucketAggregator
	quotaAggregator         *cluster.QuotaAggregator
	rateLimiter             *cluster.RateLimiter
	tenantSyncMgr           *cluster.TenantSyncManager
	userSyncMgr             *cluster.UserSyncManager
	accessKeySyncMgr        *cluster.AccessKeySyncManager
	bucketPermissionSyncMgr *cluster.BucketPermissionSyncManager
	notificationHub         *NotificationHub
	systemMetrics           *metrics.SystemMetricsTracker
	lifecycleWorker         *lifecycle.Worker
	inventoryManager        *inventory.Manager
	inventoryWorker         *inventory.Worker
	startTime               time.Time // Server start time for uptime calculation
	version                 string    // Server version
	commit                  string    // Git commit hash
	buildDate               string    // Build date
}

// New creates a new MaxIOFS server
func New(cfg *config.Config) (*Server, error) {
	// Initialize storage backend
	storageBackend, err := storage.NewBackend(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("failed to create storage backend: %w", err)
	}

	// Initialize metadata store (BadgerDB)
	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           cfg.DataDir,
		SyncWrites:        false, // Async for performance
		CompactionEnabled: true,  // Auto GC
		Logger:            logrus.StandardLogger(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata store: %w", err)
	}

	// Initialize managers
	bucketManager := bucket.NewManager(storageBackend, metadataStore)
	objectManager := object.NewManager(storageBackend, metadataStore, cfg.Storage)

	// Connect object manager to bucket manager for metrics updates
	if om, ok := objectManager.(interface {
		SetBucketManager(interface {
			IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
			DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
		})
	}); ok {
		om.SetBucketManager(bucketManager)
	}

	authManager := auth.NewManager(cfg.Auth, cfg.DataDir)

	// Initialize settings manager (uses same SQLite DB as auth)
	db, ok := authManager.GetDB().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("failed to get SQLite database from auth manager")
	}
	settingsManager, err := settings.NewManager(db, logrus.StandardLogger())
	if err != nil {
		return nil, fmt.Errorf("failed to create settings manager: %w", err)
	}

	// Initialize logging manager
	loggingManager := logging.NewManager(logrus.StandardLogger())
	loggingManager.SetSettingsManager(settingsManager)

	// Initialize audit manager
	var auditManager *audit.Manager
	if cfg.Audit.Enable {
		auditStore, err := audit.NewSQLiteStore(cfg.Audit.DBPath, logrus.StandardLogger())
		if err != nil {
			return nil, fmt.Errorf("failed to create audit store: %w", err)
		}
		auditManager = audit.NewManager(auditStore, logrus.StandardLogger())
	}

	// Connect audit manager to auth manager
	if am, ok := authManager.(interface{ SetAuditManager(*audit.Manager) }); ok && auditManager != nil {
		am.SetAuditManager(auditManager)
	}

	// Connect settings manager to auth manager for dynamic rate limiting
	if am, ok := authManager.(interface {
		SetSettingsManager(interface {
			GetInt(key string) (int, error)
		})
	}); ok {
		am.SetSettingsManager(settingsManager)
	}

	// Connect audit manager to bucket manager
	if bm, ok := bucketManager.(interface{ SetAuditManager(*audit.Manager) }); ok && auditManager != nil {
		bm.SetAuditManager(auditManager)
	}

	// Connect object manager to auth manager for tenant quota updates
	if om, ok := objectManager.(interface {
		SetAuthManager(interface {
			IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
			DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
			CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error
		})
	}); ok {
		om.SetAuthManager(authManager)
	}

	metricsManager := metrics.NewManagerWithStore(cfg.Metrics, cfg.DataDir, metadataStore)

	// Initialize system metrics
	systemMetrics := metrics.NewSystemMetrics(cfg.DataDir)

	// Connect system metrics to metrics manager
	if mm, ok := metricsManager.(interface {
		SetSystemMetrics(*metrics.SystemMetricsTracker)
	}); ok {
		mm.SetSystemMetrics(systemMetrics)
	}

	// Initialize global performance collector
	// Keep last 10,000 samples per operation, 1 hour retention
	metrics.InitGlobalPerformanceCollector(10000, 1*time.Hour)

	// Connect storage metrics provider to metrics manager
	if mm, ok := metricsManager.(interface {
		SetStorageMetricsProvider(metrics.StorageMetricsProvider)
	}); ok {
		mm.SetStorageMetricsProvider(func() (totalBuckets, totalObjects, totalSize int64) {
			// Get storage metrics by listing all buckets
			buckets, err := bucketManager.ListBuckets(context.Background(), "")
			if err != nil {
				return 0, 0, 0
			}

			totalBuckets = int64(len(buckets))
			for _, b := range buckets {
				totalObjects += b.ObjectCount
				totalSize += b.TotalSize
			}
			return
		})
	}

	// Initialize share manager with same database as auth
	shareManager, err := share.NewManagerWithDB(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create share manager: %w", err)
	}

	// Initialize notification manager
	notificationManager := notifications.NewManager(metadataStore)

	// Initialize SSE notification hub
	notificationHub := NewNotificationHub()

	// Initialize lifecycle worker
	lifecycleWorker := lifecycle.NewWorker(bucketManager, objectManager, metadataStore)

	// Initialize inventory manager and worker
	inventoryManager := inventory.NewManager(db)
	inventoryWorker := inventory.NewWorker(inventoryManager, bucketManager, metadataStore, storageBackend)

	// Initialize replication manager
	replicationConfig := replication.ReplicationConfig{
		Enable:          true, // Now enabled with AWS SDK implementation
		WorkerCount:     5,
		QueueSize:       1000,
		BatchSize:       100,
		RetryInterval:   5 * time.Minute,
		MaxRetries:      3,
		CleanupInterval: 24 * time.Hour,
		RetentionDays:   30,
	}
	// Create adapters for replication manager
	objectManagerAdapted := &objectManagerAdapter{mgr: objectManager}
	objectAdapter := replication.NewRealObjectAdapter(objectManagerAdapted)
	bucketLister := &bucketListerAdapter{mgr: objectManager}
	replicationManager, err := replication.NewManager(db, replicationConfig, objectAdapter, objectManagerAdapted, bucketLister)
	if err != nil {
		return nil, fmt.Errorf("failed to create replication manager: %w", err)
	}

	// Initialize cluster schema (uses same SQLite DB as auth and replication)
	if err := cluster.InitSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize cluster schema: %w", err)
	}

	// Initialize cluster replication schema
	if err := cluster.InitReplicationSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize cluster replication schema: %w", err)
	}

	// Initialize cluster manager
	clusterManager := cluster.NewManager(db, cfg.PublicAPIURL)

	// Set storage backend and ACL manager for cluster operations (migrations)
	clusterManager.SetStorage(storageBackend)

	// Get ACL manager from bucket manager
	aclMgrInterface := bucketManager.GetACLManager()
	if aclMgrInterface != nil {
		clusterManager.SetACLManager(aclMgrInterface.(acl.Manager))
	}

	// Get local node ID from cluster config (if cluster is initialized)
	localNodeID := ""
	clusterConfig, err := clusterManager.GetConfig(context.Background())
	if err == nil {
		localNodeID = clusterConfig.NodeID
	}

	// Create adapters for cluster router
	bucketManagerAdapter := &clusterBucketManagerAdapter{mgr: bucketManager}
	replicationManagerAdapter := &clusterReplicationManagerAdapter{mgr: replicationManager}

	// Initialize cluster router with adapters
	clusterRouter := cluster.NewRouter(clusterManager, bucketManagerAdapter, replicationManagerAdapter, localNodeID)

	// Initialize bucket aggregator for cross-node bucket listing
	bucketAggregator := cluster.NewBucketAggregator(clusterManager, bucketManager)

	// Initialize quota aggregator for cross-node quota checking
	quotaAggregator := cluster.NewQuotaAggregator(clusterManager)

	// Initialize rate limiter for internal cluster APIs
	// 100 requests per second per IP, burst of 200
	rateLimiter := cluster.NewRateLimiter(100, 200)
	logrus.Info("Rate limiter initialized for internal cluster APIs (100 req/s, burst: 200)")

	// Connect cluster manager and quota aggregator to auth manager for cluster-aware quota checking
	if am, ok := authManager.(interface {
		SetClusterManager(interface {
			IsClusterEnabled() bool
		})
	}); ok {
		am.SetClusterManager(clusterManager)
	}

	if am, ok := authManager.(interface {
		SetQuotaAggregator(interface {
			GetTenantTotalStorage(ctx context.Context, tenantID string) (int64, error)
		})
	}); ok {
		am.SetQuotaAggregator(quotaAggregator)
	}

	// Initialize tenant synchronization manager
	tenantSyncMgr := cluster.NewTenantSyncManager(db, clusterManager)

	// Initialize user synchronization manager
	userSyncMgr := cluster.NewUserSyncManager(db, clusterManager)

	// Initialize access key synchronization manager
	accessKeySyncMgr := cluster.NewAccessKeySyncManager(db, clusterManager)

	// Initialize bucket permission synchronization manager
	bucketPermissionSyncMgr := cluster.NewBucketPermissionSyncManager(db, clusterManager)

	// Initialize cluster replication manager
	clusterReplicationMgr := cluster.NewClusterReplicationManager(db, clusterManager, objectManager, tenantSyncMgr)

	// Create HTTP servers
	httpServer := &http.Server{
		Addr:         cfg.Listen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	consoleServer := &http.Server{
		Addr:         cfg.ConsoleListen,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	server := &Server{
		config:              cfg,
		httpServer:          httpServer,
		consoleServer:       consoleServer,
		storageBackend:      storageBackend,
		metadataStore:       metadataStore,
		bucketManager:       bucketManager,
		objectManager:       objectManager,
		authManager:         authManager,
		db:                  db,
		auditManager:        auditManager,
		metricsManager:      metricsManager,
		settingsManager:     settingsManager,
		loggingManager:      loggingManager,
		shareManager:        shareManager,
		notificationManager: notificationManager,
		replicationManager:      replicationManager,
		clusterManager:          clusterManager,
		clusterRouter:           clusterRouter,
		clusterReplicationMgr:   clusterReplicationMgr,
		bucketAggregator:        bucketAggregator,
		quotaAggregator:         quotaAggregator,
		rateLimiter:             rateLimiter,
		tenantSyncMgr:           tenantSyncMgr,
		userSyncMgr:             userSyncMgr,
		accessKeySyncMgr:        accessKeySyncMgr,
		bucketPermissionSyncMgr: bucketPermissionSyncMgr,
		notificationHub:         notificationHub,
		systemMetrics:           systemMetrics,
		lifecycleWorker:         lifecycleWorker,
		inventoryManager:        inventoryManager,
		inventoryWorker:         inventoryWorker,
		startTime:               time.Now(), // Record server start time
	}

	// Connect user locked callback to send SSE notifications
	logrus.Info("Setting up user locked callback for SSE notifications")
	authManager.SetUserLockedCallback(func(user *auth.User) {
		// Send notification to SSE clients
		notification := &Notification{
			Type:      "user_locked",
			Message:   fmt.Sprintf("User %s has been locked due to failed login attempts", user.Username),
			Data: map[string]interface{}{
				"userId":   user.ID,
				"username": user.Username,
				"tenantId": user.TenantID,
			},
			Timestamp: time.Now().Unix(),
			TenantID:  user.TenantID,
		}
		logrus.WithFields(logrus.Fields{
			"user_id":   user.ID,
			"username":  user.Username,
			"tenant_id": user.TenantID,
		}).Info("Sending user locked notification to SSE clients")
		server.notificationHub.SendNotification(notification)
	})

	// Setup routes
	if err := server.setupRoutes(); err != nil {
		return nil, fmt.Errorf("failed to setup routes: %w", err)
	}

	return server, nil
}

// SetVersion sets the server version information
func (s *Server) SetVersion(version, commit, date string) {
	s.version = version
	s.commit = commit
	s.buildDate = date
}

// Start starts the MaxIOFS server
func (s *Server) Start(ctx context.Context) error {
	logrus.WithFields(logrus.Fields{
		"api_address":     s.config.Listen,
		"console_address": s.config.ConsoleListen,
		"data_dir":        s.config.DataDir,
	}).Info("Starting MaxIOFS servers")

	// Enable runtime profiling
	runtime.SetBlockProfileRate(1)     // Enable block profiling
	runtime.SetMutexProfileFraction(1) // Enable mutex profiling
	logrus.Info("Runtime profiling enabled (block, mutex)")

	// Start metrics collection
	if s.config.Metrics.Enable {
		s.metricsManager.Start(ctx)
	}

	// Start audit log retention job
	if s.config.Audit.Enable && s.auditManager != nil {
		s.auditManager.StartRetentionJob(ctx, s.config.Audit.RetentionDays)
	}

	// Start lifecycle worker (runs every 1 hour)
	s.lifecycleWorker.Start(ctx, 1*time.Hour)

	// Start inventory worker (runs every 1 hour)
	s.inventoryWorker.Start(ctx, 1*time.Hour)
	logrus.Info("Inventory worker started")

	// Start replication manager
	if s.replicationManager != nil {
		s.replicationManager.Start(ctx)
		logrus.Info("Replication manager started")
	}

	// Start cluster health checker
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		go s.clusterManager.StartHealthChecker(ctx)
		logrus.Info("Cluster health checker started")

		// Start bucket count updater (runs every 30 seconds)
		go s.updateBucketCountPeriodically(ctx, 30*time.Second)
		logrus.Info("Bucket count updater started")

		// Start tenant synchronization manager
		if s.tenantSyncMgr != nil {
			s.tenantSyncMgr.Start(ctx)
			logrus.Info("Tenant synchronization manager started")
		}

		// Start user synchronization manager
		if s.userSyncMgr != nil {
			s.userSyncMgr.Start(ctx)
			logrus.Info("User synchronization manager started")
		}

		// Start access key synchronization manager
		if s.accessKeySyncMgr != nil {
			s.accessKeySyncMgr.Start(ctx)
			logrus.Info("Access key synchronization manager started")
		}

		// Start bucket permission synchronization manager
		if s.bucketPermissionSyncMgr != nil {
			s.bucketPermissionSyncMgr.Start(ctx)
			logrus.Info("Bucket permission synchronization manager started")
		}

		// Start cluster replication manager
		if s.clusterReplicationMgr != nil {
			s.clusterReplicationMgr.Start(ctx)
			logrus.Info("Cluster replication manager started")
		}
	}

	// Start API server
	go func() {
		if err := s.startAPIServer(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("API server error")
		}
	}()

	// Start console server
	go func() {
		if err := s.startConsoleServer(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("Console server error")
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	return s.shutdown()
}

func (s *Server) startAPIServer() error {
	logrus.WithField("address", s.config.Listen).Info("Starting API server")

	if s.config.EnableTLS {
		return s.httpServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	}
	return s.httpServer.ListenAndServe()
}

func (s *Server) startConsoleServer() error {
	logrus.WithField("address", s.config.ConsoleListen).Info("Starting console server")

	if s.config.EnableTLS {
		logrus.Info("Console server using TLS")
		return s.consoleServer.ListenAndServeTLS(s.config.CertFile, s.config.KeyFile)
	}
	return s.consoleServer.ListenAndServe()
}

func (s *Server) shutdown() error {
	logrus.Info("Shutting down servers")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown API server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		logrus.WithError(err).Error("Failed to shutdown API server")
	}

	// Shutdown console server
	if err := s.consoleServer.Shutdown(ctx); err != nil {
		logrus.WithError(err).Error("Failed to shutdown console server")
	}

	// Stop metrics
	if s.metricsManager != nil {
		s.metricsManager.Stop()
	}

	// Stop lifecycle worker
	if s.lifecycleWorker != nil {
		s.lifecycleWorker.Stop()
	}

	// Stop inventory worker
	if s.inventoryWorker != nil {
		s.inventoryWorker.Stop()
	}

	// Stop replication manager
	if s.replicationManager != nil {
		s.replicationManager.Stop()
		logrus.Info("Replication manager stopped")
	}

	// Stop cluster manager
	if s.clusterManager != nil {
		if err := s.clusterManager.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close cluster manager")
		}
		logrus.Info("Cluster manager stopped")
	}

	// Close audit manager
	if s.auditManager != nil {
		if err := s.auditManager.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close audit manager")
		}
	}

	// Close storage backend
	if err := s.storageBackend.Close(); err != nil {
		logrus.WithError(err).Error("Failed to close storage backend")
	}

	return nil
}

// shareManagerAdapter adapts share.Manager to the interface expected by api.NewHandler
type shareManagerAdapter struct {
	mgr share.Manager
}

func (sma *shareManagerAdapter) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error) {
	return sma.mgr.GetShareByObject(ctx, bucketName, objectKey, tenantID)
}

// clusterBucketManagerAdapter adapts bucket.Manager to cluster.BucketManager interface
type clusterBucketManagerAdapter struct {
	mgr bucket.Manager
}

func (a *clusterBucketManagerAdapter) GetBucketTenant(ctx context.Context, bucket string) (string, error) {
	// Try to get bucket info with empty tenant (will search across all tenants)
	bucketInfo, err := a.mgr.GetBucketInfo(ctx, "", bucket)
	if err != nil {
		return "", err
	}
	return bucketInfo.TenantID, nil
}

func (a *clusterBucketManagerAdapter) BucketExists(ctx context.Context, tenant, bucket string) (bool, error) {
	return a.mgr.BucketExists(ctx, tenant, bucket)
}

// clusterReplicationManagerAdapter adapts replication.Manager to cluster.ReplicationManager interface
type clusterReplicationManagerAdapter struct {
	mgr *replication.Manager
}

func (a *clusterReplicationManagerAdapter) GetReplicationRules(ctx context.Context, tenantID, bucket string) ([]cluster.ReplicationRule, error) {
	// Get all rules for this tenant
	rules, err := a.mgr.ListRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Filter rules by bucket and convert to cluster.ReplicationRule
	var clusterRules []cluster.ReplicationRule
	for _, rule := range rules {
		// Check if this rule applies to the bucket
		if rule.SourceBucket == bucket {
			clusterRules = append(clusterRules, cluster.ReplicationRule{
				ID:                  rule.ID,
				DestinationEndpoint: rule.DestinationEndpoint,
				DestinationBucket:   rule.DestinationBucket,
				Enabled:             rule.Enabled,
			})
		}
	}

	return clusterRules, nil
}

// updateBucketCountPeriodically updates the bucket count for the local node periodically
func (s *Server) updateBucketCountPeriodically(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Update immediately on start
	s.updateLocalBucketCount(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateLocalBucketCount(ctx)
		}
	}
}

// updateLocalBucketCount counts buckets and updates the local node's bucket_count
func (s *Server) updateLocalBucketCount(ctx context.Context) {
	if s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		return
	}

	// Count total buckets across all tenants
	buckets, err := s.bucketManager.ListBuckets(ctx, "") // Empty tenant = all buckets
	if err != nil {
		logrus.WithError(err).Warn("Failed to count buckets for cluster node update")
		return
	}

	bucketCount := len(buckets)

	// Update the cluster node
	err = s.clusterManager.UpdateLocalNodeBucketCount(ctx, bucketCount)
	if err != nil {
		logrus.WithError(err).Warn("Failed to update local node bucket count")
		return
	}

	logrus.WithField("bucket_count", bucketCount).Info("Updated local node bucket count")
}

func (s *Server) setupRoutes() error {
	// Setup API routes (S3 compatible)
	apiRouter := mux.NewRouter()

	// Prometheus metrics endpoint (no auth, no middleware)
	if s.config.Metrics.Enable {
		apiRouter.Handle("/metrics", s.metricsManager.GetMetricsHandler()).Methods("GET")
		logrus.Info("Prometheus metrics endpoint enabled at /metrics on S3 API")
	}

	// IMPORTANT: Register internal cluster API routes FIRST (before S3 routes)
	// to prevent S3 handler from capturing /api/internal/cluster/* routes
	if s.clusterManager != nil {
		// Register cluster join endpoints (no HMAC auth required, uses cluster token)
		// These must be available even when cluster is not yet enabled on this node
		internalPublicRouter := apiRouter.PathPrefix("/api/internal/cluster").Subrouter()
		internalPublicRouter.Use(s.rateLimiter.Middleware())

		// Cluster join endpoints (token-based auth, not HMAC)
		internalPublicRouter.HandleFunc("/validate-token", s.handleValidateClusterToken).Methods("POST")
		internalPublicRouter.HandleFunc("/register-node", s.handleRegisterNode).Methods("POST")
		internalPublicRouter.HandleFunc("/nodes", s.handleGetClusterNodesInternal).Methods("GET")

		if s.clusterManager.IsClusterEnabled() {
			clusterAuthMiddleware := middleware.NewClusterAuthMiddleware(s.db)
			internalClusterRouter := apiRouter.PathPrefix("/api/internal/cluster").Subrouter()

			// Apply rate limiting to internal cluster APIs
			internalClusterRouter.Use(s.rateLimiter.Middleware())

			// Apply HMAC authentication
			internalClusterRouter.Use(clusterAuthMiddleware.ClusterAuth)

			// Tenant synchronization endpoint
			internalClusterRouter.HandleFunc("/tenant-sync", s.handleReceiveTenantSync).Methods("POST")

			// User synchronization endpoint
			internalClusterRouter.HandleFunc("/user-sync", s.handleReceiveUserSync).Methods("POST")

			// Object replication endpoints
			internalClusterRouter.HandleFunc("/objects/{tenantID}/{bucket}/{key}", s.handleReceiveObjectReplication).Methods("PUT")
			internalClusterRouter.HandleFunc("/objects/{tenantID}/{bucket}/{key}", s.handleReceiveObjectDeletion).Methods("DELETE")

			// Bucket migration endpoints
			internalClusterRouter.HandleFunc("/bucket-permissions", s.handleReceiveBucketPermission).Methods("POST")
			internalClusterRouter.HandleFunc("/bucket-acl", s.handleReceiveBucketACL).Methods("POST")
			internalClusterRouter.HandleFunc("/bucket-config", s.handleReceiveBucketConfiguration).Methods("POST")
			internalClusterRouter.HandleFunc("/bucket-inventory", s.handleReceiveBucketInventory).Methods("POST")

			// Synchronization endpoints
			internalClusterRouter.HandleFunc("/access-key-sync", s.handleReceiveAccessKeySync).Methods("POST")
			internalClusterRouter.HandleFunc("/bucket-permission-sync", s.handleReceiveBucketPermissionSync).Methods("POST")

			// Bucket aggregation endpoint (for cross-node bucket listing)
			internalClusterRouter.HandleFunc("/buckets", s.handleGetLocalBuckets).Methods("GET")

			// Quota aggregation endpoint (for cross-node quota checking)
			internalClusterRouter.HandleFunc("/tenant/{tenantID}/storage", s.handleGetTenantStorage).Methods("GET")

			logrus.Info("Internal cluster API routes registered with HMAC authentication")
		}

		logrus.Info("Cluster join API routes registered (token-based authentication)")
	}

	// Create subrouter for authenticated S3 API routes
	s3Router := apiRouter.PathPrefix("/").Subrouter()

	// Create a wrapper for shareManager to match the interface expected by api.NewHandler
	shareManagerWrapper := &shareManagerAdapter{mgr: s.shareManager}

	apiHandler := api.NewHandler(
		s.bucketManager,
		s.objectManager,
		s.authManager,
		s.metricsManager,
		s.metadataStore,
		shareManagerWrapper,
		s.config.PublicAPIURL,
		s.config.PublicConsoleURL,
		s.config.DataDir,
		s.clusterManager,
		s.bucketAggregator,
	)

	// Apply middleware only to S3 subrouter (not to /metrics)
	// VERBOSE LOGGING - logs EVERY request with full details
	s3Router.Use(middleware.VerboseLogging())
	s3Router.Use(middleware.CORS())
	s3Router.Use(middleware.Logging())
	s3Router.Use(middleware.TracingMiddleware) // Add tracing for performance metrics
	if s.config.Auth.EnableAuth {
		s3Router.Use(s.authManager.Middleware())
	}
	if s.config.Metrics.Enable {
		s3Router.Use(s.metricsManager.Middleware())
	}

	// Register API routes on the authenticated subrouter
	apiHandler.RegisterRoutes(s3Router)

	// Setup CORS and other middleware
	s.httpServer.Handler = handlers.RecoveryHandler()(apiRouter)

	// Setup console routes (Web UI)
	consoleRouter := mux.NewRouter()
	s.setupConsoleRoutes(consoleRouter)
	s.consoleServer.Handler = handlers.RecoveryHandler()(consoleRouter)

	return nil
}

func (s *Server) setupConsoleRoutes(router *mux.Router) {
	// Extract base path from public_console_url
	basePath := extractBasePathFromURL(s.config.PublicConsoleURL)

	logrus.WithFields(logrus.Fields{
		"public_console_url": s.config.PublicConsoleURL,
		"base_path":          basePath,
	}).Info("Setting up console routes")

	// Create base router
	var baseRouter *mux.Router
	if basePath != "/" && basePath != "" {
		// All routes (including API) must be under the base path
		baseRouter = router.PathPrefix(basePath).Subrouter()
	} else {
		baseRouter = router
	}

	// API root endpoints (handle both with and without trailing slash)
	baseRouter.HandleFunc("/api/v1", s.handleAPIRoot).Methods("GET", "OPTIONS")
	baseRouter.HandleFunc("/api/v1/", s.handleAPIRoot).Methods("GET", "OPTIONS")

	// API endpoints for the web console (under base path)
	apiRouter := baseRouter.PathPrefix("/api/v1").Subrouter()
	apiRouter.Use(middleware.TracingMiddleware) // Add tracing for performance metrics
	s.setupConsoleAPIRoutes(apiRouter)

	// Register pprof profiling endpoints (under base path, authenticated)
	s.RegisterProfilingRoutes(baseRouter)

	// Serve embedded frontend for all other routes (under base path)
	frontendHandler, err := s.setupEmbeddedFrontend(router)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to setup embedded frontend - frontend must be built and embedded")
		return
	}

	baseRouter.PathPrefix("/").Handler(frontendHandler)
}

// extractBasePathFromURL extracts the path component from a URL
// Example: "https://s3.accst.local/ui" -> "/ui"
// Example: "http://localhost:8081" -> "/"
func extractBasePathFromURL(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		logrus.WithError(err).Warn("Failed to parse public console URL, using / as base path")
		return "/"
	}

	basePath := parsedURL.Path
	if basePath == "" || basePath == "/" {
		return "/"
	}

	// Ensure base path starts with / but does NOT end with /
	// This is important for PathPrefix matching in mux
	basePath = strings.TrimSuffix(basePath, "/")
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	return basePath
}

// objectManagerAdapter adapts object.Manager to replication.ObjectManager interface
type objectManagerAdapter struct {
	mgr object.Manager
}

func (oma *objectManagerAdapter) GetObject(ctx context.Context, tenantID, bucket, key string) (io.ReadCloser, int64, string, map[string]string, error) {
	// Get object using the object manager
	obj, reader, err := oma.mgr.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, 0, "", nil, err
	}

	return reader, obj.Size, obj.ContentType, obj.Metadata, nil
}

func (oma *objectManagerAdapter) GetObjectMetadata(ctx context.Context, tenantID, bucket, key string) (int64, string, map[string]string, error) {
	obj, err := oma.mgr.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return 0, "", nil, err
	}

	return obj.Size, obj.ContentType, obj.Metadata, nil
}

// bucketListerAdapter adapts object.Manager to replication.BucketLister interface
type bucketListerAdapter struct {
	mgr object.Manager
}

func (bla *bucketListerAdapter) ListObjects(ctx context.Context, tenantID, bucket, prefix string, maxKeys int) ([]string, error) {
	// List objects using the object manager
	// Pass empty delimiter and marker to get all objects
	result, err := bla.mgr.ListObjects(ctx, bucket, prefix, "", "", maxKeys)
	if err != nil {
		return nil, err
	}

	// Extract just the keys from the result
	keys := make([]string, 0, len(result.Objects))
	for _, obj := range result.Objects {
		keys = append(keys, obj.Key)
	}

	return keys, nil
}
