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
	"net/url"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// downloadTokenParam is the query parameter the browser navigates with.
const downloadTokenParam = "downloadToken"

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
	return strings.Join([]string{tenantID, bucketName, objectKey, versionID}, "\x00")
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

	// mux populates "object" for every /objects/{object:.*}/... route, so the
	// suffix is what distinguishes the download from its siblings.
	trimmed := strings.TrimSuffix(r.URL.Path, "/")
	if !strings.HasSuffix(trimmed, "/"+escapedPathSuffix(objectKey)) &&
		!strings.HasSuffix(trimmed, "/"+objectKey) {
		return "", "", "", "", false
	}

	query := r.URL.Query()
	return query.Get("tenantId"), bucketName, objectKey, query.Get("versionId"), true
}

// escapedPathSuffix renders a key the way it appears in a URL, so a key with
// spaces or accents still matches the path it arrived on.
func escapedPathSuffix(objectKey string) string {
	return (&url.URL{Path: objectKey}).EscapedPath()
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

	tenantID, bucketName, objectKey, versionID, ok := s.downloadRequestTarget(r)
	if !ok {
		s.writeError(w, "This link is only valid for downloading an object", http.StatusForbidden)
		return nil, true
	}

	user, err := s.authManager.ValidateDownloadToken(r.Context(),
		token, downloadResource(tenantID, bucketName, objectKey, versionID))
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucketName,
			"object": objectKey,
		}).Warn("Download token rejected")
		s.writeError(w, "This download link is invalid or has expired", http.StatusUnauthorized)
		return nil, true
	}

	return user, false
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
