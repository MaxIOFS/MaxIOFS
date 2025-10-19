package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Console API Response structures
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type BucketResponse struct {
	Name                string                    `json:"name"`
	TenantID            string                    `json:"tenant_id,omitempty"`
	CreationDate        string                    `json:"creation_date"`
	Region              string                    `json:"region,omitempty"`
	OwnerID             string                    `json:"owner_id,omitempty"`
	OwnerType           string                    `json:"owner_type,omitempty"`
	IsPublic            bool                      `json:"is_public,omitempty"`
	ObjectCount         int64                     `json:"object_count"`
	ObjectCountIsApprox bool                      `json:"object_count_is_approx,omitempty"` // True if count is truncated
	Size                int64                     `json:"size"`
	Versioning          *bucket.VersioningConfig  `json:"versioning,omitempty"`
	ObjectLock          *bucket.ObjectLockConfig  `json:"objectLock,omitempty"`
	Encryption          *bucket.EncryptionConfig  `json:"encryption,omitempty"`
	PublicAccessBlock   *bucket.PublicAccessBlock `json:"publicAccessBlock,omitempty"`
	Tags                map[string]string         `json:"tags,omitempty"`
	Metadata            map[string]string         `json:"metadata,omitempty"`
}

type ObjectResponse struct {
	Key          string                  `json:"key"`
	Size         int64                   `json:"size"`
	LastModified string                  `json:"last_modified"`
	ETag         string                  `json:"etag"`
	ContentType  string                  `json:"content_type"`
	Metadata     map[string]string       `json:"metadata,omitempty"`
	Retention    *object.RetentionConfig `json:"retention,omitempty"`
}

type UserResponse struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	TenantID    string   `json:"tenantId,omitempty"`
	CreatedAt   int64    `json:"createdAt"`
}

type MetricsResponse struct {
	TotalBuckets int64              `json:"total_buckets"`
	TotalObjects int64              `json:"total_objects"`
	TotalSize    int64              `json:"total_size"`
	SystemStats  map[string]float64 `json:"system_stats"`
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// setupConsoleAPIRoutes registers all console API routes
func (s *Server) setupConsoleAPIRoutes(router *mux.Router) {
	// Metrics tracking middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     200,
			}

			next.ServeHTTP(wrapped, r)

			// Record request metrics
			latencyMs := uint64(time.Since(start).Milliseconds())
			isError := wrapped.statusCode >= 400
			s.systemMetrics.RecordRequest(latencyMs, isError)
		})
	})

	// Apply CORS middleware for API
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Authentication middleware - validates JWT and adds user to context
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authentication for public endpoints
			publicPaths := []string{"/auth/login", "/health"}
			for _, path := range publicPaths {
				if strings.Contains(r.URL.Path, path) || r.Method == "OPTIONS" {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Extract JWT token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				s.writeError(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")

			// Validate JWT token
			user, err := s.authManager.ValidateJWT(r.Context(), token)
			if err != nil {
				logrus.WithError(err).Warn("JWT validation failed")
				s.writeError(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Add user to context
			ctx := context.WithValue(r.Context(), "user", user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	// Auth endpoints
	router.HandleFunc("/auth/login", s.handleLogin).Methods("POST", "OPTIONS")
	router.HandleFunc("/auth/logout", s.handleLogout).Methods("POST", "OPTIONS")
	router.HandleFunc("/auth/me", s.handleGetCurrentUser).Methods("GET", "OPTIONS")

	// Bucket endpoints
	router.HandleFunc("/buckets", s.handleListBuckets).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets", s.handleCreateBucket).Methods("POST", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}", s.handleGetBucket).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}", s.handleDeleteBucket).Methods("DELETE", "OPTIONS")

	// Share endpoints (MUST be registered BEFORE generic object endpoints to avoid route conflicts)
	router.HandleFunc("/buckets/{bucket}/shares", s.handleListBucketShares).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/objects/{object:.*}/share", s.handleShareObject).Methods("POST", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/objects/{object:.*}/share", s.handleDeleteShare).Methods("DELETE", "OPTIONS")

	// Object endpoints
	router.HandleFunc("/buckets/{bucket}/objects", s.handleListObjects).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/objects/{object:.*}", s.handleGetObject).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/objects/{object:.*}", s.handleUploadObject).Methods("PUT", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/objects/{object:.*}", s.handleDeleteObject).Methods("DELETE", "OPTIONS")

	// User endpoints
	router.HandleFunc("/users", s.handleListUsers).Methods("GET", "OPTIONS")
	router.HandleFunc("/users", s.handleCreateUser).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/{user}", s.handleGetUser).Methods("GET", "OPTIONS")
	router.HandleFunc("/users/{user}", s.handleUpdateUser).Methods("PUT", "OPTIONS")
	router.HandleFunc("/users/{user}", s.handleDeleteUser).Methods("DELETE", "OPTIONS")

	// Access Key endpoints
	router.HandleFunc("/access-keys", s.handleListAllAccessKeys).Methods("GET", "OPTIONS")
	router.HandleFunc("/users/{user}/access-keys", s.handleListAccessKeys).Methods("GET", "OPTIONS")
	router.HandleFunc("/users/{user}/access-keys", s.handleCreateAccessKey).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/{user}/access-keys/{accessKey}", s.handleDeleteAccessKey).Methods("DELETE", "OPTIONS")

	// Password management
	router.HandleFunc("/users/{user}/password", s.handleChangePassword).Methods("PUT", "OPTIONS")

	// Account lockout management
	router.HandleFunc("/users/{user}/unlock", s.handleUnlockAccount).Methods("POST", "OPTIONS")

	// Metrics endpoints
	router.HandleFunc("/metrics", s.handleGetMetrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/system", s.handleGetSystemMetrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/s3", s.handleGetS3Metrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/history", s.handleGetHistoricalMetrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/history/stats", s.handleGetHistoryStats).Methods("GET", "OPTIONS")

	// Security endpoints
	router.HandleFunc("/security/status", s.handleGetSecurityStatus).Methods("GET", "OPTIONS")

	// Tenant endpoints
	router.HandleFunc("/tenants", s.handleListTenants).Methods("GET", "OPTIONS")
	router.HandleFunc("/tenants", s.handleCreateTenant).Methods("POST", "OPTIONS")
	router.HandleFunc("/tenants/{tenant}", s.handleGetTenant).Methods("GET", "OPTIONS")
	router.HandleFunc("/tenants/{tenant}", s.handleUpdateTenant).Methods("PUT", "OPTIONS")
	router.HandleFunc("/tenants/{tenant}", s.handleDeleteTenant).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/tenants/{tenant}/users", s.handleListTenantUsers).Methods("GET", "OPTIONS")

	// Bucket permissions endpoints
	router.HandleFunc("/buckets/{bucket}/permissions", s.handleListBucketPermissions).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/permissions", s.handleGrantBucketPermission).Methods("POST", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/permissions/revoke", s.handleRevokeBucketPermission).Methods("DELETE", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}/permissions/{permission}", s.handleRevokeBucketPermission).Methods("DELETE", "OPTIONS") // Legacy endpoint
	router.HandleFunc("/buckets/{bucket}/owner", s.handleUpdateBucketOwner).Methods("PUT", "OPTIONS")

	// Health check
	router.HandleFunc("/health", s.handleAPIHealth).Methods("GET", "OPTIONS")
}

// Auth handlers
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var loginReq struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&loginReq); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get client IP address
	clientIP := getClientIP(r)

	// Step 1: Check IP-based rate limiting (5 attempts per minute)
	if !s.authManager.CheckRateLimit(clientIP) {
		logrus.WithFields(logrus.Fields{
			"ip":       clientIP,
			"username": loginReq.Username,
		}).Warn("Login rate limit exceeded")
		s.writeError(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}

	// Step 2: Validate credentials to get user (needed to check account lock)
	user, err := s.authManager.ValidateConsoleCredentials(r.Context(), loginReq.Username, loginReq.Password)
	if err != nil {
		// Try to get user by username to record failed attempt
		// We need to do this even if credentials are invalid
		userByName, userErr := s.authManager.GetUser(r.Context(), loginReq.Username)
		if userErr == nil && userByName != nil {
			// Record failed login attempt
			s.authManager.RecordFailedLogin(r.Context(), userByName.ID, clientIP)
		}

		s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Step 3: Check if account is locked
	isLocked, lockedUntil, err := s.authManager.IsAccountLocked(r.Context(), user.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to check account lock status")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if isLocked {
		remainingTime := time.Until(time.Unix(lockedUntil, 0))
		logrus.WithFields(logrus.Fields{
			"user_id":        user.ID,
			"username":       user.Username,
			"locked_until":   time.Unix(lockedUntil, 0).Format(time.RFC3339),
			"remaining_time": remainingTime.String(),
		}).Warn("Login attempt on locked account")

		s.writeJSON(w, map[string]interface{}{
			"error":        "Account is locked due to multiple failed login attempts",
			"locked_until": lockedUntil,
			"locked":       true,
		})
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Step 4: Record successful login and reset failed attempts
	s.authManager.RecordSuccessfulLogin(r.Context(), user.ID)

	// Step 5: Generate JWT token
	token, err := s.authManager.GenerateJWT(r.Context(), user)
	if err != nil {
		s.writeError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"user_id":  user.ID,
		"username": user.Username,
		"ip":       clientIP,
	}).Info("Successful login")

	s.writeJSON(w, map[string]interface{}{
		"token": token,
		"user": UserResponse{
			ID:          user.ID,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Status:      user.Status,
			Roles:       user.Roles,
			TenantID:    user.TenantID,
			CreatedAt:   user.CreatedAt,
		},
	})
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For can contain multiple IPs, use the first one
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	// Remove port if present
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]string{"message": "Logged out successfully"})
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	s.writeJSON(w, UserResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Status:      user.Status,
		Roles:       user.Roles,
		TenantID:    user.TenantID,
		CreatedAt:   user.CreatedAt,
	})
}

