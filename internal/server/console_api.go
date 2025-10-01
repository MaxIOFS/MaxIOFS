package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
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
	Name         string `json:"name"`
	CreationDate string `json:"creation_date"`
	ObjectCount  int64  `json:"object_count"`
	Size         int64  `json:"size"`
}

type ObjectResponse struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified string            `json:"last_modified"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type UserResponse struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	CreatedAt   int64    `json:"created_at"`
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

	// Metrics endpoints
	router.HandleFunc("/metrics", s.handleGetMetrics).Methods("GET", "OPTIONS")
	router.HandleFunc("/metrics/system", s.handleGetSystemMetrics).Methods("GET", "OPTIONS")

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
			Name:         b.Name,
			CreationDate: b.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ObjectCount:  objectCount,
			Size:         totalSize,
		}
	}

	s.writeJSON(w, response)
}

func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.bucketManager.CreateBucket(r.Context(), req.Name); err != nil {
		if err == bucket.ErrBucketAlreadyExists {
			s.writeError(w, "Bucket already exists", http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusBadRequest)
		}
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
		Name:         bucketInfo.Name,
		CreationDate: bucketInfo.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ObjectCount:  objectCount,
		Size:         totalSize,
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
}

func (s *Server) handleUploadObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	// Debug logging
	fmt.Printf("DEBUG Upload: bucket=%s, objectKey=%s\n", bucketName, objectKey)
	fmt.Printf("DEBUG Upload: raw URL path=%s\n", r.URL.Path)
	fmt.Printf("DEBUG Upload: method=%s\n", r.Method)

	obj, err := s.objectManager.PutObject(r.Context(), bucketName, objectKey, r.Body, r.Header)
	if err != nil {
		if err == object.ErrBucketNotFound {
			s.writeError(w, "Bucket not found", http.StatusNotFound)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
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
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
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
	// Implementation for creating users
	s.writeError(w, "Not implemented", http.StatusNotImplemented)
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	// Implementation for getting user details
	s.writeError(w, "Not implemented", http.StatusNotImplemented)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	// Implementation for updating users
	s.writeError(w, "Not implemented", http.StatusNotImplemented)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	// Implementation for deleting users
	s.writeError(w, "Not implemented", http.StatusNotImplemented)
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
