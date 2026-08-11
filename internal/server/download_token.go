package server

// Downloading a large object from the console.
//
// A browser only streams a response to disk when it NAVIGATES to it. Anything
// fetched from JavaScript is assembled in memory first, which for a multi-
// gigabyte object means the tab holds the whole file before the user gets a
// single byte of it — and no request timeout, however generous, fixes that.
//
// Navigation cannot carry an Authorization header, so the credential has to be
// in the URL. That is the whole reason this exists, and the reason it is scoped
// as tightly as it is: a token names one object, lasts two minutes, and is
// refused by ValidateJWT everywhere else.

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
// by identity rather than by shape.
//
// The previous test asked whether the path ENDED WITH the object key, which a
// sibling route satisfies whenever the key is exactly that segment: a token for
// an object literally named "acl" also redeemed on .../objects/acl/acl, the
// ACL route. Bounded — same bucket, same key, GET only, and those handlers run
// their own permission checks — but wider than it was written to be, and the
// kind of thing that stops being bounded when a route is added.
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
// the key and the version. It is what the mint request signs and what the
// redeeming request is checked against, so a token for one object opens no
// other.
//
// The tenant is part of it because a bucket name alone does not identify a
// bucket — two tenants may each hold one of the same name. The separator is a
// NUL so no combination of values can be made to spell another: without it
// bucket "a/b" with key "c" and bucket "a" with key "b/c" are the same string.
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
// request is the object-download route at all.
//
// Only GET on that exact route may be authorised by a token. Matching on the
// route rather than on a path prefix is deliberate: "/objects/{key}" also
// prefixes "/objects/{key}/acl", and a token good for reading a file must not
// become one for writing its permissions.
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
// token instead of a session.
//
// Returns (nil, false) when there is no token, which is every ordinary request:
// the caller then authenticates normally. Returns (nil, true) when a token was
// offered and refused — the response is already written, and no fallback to the
// Authorization header is allowed, so a bad token cannot be probed against a
// session that happens to be present.
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
// a download token may authorise it at all.
//
// Two routes qualify: the object download, and the folder archive. Both are
// GET, both stream, and both are things a browser must navigate to rather than
// fetch. Everything else is refused.
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
//
// It is an ordinary authenticated request, so the caller's session and
// permissions are checked here in the normal way. What it hands back is worth
// strictly less than the session that asked for it.
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

	// The object has to exist and be readable by this caller BEFORE a token is
	// issued for it. Minting first and discovering later would turn the token
	// into a way of asking which keys exist.
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

	// The token carries the tenant exactly as the download URL will send it,
	// which is the query parameter, not the resolved one — otherwise a global
	// admin's token would never match the request it was minted for.
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