// Bucket handlers
func (s *Server) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	// Get user from context and apply permission filtering
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		// No user context, return empty list
		s.writeJSON(w, []BucketResponse{})
		return
	}

	// Extract tenant ID from user context
	tenantID := user.TenantID

	buckets, err := s.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Global admin = admin role WITHOUT tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""

	var filteredBuckets []bucket.Bucket

	if isGlobalAdmin {
		// ONLY global admins see all buckets
		filteredBuckets = buckets
	} else if user.TenantID != "" {
		// Tenant users (including tenant admins) see only their tenant's buckets
		for _, b := range buckets {
			if (b.OwnerType == "tenant" && b.OwnerID == user.TenantID) ||
				(b.OwnerType == "user" && b.OwnerID == user.ID) {
				filteredBuckets = append(filteredBuckets, b)
			}
		}
	} else {
		// Non-admin users without tenant: use permission filtering
		bucketPointers := make([]*bucket.Bucket, len(buckets))
		for i := range buckets {
			bucketPointers[i] = &buckets[i]
		}

		filteredPointers, err := bucket.FilterBucketsByPermissions(r.Context(), bucketPointers, user.ID, user.Roles, s.authManager)
		if err != nil {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		filteredBuckets = make([]bucket.Bucket, len(filteredPointers))
		for i, bp := range filteredPointers {
			filteredBuckets[i] = *bp
		}
	}

	response := make([]BucketResponse, len(filteredBuckets))
	for i, b := range filteredBuckets {
		// Use cached metrics from bucket metadata (fast!)
		// No need to list objects anymore - metrics are updated incrementally
		response[i] = BucketResponse{
			Name:                b.Name,
			TenantID:            b.TenantID,
			CreationDate:        b.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Region:              b.Region,
			OwnerID:             b.OwnerID,
			OwnerType:           b.OwnerType,
			IsPublic:            b.IsPublic,
			ObjectCount:         b.ObjectCount,
			ObjectCountIsApprox: false, // Exact count from incremental updates
			Size:                b.TotalSize,
			Versioning:          b.Versioning,
			ObjectLock:          b.ObjectLock,
			Encryption:          b.Encryption,
			PublicAccessBlock:   b.PublicAccessBlock,
			Tags:                b.Tags,
			Metadata:            b.Metadata,
		}
	}

	s.writeJSON(w, response)
}

