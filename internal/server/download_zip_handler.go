package server

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
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

// handleDownloadZip streams all objects under a given prefix as a ZIP archive.
// GET /buckets/{bucket}/download-zip?prefix=folder/[&tenantId=...]
//
// Limits (enforced before streaming begins):
//   - Maximum 10 000 objects
//   - Maximum 10 GB total size
//
// Objects are stored without compression (zip.Store) to minimise CPU usage.
func (s *Server) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	prefix := r.URL.Query().Get("prefix")

	user, exists := auth.GetUserFromContext(r.Context())
	if !exists {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// This handler authorized nothing, so it served a ZIP of any bucket to any
	// session — the bulk-read path standing beside every per-action guard the
	// console gained. It enumerates and then reads, so it needs both
	// permissions; the read is checked per object below, during enumeration,
	// because a caller granted only part of a prefix must not receive the rest.
	if !s.requireConsoleBucketS3Action(w, r, bucketName, auth.ActionListBucket,
		"You do not have permission to list this bucket") {
		return
	}

	tenantID := user.TenantID
	if q := r.URL.Query().Get("tenantId"); q != "" && auth.IsAdminUser(r.Context()) && user.TenantID == "" {
		tenantID = q
	}

	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	// ── Phase 1: enumerate objects and enforce limits ─────────────────────────
	// This must complete before sending any response headers so that we can
	// still return a JSON error if the limits are exceeded.
	var entries []zipEntry
	var totalSize int64
	marker := ""

	for {
		result, err := s.objectManager.ListObjects(r.Context(), bucketPath, prefix, "", marker, 1000)
		if err != nil {
			s.writeError(w, fmt.Sprintf("Failed to list objects: %v", err), http.StatusInternalServerError)
			return
		}

		for _, obj := range result.Objects {
			// Skip folder markers (virtual directories)
			if strings.HasSuffix(obj.Key, "/") && obj.Size == 0 {
				continue
			}
			// Refused rather than skipped: a ZIP quietly missing the files the
			// caller may not read looks like a complete archive of a smaller
			// folder, and a backup taken from it would be short without saying so.
			if !s.userCanPerformConsoleS3Action(r, user,
				s.resolveConsoleBucketTenantID(r, bucketName, user),
				auth.ActionGetObject, consoleObjectARN(bucketName, obj.Key)) {
				s.writeError(w, "You do not have permission to download every object in this folder",
					http.StatusForbidden)
				return
			}
			entries = append(entries, zipEntry{key: obj.Key, size: obj.Size, modified: obj.LastModified})
			totalSize += obj.Size
		}

		if len(entries) > zipDownloadMaxObjects {
			s.writeError(w,
				fmt.Sprintf("Folder contains more than %d objects — download limit exceeded", zipDownloadMaxObjects),
				http.StatusBadRequest)
			return
		}
		if totalSize > zipDownloadMaxBytes {
			s.writeError(w, "Folder total size exceeds the 10 GB download limit", http.StatusBadRequest)
			return
		}

		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	if len(entries) == 0 {
		s.writeError(w, "No objects found under this prefix", http.StatusNotFound)
		return
	}

	// ── Derive a friendly filename from the last path segment ─────────────────
	zipName := bucketName
	if prefix != "" {
		trimmed := strings.TrimSuffix(prefix, "/")
		parts := strings.Split(trimmed, "/")
		zipName = parts[len(parts)-1]
	}

	// ── Phase 2: stream the ZIP ───────────────────────────────────────────────
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, zipName))
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	zw := zip.NewWriter(w)

	written := 0
	for _, entry := range entries {
		select {
		case <-r.Context().Done():
			zw.Close()
			return
		default:
		}

		// Path inside the ZIP is relative to the requested prefix
		entryName := entry.key
		if prefix != "" {
			entryName = strings.TrimPrefix(entry.key, prefix)
		}
		entryName = strings.TrimPrefix(entryName, "/")
		if entryName == "" {
			continue
		}

		// Open the object BEFORE creating the ZIP entry so that a missing/unreadable
		// object does not leave an empty entry in the archive.
		_, objReader, err := s.objectManager.GetObject(r.Context(), bucketPath, entry.key)
		if err != nil {
			logrus.WithError(err).WithField("key", entry.key).Error("zip: failed to open object")
			zw.Close()
			return
		}

		// Setting UncompressedSize64 / CompressedSize64 causes Go's zip writer to
		// write the sizes in the local file header instead of a trailing data
		// descriptor.  Data descriptors are part of the ZIP spec but some tools
		// (including Windows Explorer with Store-method entries) do not handle them
		// correctly and show the archive as empty or corrupted.
		fh := &zip.FileHeader{
			Name:               entryName,
			Method:             zip.Store,
			Modified:           entry.modified,
			UncompressedSize64: uint64(entry.size),
			CompressedSize64:   uint64(entry.size),
		}
		fw, err := zw.CreateHeader(fh)
		if err != nil {
			objReader.Close()
			logrus.WithError(err).WithField("key", entry.key).Error("zip: failed to create entry header")
			zw.Close()
			return
		}

		_, copyErr := io.Copy(fw, objReader)
		objReader.Close()
		if copyErr != nil {
			logrus.WithError(copyErr).WithField("key", entry.key).Error("zip: failed to write object data")
			zw.Close()
			return
		}

		written++
		if canFlush {
			flusher.Flush()
		}
	}

	zw.Close()

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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"bucket":     bucketName,
			"prefix":     prefix,
			"file_count": written,
			"total_size": totalSize,
			"zip_name":   zipName + ".zip",
		},
	})
}
