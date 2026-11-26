package cluster

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nodeCertExpiringIn is a certificate of the CA's for a node, that expires
// after valid.
func nodeCertExpiringIn(t *testing.T, caCertPEM, caKeyPEM []byte, valid time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()
	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	require.NoError(t, err)
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "node-a"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(valid),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// renewalNode is a node of a cluster whose certificate expires after valid.
func renewalNode(t *testing.T, valid time.Duration) (*Manager, string) {
	t.Helper()
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	caCert, caKey, err := GenerateCA()
	require.NoError(t, err)
	nodeCert, nodeKey := nodeCertExpiringIn(t, caCert, caKey, valid)
	_, err = db.Exec(`INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled, ca_cert, ca_key, node_cert, node_key)
		VALUES ('node-a', 'node-a', 'token', 1, ?, ?, ?, ?)`, string(caCert), string(caKey), string(nodeCert), string(nodeKey))
	require.NoError(t, err)
	m := newTestManager(t, db)
	m.publicAPIURL = "https://127.0.0.1:8080"
	require.NoError(t, m.loadTLSConfig())
	return m, string(nodeCert)
}

func storedNodeCert(t *testing.T, m *Manager) string {
	t.Helper()
	var cert string
	require.NoError(t, m.db.QueryRow(`SELECT node_cert FROM cluster_config`).Scan(&cert))
	return cert
}

// A node renews its certificate within 30 days of its end as soon as it
// starts, not after a month without a restart, and serves the new one.
func TestANodeCertificateIsRenewedWhenTheNodeStarts(t *testing.T) {
	m, expiring := renewalNode(t, 10*24*time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.StartCertRenewal(ctx)
	t.Cleanup(m.Stop)

	require.Eventually(t, func() bool { return storedNodeCert(t, m) != expiring }, 5*time.Second, 20*time.Millisecond)
	soon, err := IsCertExpiringSoon([]byte(storedNodeCert(t, m)), 300)
	require.NoError(t, err)
	assert.False(t, soon, "renewed for a year")
	served := m.currentCert.Load()
	require.NotNil(t, served)
	leaf, err := x509.ParseCertificate(served.Certificate[0])
	require.NoError(t, err)
	assert.True(t, leaf.NotAfter.After(time.Now().Add(300*24*time.Hour)), "the renewed certificate is served")
}

// A certificate far from its end is kept.
func TestANodeCertificateFarFromItsEndIsKept(t *testing.T) {
	m, current := renewalNode(t, 200*24*time.Hour)
	m.checkAndRenewCert()
	assert.Equal(t, current, storedNodeCert(t, m))
}