func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string                   `json:"name"`
		Region     string                   `json:"region,omitempty"`
		OwnerID    string                   `json:"ownerId,omitempty"`
		OwnerType  string                   `json:"ownerType,omitempty"` // "user" or "tenant"
		IsPublic   bool                     `json:"isPublic,omitempty"`
		Versioning *bucket.VersioningConfig `json:"versioning,omitempty"`
		ObjectLock *struct {
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"` // GOVERNANCE, COMPLIANCE
			Days    int    `json:"days"`
			Years   int    `json:"years"`
		} `json:"objectLock,omitempty"`
		Encryption        *bucket.EncryptionConfig  `json:"encryption,omitempty"`
		PublicAccessBlock *bucket.PublicAccessBlock `json:"publicAccessBlock,omitempty"`
		Lifecycle         *bucket.LifecycleConfig   `json:"lifecycle,omitempty"`
		Tags              map[string]string         `json:"tags,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validaciones básicas
	if req.Name == "" {
		s.writeError(w, "Bucket name is required", http.StatusBadRequest)
		return
	}

	// Get current user for ownership and quota validation
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Determine the tenant ID for quota checking
	var targetTenantID string
	if user.TenantID != "" {
		targetTenantID = user.TenantID
	}

	// Check tenant bucket quota before creation
	if targetTenantID != "" {
		tenant, err := s.authManager.GetTenant(r.Context(), targetTenantID)
		if err != nil {
			s.writeError(w, "Failed to retrieve tenant information", http.StatusInternalServerError)
			return
		}

		if tenant.CurrentBuckets >= tenant.MaxBuckets {
			s.writeError(w, fmt.Sprintf("Tenant bucket quota exceeded (%d/%d). Cannot create more buckets.", tenant.CurrentBuckets, tenant.MaxBuckets), http.StatusForbidden)
			return
		}
	}

	// Validar Object Lock - requiere versionado
	if req.ObjectLock != nil && req.ObjectLock.Enabled {
		if req.Versioning == nil || req.Versioning.Status != "Enabled" {
			s.writeError(w, "Object Lock requires versioning to be enabled", http.StatusBadRequest)
			return
		}

		// Validar que tenga modo de retención
		if req.ObjectLock.Mode == "" {
			s.writeError(w, "Object Lock mode (GOVERNANCE or COMPLIANCE) is required", http.StatusBadRequest)
			return
		}

		// Validar que tenga al menos días o años
		if req.ObjectLock.Days == 0 && req.ObjectLock.Years == 0 {
			s.writeError(w, "Object Lock requires at least days or years to be specified", http.StatusBadRequest)
			return
		}
	}

	// Extract tenant ID from user context
	tenantID := user.TenantID

	// Crear el bucket
	if err := s.bucketManager.CreateBucket(r.Context(), tenantID, req.Name); err != nil {
		if err == bucket.ErrBucketAlreadyExists {
			s.writeError(w, "Bucket already exists", http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Aplicar configuraciones
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, req.Name)
	if err != nil {
		s.writeError(w, "Bucket created but failed to retrieve info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Aplicar ownership - determinar basado en el usuario autenticado
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""

	// Tenant users (including tenant admins) ALWAYS get buckets assigned to their tenant
	if user.TenantID != "" {
		bucketInfo.OwnerID = user.TenantID
		bucketInfo.OwnerType = "tenant"
		bucketInfo.IsPublic = false // Los buckets de tenant no pueden ser públicos
	} else if isGlobalAdmin {
		// ONLY global admins can specify custom ownership
		if req.OwnerID != "" {
			bucketInfo.OwnerID = req.OwnerID
		}
		if req.OwnerType != "" {
			bucketInfo.OwnerType = req.OwnerType
		}
		bucketInfo.IsPublic = req.IsPublic
	} else {
		// Usuario global sin tenant - bucket global
		bucketInfo.OwnerID = ""
		bucketInfo.OwnerType = ""
		bucketInfo.IsPublic = req.IsPublic
	}

	// Aplicar versionado
	if req.Versioning != nil {
		bucketInfo.Versioning = req.Versioning
	}

	// Aplicar Object Lock
	if req.ObjectLock != nil && req.ObjectLock.Enabled {
		retention := &bucket.DefaultRetention{
			Mode: req.ObjectLock.Mode,
		}

		// Only set Days or Years, not both (as per S3 specification)
		if req.ObjectLock.Days > 0 {
			days := req.ObjectLock.Days
			retention.Days = &days
		} else if req.ObjectLock.Years > 0 {
			years := req.ObjectLock.Years
			retention.Years = &years
		}

		bucketInfo.ObjectLock = &bucket.ObjectLockConfig{
			ObjectLockEnabled: true,
			Rule: &bucket.ObjectLockRule{
				DefaultRetention: retention,
			},
		}
	}

	// Aplicar encriptación
	if req.Encryption != nil {
		bucketInfo.Encryption = req.Encryption
	}

	// Aplicar public access block
	if req.PublicAccessBlock != nil {
		bucketInfo.PublicAccessBlock = req.PublicAccessBlock
	}

	// Aplicar lifecycle
	if req.Lifecycle != nil {
		bucketInfo.Lifecycle = req.Lifecycle
	}

	// Aplicar tags
	if req.Tags != nil {
		bucketInfo.Tags = req.Tags
	}

	// Aplicar región
	if req.Region != "" {
		bucketInfo.Region = req.Region
	}

	// Guardar configuraciones
	if err := s.bucketManager.UpdateBucket(r.Context(), tenantID, req.Name, bucketInfo); err != nil {
		s.writeError(w, "Bucket created but failed to apply configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Incrementar el contador de buckets del tenant si tiene owner de tipo tenant
	if bucketInfo.OwnerType == "tenant" && bucketInfo.OwnerID != "" {
		if err := s.authManager.IncrementTenantBucketCount(r.Context(), bucketInfo.OwnerID); err != nil {
			// Log error but don't fail the request
			logrus.WithError(err).WithField("tenantID", bucketInfo.OwnerID).Error("Failed to increment tenant bucket count")
		}
	}

	s.writeJSON(w, map[string]string{"name": req.Name})
}

func (s *Server) handleGetBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins accessing other tenants' buckets)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Use cached metrics (fast!)
	response := BucketResponse{
		Name:              bucketInfo.Name,
		TenantID:          bucketInfo.TenantID,
		CreationDate:      bucketInfo.CreatedAt.Format("2006-01-02T15:04:05Z"),
		Region:            bucketInfo.Region,
		OwnerID:           bucketInfo.OwnerID,
		OwnerType:         bucketInfo.OwnerType,
		ObjectCount:       bucketInfo.ObjectCount,
		Size:              bucketInfo.TotalSize,
		Versioning:        bucketInfo.Versioning,
		ObjectLock:        bucketInfo.ObjectLock,
		Encryption:        bucketInfo.Encryption,
		PublicAccessBlock: bucketInfo.PublicAccessBlock,
		Tags:              bucketInfo.Tags,
		Metadata:          bucketInfo.Metadata,
	}

	s.writeJSON(w, response)
}

func (s *Server) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	tenantID := user.TenantID

	// Obtener información del bucket antes de eliminarlo para actualizar contadores
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if err := s.bucketManager.DeleteBucket(r.Context(), tenantID, bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else if err == bucket.ErrBucketNotEmpty {
			s.writeError(w, "Bucket is not empty", http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Decrementar el contador de buckets del tenant si tiene owner de tipo tenant
	if bucketInfo.OwnerType == "tenant" && bucketInfo.OwnerID != "" {
		if err := s.authManager.DecrementTenantBucketCount(r.Context(), bucketInfo.OwnerID); err != nil {
			// Log error but don't fail the request
			logrus.WithError(err).WithField("tenantID", bucketInfo.OwnerID).Error("Failed to decrement tenant bucket count")
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// Object handlers
func (s *Server) handleListObjects(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins accessing other tenants' buckets)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	bucketPath := tenantID + "/" + bucketName
	if tenantID == "" {
		bucketPath = bucketName
	}

	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	marker := r.URL.Query().Get("marker")
	maxKeys := 1000

	if maxKeysStr := r.URL.Query().Get("max_keys"); maxKeysStr != "" {
		if parsed, err := strconv.Atoi(maxKeysStr); err == nil && parsed > 0 {
			maxKeys = parsed
		}
	}

	result, err := s.objectManager.ListObjects(r.Context(), bucketPath, prefix, delimiter, marker, maxKeys)
	if err != nil {
		if err == object.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Convert objects to response format
	objectsResponse := make([]ObjectResponse, len(result.Objects))
	for i, obj := range result.Objects {
		objectsResponse[i] = ObjectResponse{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified.Format("2006-01-02T15:04:05Z"),
			ETag:         obj.ETag,
			ContentType:  obj.ContentType,
			Metadata:     obj.Metadata,
			Retention:    obj.Retention,
		}
	}

	// Convert common prefixes to response format
	commonPrefixesResponse := make([]string, len(result.CommonPrefixes))
	for i, cp := range result.CommonPrefixes {
		commonPrefixesResponse[i] = cp.Prefix
	}

	s.writeJSON(w, map[string]interface{}{
		"objects":        objectsResponse,
		"commonPrefixes": commonPrefixesResponse,
		"isTruncated":    result.IsTruncated,
		"nextMarker":     result.NextMarker,
		"prefix":         result.Prefix,
		"delimiter":      result.Delimiter,
	})
}

func (s *Server) handleGetObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins accessing other tenants' buckets)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	bucketPath := tenantID + "/" + bucketName
	if tenantID == "" {
		bucketPath = bucketName
	}

	// Check if client wants metadata only (Accept: application/json) or the actual file
	acceptHeader := r.Header.Get("Accept")
	wantsJSON := acceptHeader == "application/json"

	// If client wants JSON metadata only, return metadata
	if wantsJSON {
		metadata, err := s.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey)
		if err != nil {
			if err == object.ErrObjectNotFound {
				s.writeError(w, "Object not found", http.StatusNotFound)
			} else {
				s.writeError(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		response := ObjectResponse{
			Key:          metadata.Key,
			Size:         metadata.Size,
			LastModified: metadata.LastModified.Format("2006-01-02T15:04:05Z"),
			ETag:         metadata.ETag,
			ContentType:  metadata.ContentType,
			Metadata:     metadata.Metadata,
		}

		s.writeJSON(w, response)
		return
	}

	// Otherwise, return the actual file content
	obj, reader, err := s.objectManager.GetObject(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Object not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	// Set appropriate headers for file download
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(objectKey)))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.Format(http.TimeFormat))

	// Copy the object content to response
	if _, err := io.Copy(w, reader); err != nil {
		logrus.WithError(err).Debug("Error streaming object content")
	}
}

func (s *Server) handleUploadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins accessing other tenants' buckets)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	// Get bucket to check tenant
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Check tenant storage quota before upload
	if bucketInfo.OwnerType == "tenant" && bucketInfo.OwnerID != "" {
		tenant, err := s.authManager.GetTenant(r.Context(), bucketInfo.OwnerID)
		if err != nil {
			s.writeError(w, "Failed to retrieve tenant information", http.StatusInternalServerError)
			return
		}

		// Get Content-Length to check if upload would exceed quota
		contentLength := r.ContentLength
		if contentLength > 0 {
			if tenant.CurrentStorageBytes+contentLength > tenant.MaxStorageBytes {
				s.writeError(w, fmt.Sprintf("Tenant storage quota exceeded (%d/%d bytes). Cannot upload object.", tenant.CurrentStorageBytes, tenant.MaxStorageBytes), http.StatusForbidden)
				return
			}
		}
	}

	bucketPath := tenantID + "/" + bucketName
	if tenantID == "" {
		bucketPath = bucketName
	}

	obj, err := s.objectManager.PutObject(r.Context(), bucketPath, objectKey, r.Body, r.Header)
	if err != nil {
		if err == object.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Check if bucket has Object Lock enabled and apply default retention
	lockConfig, err := s.bucketManager.GetObjectLockConfig(r.Context(), tenantID, bucketName)
	if err == nil && lockConfig != nil && lockConfig.ObjectLockEnabled {
		// Apply default retention if configured
		if lockConfig.Rule != nil && lockConfig.Rule.DefaultRetention != nil {
			retention := &object.RetentionConfig{
				Mode: lockConfig.Rule.DefaultRetention.Mode,
			}

			// Calculate retain until date based on days or years
			if lockConfig.Rule.DefaultRetention.Days != nil {
				retention.RetainUntilDate = time.Now().AddDate(0, 0, *lockConfig.Rule.DefaultRetention.Days)
			} else if lockConfig.Rule.DefaultRetention.Years != nil {
				retention.RetainUntilDate = time.Now().AddDate(*lockConfig.Rule.DefaultRetention.Years, 0, 0)
			}

			// Set retention on the newly uploaded object
			if !retention.RetainUntilDate.IsZero() {
				_ = s.objectManager.SetObjectRetention(r.Context(), bucketPath, objectKey, retention)
				// Ignore errors here - object is already uploaded
			}
		}
	}

	response := ObjectResponse{
		Key:          obj.Key,
		Size:         obj.Size,
		LastModified: obj.LastModified.Format("2006-01-02T15:04:05Z"),
		ETag:         obj.ETag,
		ContentType:  obj.ContentType,
	}

	s.writeJSON(w, response)
}

func (s *Server) handleShareObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// Get user from context
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for accessing tenant buckets from console)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// If tenantId is explicitly provided in query, use it (for global admins or console navigation)
	if queryTenantID != "" {
		tenantID = queryTenantID
		logrus.WithFields(logrus.Fields{
			"queryTenantID": queryTenantID,
			"userTenantID":  user.TenantID,
		}).Debug("Using tenantId from query parameter")
	}

	// Get bucket info to determine tenant ID
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		// If not found in user's tenant, try as global admin
		isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
		if isGlobalAdmin {
			tenantID = ""
			bucketInfo, err = s.bucketManager.GetBucketInfo(r.Context(), "", bucketName)
			if err != nil {
				s.writeError(w, "Bucket not found", http.StatusNotFound)
				return
			}
		} else {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
	}

	// Use the bucket's tenant ID for the share
	shareTenantID := bucketInfo.TenantID

	// Check if object already has an active share
	existingShare, err := s.shareManager.GetShareByObject(r.Context(), bucketName, objectKey, shareTenantID)
	if err == nil && existingShare != nil {
		// Return existing share
		logrus.WithFields(logrus.Fields{
			"bucket":  bucketName,
			"object":  objectKey,
			"shareID": existingShare.ID,
		}).Info("Found existing share for object")

		// Generate clean S3 URL with proper protocol and host
		// Use PublicAPIURL if configured, otherwise build from request
		var s3URL string
		if s.config.PublicAPIURL != "" {
			// Use configured public URL
			if shareTenantID != "" {
				s3URL = fmt.Sprintf("%s/%s/%s/%s", s.config.PublicAPIURL, shareTenantID, bucketName, objectKey)
			} else {
				s3URL = fmt.Sprintf("%s/%s/%s", s.config.PublicAPIURL, bucketName, objectKey)
			}
		} else {
			// Build URL from request context
			protocol := "http"
			if r.TLS != nil {
				protocol = "https"
			} else if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				protocol = proto
			}
			host := r.Host
			// If host doesn't include port, add the API listen port
			if !strings.Contains(host, ":") {
				host = strings.Split(r.Host, ":")[0] + s.config.Listen
			}
			if shareTenantID != "" {
				s3URL = fmt.Sprintf("%s://%s/%s/%s/%s", protocol, host, shareTenantID, bucketName, objectKey)
			} else {
				s3URL = fmt.Sprintf("%s://%s/%s/%s", protocol, host, bucketName, objectKey)
			}
		}

		logrus.WithFields(logrus.Fields{
			"tenantID": shareTenantID,
			"url":      s3URL,
			"existing": true,
		}).Info("Generated share URL for existing share")

		s.writeJSON(w, map[string]interface{}{
			"id":        existingShare.ID,
			"url":       s3URL,
			"expiresAt": existingShare.ExpiresAt,
			"createdAt": existingShare.CreatedAt.Format(time.RFC3339),
			"isExpired": false,
			"existing":  true,
		})
		return
	} else if err != nil {
		// Log error if it's not "not found"
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
			"error":  err.Error(),
		}).Debug("No existing share found or error occurred")
	}

	// Parse request body for expiration time
	var req struct {
		ExpiresIn *int64 `json:"expiresIn"` // seconds, null = never expires
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Default to 1 hour if no body provided
		defaultExpiry := int64(3600)
		req.ExpiresIn = &defaultExpiry
	}

	// Get user's first access key
	accessKeys, err := s.authManager.ListAccessKeys(r.Context(), user.ID)
	if err != nil || len(accessKeys) == 0 {
		s.writeError(w, "No access keys found for user. Create an access key first.", http.StatusBadRequest)
		return
	}

	accessKey := accessKeys[0]

	// Create persistent share
	share, err := s.shareManager.CreateShare(
		r.Context(),
		bucketName,
		objectKey,
		shareTenantID,
		accessKey.AccessKeyID,
		accessKey.SecretAccessKey,
		user.ID,
		req.ExpiresIn,
	)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to create share: %v", err), http.StatusInternalServerError)
		return
	}

	// Generate clean S3 URL with proper protocol and host
	// Use PublicAPIURL if configured, otherwise build from request
	var s3URL string
	if s.config.PublicAPIURL != "" {
		// Use configured public URL
		if shareTenantID != "" {
			s3URL = fmt.Sprintf("%s/%s/%s/%s", s.config.PublicAPIURL, shareTenantID, bucketName, objectKey)
		} else {
			s3URL = fmt.Sprintf("%s/%s/%s", s.config.PublicAPIURL, bucketName, objectKey)
		}
	} else {
		// Build URL from request context
		protocol := "http"
		if r.TLS != nil {
			protocol = "https"
		} else if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			protocol = proto
		}
		host := r.Host
		// If host doesn't include port, add the API listen port
		if !strings.Contains(host, ":") {
			host = strings.Split(r.Host, ":")[0] + s.config.Listen
		}
		if shareTenantID != "" {
			s3URL = fmt.Sprintf("%s://%s/%s/%s/%s", protocol, host, shareTenantID, bucketName, objectKey)
		} else {
			s3URL = fmt.Sprintf("%s://%s/%s/%s", protocol, host, bucketName, objectKey)
		}
	}

	logrus.WithFields(logrus.Fields{
		"tenantID": shareTenantID,
		"url":      s3URL,
		"bucket":   bucketName,
		"object":   objectKey,
	}).Info("Generated share URL for new share")

	// Return share response
	s.writeJSON(w, map[string]interface{}{
		"id":        share.ID,
		"url":       s3URL,
		"expiresAt": share.ExpiresAt,
		"createdAt": share.CreatedAt.Format(time.RFC3339),
		"isExpired": false,
		"existing":  false,
	})
}

func (s *Server) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins accessing other tenants' buckets)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	bucketPath := tenantID + "/" + bucketName
	if tenantID == "" {
		bucketPath = bucketName
	}

	if err := s.objectManager.DeleteObject(r.Context(), bucketPath, objectKey); err != nil {
		if err == object.ErrObjectNotFound {
			s.writeError(w, "Object not found", http.StatusNotFound)
			return
		}

		// Check if it's a retention error with detailed information
		if retErr, ok := err.(*object.RetentionError); ok {
			s.writeError(w, retErr.Error(), http.StatusForbidden)
			return
		}

		// Check for other Object Lock errors
		if err == object.ErrObjectUnderLegalHold {
			s.writeError(w, "Object is under legal hold and cannot be deleted", http.StatusForbidden)
			return
		}

		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// User handlers
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	users, err := s.authManager.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter users by tenant for non-admin users
	isAdmin := auth.IsAdminUser(r.Context())
	var filteredUsers []auth.User

	if isAdmin && currentUser.TenantID == "" {
		// Global admin sees all users
		filteredUsers = users
	} else if currentUser.TenantID != "" {
		// Tenant admin sees only users from their tenant
		for _, u := range users {
			if u.TenantID == currentUser.TenantID {
				filteredUsers = append(filteredUsers, u)
			}
		}
	} else {
		// Non-admin users without tenant see only themselves
		for _, u := range users {
			if u.ID == currentUser.ID {
				filteredUsers = append(filteredUsers, u)
				break
			}
		}
	}

	response := make([]UserResponse, len(filteredUsers))
	for i, u := range filteredUsers {
		response[i] = UserResponse{
			ID:          u.ID,
			Username:    u.ID,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			Status:      u.Status,
			Roles:       u.Roles,
			TenantID:    u.TenantID,
			CreatedAt:   u.CreatedAt,
		}
	}

	s.writeJSON(w, response)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var createRequest struct {
		Username string   `json:"username"`
		Email    string   `json:"email,omitempty"`
		Password string   `json:"password"`
		Roles    []string `json:"roles,omitempty"`
		Status   string   `json:"status,omitempty"`
		TenantID string   `json:"tenantId,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if createRequest.Username == "" || createRequest.Password == "" {
		s.writeError(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	// Get current user for tenant validation
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""

	// Tenant admins can only create users in their own tenant
	if !isGlobalAdmin && currentUser.TenantID != "" {
		if createRequest.TenantID != "" && createRequest.TenantID != currentUser.TenantID {
			s.writeError(w, "Tenant admins can only create users in their own tenant", http.StatusForbidden)
			return
		}
		// Force tenant assignment
		createRequest.TenantID = currentUser.TenantID
	}

	// Set defaults
	if createRequest.Status == "" {
		createRequest.Status = "active"
	}
	if len(createRequest.Roles) == 0 {
		createRequest.Roles = []string{"read"}
	}

	// Create user (password will be hashed by CreateUser)
	user := &auth.User{
		ID:          createRequest.Username,
		Username:    createRequest.Username,
		Password:    createRequest.Password, // Will be hashed with bcrypt by SQLiteStore
		DisplayName: createRequest.Username,
		Email:       createRequest.Email,
		Status:      createRequest.Status,
		Roles:       createRequest.Roles,
		TenantID:    createRequest.TenantID,
		CreatedAt:   time.Now().Unix(),
	}

	if err := s.authManager.CreateUser(r.Context(), user); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	userResponse := UserResponse{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       user.Roles,
		Status:      user.Status,
		TenantID:    user.TenantID,
		CreatedAt:   user.CreatedAt,
	}

	s.writeJSON(w, userResponse)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	user, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "User not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Convert to response format
	userResponse := UserResponse{
		ID:          user.ID,
		Username:    user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       user.Roles,
		Status:      user.Status,
		TenantID:    user.TenantID,
		CreatedAt:   user.CreatedAt,
	}

	s.writeJSON(w, userResponse)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	var updateRequest struct {
		Email    *string  `json:"email,omitempty"`
		Roles    []string `json:"roles,omitempty"`
		Status   string   `json:"status,omitempty"`
		TenantID *string  `json:"tenantId,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateRequest); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get existing user
	user, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "User not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update fields if provided
	if updateRequest.Email != nil {
		user.Email = *updateRequest.Email
	}
	if updateRequest.Roles != nil {
		user.Roles = updateRequest.Roles
	}
	if updateRequest.Status != "" {
		user.Status = updateRequest.Status
	}
	if updateRequest.TenantID != nil {
		user.TenantID = *updateRequest.TenantID
	}

	// Update user
	if err := s.authManager.UpdateUser(r.Context(), user); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	userResponse := UserResponse{
		ID:          user.ID,
		Username:    user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       user.Roles,
		Status:      user.Status,
		TenantID:    user.TenantID,
		CreatedAt:   user.CreatedAt,
	}

	s.writeJSON(w, userResponse)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	// Delete user
	if err := s.authManager.DeleteUser(r.Context(), userID); err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "User not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Account lockout handlers
func (s *Server) handleUnlockAccount(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	targetUserID := vars["user"]

	// Get current user from context
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Unlock account (permissions are checked in authManager)
	err := s.authManager.UnlockAccount(r.Context(), currentUser.ID, targetUserID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "insufficient permissions") {
			s.writeError(w, err.Error(), http.StatusForbidden)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Account unlocked successfully",
	})
}

// Metrics handlers
func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	// This endpoint is accessible to any authenticated user
	// Users will see metrics filtered by their permissions

	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	tenantID := user.TenantID

	buckets, _ := s.bucketManager.ListBuckets(r.Context(), tenantID)
	filteredBuckets := buckets

	totalBuckets := int64(len(filteredBuckets))

	var totalObjects, totalSize int64

	bucketMetrics := make(map[string]interface{})

	// Use cached bucket metrics from BadgerDB (O(1) per bucket instead of O(n) objects)
	for _, b := range filteredBuckets {
		// Use the pre-computed ObjectCount and TotalSize from bucket metadata
		// These are maintained incrementally by UpdateBucketMetrics
		bucketObjectCount := b.ObjectCount
		bucketSize := b.TotalSize

		totalObjects += bucketObjectCount
		totalSize += bucketSize

		// Store per-bucket metrics
		bucketMetrics[b.Name] = map[string]interface{}{
			"name":        b.Name,
			"objectCount": bucketObjectCount,
			"size":        bucketSize,
		}
	}

	// Calculate average object size
	var averageObjectSize int64
	if totalObjects > 0 {
		averageObjectSize = totalSize / totalObjects
	}

	// Return response in camelCase format expected by frontend
	response := map[string]interface{}{
		"totalBuckets":           totalBuckets,
		"totalObjects":           totalObjects,
		"totalSize":              totalSize,
		"bucketMetrics":          bucketMetrics,
		"storageOperations":      make(map[string]int),
		"averageObjectSize":      averageObjectSize,
		"objectSizeDistribution": make(map[string]int),
		"timestamp":              time.Now().Unix(),
	}

	s.writeJSON(w, response)
}

func (s *Server) handleGetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	// Only Global Admins can access system metrics
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: Only Global Admins can access system metrics", http.StatusForbidden)
		return
	}

	// Get system metrics
	cpuStats, _ := s.systemMetrics.GetCPUStats()
	memStats, _ := s.systemMetrics.GetMemoryUsage()
	diskStats, _ := s.systemMetrics.GetDiskUsage()

	// Get Go runtime statistics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Calculate uptime
	uptime := time.Since(s.startTime).Seconds()

	// Return response in camelCase format expected by frontend
	response := map[string]interface{}{
		"cpuUsagePercent":    0.0,
		"cpuCores":           0,
		"cpuLogicalCores":    0,
		"cpuFrequencyMhz":    0.0,
		"cpuModelName":       "Unknown",
		"memoryUsagePercent": 0.0,
		"memoryUsedBytes":    uint64(0),
		"memoryTotalBytes":   uint64(0),
		"diskUsagePercent":   0.0,
		"diskUsedBytes":      uint64(0),
		"diskTotalBytes":     uint64(0),
		"networkBytesIn":     uint64(0),
		"networkBytesOut":    uint64(0),
		"uptime":             uptime,                 // Server uptime in seconds
		"goroutines":         runtime.NumGoroutine(), // Active goroutines
		"heapAllocBytes":     m.HeapAlloc,            // Bytes allocated in heap
		"gcRuns":             m.NumGC,                // Number of GC runs
		"timestamp":          time.Now().Unix(),
	}

	// Populate CPU stats if available
	if cpuStats != nil {
		response["cpuUsagePercent"] = cpuStats.UsagePercent
		response["cpuCores"] = cpuStats.Cores
		response["cpuLogicalCores"] = cpuStats.LogicalCores
		response["cpuFrequencyMhz"] = cpuStats.FrequencyMHz
		response["cpuModelName"] = cpuStats.ModelName
	}

	// Populate memory stats if available
	if memStats != nil {
		response["memoryUsagePercent"] = memStats.UsedPercent
		response["memoryUsedBytes"] = memStats.UsedBytes
		response["memoryTotalBytes"] = memStats.TotalBytes
	}

	// Populate disk stats if available
	if diskStats != nil {
		response["diskUsagePercent"] = diskStats.UsedPercent
		response["diskUsedBytes"] = diskStats.UsedBytes
		response["diskTotalBytes"] = diskStats.TotalBytes
	}

	s.writeJSON(w, response)
}

func (s *Server) handleGetS3Metrics(w http.ResponseWriter, r *http.Request) {
	// Only Global Admins can access S3 metrics
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: Only Global Admins can access S3 metrics", http.StatusForbidden)
		return
	}

	// Get S3 metrics snapshot from metrics manager
	s3Metrics, err := s.metricsManager.GetS3MetricsSnapshot()
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get S3 metrics: %v", err), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, s3Metrics)
}

func (s *Server) handleGetHistoricalMetrics(w http.ResponseWriter, r *http.Request) {
	// Only Global Admins can access historical metrics
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: Only Global Admins can access historical metrics", http.StatusForbidden)
		return
	}

	// Parse query parameters
	metricType := r.URL.Query().Get("type")
	if metricType == "" {
		metricType = "system" // Default to system metrics
	}

	// Parse time range
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		// Try parsing as Unix timestamp first
		if timestamp, parseErr := strconv.ParseInt(startStr, 10, 64); parseErr == nil {
			start = time.Unix(timestamp, 0)
		} else {
			// Try parsing as RFC3339
			start, err = time.Parse(time.RFC3339, startStr)
			if err != nil {
				s.writeError(w, fmt.Sprintf("Invalid start time format: %v", err), http.StatusBadRequest)
				return
			}
		}
	} else {
		// Default: last 24 hours
		start = time.Now().Add(-24 * time.Hour)
	}

	if endStr != "" {
		// Try parsing as Unix timestamp first
		if timestamp, parseErr := strconv.ParseInt(endStr, 10, 64); parseErr == nil {
			end = time.Unix(timestamp, 0)
		} else {
			// Try parsing as RFC3339
			end, err = time.Parse(time.RFC3339, endStr)
			if err != nil {
				s.writeError(w, fmt.Sprintf("Invalid end time format: %v", err), http.StatusBadRequest)
				return
			}
		}
	} else {
		// Default: now
		end = time.Now()
	}

	// Get historical metrics from metrics manager
	snapshots, err := s.metricsManager.GetHistoricalMetrics(metricType, start, end)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get historical metrics: %v", err), http.StatusInternalServerError)
		return
	}

	// Transform snapshots to frontend format
	response := map[string]interface{}{
		"type":      metricType,
		"start":     start.Unix(),
		"end":       end.Unix(),
		"snapshots": snapshots,
		"count":     len(snapshots),
	}

	s.writeJSON(w, response)
}

func (s *Server) handleGetHistoryStats(w http.ResponseWriter, r *http.Request) {
	// Only Global Admins can access history stats
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Forbidden: Only Global Admins can access history stats", http.StatusForbidden)
		return
	}

	// Get history statistics from metrics manager
	stats, err := s.metricsManager.GetHistoryStats()
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get history stats: %v", err), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, stats)
}

func (s *Server) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]string{"status": "healthy"})
}

// Helper methods
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data})
}

func (s *Server) writeError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIResponse{Success: false, Error: message})
	logrus.WithField("error", message).WithField("status", statusCode).Warn("API error")
}

// Access Key handlers
func (s *Server) handleListAllAccessKeys(w http.ResponseWriter, r *http.Request) {
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// List all users first
	users, err := s.authManager.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter users by tenant
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	var filteredUsers []auth.User

	if isGlobalAdmin {
		filteredUsers = users
	} else if currentUser.TenantID != "" {
		// Tenant admin sees only keys from their tenant users
		for _, u := range users {
			if u.TenantID == currentUser.TenantID {
				filteredUsers = append(filteredUsers, u)
			}
		}
	} else {
		// Non-admin sees only their own keys
		for _, u := range users {
			if u.ID == currentUser.ID {
				filteredUsers = append(filteredUsers, u)
				break
			}
		}
	}

	// Convert to response format (don't expose secret keys)
	type AccessKeyResponse struct {
		ID        string `json:"id"`
		UserID    string `json:"userId"`
		Status    string `json:"status"`
		CreatedAt int64  `json:"createdAt"`
		LastUsed  int64  `json:"lastUsed,omitempty"`
	}

	var allAccessKeys []AccessKeyResponse

	// Collect all access keys from filtered users
	for _, user := range filteredUsers {
		accessKeys, err := s.authManager.ListAccessKeys(r.Context(), user.ID)
		if err != nil {
			// Log error but continue with other users
			logrus.WithError(err).WithField("user_id", user.ID).Debug("Error listing access keys")
			continue
		}

		for _, key := range accessKeys {
			allAccessKeys = append(allAccessKeys, AccessKeyResponse{
				ID:        key.AccessKeyID,
				UserID:    key.UserID,
				Status:    key.Status,
				CreatedAt: key.CreatedAt,
				LastUsed:  key.LastUsed,
			})
		}
	}

	s.writeJSON(w, allAccessKeys)
}

func (s *Server) handleListAccessKeys(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	accessKeys, err := s.authManager.ListAccessKeys(r.Context(), userID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to response format (don't expose secret keys)
	type AccessKeyResponse struct {
		ID        string `json:"id"`
		UserID    string `json:"userId"`
		Status    string `json:"status"`
		CreatedAt int64  `json:"createdAt"`
		LastUsed  int64  `json:"lastUsed,omitempty"`
	}

	response := make([]AccessKeyResponse, len(accessKeys))
	for i, key := range accessKeys {
		response[i] = AccessKeyResponse{
			ID:        key.AccessKeyID,
			UserID:    key.UserID,
			Status:    key.Status,
			CreatedAt: key.CreatedAt,
			LastUsed:  key.LastUsed,
		}
	}

	s.writeJSON(w, response)
}

func (s *Server) handleCreateAccessKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	// Get user to check tenant
	user, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}

	// Check tenant access keys quota before creation
	if user.TenantID != "" {
		tenant, err := s.authManager.GetTenant(r.Context(), user.TenantID)
		if err != nil {
			s.writeError(w, "Failed to retrieve tenant information", http.StatusInternalServerError)
			return
		}

		if tenant.CurrentAccessKeys >= tenant.MaxAccessKeys {
			s.writeError(w, fmt.Sprintf("Tenant access keys quota exceeded (%d/%d). Cannot create more access keys.", tenant.CurrentAccessKeys, tenant.MaxAccessKeys), http.StatusForbidden)
			return
		}
	}

	// Generate new access key
	accessKey, err := s.authManager.GenerateAccessKey(r.Context(), userID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return complete key with secret (only shown once)
	type CreateAccessKeyResponse struct {
		ID        string `json:"id"`
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
		UserID    string `json:"userId"`
		Status    string `json:"status"`
		CreatedAt int64  `json:"createdAt"`
	}

	response := CreateAccessKeyResponse{
		ID:        accessKey.AccessKeyID,
		AccessKey: accessKey.AccessKeyID,
		SecretKey: accessKey.SecretAccessKey,
		UserID:    accessKey.UserID,
		Status:    accessKey.Status,
		CreatedAt: accessKey.CreatedAt,
	}

	s.writeJSON(w, response)
}

func (s *Server) handleDeleteAccessKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	accessKeyID := vars["accessKey"]

	if err := s.authManager.RevokeAccessKey(r.Context(), accessKeyID); err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "Access key not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.writeJSON(w, map[string]string{"message": "Access key deleted successfully"})
}

// Password management handler
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	var changeRequest struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}

	if err := json.NewDecoder(r.Body).Decode(&changeRequest); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if changeRequest.CurrentPassword == "" || changeRequest.NewPassword == "" {
		s.writeError(w, "Current password and new password are required", http.StatusBadRequest)
		return
	}

	// Validate new password strength
	if len(changeRequest.NewPassword) < 6 {
		s.writeError(w, "New password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	// Get existing user
	user, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "User not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Verify current password
	if !auth.VerifyPassword(changeRequest.CurrentPassword, user.Password) {
		s.writeError(w, "Current password is incorrect", http.StatusUnauthorized)
		return
	}

	// Hash new password
	hashedPassword, err := auth.HashPassword(changeRequest.NewPassword)
	if err != nil {
		s.writeError(w, "Failed to hash new password", http.StatusInternalServerError)
		return
	}

	// Update password
	user.Password = hashedPassword
	user.UpdatedAt = time.Now().Unix()

	if err := s.authManager.UpdateUser(r.Context(), user); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]string{"message": "Password changed successfully"})
}

// Security handlers
func (s *Server) handleGetSecurityStatus(w http.ResponseWriter, r *http.Request) {
	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	tenantID := user.TenantID

	// Get encryption status
	encryptionEnabled := s.config.Storage.EnableEncryption
	algorithm := "AES-256-GCM"

	// Get object lock statistics
	buckets, err := s.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bucketsWithLock := 0
	totalLockedObjects := int64(0)
	complianceMode := int64(0)
	governanceMode := int64(0)

	for _, b := range buckets {
		lockConfig, err := s.bucketManager.GetObjectLockConfig(r.Context(), tenantID, b.Name)
		if err == nil && lockConfig != nil {
			bucketsWithLock++

			// Count locked objects (simplified - just count buckets with lock enabled)
			// Full implementation would require iterating all objects
		}
	}

	// Get authentication stats
	allUsers, err := s.authManager.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	activeUsers := 0
	for _, user := range allUsers {
		if user.Status == "active" {
			activeUsers++
		}
	}

	// Get bucket policies count
	totalPolicies := 0
	bucketPolicies := 0
	for _, b := range buckets {
		policy, err := s.bucketManager.GetBucketPolicy(r.Context(), tenantID, b.Name)
		if err == nil && policy != nil {
			bucketPolicies++
			totalPolicies++
		}
	}

	securityStatus := map[string]interface{}{
		"encryption": map[string]interface{}{
			"enabled":      encryptionEnabled,
			"algorithm":    algorithm,
			"keyRotation":  true,
			"lastRotation": time.Now().Add(-30 * 24 * time.Hour).Format(time.RFC3339),
		},
		"objectLock": map[string]interface{}{
			"enabled":            bucketsWithLock > 0,
			"bucketsWithLock":    bucketsWithLock,
			"totalLockedObjects": totalLockedObjects,
			"complianceMode":     complianceMode,
			"governanceMode":     governanceMode,
		},
		"authentication": map[string]interface{}{
			"requireAuth":     true,
			"mfaEnabled":      false,
			"activeUsers":     activeUsers,
			"activeSessions":  0, // TODO: Track sessions
			"failedLogins24h": 0, // TODO: Track failed logins
		},
		"policies": map[string]interface{}{
			"totalPolicies":  totalPolicies,
			"bucketPolicies": bucketPolicies,
			"userPolicies":   0, // TODO: Implement user policies
			"lastUpdate":     time.Now().Format(time.RFC3339),
		},
		"audit": map[string]interface{}{
			"enabled":      false, // TODO: Implement audit logging
			"logRetention": 90,
			"totalEvents":  0,
			"eventsToday":  0,
		},
	}

	response := APIResponse{
		Success: true,
		Data:    securityStatus,
	}

	s.writeJSON(w, response)
}

// Tenant handlers
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenants, err := s.authManager.ListTenants(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Enrich tenants with real-time usage statistics
	for i := range tenants {
		// Calculate current storage bytes from tenant's buckets
		// Use the tenant's ID for filtering buckets
		buckets, err := s.bucketManager.ListBuckets(r.Context(), tenants[i].ID)
		if err == nil {
			var totalStorage int64
			var bucketCount int64
			for _, b := range buckets {
				if b.OwnerType == "tenant" && b.OwnerID == tenants[i].ID {
					bucketCount++
					// Get object count and size for this bucket
					bucketPath := tenants[i].ID + "/" + b.Name
					result, err := s.objectManager.ListObjects(r.Context(), bucketPath, "", "", "", 10000)
					if err == nil {
						for _, obj := range result.Objects {
							totalStorage += obj.Size
						}
					}
				}
			}
			tenants[i].CurrentStorageBytes = totalStorage
			tenants[i].CurrentBuckets = bucketCount
		}

		// Calculate current access keys from tenant's users
		users, err := s.authManager.ListUsers(r.Context())
		if err == nil {
			var totalKeys int64
			for _, user := range users {
				if user.TenantID == tenants[i].ID {
					keys, err := s.authManager.ListAccessKeys(r.Context(), user.ID)
					if err == nil {
						totalKeys += int64(len(keys))
					}
				}
			}
			tenants[i].CurrentAccessKeys = totalKeys
		}
	}

	// Filter tenants based on user role
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""

	// Initialize as empty slice instead of nil to ensure JSON returns [] not null
	filteredTenants := make([]*auth.Tenant, 0)

	if isGlobalAdmin {
		// Global admins see all tenants
		filteredTenants = tenants
	} else if currentUser.TenantID != "" {
		// Tenant users only see their own tenant
		for _, t := range tenants {
			if t.ID == currentUser.TenantID {
				filteredTenants = []*auth.Tenant{t}
				break
			}
		}
	}

	// Return in APIResponse format
	response := APIResponse{
		Success: true,
		Data:    filteredTenants,
	}
	s.writeJSON(w, response)
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	// Only global admins can create tenants
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Only global administrators can create tenants", http.StatusForbidden)
		return
	}

	var req struct {
		Name            string            `json:"name"`
		DisplayName     string            `json:"displayName"`
		Description     string            `json:"description"`
		MaxAccessKeys   int64             `json:"maxAccessKeys,omitempty"`
		MaxStorageBytes int64             `json:"maxStorageBytes,omitempty"`
		MaxBuckets      int64             `json:"maxBuckets,omitempty"`
		Metadata        map[string]string `json:"metadata,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		s.writeError(w, "Tenant name is required", http.StatusBadRequest)
		return
	}

	tenant := &auth.Tenant{
		ID:              auth.GenerateTenantID(),
		Name:            req.Name,
		DisplayName:     req.DisplayName,
		Description:     req.Description,
		Status:          "active",
		MaxAccessKeys:   req.MaxAccessKeys,
		MaxStorageBytes: req.MaxStorageBytes,
		MaxBuckets:      req.MaxBuckets,
		Metadata:        req.Metadata,
		CreatedAt:       time.Now().Unix(),
		UpdatedAt:       time.Now().Unix(),
	}

	if err := s.authManager.CreateTenant(r.Context(), tenant); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, tenant)
}

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID := vars["tenant"]

	tenant, err := s.authManager.GetTenant(r.Context(), tenantID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "Tenant not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.writeJSON(w, tenant)
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	// Only global admins can update tenants
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Only global administrators can update tenants", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	tenantID := vars["tenant"]

	var req struct {
		DisplayName         *string           `json:"displayName,omitempty"`
		Description         *string           `json:"description,omitempty"`
		Status              *string           `json:"status,omitempty"`
		MaxAccessKeys       *int64            `json:"maxAccessKeys,omitempty"`
		MaxStorageBytes     *int64            `json:"maxStorageBytes,omitempty"`
		MaxBuckets          *int64            `json:"maxBuckets,omitempty"`
		CurrentStorageBytes *int64            `json:"currentStorageBytes,omitempty"`
		CurrentBuckets      *int64            `json:"currentBuckets,omitempty"`
		Metadata            map[string]string `json:"metadata,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tenant, err := s.authManager.GetTenant(r.Context(), tenantID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			s.writeError(w, "Tenant not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update fields if provided
	if req.DisplayName != nil {
		tenant.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		tenant.Description = *req.Description
	}
	if req.Status != nil {
		tenant.Status = *req.Status
	}
	if req.MaxAccessKeys != nil {
		tenant.MaxAccessKeys = *req.MaxAccessKeys
	}
	if req.MaxStorageBytes != nil {
		tenant.MaxStorageBytes = *req.MaxStorageBytes
	}
	if req.MaxBuckets != nil {
		tenant.MaxBuckets = *req.MaxBuckets
	}
	if req.CurrentStorageBytes != nil {
		tenant.CurrentStorageBytes = *req.CurrentStorageBytes
	}
	if req.CurrentBuckets != nil {
		tenant.CurrentBuckets = *req.CurrentBuckets
	}
	if req.Metadata != nil {
		tenant.Metadata = req.Metadata
	}

	tenant.UpdatedAt = time.Now().Unix()

	if err := s.authManager.UpdateTenant(r.Context(), tenant); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, tenant)
}

func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	// Only global admins can delete tenants
	currentUser, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && currentUser.TenantID == ""
	if !isGlobalAdmin {
		s.writeError(w, "Only global administrators can delete tenants", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	tenantID := vars["tenant"]

	// Validate that tenant has no buckets before allowing deletion
	buckets, err := s.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to check tenant buckets: %v", err), http.StatusInternalServerError)
		return
	}

	if len(buckets) > 0 {
		s.writeError(w, fmt.Sprintf("Cannot delete tenant: tenant has %d bucket(s). Please delete all buckets before deleting the tenant", len(buckets)), http.StatusConflict)
		return
	}

	if err := s.authManager.DeleteTenant(r.Context(), tenantID); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListTenantUsers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID := vars["tenant"]

	users, err := s.authManager.ListTenantUsers(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := make([]UserResponse, len(users))
	for i, u := range users {
		response[i] = UserResponse{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			Status:      u.Status,
			Roles:       u.Roles,
			TenantID:    u.TenantID,
			CreatedAt:   u.CreatedAt,
		}
	}

	s.writeJSON(w, response)
}

// Bucket permission handlers
func (s *Server) handleListBucketPermissions(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	permissions, err := s.authManager.ListBucketPermissions(r.Context(), bucketName)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, permissions)
}

func (s *Server) handleGrantBucketPermission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	var req struct {
		UserID          string `json:"userId,omitempty"`
		TenantID        string `json:"tenantId,omitempty"`
		PermissionLevel string `json:"permissionLevel"`
		GrantedBy       string `json:"grantedBy"`
		ExpiresAt       int64  `json:"expiresAt,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if req.UserID == "" && req.TenantID == "" {
		s.writeError(w, "Either userId or tenantId must be specified", http.StatusBadRequest)
		return
	}

	if req.PermissionLevel == "" {
		s.writeError(w, "Permission level is required", http.StatusBadRequest)
		return
	}

	if req.GrantedBy == "" {
		s.writeError(w, "GrantedBy is required", http.StatusBadRequest)
		return
	}

	err := s.authManager.GrantBucketAccess(r.Context(), bucketName, req.UserID, req.TenantID, req.PermissionLevel, req.GrantedBy, req.ExpiresAt)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]string{"message": "Permission granted successfully"})
}

