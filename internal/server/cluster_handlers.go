package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleInitializeCluster initializes a new cluster
func (s *Server) handleInitializeCluster(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	var req struct {
		NodeName    string `json:"node_name"`
		Region      string `json:"region"`
		NodeAddress string `json:"node_address"` // IP address of this node for cluster communication
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NodeName == "" {
		s.writeError(w, "Node name is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.NodeAddress) == "" {
		s.writeError(w, "Node address (IP) is required", http.StatusBadRequest)
		return
	}

	// Derive this node's cluster endpoint. If the admin selected a specific IP
	// (from the network-interfaces list) respect it; otherwise auto-detect.
	nodeClusterEndpoint, err := s.resolveLocalClusterEndpoint(r, req.NodeAddress)
	if err != nil {
		logrus.WithError(err).Error("Failed to resolve local cluster endpoint")
		s.writeError(w, "Failed to determine local cluster address: "+err.Error(), http.StatusInternalServerError)
		return
	}

	clusterToken, err := s.clusterManager.InitializeCluster(r.Context(), req.NodeName, req.Region, nodeClusterEndpoint)
	if err != nil {
		logrus.WithError(err).Error("Failed to initialize cluster")
		s.writeError(w, "Failed to initialize cluster: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Cluster certs were just generated — restart the cluster listener with TLS.
	go s.enableClusterTLS()

	s.writeJSON(w, map[string]interface{}{
		"message":       "Cluster initialized successfully",
		"cluster_token": clusterToken,
		"node_name":     req.NodeName,
		"region":        req.Region,
	})
}

// handleJoinCluster joins an existing cluster
func (s *Server) handleJoinCluster(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	var pkg cluster.ClusterJoinPackage
	if err := json.NewDecoder(r.Body).Decode(&pkg); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Apply the join package: store config + certs, load TLS into memory.
	if err := s.clusterManager.AcceptClusterJoin(r.Context(), &pkg); err != nil {
		logrus.WithError(err).Error("Failed to accept cluster join package")
		s.writeError(w, "Failed to join cluster: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Synchronize the cluster JWT secret so cross-node sessions work immediately.
	if pkg.JWTSecret != "" {
		if setter, ok := s.authManager.(interface{ SetJWTSecret(string) }); ok {
			setter.SetJWTSecret(pkg.JWTSecret)
			logrus.Info("JWT secret synchronized from cluster join package")
		}
	}

	// Open port 8082 with TLS now that certs are loaded.
	s.enableClusterTLS()

	// Validate connectivity to Node A's 8082 using the cluster TLS client.
	// This confirms the TLS channel between the two nodes is working end-to-end.
	if pkg.NodeEndpoint != "" {
		healthURL := strings.TrimRight(pkg.NodeEndpoint, "/") + "/health"
		if err := s.clusterManager.CheckNodeConnectivity(healthURL); err != nil {
			logrus.WithError(err).WithField("node_endpoint", pkg.NodeEndpoint).Warn("Connectivity check to cluster node failed")
			s.writeError(w, "Joined but connectivity check to cluster node failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		logrus.WithField("node_endpoint", pkg.NodeEndpoint).Info("Connectivity to cluster node confirmed via TLS")
	}

	s.writeJSON(w, map[string]interface{}{
		"message": "Successfully joined cluster",
	})
}

// handleLeaveCluster removes this node from the cluster
func (s *Server) handleLeaveCluster(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

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

// clusterNodeResponse is the JSON shape returned by handleListClusterNodes.
// It adds an is_local flag on top of the standard cluster.Node fields.
type clusterNodeResponse struct {
	cluster.Node
	IsLocal bool `json:"is_local"`
}

// handleListClusterNodes lists all nodes in the cluster.
// The local node is enriched with live OS disk stats and flagged with is_local=true,
// since the local entry in the DB starts at zero and is not updated by health checks.
func (s *Server) handleListClusterNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list cluster nodes")
		s.writeError(w, "Failed to list cluster nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	localNodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())

	enriched := make([]clusterNodeResponse, 0, len(nodes))
	for _, n := range nodes {
		entry := clusterNodeResponse{Node: *n, IsLocal: n.ID == localNodeID}
		if entry.IsLocal {
			// Replace DB capacity (starts at zero) with live OS disk stats
			if diskStats, diskErr := s.systemMetrics.GetDiskUsage(); diskErr == nil && diskStats != nil {
				entry.Node.CapacityTotal = int64(diskStats.TotalBytes)
				entry.Node.CapacityUsed = int64(diskStats.UsedBytes)
			}
		}
		enriched = append(enriched, entry)
	}

	s.writeJSON(w, map[string]interface{}{
		"nodes": enriched,
		"total": len(enriched),
	})
}

// handleAddClusterNode adds a remote standalone node to this cluster.
// It authenticates to the remote node using admin credentials, then
// triggers a cluster join on the remote node.
func (s *Server) handleAddClusterNode(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	var req struct {
		Endpoint string `json:"endpoint"`
		Username string `json:"username"`
		Password string `json:"password"`
		NodeName string `json:"node_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Endpoint == "" || req.Username == "" || req.Password == "" {
		s.writeError(w, "Endpoint, username, and password are required", http.StatusBadRequest)
		return
	}

	// Parse B's console address (default port 8081).
	remoteConsoleURL, err := parseNodeAddress(req.Endpoint, "8081")
	if err != nil {
		s.writeError(w, "Invalid node address: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Endpoint = remoteConsoleURL

	// Get Node A's cluster config (token, region) and CA key pair for cert generation
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		s.writeError(w, "Cluster not initialized", http.StatusBadRequest)
		return
	}
	caCertPEM := s.clusterManager.GetCACertPEM()
	caKeyPEM := s.clusterManager.GetCAKeyPEM()
	if caCertPEM == "" || caKeyPEM == "" {
		s.writeError(w, "Cluster CA certificates not available", http.StatusInternalServerError)
		return
	}

	// Use insecure TLS — Node B is standalone and may not have TLS at all
	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		},
	}

	remoteEndpoint := strings.TrimRight(req.Endpoint, "/")

	// Step 1: Authenticate to Node B via 8081
	loginPayload, _ := json.Marshal(map[string]string{
		"username": req.Username,
		"password": req.Password,
	})
	loginResp, err := insecureClient.Post(remoteEndpoint+"/api/v1/auth/login", "application/json", bytes.NewReader(loginPayload))
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
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginResult); err != nil {
		s.writeError(w, "Failed to parse authentication response from remote node", http.StatusBadGateway)
		return
	}
	remoteToken := loginResult.AccessToken
	if remoteToken == "" {
		remoteToken = loginResult.Token
	}
	if remoteToken == "" {
		s.writeError(w, "Failed to obtain token from remote node", http.StatusBadGateway)
		return
	}

	// Step 2: Verify Node B is in standalone mode
	configReq, _ := http.NewRequestWithContext(r.Context(), "GET", remoteEndpoint+"/api/v1/cluster/config", nil)
	configReq.Header.Set("Authorization", "Bearer "+remoteToken)
	configResp, err := insecureClient.Do(configReq)
	if err != nil {
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

	// Step 3: Build the join package
	// Generate Node B's node ID, name, and TLS cert+key signed by Node A's CA
	nodeID := uuid.New().String()
	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = fmt.Sprintf("node-%s", nodeID[:8])
	}

	remoteClusterPort := "8082"
	if s.config.ClusterListen != "" {
		if _, p, err := net.SplitHostPort(s.config.ClusterListen); err == nil && p != "" {
			remoteClusterPort = p
		}
	}
	remoteParsed, _ := url.Parse(remoteConsoleURL)
	remoteClusterURL := "https://" + remoteParsed.Hostname() + ":" + remoteClusterPort

	localClusterEndpoint, err := s.resolveLocalClusterEndpoint(r, "")
	if err != nil {
		logrus.WithError(err).Error("Failed to resolve local cluster endpoint")
		s.writeError(w, "Failed to determine local cluster address: "+err.Error(), http.StatusInternalServerError)
		return
	}

	nodeCertPEM, nodeKeyPEM, err := cluster.GenerateNodeCert([]byte(caCertPEM), []byte(caKeyPEM), nodeName, remoteClusterURL)
	if err != nil {
		logrus.WithError(err).Error("Failed to generate node certificate for joining node")
		s.writeError(w, "Failed to generate node certificate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var jwtSecret string
	_ = s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, "jwt_secret").Scan(&jwtSecret)

	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list cluster nodes")
		s.writeError(w, "Failed to list cluster nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Node B's S3 API URL: same host as console, port 8080
	remoteAPIURL := "http://" + remoteParsed.Hostname() + ":8080"

	pkg := cluster.ClusterJoinPackage{
		NodeID:       nodeID,
		NodeName:     nodeName,
		ClusterToken: config.ClusterToken,
		Region:       config.Region,
		CACertPEM:    caCertPEM,
		NodeCertPEM:  string(nodeCertPEM),
		NodeKeyPEM:   string(nodeKeyPEM),
		JWTSecret:    jwtSecret,
		SelfEndpoint: remoteClusterURL,
		NodeEndpoint: localClusterEndpoint,
		APIURL:       remoteAPIURL,
		Nodes:        nodes,
	}

	// Step 4: Push the join package to Node B via 8081
	joinPayload, _ := json.Marshal(pkg)
	joinReq, _ := http.NewRequestWithContext(r.Context(), "POST", remoteEndpoint+"/api/v1/cluster/join", bytes.NewReader(joinPayload))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+remoteToken)
	joinResp, err := insecureClient.Do(joinReq)
	if err != nil {
		logrus.WithError(err).Error("Failed to send join package to remote node")
		s.writeError(w, "Failed to send join package to remote node: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(joinResp.Body)
		s.writeError(w, "Remote node failed to join cluster: "+string(body), http.StatusBadGateway)
		return
	}

	// Step 5: Register Node B in Node A's cluster_nodes
	if err := s.clusterManager.AddNode(r.Context(), &cluster.Node{
		ID:        nodeID,
		Name:      nodeName,
		Endpoint:  remoteClusterURL,
		APIURL:    remoteAPIURL,
		NodeToken: config.ClusterToken,
		Region:    config.Region,
		Priority:  5,
	}); err != nil {
		logrus.WithError(err).Warn("Failed to register new node in cluster after successful join")
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
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

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
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

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
		ObjectCount      int64  `json:"object_count"`
		TotalSize        int64  `json:"total_size"`
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
			ObjectCount:      bucket.ObjectCount,
			TotalSize:        bucket.TotalSize,
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
		"bucket": bucketName,
		"rules":  rules,
		"total":  len(rules),
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

	s.writeClusterJSON(w, map[string]interface{}{
		"buckets": bucketsWithLocation,
	})
}

// handleBucketExists checks if a bucket exists on this node (internal cluster API).
// Returns 200 if the bucket exists locally, 404 if not found.
func (s *Server) handleBucketExists(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["name"]

	exists, err := s.bucketManager.BucketExists(r.Context(), "", bucketName)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketName).Error("Failed to check bucket existence")
		s.writeError(w, "Failed to check bucket: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !exists {
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}

	s.writeClusterJSON(w, map[string]interface{}{
		"exists": true,
		"bucket": bucketName,
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

	s.writeClusterJSON(w, storageInfo)
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

	// Validate token matches local cluster token (timing-safe to prevent timing attacks).
	if !hmac.Equal([]byte(config.ClusterToken), []byte(req.ClusterToken)) {
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

	// Return cluster info along with CA cert+key for inter-node TLS setup
	clusterInfo := cluster.ClusterInfo{
		ClusterID: config.NodeID, // Use first node ID as cluster ID
		Region:    config.Region,
		NodeCount: len(nodes),
	}

	resp := map[string]interface{}{
		"valid":        true,
		"cluster_info": clusterInfo,
	}

	// Include only the CA *certificate* (public). The CA private key is never transmitted.
	// Joining nodes obtain their signed certificate via the /sign-csr endpoint instead.
	if caCert := s.clusterManager.GetCACertPEM(); caCert != "" {
		resp["ca_cert"] = caCert
	}

	// Include the JWT secret so joining nodes can adopt it immediately.
	// Protected by the cluster token that was already validated above.
	var jwtSecret string
	if err := s.db.QueryRow(`SELECT value FROM system_settings WHERE key = ?`, "jwt_secret").Scan(&jwtSecret); err == nil && jwtSecret != "" {
		resp["jwt_secret"] = jwtSecret
	}

	s.writeClusterJSON(w, resp)
}

// handleSignCSR signs a Certificate Signing Request (CSR) from a node that is joining the cluster.
// Authentication uses the shared cluster token (pre-HMAC bootstrap flow).
// The CA private key never leaves this handler — only the signed certificate is returned.
func (s *Server) handleSignCSR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string `json:"cluster_token"`
		CSR          string `json:"csr"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ClusterToken == "" || req.CSR == "" {
		s.writeError(w, "cluster_token and csr are required", http.StatusBadRequest)
		return
	}

	// Validate cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		s.writeError(w, "Cluster not initialized", http.StatusInternalServerError)
		return
	}
	if !hmac.Equal([]byte(config.ClusterToken), []byte(req.ClusterToken)) {
		s.writeError(w, "Invalid cluster token", http.StatusUnauthorized)
		return
	}

	// Get CA cert and key (key stays server-side, never transmitted)
	caCertPEM := s.clusterManager.GetCACertPEM()
	caKeyPEM := s.clusterManager.GetCAKeyPEM()
	if caCertPEM == "" || caKeyPEM == "" {
		s.writeError(w, "CA certificates not available on this node", http.StatusInternalServerError)
		return
	}

	// Sign the CSR
	nodeCertPEM, err := cluster.SignCSR([]byte(req.CSR), []byte(caCertPEM), []byte(caKeyPEM))
	if err != nil {
		logrus.WithError(err).Error("Failed to sign node CSR")
		s.writeError(w, "Failed to sign CSR: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeClusterJSON(w, map[string]string{
		"node_cert": string(nodeCertPEM),
	})
}

// handleRegisterNode registers a new node joining the cluster
func (s *Server) handleRegisterNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string        `json:"cluster_token"`
		Node         *cluster.Node `json:"node"`
		NodeToken    string        `json:"node_token"` // sent separately because Node.NodeToken has json:"-"
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

	// Restore node_token from the dedicated field (Node.NodeToken is json:"-")
	if req.NodeToken != "" {
		req.Node.NodeToken = req.NodeToken
	}

	// Validate cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get cluster config")
		s.writeError(w, "Failed to get cluster config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !hmac.Equal([]byte(config.ClusterToken), []byte(req.ClusterToken)) {
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

	// Broadcast the new node to all other existing nodes (best-effort, async).
	// This ensures that when a 3rd node joins via node A, nodes B, C, etc.
	// also learn about the new node immediately instead of never discovering it.
	go s.broadcastNewNode(r.Context(), req.Node, config.ClusterToken)

	resp := map[string]interface{}{
		"node": req.Node,
	}
	if caCert := s.clusterManager.GetCACertPEM(); caCert != "" {
		resp["ca_cert"] = caCert
	}
	s.writeClusterJSON(w, resp)
}

// broadcastNewNode notifies all existing nodes about a newly registered node.
// Runs in a goroutine — best-effort, logs errors but does not block the caller.
func (s *Server) broadcastNewNode(ctx context.Context, newNode *cluster.Node, clusterToken string) {
	// Use a background context so this is not cancelled when the HTTP request ends.
	bgCtx := context.Background()

	nodes, err := s.clusterManager.ListNodes(bgCtx)
	if err != nil {
		logrus.WithError(err).Error("Failed to list nodes for broadcast")
		return
	}

	localNodeID, _ := s.clusterManager.GetLocalNodeID(bgCtx)

	for _, node := range nodes {
		// Skip the new node itself and ourselves
		if node.ID == newNode.ID || node.ID == localNodeID {
			continue
		}

		url := fmt.Sprintf("%s/api/internal/cluster/notify-node", node.Endpoint)
		payload, _ := json.Marshal(map[string]interface{}{
			"cluster_token": clusterToken,
			"node":          newNode,
			"node_token":    newNode.NodeToken,
		})

		req, err := http.NewRequestWithContext(bgCtx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			logrus.WithError(err).WithField("target_node", node.ID).Warn("Failed to create broadcast request")
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logrus.WithError(err).WithField("target_node", node.ID).Warn("Failed to broadcast new node")
			continue
		}
		resp.Body.Close()

		logrus.WithFields(logrus.Fields{
			"new_node":    newNode.ID,
			"target_node": node.ID,
			"status":      resp.StatusCode,
		}).Info("Broadcasted new node to existing cluster member")
	}
}

// handleNotifyNewNode receives a notification about a new node joining the cluster.
// This is called by the node that accepted the join, to notify all other members.
func (s *Server) handleNotifyNewNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClusterToken string        `json:"cluster_token"`
		Node         *cluster.Node `json:"node"`
		NodeToken    string        `json:"node_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ClusterToken == "" || req.Node == nil {
		s.writeError(w, "Cluster token and node are required", http.StatusBadRequest)
		return
	}

	// Restore node_token from the dedicated field (Node.NodeToken is json:"-")
	if req.NodeToken != "" {
		req.Node.NodeToken = req.NodeToken
	}

	// Validate cluster token
	config, err := s.clusterManager.GetConfig(r.Context())
	if err != nil {
		s.writeError(w, "Failed to get cluster config", http.StatusInternalServerError)
		return
	}

	if !hmac.Equal([]byte(config.ClusterToken), []byte(req.ClusterToken)) {
		s.writeError(w, "Invalid cluster token", http.StatusUnauthorized)
		return
	}

	// Add the new node (idempotent — uses INSERT OR REPLACE)
	if err := s.clusterManager.AddNode(r.Context(), req.Node); err != nil {
		logrus.WithError(err).Error("Failed to add broadcasted node")
		s.writeError(w, "Failed to add node", http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"node_id":   req.Node.ID,
		"node_name": req.Node.Name,
	}).Info("Received new node notification from cluster")

	w.WriteHeader(http.StatusOK)
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

	s.writeClusterJSON(w, map[string]string{
		"jwt_secret": jwtSecret,
	})
}

// handleGetClusterNodesInternal returns cluster nodes for internal cluster sync (with token auth).
// The cluster token is expected in the Authorization header as "Bearer <token>" to avoid
// leaking the token into HTTP access logs which record the full URL.
func (s *Server) handleGetClusterNodesInternal(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		s.writeError(w, "Cluster token is required (Authorization: Bearer <token>)", http.StatusBadRequest)
		return
	}
	clusterToken := strings.TrimPrefix(authHeader, bearerPrefix)
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

	if !hmac.Equal([]byte(config.ClusterToken), []byte(clusterToken)) {
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

	// Build response with node_token included.
	// Node.NodeToken has json:"-" so we must expose it explicitly here.
	// This endpoint is already authenticated via cluster token, so sharing
	// node tokens is safe — they are needed for HMAC inter-node auth.
	type nodeWithToken struct {
		*cluster.Node
		NodeToken string `json:"node_token"`
	}
	nodesWithTokens := make([]nodeWithToken, len(nodes))
	for i, n := range nodes {
		nodesWithTokens[i] = nodeWithToken{Node: n, NodeToken: n.NodeToken}
	}

	s.writeClusterJSON(w, map[string]interface{}{
		"nodes": nodesWithTokens,
	})
}

// writeClusterJSON writes a raw JSON response for inter-node cluster API endpoints.
// Unlike writeJSON (console API), it does NOT wrap the response in {"success":true,"data":{...}}.
// The Go cluster manager client code (manager.go) decodes flat structs, so the envelope must be absent.
func (s *Server) writeClusterJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

// resolveLocalClusterEndpoint returns this node's cluster endpoint.
// Priority: 1) overrideAddr (admin picked from UI), 2) cluster_advertise_address
// config (Docker/K8s), 3) ClusterListen host, 4) auto-detection via UDP dial.
//
// The cluster inter-node port always uses HTTPS — it manages its own TLS via
// the internal cluster CA, independent of the console reverse proxy scheme.
func (s *Server) resolveLocalClusterEndpoint(r *http.Request, overrideAddr string) (string, error) {
	scheme := "https"

	clusterPort := "8082"
	clusterHost := strings.TrimSpace(overrideAddr)

	// cluster_advertise_address: explicit IP for Docker/K8s deployments
	if clusterHost == "" && s.config.ClusterAdvertiseAddress != "" {
		clusterHost = strings.TrimSpace(s.config.ClusterAdvertiseAddress)
	}

	if s.config.ClusterListen != "" {
		if h, p, err := net.SplitHostPort(s.config.ClusterListen); err == nil {
			if p != "" {
				clusterPort = p
			}
			if clusterHost == "" && h != "" && h != "0.0.0.0" && h != "::" {
				clusterHost = h
			}
		}
	}

	if clusterHost == "" {
		// Discover the local IP by opening a UDP "connection" (no traffic sent).
		conn, err := net.Dial("udp4", "8.8.8.8:80")
		if err != nil {
			return "", fmt.Errorf("unable to determine local IP: %w", err)
		}
		defer conn.Close()
		clusterHost = conn.LocalAddr().(*net.UDPAddr).IP.String()
	}

	// Validate that the chosen IP is a valid IP address
	if net.ParseIP(clusterHost) == nil {
		return "", fmt.Errorf("invalid IP address: %s", clusterHost)
	}

	return scheme + "://" + clusterHost + ":" + clusterPort, nil
}

// parseNodeAddress parses a user-provided node address into a full URL.
// The input can be:
//   - "192.168.1.10"          → http://192.168.1.10:<defaultPort>
//   - "192.168.1.10:9000"     → http://192.168.1.10:9000
//   - "http://192.168.1.10"   → http://192.168.1.10:<defaultPort>  (scheme kept, port added)
//   - "http://192.168.1.10:9000" → http://192.168.1.10:9000        (used as-is)
//
// defaultPort is "8081" for console communication or "8082" for cluster communication.
func parseNodeAddress(input, defaultPort string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("address is required")
	}

	// If no scheme, prepend http:// so url.Parse works correctly
	raw := input
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid address: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("host is empty")
	}
	if host == "169.254.169.254" {
		return "", fmt.Errorf("address resolves to a cloud metadata address")
	}

	port := u.Port()
	if port == "" {
		port = defaultPort
	}

	return u.Scheme + "://" + host + ":" + port, nil
}
