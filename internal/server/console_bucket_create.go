package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
)

// createBucketRequest is what the console creates a bucket with.
type createBucketRequest struct {
	Name              string                    `json:"name"`
	Region            string                    `json:"region,omitempty"`
	OwnerID           string                    `json:"ownerId,omitempty"`
	OwnerType         string                    `json:"ownerType,omitempty"` // "user" or "tenant"
	Versioning        *bucket.VersioningConfig  `json:"versioning,omitempty"`
	ObjectLock        *createBucketObjectLock   `json:"objectLock,omitempty"`
	Encryption        *bucket.EncryptionConfig  `json:"encryption,omitempty"`
	PublicAccessBlock *bucket.PublicAccessBlock `json:"publicAccessBlock,omitempty"`
	Lifecycle         *bucket.LifecycleConfig   `json:"lifecycle,omitempty"`
	Tags              map[string]string         `json:"tags,omitempty"`
	ObjectOwnership   string                    `json:"objectOwnership,omitempty"`
}

// createBucketObjectLock is Object Lock as the console asks it: on, with a
// default retention when a mode or a period is given.
type createBucketObjectLock struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"` // GOVERNANCE, COMPLIANCE
	Days    int    `json:"days"`
	Years   int    `json:"years"`
}

func (l *createBucketObjectLock) on() bool { return l != nil && l.Enabled }

// consoleRefusal is a console request refused, and the status it is answered.
type consoleRefusal struct {
	status  int
	message string
}

func (r *consoleRefusal) Error() string { return r.message }

func refused(status int, message string) error { return &consoleRefusal{status, message} }

// writeRefusal answers err: a refusal with its status, anything else 500.
func (s *Server) writeRefusal(w http.ResponseWriter, err error) {
	var refusal *consoleRefusal
	if errors.As(err, &refusal) {
		s.writeError(w, refusal.message, refusal.status)
		return
	}
	s.writeError(w, err.Error(), http.StatusInternalServerError)
}

func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	var req createBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		s.writeError(w, "Bucket name is required", http.StatusBadRequest)
		return
	}
	user, userExists := auth.GetUserFromContext(r.Context())
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	ownership, ok := s.mayCreateBucketWith(w, r, &req)
	if !ok {
		return
	}
	globalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	tenantID, creator, err := s.newBucketOwner(r.Context(), user, globalAdmin, &req)
	if err == nil {
		err = s.tenantHasRoomForABucket(r.Context(), tenantID)
	}
	if err == nil {
		s.withDefaultVersioning(&req)
		err = validateNewBucketObjectLock(&req)
	}
	if err == nil {
		err = s.createConfiguredBucket(r.Context(), tenantID, req.Name, creator, ownership, func(info *bucket.Bucket) {
			ownNewBucket(info, user, user.TenantID != "" && s.isAdmin(user), globalAdmin, &req)
			configureNewBucket(info, &req)
		})
	}
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	s.writeJSON(w, map[string]string{"name": req.Name})
}

// mayCreateBucketWith checks the caller may create the bucket with what the
// request sets: each setting asks the permission its own S3 call asks, as on
// the S3 API. It gives the bucket's Object Ownership: ACLs off unless asked
// otherwise, as AWS creates a bucket.
func (s *Server) mayCreateBucketWith(w http.ResponseWriter, r *http.Request, req *createBucketRequest) (string, bool) {
	if !s.requireConsoleBucketS3Action(w, r, req.Name, auth.ActionCreateBucket, "You do not have permission to create buckets") {
		return "", false
	}
	for _, need := range creationSettingActions(req.Versioning, req.ObjectLock.on(),
		req.Encryption, req.PublicAccessBlock, req.Lifecycle, req.Tags) {
		if !s.requireConsoleBucketS3Action(w, r, req.Name, need.action, "You do not have permission to "+need.what) {
			return "", false
		}
	}
	ownership := cmp.Or(req.ObjectOwnership, bucket.ObjectOwnershipBucketOwnerEnforced)
	if !bucket.ValidObjectOwnership(ownership) {
		s.writeError(w, "objectOwnership must be BucketOwnerEnforced, BucketOwnerPreferred or ObjectWriter", http.StatusBadRequest)
		return "", false
	}
	if ownership != bucket.ObjectOwnershipBucketOwnerEnforced &&
		!s.requireConsoleBucketS3Action(w, r, req.Name, auth.ActionPutBucketOwnership, "You do not have permission to enable ACLs") {
		return "", false
	}
	return ownership, true
}