func (s *Server) handleRevokeBucketPermission(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Extract userID or tenantID from query params
	userID := r.URL.Query().Get("userId")
	tenantID := r.URL.Query().Get("tenantId")

	if userID == "" && tenantID == "" {
		s.writeError(w, "Either userId or tenantId query parameter is required", http.StatusBadRequest)
		return
	}

	err := s.authManager.RevokeBucketAccess(r.Context(), bucketName, userID, tenantID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateBucketOwner(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Extract user and tenant ID from context
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	tenantID := user.TenantID

	var req struct {
		OwnerID   string `json:"ownerId"`
		OwnerType string `json:"ownerType"` // "user" or "tenant"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.OwnerID == "" || req.OwnerType == "" {
		s.writeError(w, "ownerId and ownerType are required", http.StatusBadRequest)
		return
	}

	if req.OwnerType != "user" && req.OwnerType != "tenant" {
		s.writeError(w, "ownerType must be 'user' or 'tenant'", http.StatusBadRequest)
		return
	}

	// Get bucket info
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Update owner
	bucketInfo.OwnerID = req.OwnerID
	bucketInfo.OwnerType = req.OwnerType

	// Save changes
	if err := s.bucketManager.UpdateBucket(r.Context(), tenantID, bucketName, bucketInfo); err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"bucketName": bucketName,
		"ownerId":    req.OwnerID,
		"ownerType":  req.OwnerType,
	})
}

