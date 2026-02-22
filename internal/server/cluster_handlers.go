package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleInitializeCluster initializes a new cluster
func (s *Server) handleInitializeCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeName string `json:"node_name"`
		Region   string `json:"region"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NodeName == "" {
		s.writeError(w, "Node name is required", http.StatusBadRequest)
		return
	}

	clusterToken, err := s.clusterManager.InitializeCluster(r.Context(), req.NodeName, req.Region)
	if err != nil {
		logrus.WithError(err).Error("Failed to initialize cluster")
		s.writeError(w, "Failed to initialize cluster: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"message":       "Cluster initialized successfully",
		"cluster_token": clusterToken,
		"node_name":     req.NodeName,
		"region":        req.Region,
	})
}

// handleJoinCluster joins an existing cluster
func (s *Server) handleJoinCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string `json:"cluster_token"`
		NodeEndpoint string `json:"node_endpoint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ClusterToken == "" || req.NodeEndpoint == "" {
		s.writeError(w, "Cluster token and node endpoint are required", http.StatusBadRequest)
		return
	}

	err := s.clusterManager.JoinCluster(r.Context(), req.ClusterToken, req.NodeEndpoint)
	if err != nil {
		logrus.WithError(err).Error("Failed to join cluster")
		s.writeError(w, "Failed to join cluster: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// After successful join, fetch JWT secret from the existing node
	// so all cluster nodes share the same JWT signing key
	jwtSecret, err := s.clusterManager.FetchJWTSecretFromNode(r.Context(), req.NodeEndpoint)
	if err != nil {
		logrus.WithError(err).Warn("Failed to fetch JWT secret from cluster node (sessions may not be shared across nodes)")
	} else {
		// Update the auth manager's JWT secret at runtime
		if setter, ok := s.authManager.(interface{ SetJWTSecret(string) }); ok {
			setter.SetJWTSecret(jwtSecret)
			logrus.Info("JWT secret synchronized from cluster node")
		} else {
			logrus.Warn("Auth manager does not support SetJWTSecret")
		}
	}

	s.writeJSON(w, map[string]interface{}{
		"message": "Successfully joined cluster",
	})
}

// handleLeaveCluster removes this node from the cluster
func (s *Server) handleLeaveCluster(w http.ResponseWriter, r *http.Request) {
	err := s.clusterManager.LeaveCluster(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to leave cluster")
		s.writeError(w, "Failed to leave cluster: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"message": "Successfully left cluster",
	})
}

// handleGetClusterStatus gets the overall cluster status
func (s *Server) handleGetClusterStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.clusterManager.GetClusterStatus(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster status")
		s.writeError(w, "Failed to get cluster status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Enrich with real bucket counts from local storage
	if s.bucketManager != nil {
		buckets, err := s.bucketManager.ListBuckets(r.Context(), "")
		if err == nil {
			status.TotalBuckets = len(buckets)
			replicated := 0
			if s.replicationManager != nil {
				for _, b := range buckets {
					rules, err := s.replicationManager.GetRulesForBucket(r.Context(), b.Name)
					if err == nil && len(rules) > 0 {
						replicated++
					}
				}
			}
			status.ReplicatedBuckets = replicated
			status.LocalBuckets = status.TotalBuckets - replicated
		}
	}

	s.writeJSON(w, status)
}

// handleGetClusterToken returns the cluster token (global admin only)
func (s *Server) handleGetClusterToken(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		s.writeError(w, "Cluster not initialized", http.StatusBadRequest)
		return
	}

	s.writeJSON(w, map[string]string{
		"cluster_token": config.ClusterToken,
	})
}

// handleGetClusterConfig gets this node's cluster configuration
func (s *Server) handleGetClusterConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		// If cluster is not initialized, return a default standalone config
		if err.Error() == "cluster not initialized" {
			s.writeJSON(w, map[string]interface{}{
				"is_cluster_enabled": false,
				"node_id":            "",
				"node_name":          "",
				"cluster_token":      "",
				"region":             "",
				"created_at":         0,
			})
			return
		}
		logrus.WithError(err).Error("Failed to get cluster config")
		s.writeError(w, "Failed to get cluster config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, config)
}

// handleListClusterNodes lists all nodes in the cluster
func (s *Server) handleListClusterNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list cluster nodes")
		s.writeError(w, "Failed to list cluster nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"nodes": nodes,
		"total": len(nodes),
	})
}