// newBucketOwner is the tenant a bucket is created in and the user it is
// created for: a tenant a global administrator names, or the tenant of the
// user it names; otherwise the caller's own.
func (s *Server) newBucketOwner(ctx context.Context, user *auth.User, globalAdmin bool, req *createBucketRequest) (string, string, error) {
	if !globalAdmin {
		return user.TenantID, user.ID, nil
	}
	switch req.OwnerType {
	case "":
		return user.TenantID, user.ID, nil
	case "tenant":
		if req.OwnerID == "" {
			return "", "", refused(http.StatusBadRequest, "An owner tenant is required")
		}
		if _, err := s.authManager.GetTenant(ctx, req.OwnerID); err != nil {
			return "", "", refused(http.StatusBadRequest, "The owner tenant does not exist")
		}
		return req.OwnerID, user.ID, nil
	case "user":
		if req.OwnerID == "" {
			return user.TenantID, user.ID, nil
		}
		owner, err := s.authManager.GetUser(ctx, req.OwnerID)
		if err != nil {
			return "", "", refused(http.StatusBadRequest, "The owner user does not exist")
		}
		// A user named as the owner gets the bucket as if it had created it:
		// the owner policy is written for that user.
		return owner.TenantID, owner.ID, nil
	default:
		return "", "", refused(http.StatusBadRequest, `ownerType must be "user" or "tenant"`)
	}
}

// tenantHasRoomForABucket refuses a bucket a tenant at its bucket quota.
func (s *Server) tenantHasRoomForABucket(ctx context.Context, tenantID string) error {
	if tenantID == "" {
		return nil
	}
	tenant, err := s.authManager.GetTenant(ctx, tenantID)
	if err != nil {
		return errors.New("Failed to retrieve tenant information")
	}
	if tenant.MaxBuckets <= 0 {
		return nil
	}
	count, err := s.tenantBucketCount(ctx, tenantID)
	if err != nil {
		return errors.New("Failed to count the tenant's buckets")
	}
	if count >= int64(tenant.MaxBuckets) {
		return refused(http.StatusForbidden,
			fmt.Sprintf("Tenant bucket quota exceeded (%d/%d). Cannot create more buckets.", count, tenant.MaxBuckets))
	}
	return nil
}

// withDefaultVersioning gives a request that says nothing of versioning the
// installation's default.
func (s *Server) withDefaultVersioning(req *createBucketRequest) {
	if req.Versioning != nil {
		return
	}
	if enabled, err := s.settingsManager.GetBool("storage.default_bucket_versioning"); err == nil && enabled {
		req.Versioning = &bucket.VersioningConfig{Status: "Enabled"}
	}
}

// validateNewBucketObjectLock refuses Object Lock without versioning, and a
// default retention AWS would refuse.
func validateNewBucketObjectLock(req *createBucketRequest) error {
	lock := req.ObjectLock
	if !lock.on() {
		return nil
	}
	if req.Versioning == nil || req.Versioning.Status != "Enabled" {
		return refused(http.StatusBadRequest, "Object Lock requires versioning to be enabled")
	}
	if lock.Mode == "" && lock.Days == 0 && lock.Years == 0 {
		return nil
	}
	if err := bucket.ValidateDefaultRetention(lock.Mode, lock.Days, lock.Years); err != nil {
		return refused(http.StatusBadRequest, "Object Lock default retention: "+err.Error())
	}
	return nil
}

// createConfiguredBucket creates a bucket and stores it as configure leaves
// it. A bucket whose settings are not stored is deleted again: none is left
// without the Object Lock or the encryption it was created with.
func (s *Server) createConfiguredBucket(ctx context.Context, tenantID, name, creator, ownership string, configure func(*bucket.Bucket)) error {
	if err := s.bucketManager.CreateBucket(ctx, tenantID, name, creator); err != nil {
		if errors.Is(err, bucket.ErrBucketAlreadyExists) {
			return refused(http.StatusConflict, "Bucket already exists")
		}
		return refused(http.StatusBadRequest, err.Error())
	}
	undo := func(message string, err error) error {
		_ = s.bucketManager.DeleteBucket(ctx, tenantID, name)
		return errors.New(message + err.Error())
	}
	if err := s.bucketManager.SetOwnershipControls(ctx, tenantID, name,
		&bucket.OwnershipControlsConfig{ObjectOwnership: ownership}); err != nil {
		return undo("Failed to set the bucket's Object Ownership: ", err)
	}
	info, err := s.bucketManager.GetBucketInfo(ctx, tenantID, name)
	if err != nil {
		return undo("Bucket could not be read once created: ", err)
	}
	configure(info)
	if err := s.bucketManager.UpdateBucket(ctx, tenantID, name, info); err != nil {
		return undo("Bucket could not be created with its configuration: ", err)
	}
	return nil
}