// handleListBucketShares lists all active shares for a bucket
func (s *Server) handleListBucketShares(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get user from context to determine tenant ID
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	if queryTenantID != "" {
		tenantID = queryTenantID
	}

	// Get bucket info to determine tenant ID
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		// If not found in user's tenant, try as global admin
		isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
		if isGlobalAdmin {
			tenantID = ""
			bucketInfo, err = s.bucketManager.GetBucketInfo(r.Context(), "", bucketName)
			if err != nil {
				s.writeError(w, "Bucket not found", http.StatusNotFound)
				return
			}
		} else {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
	}

	// Use the bucket's tenant ID
	shareTenantID := bucketInfo.TenantID

	shares, err := s.shareManager.ListBucketShares(r.Context(), bucketName, shareTenantID)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to list shares: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a map of object_key -> share for quick lookup
	shareMap := make(map[string]interface{})
	for _, share := range shares {
		shareMap[share.ObjectKey] = map[string]interface{}{
			"id":        share.ID,
			"expiresAt": share.ExpiresAt,
			"createdAt": share.CreatedAt.Format(time.RFC3339),
		}
	}

	s.writeJSON(w, shareMap)
}

// handleDeleteShare deletes a share for an object
func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"object": objectKey,
		"method": r.Method,
	}).Info("Delete share request received")

	// Get user from context to determine tenant ID
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for accessing tenant buckets from console)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID

	// If tenantId is explicitly provided in query, use it (for global admins or console navigation)
	if queryTenantID != "" {
		tenantID = queryTenantID
		logrus.WithFields(logrus.Fields{
			"queryTenantID": queryTenantID,
			"userTenantID":  user.TenantID,
		}).Debug("Using tenantId from query parameter for delete share")
	}

	// Get bucket info to determine tenant ID
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), tenantID, bucketName)
	if err != nil {
		// If not found in user's tenant, try as global admin
		isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
		if isGlobalAdmin {
			tenantID = ""
			bucketInfo, err = s.bucketManager.GetBucketInfo(r.Context(), "", bucketName)
			if err != nil {
				s.writeError(w, "Bucket not found", http.StatusNotFound)
				return
			}
		} else {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
			return
		}
	}

	// Use the bucket's tenant ID for looking up shares
	shareTenantID := bucketInfo.TenantID

	// Get the share first to get its ID
	share, err := s.shareManager.GetShareByObject(r.Context(), bucketName, objectKey, shareTenantID)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
			"error":  err.Error(),
		}).Warn("Share not found for deletion")
		s.writeError(w, "Share not found", http.StatusNotFound)
		return
	}

	logrus.WithFields(logrus.Fields{
		"shareID": share.ID,
		"bucket":  bucketName,
		"object":  objectKey,
	}).Info("Found share, deleting...")

	// Delete the share
	if err := s.shareManager.DeleteShare(r.Context(), share.ID); err != nil {
		logrus.WithFields(logrus.Fields{
			"shareID": share.ID,
			"error":   err.Error(),
		}).Error("Failed to delete share")
		s.writeError(w, fmt.Sprintf("Failed to delete share: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"shareID": share.ID,
		"bucket":  bucketName,
		"object":  objectKey,
	}).Info("Share deleted successfully")

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Share deleted successfully",
	})
}