// handleAddClusterNode adds a remote standalone node to this cluster.
// It authenticates to the remote node using admin credentials, then
// triggers a cluster join on the remote node.
func (s *Server) handleAddClusterNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" || req.Username == "" || req.Password == "" {
		s.writeError(w, "Endpoint, username, and password are required", http.StatusBadRequest)
		return
	}

	// Get local cluster config to obtain the cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		s.writeError(w, "Cluster not initialized", http.StatusBadRequest)
		return
	}

	// Determine this node's console URL for the remote node to connect back
	localEndpoint := s.config.PublicConsoleURL
	if localEndpoint == "" {
		s.writeError(w, "PublicConsoleURL is not configured on this node", http.StatusInternalServerError)
		return
	}

	// Step 1: Authenticate to the remote node
	remoteEndpoint := strings.TrimRight(req.Endpoint, "/")
	loginPayload, _ := json.Marshal(map[string]string{
		"username": req.Username,
		"password": req.Password,
	})

	loginResp, err := http.Post(remoteEndpoint+"/api/v1/auth/login", "application/json", bytes.NewReader(loginPayload))
	if err != nil {
		logrus.WithError(err).Error("Failed to connect to remote node")
		s.writeError(w, "Failed to connect to remote node: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		s.writeError(w, "Authentication failed on remote node (check credentials)", http.StatusUnauthorized)
		return
	}

	var loginResult struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginResult); err != nil || loginResult.Data.Token == "" {
		s.writeError(w, "Failed to parse authentication response from remote node", http.StatusBadGateway)
		return
	}

	// Step 2: Check if remote node is already in a cluster
	configReq, _ := http.NewRequestWithContext(r.Context(), "GET", remoteEndpoint+"/api/v1/cluster/config", nil)
	configReq.Header.Set("Authorization", "Bearer "+loginResult.Data.Token)

	configResp, err := http.DefaultClient.Do(configReq)
	if err != nil {
		logrus.WithError(err).Error("Failed to check remote node cluster status")
		s.writeError(w, "Failed to check remote node cluster status: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer configResp.Body.Close()

	if configResp.StatusCode == http.StatusOK {
		var remoteConfig struct {
			Data struct {
				IsClusterEnabled bool `json:"is_cluster_enabled"`
			} `json:"data"`
		}
		if err := json.NewDecoder(configResp.Body).Decode(&remoteConfig); err == nil && remoteConfig.Data.IsClusterEnabled {
			s.writeError(w, "Remote node is already part of a cluster. It must leave its current cluster before joining a new one.", http.StatusConflict)
			return
		}
	}

	// Step 3: Call the remote node's join endpoint
	joinPayload, _ := json.Marshal(map[string]string{
		"cluster_token": config.ClusterToken,
		"node_endpoint": localEndpoint,
	})

	joinReq, _ := http.NewRequestWithContext(r.Context(), "POST", remoteEndpoint+"/api/v1/cluster/join", bytes.NewReader(joinPayload))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+loginResult.Data.Token)

	joinResp, err := http.DefaultClient.Do(joinReq)
	if err != nil {
		logrus.WithError(err).Error("Failed to send join request to remote node")
		s.writeError(w, "Failed to send join request to remote node: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer joinResp.Body.Close()

	if joinResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(joinResp.Body)
		s.writeError(w, "Remote node failed to join cluster: "+string(body), http.StatusBadGateway)
		return
	}

	logrus.WithField("endpoint", req.Endpoint).Info("Remote node joined cluster successfully")

	s.writeJSON(w, map[string]interface{}{
		"message": "Node added to cluster successfully",
	})
}

// handleGetClusterNode gets details of a specific node
func (s *Server) handleGetClusterNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["nodeId"]

	node, err := s.clusterManager.GetNode(r.Context(), nodeID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster node")
		s.writeError(w, "Failed to get cluster node: "+err.Error(), http.StatusNotFound)
		return
	}

	s.writeJSON(w, node)
}

