package server

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// downloadTokenParam is the query parameter the browser navigates with.
const downloadTokenParam = "downloadToken"

// The two routes a download token may authorise, named so they can be matched
const (
	routeObjectDownload = "console.object.download"
	routeFolderDownload = "console.folder.download"
)

// matchedRouteName reports which route gorilla/mux selected for this request.
func matchedRouteName(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		return route.GetName()
	}
	return ""
}

// downloadResource is the identity a token is bound to: the tenant, the bucket,
func downloadResource(tenantID, bucketName, objectKey, versionID string) string {
	return strings.Join([]string{"object", tenantID, bucketName, objectKey, versionID}, "\x00")
}

// downloadZipResource names a folder download. The leading kind keeps the two
// apart: a token minted to read one object must not redeem for an archive of
// everything under a prefix that happens to spell the same thing.
func downloadZipResource(tenantID, bucketName, prefix string) string {
	return strings.Join([]string{"zip", tenantID, bucketName, prefix}, "\x00")
}

// downloadRequestTarget reports the object a request is for, and whether the
func (s *Server) downloadRequestTarget(r *http.Request) (tenantID, bucketName, objectKey, versionID string, ok bool) {
	if r.Method != http.MethodGet {
		return "", "", "", "", false
	}

	vars := mux.Vars(r)
	bucketName = vars["bucket"]
	objectKey = vars["object"]
	if bucketName == "" || objectKey == "" {
		return "", "", "", "", false
	}

	// Which route matched, not what the path looks like. mux populates "object"
	// for every /objects/{object:.*}/... route, so shape cannot tell them apart.
	if matchedRouteName(r) != routeObjectDownload {
		return "", "", "", "", false
	}

	query := r.URL.Query()
	return query.Get("tenantId"), bucketName, objectKey, query.Get("versionId"), true
}

// userFromDownloadToken resolves the caller when the request carries a download
func (s *Server) userFromDownloadToken(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	token := r.URL.Query().Get(downloadTokenParam)
	if token == "" {
		return nil, false
	}

	resource, ok := s.downloadTokenResource(r)
	if !ok {
		s.writeError(w, "This link is only valid for downloading", http.StatusForbidden)
		return nil, true
	}

	user, err := s.authManager.ValidateDownloadToken(r.Context(), token, resource)
	if err != nil {
		logrus.WithError(err).WithField("path", r.URL.Path).
			Warn("Download token rejected")
		s.writeError(w, "This download link is invalid or has expired", http.StatusUnauthorized)
		return nil, true
	}

	return user, false
}

// downloadTokenResource names what a request is asking for, and reports whether
func (s *Server) downloadTokenResource(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		return "", false
	}

	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	if bucketName == "" {
		return "", false
	}
	query := r.URL.Query()

	switch matchedRouteName(r) {
	case routeFolderDownload:
		return downloadZipResource(query.Get("tenantId"), bucketName, query.Get("prefix")), true
	case routeObjectDownload:
	default:
		return "", false
	}

	tenantID, bucket, objectKey, versionID, ok := s.downloadRequestTarget(r)
	if !ok {
		return "", false
	}
	return downloadResource(tenantID, bucket, objectKey, versionID), true
}

// handleCreateDownloadZipToken mints the token for one folder archive. The
// permission is checked here, as an ordinary authenticated request; the archive
// itself re-checks every object as it enumerates.
func (s *Server) handleCreateDownloadZipToken(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket,
		"You do not have permission to list this bucket") {
		return
	}

	query := r.URL.Query()
	token, err := s.authManager.GenerateDownloadToken(r.Context(), user,
		downloadZipResource(query.Get("tenantId"), bucketName, query.Get("prefix")))
	if err != nil {
		logrus.WithError(err).Error("Failed to mint folder download token")
		s.writeError(w, "Could not prepare the download", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"token":     token,
		"expiresIn": auth.DownloadTokenTTL,
	})
}

// handleCreateDownloadToken mints the token for one object.
func (s *Server) handleCreateDownloadToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := vars["object"]

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// A token names the version it was minted for, so one issued for an
	// earlier version cannot be redeemed for the current object or the reverse.
	versionID := r.URL.Query().Get("versionId")
	readAction := auth.ActionGetObject
	if versionID != "" {
		readAction = auth.ActionGetObjectVersion
	}
	if !s.requireConsoleObjectS3Action(w, r, bucketName, objectKey, readAction, "You do not have permission to download objects") {
		return
	}

	// Same resolution the download itself performs, so the token is minted for
	// the object that will actually be served.
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := user.TenantID
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	bucketPath := tenantID + "/" + bucketName
	if tenantID == "" {
		bucketPath = bucketName
	}

	if versionID != "" {
		_, reader, err := s.objectManager.GetObject(r.Context(), bucketPath, objectKey, versionID)
		if reader != nil {
			reader.Close()
		}
		if err != nil {
			s.writeError(w, "Object not found", http.StatusNotFound)
			return
		}
	} else if _, err := s.objectManager.GetObjectMetadata(r.Context(), bucketPath, objectKey); err != nil {
		s.writeError(w, "Object not found", http.StatusNotFound)
		return
	}

	token, err := s.authManager.GenerateDownloadToken(r.Context(), user,
		downloadResource(queryTenantID, bucketName, objectKey, versionID))
	if err != nil {
		logrus.WithError(err).Error("Failed to mint download token")
		s.writeError(w, "Could not prepare the download", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"token":     token,
		"expiresIn": auth.DownloadTokenTTL,
	})
}
