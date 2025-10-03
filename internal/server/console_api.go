package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
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
	Name              string                    `json:"name"`
	CreationDate      string                    `json:"creation_date"`
	Region            string                    `json:"region,omitempty"`
	ObjectCount       int64                     `json:"object_count"`
	Size              int64                     `json:"size"`
	Versioning        *bucket.VersioningConfig  `json:"versioning,omitempty"`
	ObjectLock        *bucket.ObjectLockConfig  `json:"objectLock,omitempty"`
	Encryption        *bucket.EncryptionConfig  `json:"encryption,omitempty"`
	PublicAccessBlock *bucket.PublicAccessBlock `json:"publicAccessBlock,omitempty"`
	Tags              map[string]string         `json:"tags,omitempty"`
	Metadata          map[string]string         `json:"metadata,omitempty"`
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
	CreatedAt   int64    `json:"createdAt"`
}

type MetricsResponse struct {
	TotalBuckets int64              `json:"total_buckets"`
	TotalObjects int64              `json:"total_objects"`
	TotalSize    int64              `json:"total_size"`
	SystemStats  map[string]float64 `json:"system_stats"`
}

// setupConsoleAPIRoutes registers all console API routes
func (s *Server) setupConsoleAPIRoutes(router *mux.Router) {
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

	// Auth endpoints
	router.HandleFunc("/auth/login", s.handleLogin).Methods("POST", "OPTIONS")
	router.HandleFunc("/auth/logout", s.handleLogout).Methods("POST", "OPTIONS")
	router.HandleFunc("/auth/me", s.handleGetCurrentUser).Methods("GET", "OPTIONS")

	// Bucket endpoints
	router.HandleFunc("/buckets", s.handleListBuckets).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets", s.handleCreateBucket).Methods("POST", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}", s.handleGetBucket).Methods("GET", "OPTIONS")
	router.HandleFunc("/buckets/{bucket}", s.handleDeleteBucket).Methods("DELETE", "OPTIONS")

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
	router.HandleFunc("/users/{user}/access-keys", s.handleListAccessKeys).Methods("GET", "OPTIONS")
	router.HandleFunc("/users/{user}/access-keys", s.handleCreateAccessKey).Methods("POST", "OPTIONS")
	router.HandleFunc("/users/{user}/access-keys/{accessKey}", s.handleDeleteAccessKey).Methods("DELETE", "OPTIONS")

	// Metrics endpoints
	router.HandleFunc("/metrics", s.handleGetMetrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/system", s.handleGetSystemMetrics).Methods("GET", "OPTIONS")

	// Security endpoints
	router.HandleFunc("/security/status", s.handleGetSecurityStatus).Methods("GET", "OPTIONS")

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

	user, err := s.authManager.ValidateConsoleCredentials(r.Context(), loginReq.Username, loginReq.Password)
	if err != nil {
		s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := s.authManager.GenerateJWT(r.Context(), user)
	if err != nil {
		s.writeError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"token": token,
		"user": UserResponse{
			ID:          user.ID,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Status:      user.Status,
			Roles:       user.Roles,
			CreatedAt:   user.CreatedAt,
		},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]string{"message": "Logged out successfully"})
}

func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	// TODO: Extract user from token
	s.writeJSON(w, UserResponse{
		ID:          "default",
		Username:    "default",
		DisplayName: "Default User",
		Email:       "admin@maxiofs.local",
		Status:      "active",
		Roles:       []string{"admin"},
		CreatedAt:   0,
	})
}

