package server

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

const (
	zipDownloadMaxObjects = 10_000
	zipDownloadMaxBytes   = int64(10) * 1024 * 1024 * 1024 // 10 GB
)

type zipEntry struct {
	key      string
	size     int64
	modified time.Time
}

// zipEntryName is the name in a ZIP of an object named name inside its folder:
// a relative path of "/"-separated segments, as the ZIP format names a file.
// Empty and "." segments are dropped; "..", and a drive letter first, are
// written percent-encoded, so no entry is extracted out of its folder.
func zipEntryName(name string) string {
	var segments []string
	for _, segment := range strings.Split(strings.ReplaceAll(name, `\`, "/"), "/") {
		switch {
		case segment == "" || segment == ".":
			continue
		case segment == "..":
			segment = "%2E%2E"
		case len(segments) == 0 && hasDriveLetter(segment):
			segment = segment[:1] + "%3A" + segment[2:]
		}
		segments = append(segments, segment)
	}
	return strings.Join(segments, "/")
}

// hasDriveLetter reports whether a path segment starts as "C:" does.
func hasDriveLetter(segment string) bool {
	if len(segment) < 2 || segment[1] != ':' {
		return false
	}
	c := segment[0]
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// handleDownloadZip streams all objects under a given prefix as a ZIP archive.
func (s *Server) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucket"]
	prefix := r.URL.Query().Get("prefix")
	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket,
		"You do not have permission to list this bucket") {
		return
	}
	tenantID := consoleBucketTenant(r, user)
	bucketPath := buildBucketPath(tenantID, bucketName)

	// The whole folder is checked before any header is sent, so a refusal
	// can still be answered.
	folder, err := s.zipFolderOf(r, user, bucketName, bucketPath, prefix, zipLimits{zipDownloadMaxObjects, zipDownloadMaxBytes})
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	zipName := path.Base(zipEntryName(prefix))
	if zipName == "." {
		zipName = bucketName
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", sanitizeFilename(zipName+".zip")))
	w.WriteHeader(http.StatusOK)
	written, complete := s.writeZip(r.Context(), w, bucketPath, prefix, folder.entries)
	if !complete {
		return
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeObjectDownloaded,
		ResourceType: audit.ResourceTypeObject,
		ResourceID:   prefix,
		ResourceName: zipName + ".zip",
		Action:       audit.ActionDownload,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.proxyTrust()),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"bucket":     bucketName,
			"prefix":     prefix,
			"file_count": written,
			"total_size": folder.size,
			"zip_name":   zipName + ".zip",
		},
	})
}

// zipFolder is what a folder downloaded as a ZIP holds.
type zipFolder struct {
	entries []zipEntry
	size    int64
}

// zipLimits is how many objects, and how many bytes, a ZIP may hold.
type zipLimits struct {
	objects int
	bytes   int64
}

// zipFolderOf lists the objects under a prefix, without folder markers: all
// of them readable by the user, within the limits, and at least one.
func (s *Server) zipFolderOf(r *http.Request, user *auth.User, bucketName, bucketPath, prefix string, limits zipLimits) (zipFolder, error) {
	bucketTenant := s.resolveConsoleBucketTenantID(r, bucketName, user)
	var folder zipFolder
	marker := ""
	for {
		result, err := s.objectManager.ListObjects(r.Context(), bucketPath, prefix, "", marker, 1000)
		if err != nil {
			return zipFolder{}, fmt.Errorf("Failed to list objects: %w", err)
		}
		if err := s.addToZipFolder(r, user, bucketTenant, bucketName, &folder, result.Objects, limits); err != nil {
			return zipFolder{}, err
		}
		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}
	if len(folder.entries) == 0 {
		return zipFolder{}, refused(http.StatusNotFound, "No objects found under this prefix")
	}
	return folder, nil
}

// addToZipFolder adds a page of objects to a folder, refused when the user may
// not read one or the folder passes its limits.
func (s *Server) addToZipFolder(r *http.Request, user *auth.User, bucketTenant, bucketName string, folder *zipFolder, objects []object.Object, limits zipLimits) error {
	for _, obj := range objects {
		if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
			continue
		}
		if !s.userCanPerformConsoleS3Action(r, user, bucketTenant, auth.ActionGetObject, consoleObjectARN(bucketName, obj.Key)) {
			return refused(http.StatusForbidden, "You do not have permission to download every object in this folder")
		}
		folder.entries = append(folder.entries, zipEntry{key: obj.Key, size: obj.Size, modified: obj.LastModified})
		folder.size += obj.Size
	}
	switch {
	case len(folder.entries) > limits.objects:
		return refused(http.StatusBadRequest, fmt.Sprintf("Folder contains more than %d objects — download limit exceeded", limits.objects))
	case folder.size > limits.bytes:
		return refused(http.StatusBadRequest, "Folder total size exceeds the 10 GB download limit")
	}
	return nil
}

// writeZip streams the entries as a ZIP, each named by its path under the
// prefix. It reports how many it wrote, and false when it stopped short.
func (s *Server) writeZip(ctx context.Context, w http.ResponseWriter, bucketPath, prefix string, entries []zipEntry) (int, bool) {
	flusher, canFlush := w.(http.Flusher)
	zw := zip.NewWriter(w)
	defer zw.Close()
	written := 0
	for _, entry := range entries {
		if ctx.Err() != nil {
			return written, false
		}
		name := zipEntryName(strings.TrimPrefix(entry.key, prefix))
		if name == "" {
			continue
		}
		added, err := s.zipObject(ctx, zw, bucketPath, name, entry)
		if err != nil {
			return written, false
		}
		if !added {
			continue
		}
		written++
		if canFlush {
			flusher.Flush()
		}
	}
	return written, true
}

// zipObject writes an object into the archive. It is opened before its entry
// is made, so an unreadable one leaves no empty entry; one only its client's
// key reads is left out.
func (s *Server) zipObject(ctx context.Context, zw *zip.Writer, bucketPath, name string, entry zipEntry) (bool, error) {
	_, reader, err := s.objectManager.GetObject(ctx, bucketPath, entry.key)
	if errors.Is(err, object.ErrCustomerKeyRequired) {
		logrus.WithField("key", entry.key).Info("zip: an object encrypted with a customer key is left out")
		return false, nil
	}
	if err != nil {
		logrus.WithError(err).WithField("key", entry.key).Error("zip: failed to open object")
		return false, err
	}
	defer reader.Close()
	fw, err := zw.CreateHeader(&zip.FileHeader{
		Name:               name,
		Method:             zip.Store,
		Modified:           entry.modified,
		UncompressedSize64: uint64(entry.size),
		CompressedSize64:   uint64(entry.size),
	})
	if err != nil {
		logrus.WithError(err).WithField("key", entry.key).Error("zip: failed to create entry header")
		return false, err
	}
	if _, err := io.Copy(fw, reader); err != nil {
		logrus.WithError(err).WithField("key", entry.key).Error("zip: failed to write object data")
		return false, err
	}
	return true, nil
}
