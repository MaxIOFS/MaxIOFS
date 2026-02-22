package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"sync/atomic"
	"time"
)

// GenerateCA creates a self-signed CA certificate and private key (ECDSA P-256, 10-year validity).
func GenerateCA() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "MaxIOFS Internal CA",
			Organization: []string{"MaxIOFS"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal CA key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// GenerateNodeCert creates a node certificate signed by the given CA (1-year validity).
// The certificate includes the commonName as a DNS SAN and also covers localhost and 127.0.0.1.
func GenerateNodeCert(caCertPEM, caKeyPEM []byte, commonName string) (certPEM, keyPEM []byte, err error) {
	// Parse CA cert
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA key
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return nil, nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	// Generate node key
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate node key: %w", err)
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"MaxIOFS"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames:    []string{commonName, "localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &nodeKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create node certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(nodeKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal node key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// BuildClusterTLSConfig creates a *tls.Config for inter-node communication.
// It trusts only the internal CA and uses the provided node certificate.
// The currentCert atomic pointer enables hot-swapping certs without restart.
func BuildClusterTLSConfig(caCertPEM, nodeCertPEM, nodeKeyPEM []byte, currentCert *atomic.Pointer[tls.Certificate]) (*tls.Config, error) {
	// Build CA pool
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to add CA certificate to pool")
	}

	// Parse initial node cert
	cert, err := tls.X509KeyPair(nodeCertPEM, nodeKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse node certificate: %w", err)
	}

	// Store initial cert in atomic pointer
	currentCert.Store(&cert)

	tlsConfig := &tls.Config{
		RootCAs: caPool,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			return currentCert.Load(), nil
		},
		GetClientCertificate: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return currentCert.Load(), nil
		},
		MinVersion: tls.VersionTLS12,
	}

	return tlsConfig, nil
}

// ParseCertKeyPEM parses PEM-encoded certificate and key into a tls.Certificate.
func ParseCertKeyPEM(certPEM, keyPEM []byte) (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate/key pair: %w", err)
	}
	return &cert, nil
}

// IsCertExpiringSoon checks if a PEM-encoded certificate expires within the given number of days.
func IsCertExpiringSoon(certPEM []byte, days int) (bool, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("failed to parse certificate: %w", err)
	}

	threshold := time.Now().Add(time.Duration(days) * 24 * time.Hour)
	return cert.NotAfter.Before(threshold), nil
}

// InsecureClusterTLSConfig returns a TLS config that skips server verification.
// Used only during the initial join handshake before the CA cert is available.
func InsecureClusterTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
}

func randomSerialNumber() (*big.Int, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}
	return serialNumber, nil
}