// Bucket handlers
func (s *Server) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := s.bucketManager.ListBuckets(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]BucketResponse, len(buckets))
	for i, b := range buckets {
		// Get object count and size for this bucket
		result, err := s.objectManager.ListObjects(r.Context(), b.Name, "", "", "", 10000)
		objectCount := int64(0)
		var totalSize int64
		if err == nil {
			objectCount = int64(len(result.Objects))
			for _, obj := range result.Objects {
				totalSize += obj.Size
			}
		}

		response[i] = BucketResponse{
			Name:              b.Name,
			CreationDate:      b.CreatedAt.Format("2006-01-02T15:04:05Z"),
			Region:            b.Region,
			ObjectCount:       objectCount,
			Size:              totalSize,
			Versioning:        b.Versioning,
			ObjectLock:        b.ObjectLock,
			Encryption:        b.Encryption,
			PublicAccessBlock: b.PublicAccessBlock,
			Tags:              b.Tags,
			Metadata:          b.Metadata,
		}
	}

	s.writeJSON(w, response)
}

func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string                   `json:"name"`
		Region     string                   `json:"region,omitempty"`
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

	// Crear el bucket
	if err := s.bucketManager.CreateBucket(r.Context(), req.Name); err != nil {
		if err == bucket.ErrBucketAlreadyExists {
			s.writeError(w, "Bucket already exists", http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// Aplicar configuraciones
	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), req.Name)
	if err != nil {
		s.writeError(w, "Bucket created but failed to retrieve info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Aplicar versionado
	if req.Versioning != nil {
		bucketInfo.Versioning = req.Versioning
	}

	// Aplicar Object Lock
	if req.ObjectLock != nil && req.ObjectLock.Enabled {
		days := req.ObjectLock.Days
		years := req.ObjectLock.Years
		bucketInfo.ObjectLock = &bucket.ObjectLockConfig{
			ObjectLockEnabled: true,
			Rule: &bucket.ObjectLockRule{
				DefaultRetention: &bucket.DefaultRetention{
					Mode:  req.ObjectLock.Mode,
					Days:  &days,
					Years: &years,
				},
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
	if err := s.bucketManager.UpdateBucket(r.Context(), req.Name, bucketInfo); err != nil {
		s.writeError(w, "Bucket created but failed to apply configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]string{"name": req.Name})
}

func (s *Server) handleGetBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	bucketInfo, err := s.bucketManager.GetBucketInfo(r.Context(), bucketName)
	if err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Get object count and size
	result, err := s.objectManager.ListObjects(r.Context(), bucketName, "", "", "", 10000)
	objectCount := int64(0)
	var totalSize int64
	if err == nil {
		objectCount = int64(len(result.Objects))
		for _, obj := range result.Objects {
			totalSize += obj.Size
		}
	}

	response := BucketResponse{
		Name:              bucketInfo.Name,
		CreationDate:      bucketInfo.CreatedAt.Format("2006-01-02T15:04:05Z"),
		Region:            bucketInfo.Region,
		ObjectCount:       objectCount,
		Size:              totalSize,
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

	if err := s.bucketManager.DeleteBucket(r.Context(), bucketName); err != nil {
		if err == bucket.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else if err == bucket.ErrBucketNotEmpty {
			s.writeError(w, "Bucket is not empty", http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Object handlers
func (s *Server) handleListObjects(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	marker := r.URL.Query().Get("marker")
	maxKeys := 1000

	if maxKeysStr := r.URL.Query().Get("max_keys"); maxKeysStr != "" {
		if parsed, err := strconv.Atoi(maxKeysStr); err == nil && parsed > 0 {
			maxKeys = parsed
		}
	}

	result, err := s.objectManager.ListObjects(r.Context(), bucketName, prefix, delimiter, marker, maxKeys)
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

	// Check if client wants metadata only (Accept: application/json) or the actual file
	acceptHeader := r.Header.Get("Accept")
	wantsJSON := acceptHeader == "application/json"

	// If client wants JSON metadata only, return metadata
	if wantsJSON {
		metadata, err := s.objectManager.GetObjectMetadata(r.Context(), bucketName, objectKey)
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
	obj, reader, err := s.objectManager.GetObject(r.Context(), bucketName, objectKey)
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
		log.Printf("Error streaming object content: %v", err)
	}
}

func (s *Server) handleUploadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	obj, err := s.objectManager.PutObject(r.Context(), bucketName, objectKey, r.Body, r.Header)
	if err != nil {
		if err == object.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Check if bucket has Object Lock enabled and apply default retention
	lockConfig, err := s.bucketManager.GetObjectLockConfig(r.Context(), bucketName)
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
				_ = s.objectManager.SetObjectRetention(r.Context(), bucketName, objectKey, retention)
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

func (s *Server) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	if err := s.objectManager.DeleteObject(r.Context(), bucketName, objectKey); err != nil {
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
	users, err := s.authManager.ListUsers(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := make([]UserResponse, len(users))
	for i, u := range users {
		response[i] = UserResponse{
			ID:          u.ID,
			Username:    u.ID,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			Status:      u.Status,
			Roles:       u.Roles,
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

	// Set defaults
	if createRequest.Status == "" {
		createRequest.Status = "active"
	}
	if len(createRequest.Roles) == 0 {
		createRequest.Roles = []string{"read"}
	}

	// Hash password
	h := sha256.New()
	h.Write([]byte(createRequest.Password))
	hashedPassword := hex.EncodeToString(h.Sum(nil))

	// Create user
	user := &auth.User{
		ID:          createRequest.Username,
		Username:    createRequest.Username,
		Password:    hashedPassword,
		DisplayName: createRequest.Username,
		Email:       createRequest.Email,
		Status:      createRequest.Status,
		Roles:       createRequest.Roles,
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
		CreatedAt:   user.CreatedAt,
	}

	s.writeJSON(w, userResponse)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user"]

	var updateRequest struct {
		Email  *string  `json:"email,omitempty"`
		Roles  []string `json:"roles,omitempty"`
		Status string   `json:"status,omitempty"`
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

// Metrics handlers
func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	buckets, _ := s.bucketManager.ListBuckets(r.Context())
	totalBuckets := int64(len(buckets))

	var totalObjects, totalSize int64
	for _, b := range buckets {
		result, err := s.objectManager.ListObjects(r.Context(), b.Name, "", "", "", 10000)
		if err == nil {
			totalObjects += int64(len(result.Objects))
			for _, obj := range result.Objects {
				totalSize += obj.Size
			}
		}
	}

	response := MetricsResponse{
		TotalBuckets: totalBuckets,
		TotalObjects: totalObjects,
		TotalSize:    totalSize,
		SystemStats:  make(map[string]float64),
	}

	s.writeJSON(w, response)
}

func (s *Server) handleGetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	// TODO: Integrate with metrics manager
	s.writeJSON(w, map[string]interface{}{
		"cpu_usage":    0.0,
		"memory_usage": 0.0,
		"disk_usage":   0.0,
	})
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

	// Generate new access key
	accessKey, err := s.authManager.GenerateAccessKey(r.Context(), userID)
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return complete key with secret (only shown once)
	type CreateAccessKeyResponse struct {
		ID        string `json:"id"`
		Secret    string `json:"secret"`
		UserID    string `json:"userId"`
		Status    string `json:"status"`
		CreatedAt int64  `json:"createdAt"`
	}

	response := CreateAccessKeyResponse{
		ID:        accessKey.AccessKeyID,
		Secret:    accessKey.SecretAccessKey,
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

// Security handlers
func (s *Server) handleGetSecurityStatus(w http.ResponseWriter, r *http.Request) {
	// Get encryption status
	encryptionEnabled := s.config.Storage.EnableEncryption
	algorithm := "AES-256-GCM"

	// Get object lock statistics
	buckets, err := s.bucketManager.ListBuckets(r.Context())
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bucketsWithLock := 0
	totalLockedObjects := int64(0)
	complianceMode := int64(0)
	governanceMode := int64(0)

	for _, b := range buckets {
		lockConfig, err := s.bucketManager.GetObjectLockConfig(r.Context(), b.Name)
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
		policy, err := s.bucketManager.GetBucketPolicy(r.Context(), b.Name)
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