// handleUpdateClusterNode updates a node's information
func (s *Server) handleUpdateClusterNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["nodeId"]

	var node cluster.Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	node.ID = nodeID

	err := s.clusterManager.UpdateNode(r.Context(), &node)
	if err != nil {
		logrus.WithError(err).Error("Failed to update cluster node")
		s.writeError(w, "Failed to update cluster node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"message": "Node updated successfully",
	})
}

// handleRemoveClusterNode removes a node from the cluster
func (s *Server) handleRemoveClusterNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["nodeId"]

	// Prevent removing the local node
	config, err := s.clusterManager.GetConfig(r.Context())
	if err == nil && config.NodeID == nodeID {
		s.writeError(w, "Cannot remove the local node from its own cluster. Use 'Leave Cluster' instead.", http.StatusBadRequest)
		return
	}

	err = s.clusterManager.RemoveNode(r.Context(), nodeID)
	if err != nil {
		logrus.WithError(err).Error("Failed to remove cluster node")
		s.writeError(w, "Failed to remove cluster node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate router cache when a node is removed
	// This ensures requests don't get routed to the removed node
	if s.clusterRouter != nil {
		// We don't have a way to invalidate all cache entries for a specific node,
		// but the cache will naturally expire within TTL (5 minutes)
		logrus.WithField("node_id", nodeID).Info("Node removed, cache will expire naturally")
	}

	s.writeJSON(w, map[string]interface{}{
		"message": "Node removed successfully",
	})
}

// handleCheckNodeHealth performs a health check on a specific node
func (s *Server) handleCheckNodeHealth(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["nodeId"]

	healthStatus, err := s.clusterManager.CheckNodeHealth(r.Context(), nodeID)
	if err != nil {
		logrus.WithError(err).Error("Failed to check node health")
		s.writeError(w, "Failed to check node health: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, healthStatus)
}

// handleGetClusterBuckets lists all buckets with replication information
func (s *Server) handleGetClusterBuckets(w http.ResponseWriter, r *http.Request) {
	// Get tenant from context (for multi-tenancy support)
	tenantID, ok := r.Context().Value("tenant_id").(string)
	if !ok {
		tenantID = "" // Global admin sees all buckets
	}

	// List all buckets
	buckets, err := s.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		logrus.WithError(err).Error("Failed to list buckets")
		s.writeError(w, "Failed to list buckets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response with replication info for each bucket
	type BucketWithReplication struct {
		Name             string `json:"name"`
		TenantID         string `json:"tenant_id,omitempty"`
		PrimaryNode      string `json:"primary_node"`
		ReplicaCount     int    `json:"replica_count"`
		HasReplication   bool   `json:"has_replication"`
		ReplicationRules int    `json:"replication_rules"`
	}

	var bucketsWithReplication []BucketWithReplication

	for _, bucket := range buckets {
		// Get replication rules for this bucket
		rules, err := s.replicationManager.GetRulesForBucket(r.Context(), bucket.Name)
		if err != nil {
			logrus.WithError(err).WithField("bucket", bucket.Name).Warn("Failed to get replication rules")
			rules = nil
		}

		replicaCount := len(rules)
		hasReplication := replicaCount > 0

		// Determine primary node (local node if cluster is enabled)
		primaryNode := "local"
		if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
			config, err := s.clusterManager.GetConfig(r.Context())
			if err == nil {
				primaryNode = config.NodeName
			}
		}

		bucketsWithReplication = append(bucketsWithReplication, BucketWithReplication{
			Name:             bucket.Name,
			TenantID:         bucket.TenantID,
			PrimaryNode:      primaryNode,
			ReplicaCount:     replicaCount,
			HasReplication:   hasReplication,
			ReplicationRules: replicaCount,
		})
	}

	s.writeJSON(w, map[string]interface{}{
		"buckets": bucketsWithReplication,
		"total":   len(bucketsWithReplication),
	})
}

// handleGetBucketReplicas gets replication info for a specific bucket
func (s *Server) handleGetBucketReplicas(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get replication rules for this bucket
	rules, err := s.replicationManager.GetRulesForBucket(r.Context(), bucketName)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rules")
		s.writeError(w, "Failed to get replication rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"bucket":  bucketName,
		"rules":   rules,
		"total":   len(rules),
	})
}

// handleGetCacheStats gets bucket location cache statistics
func (s *Server) handleGetCacheStats(w http.ResponseWriter, r *http.Request) {
	if s.clusterRouter == nil {
		s.writeError(w, "Cluster router not initialized", http.StatusServiceUnavailable)
		return
	}

	stats := s.clusterRouter.GetCacheStats()
	s.writeJSON(w, stats)
}

// handleInvalidateCache invalidates the bucket location cache
func (s *Server) handleInvalidateCache(w http.ResponseWriter, r *http.Request) {
	if s.clusterRouter == nil {
		s.writeError(w, "Cluster router not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Bucket string `json:"bucket,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// If bucket is specified, invalidate just that bucket
	// Otherwise, clear the entire cache
	if req.Bucket != "" {
		s.clusterRouter.InvalidateCache(req.Bucket)
		s.writeJSON(w, map[string]interface{}{
			"message": "Cache invalidated for bucket: " + req.Bucket,
		})
	} else {
		// To clear entire cache, we need to add a method to Router
		// For now, return error
		s.writeError(w, "Bucket parameter is required. To clear entire cache, restart the service.", http.StatusBadRequest)
		return
	}
}

// handleGetLocalBuckets returns buckets from this node only (internal cluster API)
func (s *Server) handleGetLocalBuckets(w http.ResponseWriter, r *http.Request) {
	// Extract tenant_id from query parameters
	tenantID := r.URL.Query().Get("tenant_id")

	// List buckets from local node only
	buckets, err := s.bucketManager.ListBuckets(r.Context(), tenantID)
	if err != nil {
		logrus.WithError(err).Error("Failed to list local buckets")
		s.writeError(w, "Failed to list buckets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to BucketWithLocation format
	bucketsWithLocation := make([]cluster.BucketWithLocation, len(buckets))
	for i, bucket := range buckets {
		versioningStr := ""
		if bucket.Versioning != nil {
			versioningStr = bucket.Versioning.Status
		}

		bucketsWithLocation[i] = cluster.BucketWithLocation{
			Name:        bucket.Name,
			TenantID:    bucket.TenantID,
			CreatedAt:   bucket.CreatedAt,
			Versioning:  versioningStr,
			ObjectCount: bucket.ObjectCount,
			SizeBytes:   bucket.TotalSize,
			Metadata:    bucket.Metadata,
			Tags:        bucket.Tags,
			// NodeID and NodeName will be filled by the aggregator
		}
	}

	s.writeJSON(w, map[string]interface{}{
		"buckets": bucketsWithLocation,
	})
}

// handleGetTenantStorage returns tenant storage usage from this node only (internal cluster API)
func (s *Server) handleGetTenantStorage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	tenantID := vars["tenantID"]

	// Get tenant from auth manager
	tenant, err := s.authManager.GetTenant(r.Context(), tenantID)
	if err != nil {
		logrus.WithError(err).WithField("tenant_id", tenantID).Error("Failed to get tenant")
		s.writeError(w, "Failed to get tenant: "+err.Error(), http.StatusNotFound)
		return
	}

	// Return storage info in TenantStorageInfo format
	storageInfo := cluster.TenantStorageInfo{
		TenantID:            tenant.ID,
		CurrentStorageBytes: tenant.CurrentStorageBytes,
		NodeID:              "", // Will be filled by aggregator
		NodeName:            "", // Will be filled by aggregator
	}

	s.writeJSON(w, storageInfo)
}

// handleValidateClusterToken validates a cluster token for node join operations
func (s *Server) handleValidateClusterToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string `json:"cluster_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ClusterToken == "" {
		s.writeError(w, "Cluster token is required", http.StatusBadRequest)
		return
	}

	// Get local cluster config
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster config")
		s.writeError(w, "Failed to get cluster config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Validate token matches local cluster token
	if config.ClusterToken != req.ClusterToken {
		s.writeError(w, "Invalid cluster token", http.StatusUnauthorized)
		return
	}

	// Get cluster node count
	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list nodes")
		s.writeError(w, "Failed to list nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return cluster info
	clusterInfo := cluster.ClusterInfo{
		ClusterID: config.NodeID, // Use first node ID as cluster ID
		Region:    config.Region,
		NodeCount: len(nodes),
	}

	s.writeJSON(w, map[string]interface{}{
		"valid":        true,
		"cluster_info": clusterInfo,
	})
}

// handleRegisterNode registers a new node joining the cluster
func (s *Server) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string        `json:"cluster_token"`
		Node         *cluster.Node `json:"node"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ClusterToken == "" {
		s.writeError(w, "Cluster token is required", http.StatusBadRequest)
		return
	}

	if req.Node == nil {
		s.writeError(w, "Node information is required", http.StatusBadRequest)
		return
	}

	// Validate cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster config")
		s.writeError(w, "Failed to get cluster config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if config.ClusterToken != req.ClusterToken {
		s.writeError(w, "Invalid cluster token", http.StatusUnauthorized)
		return
	}

	// Add node to cluster
	err = s.clusterManager.AddNode(r.Context(), req.Node)
	if err != nil {
		logrus.WithError(err).Error("Failed to register node")
		s.writeError(w, "Failed to register node: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"node_id":   req.Node.ID,
		"node_name": req.Node.Name,
		"endpoint":  req.Node.Endpoint,
	}).Info("Node registered successfully")

	s.writeJSON(w, map[string]interface{}{
		"node": req.Node,
	})
}

// handleGetClusterJWTSecret returns the JWT secret for cluster synchronization (HMAC-authenticated)
func (s *Server) handleGetClusterJWTSecret(w http.ResponseWriter, r *http.Request) {
	// Read JWT secret from system_settings
	var jwtSecret string
	err := s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, "jwt_secret").Scan(&jwtSecret)
	if err != nil {
		logrus.WithError(err).Error("Failed to read JWT secret from database")
		s.writeError(w, "Failed to read JWT secret", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]string{
		"jwt_secret": jwtSecret,
	})
}

// handleGetClusterNodesInternal returns cluster nodes for internal cluster sync (with token auth)
func (s *Server) handleGetClusterNodesInternal(w http.ResponseWriter, r *http.Request) {
	clusterToken := r.URL.Query().Get("cluster_token")
	if clusterToken == "" {
		s.writeError(w, "Cluster token is required", http.StatusBadRequest)
		return
	}

	// Validate cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster config")
		s.writeError(w, "Failed to get cluster config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if config.ClusterToken != clusterToken {
		s.writeError(w, "Invalid cluster token", http.StatusUnauthorized)
		return
	}

	// Get all nodes
	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list nodes")
		s.writeError(w, "Failed to list nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"nodes": nodes,
	})
}