// ownNewBucket sets who owns a bucket created: the tenant of the
// administrator of a tenant, the user of a tenant who creates it, the owner a
// global administrator names, or the user of no tenant who creates it.
func ownNewBucket(info *bucket.Bucket, user *auth.User, tenantAdmin, globalAdmin bool, req *createBucketRequest) {
	switch {
	case tenantAdmin:
		info.OwnerID, info.OwnerType = user.TenantID, "tenant"
	case globalAdmin:
		info.OwnerID = cmp.Or(req.OwnerID, info.OwnerID)
		info.OwnerType = cmp.Or(req.OwnerType, info.OwnerType)
	default:
		info.OwnerID, info.OwnerType = user.ID, "user"
	}
}

// configureNewBucket gives a bucket created the settings asked; its
// encryption is AES256 unless another is asked.
func configureNewBucket(info *bucket.Bucket, req *createBucketRequest) {
	if req.Versioning != nil {
		info.Versioning = req.Versioning
	}
	if req.ObjectLock.on() {
		info.ObjectLock = objectLockOf(req.ObjectLock)
	}
	info.Encryption = cmp.Or(req.Encryption, &bucket.EncryptionConfig{Type: "AES256"})
	if req.PublicAccessBlock != nil {
		info.PublicAccessBlock = req.PublicAccessBlock
	}
	if req.Lifecycle != nil {
		info.Lifecycle = req.Lifecycle
	}
	if req.Tags != nil {
		info.Tags = req.Tags
	}
	info.Region = cmp.Or(req.Region, info.Region)
}

// objectLockOf is Object Lock on, with its default retention when a mode is
// given.
func objectLockOf(lock *createBucketObjectLock) *bucket.ObjectLockConfig {
	config := &bucket.ObjectLockConfig{ObjectLockEnabled: true}
	if lock.Mode == "" {
		return config
	}
	retention := &bucket.DefaultRetention{Mode: lock.Mode}
	if lock.Days > 0 {
		retention.Days = &lock.Days
	} else {
		retention.Years = &lock.Years
	}
	config.Rule = &bucket.ObjectLockRule{DefaultRetention: retention}
	return config
}

// settingAction is the permission one setting of a new bucket asks, and what
// a refusal says the caller may not do.
type settingAction struct{ action, what string }

// creationSettingActions are the permissions the settings a bucket is created
// with ask: those of the S3 calls that set them. What a new bucket has anyway
// asks nothing: versioning off, the encryption it always has, and public
// access blocked in full.
func creationSettingActions(versioning *bucket.VersioningConfig, objectLock bool, encryption *bucket.EncryptionConfig,
	publicAccess *bucket.PublicAccessBlock, lifecycle *bucket.LifecycleConfig, tags map[string]string) []settingAction {
	var needs []settingAction
	if objectLock || (versioning != nil && versioning.Status == "Enabled") {
		needs = append(needs, settingAction{auth.ActionPutBucketVersioning, "enable versioning"})
	}
	if objectLock {
		needs = append(needs, settingAction{auth.ActionPutBucketObjectLockConfiguration, "enable Object Lock"})
	}
	if encryption != nil && encryption.Type != "" && encryption.Type != "AES256" {
		needs = append(needs, settingAction{auth.ActionPutBucketEncryption, "set the bucket's encryption"})
	}
	if publicAccess != nil && !(publicAccess.BlockPublicAcls && publicAccess.IgnorePublicAcls &&
		publicAccess.BlockPublicPolicy && publicAccess.RestrictPublicBuckets) {
		needs = append(needs, settingAction{auth.ActionPutBucketPublicAccess, "allow public access"})
	}
	if lifecycle != nil {
		needs = append(needs, settingAction{auth.ActionPutBucketLifecycle, "set a lifecycle"})
	}
	if len(tags) > 0 {
		needs = append(needs, settingAction{auth.ActionPutBucketTagging, "tag buckets"})
	}
	return needs
}
