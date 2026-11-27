package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/sirupsen/logrus"
)

// s3Endpoint is the base of the S3 API's links: its public URL, or else the
// request's host on the S3 port.
func (s *Server) s3Endpoint(r *http.Request) string {
	if s.config.PublicAPIURL != "" {
		return s.config.PublicAPIURL
	}
	scheme := "http"
	if r.TLS != nil || bucket.RequestFactsFrom(r.Context()).SecureTransport {
		scheme = "https"
	}
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	if _, port, err := net.SplitHostPort(s.config.Listen); err == nil {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	return scheme + "://" + host
}

// handleShareObject shares an object: anyone with the link reads it while its
// maker may. An object is shared once; sharing it again gives that share.
func (s *Server) handleShareObject(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName, objectKey := vars["bucket"], vars["object"]
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, auth.ActionGetObject,
		"You do not have permission to share this object") {
		return
	}
	tenantID, err := s.shareBucketTenant(r, user, bucketName)
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	// Bucket names are unique: the link is /bucket/key, with no tenant.
	link := s.s3Endpoint(r) + "/" + auth.URIEncode(bucketName) + "/" + auth.URIEncode(objectKey)

	existing, err := s.shareManager.GetShareByObject(r.Context(), bucketName, objectKey, tenantID)
	if err == nil {
		logrus.WithFields(logrus.Fields{"tenantID": tenantID, "url": link}).Info("Generated share URL for existing share")
		s.writeJSON(w, shareAnswer(existing, link, true))
		return
	}
	logrus.WithFields(logrus.Fields{"bucket": bucketName, "object": objectKey, "error": err.Error()}).
		Debug("No existing share found or error occurred")

	created, err := s.newShare(r, user, bucketName, objectKey, tenantID)
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectShared,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   objectKey,
		ResourceName: objectKey,
		Action:       audit.ActionShare,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.proxyTrust()),
		UserAgent:    r.Header.Get("User-Agent"),
		Details:      map[string]interface{}{"bucket": bucketName, "share_id": created.ID},
	})
	logrus.WithFields(logrus.Fields{"tenantID": tenantID, "url": link, "bucket": bucketName, "object": objectKey}).
		Info("Generated share URL for new share")
	s.writeJSON(w, shareAnswer(created, link, false))
}

// shareBucketTenant is the tenant of the bucket an object is shared from.
func (s *Server) shareBucketTenant(r *http.Request, user *auth.User, bucketName string) (string, error) {
	info, err := s.bucketManager.GetBucketInfo(r.Context(), consoleBucketTenant(r, user), bucketName)
	if err != nil && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		info, err = s.bucketManager.GetBucketInfo(r.Context(), "", bucketName)
	}
	if err != nil {
		return "", refused(http.StatusNotFound, "Bucket not found")
	}
	return info.TenantID, nil
}

// newShare makes a share of an object with its maker's access key. It lasts
// the seconds the request gives, null for ever, an hour without a body.
func (s *Server) newShare(r *http.Request, user *auth.User, bucketName, objectKey, tenantID string) (*share.Share, error) {
	var req struct {
		ExpiresIn *int64 `json:"expiresIn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		hour := int64(3600)
		req.ExpiresIn = &hour
	}
	keys, err := s.authManager.ListAccessKeys(r.Context(), user.ID)
	if err != nil || len(keys) == 0 {
		return nil, refused(http.StatusBadRequest, "No access keys found for user. Create an access key first.")
	}
	created, err := s.shareManager.CreateShare(r.Context(), bucketName, objectKey, tenantID,
		keys[0].AccessKeyID, keys[0].SecretAccessKey, user.ID, req.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("Failed to create share: %w", err)
	}
	return created, nil
}

// shareAnswer is what the console is told of a share.
func shareAnswer(sh *share.Share, link string, existing bool) map[string]interface{} {
	return map[string]interface{}{
		"id":        sh.ID,
		"url":       link,
		"expiresAt": sh.ExpiresAt,
		"createdAt": sh.CreatedAt.Format(time.RFC3339),
		"isExpired": false,
		"existing":  existing,
	}
}
