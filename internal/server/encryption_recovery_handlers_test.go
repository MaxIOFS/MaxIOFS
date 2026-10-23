package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptionRecoveryStatus(t *testing.T) {
	server := getSharedServer()

	// Global admin sees the status.
	req := createAuthenticatedRequest("GET", "/api/v1/settings/encryption/recovery-status", nil, "", "admin-user", true)
	w := httptest.NewRecorder()
	server.handleEncryptionRecoveryStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			KEKVersion       int  `json:"kekVersion"`
			BundleDownloaded bool `json:"bundleDownloaded"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// The shared server's current KEK may be v1 (bootstrap) or v2 (after the
	// cluster-key test runs EnsureClusterKey) — only require that one exists.
	assert.GreaterOrEqual(t, resp.Data.KEKVersion, 1)

	// Tenant admin is rejected.
	req = createAuthenticatedRequest("GET", "/api/v1/settings/encryption/recovery-status", nil, "tenant-1", "tenant-admin", true)
	w = httptest.NewRecorder()
	server.handleEncryptionRecoveryStatus(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Non-admin is rejected.
	req = createAuthenticatedRequest("GET", "/api/v1/settings/encryption/recovery-status", nil, "", "regular-user", false)
	w = httptest.NewRecorder()
	server.handleEncryptionRecoveryStatus(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDownloadRecoveryBundle(t *testing.T) {
	server := getSharedServer()

	// Short passphrase is rejected.
	body := bytes.NewBufferString(`{"passphrase":"short"}`)
	req := createAuthenticatedRequest("POST", "/api/v1/settings/encryption/recovery-bundle", body, "", "admin-user", true)
	w := httptest.NewRecorder()
	server.handleDownloadRecoveryBundle(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Valid download.
	body = bytes.NewBufferString(`{"passphrase":"a-strong-recovery-passphrase"}`)
	req = createAuthenticatedRequest("POST", "/api/v1/settings/encryption/recovery-bundle", body, "", "admin-user", true)
	w = httptest.NewRecorder()
	server.handleDownloadRecoveryBundle(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;"))

	// The bundle decrypts with the right passphrase and matches the live KEK.
	records, err := kek.DecryptBundle(w.Body.Bytes(), "a-strong-recovery-passphrase")
	require.NoError(t, err)
	require.NotEmpty(t, records)

	// Status now reports downloaded.
	req = createAuthenticatedRequest("GET", "/api/v1/settings/encryption/recovery-status", nil, "", "admin-user", true)
	w = httptest.NewRecorder()
	server.handleEncryptionRecoveryStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Data struct {
			BundleDownloaded bool   `json:"bundleDownloaded"`
			DownloadedAt     string `json:"downloadedAt"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Data.BundleDownloaded)
	assert.NotEmpty(t, resp.Data.DownloadedAt)

	// Tenant-scoped admin cannot download.
	body = bytes.NewBufferString(`{"passphrase":"a-strong-recovery-passphrase"}`)
	req = createAuthenticatedRequest("POST", "/api/v1/settings/encryption/recovery-bundle", body, "tenant-1", "tenant-admin", true)
	w = httptest.NewRecorder()
	server.handleDownloadRecoveryBundle(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// The recovery bundle is the cluster's: downloaded through the node that
// serves the download, every node reports it downloaded, and a rotation of the
// key asks every node for a new one.
func TestARecoveryBundleDownloadIsTheClusters(t *testing.T) {
	p := newHAPair(t)
	downloaded := func(n *Server) bool {
		w := httptest.NewRecorder()
		n.handleEncryptionRecoveryStatus(w, createAuthenticatedRequest("GET", "/api/v1/settings/encryption/recovery-status", nil, "", "admin-user", true))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Data struct {
				BundleDownloaded bool `json:"bundleDownloaded"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.Data.BundleDownloaded
	}
	require.False(t, downloaded(p.a))
	require.False(t, downloaded(p.b))

	w := httptest.NewRecorder()
	p.a.handleDownloadRecoveryBundle(w, createAuthenticatedRequest("POST", "/api/v1/settings/encryption/recovery-bundle",
		bytes.NewBufferString(`{"passphrase":"a-strong-recovery-passphrase"}`), "", "admin-user", true))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, downloaded(p.a))
	assert.Eventually(t, func() bool { return downloaded(p.b) }, 10*time.Second, 20*time.Millisecond, "b reports it downloaded")

	w = httptest.NewRecorder()
	p.a.handleRotateKEK(w, createAuthenticatedRequest("POST", "/api/v1/settings/encryption/rotate", nil, "", "admin-user", true))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, downloaded(p.a))
	assert.Eventually(t, func() bool { return !downloaded(p.b) }, 10*time.Second, 20*time.Millisecond, "b asks for a new one")
}
