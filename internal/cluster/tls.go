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
	"net/url"
	"sync/atomic"
	"time"
)

// endpointSANs extracts the DNS name or IP address from a node endpoint URL so it can be
// included in certificate Subject Alternative Names. This is required for TLS verification
// to succeed when nodes communicate via real IPs or hostnames (not just localhost).
func endpointSANs(endpointURL string) (dnsNames []string, ipAddresses []net.IP) {
	if endpointURL == "" {
		return
	}
	parsed, err := url.Parse(endpointURL)
	if err != nil || parsed.Hostname() == "" {
		return
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		ipAddresses = append(ipAddresses, ip)
	} else {
		dnsNames = append(dnsNames, host)
	}
	return
}

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
// The certificate covers the commonName, localhost, 127.0.0.1, and the hostname or IP
// extracted from endpointURL so that TLS verification succeeds for real cluster addresses.
func GenerateNodeCert(caCertPEM, caKeyPEM []byte, commonName, endpointURL string) (certPEM, keyPEM []byte, err error) {
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

	extraDNS, extraIPs := endpointSANs(endpointURL)
	dnsNames := append([]string{commonName, "localhost"}, extraDNS...)
	ipAddresses := append([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, extraIPs...)

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
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
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

// GenerateKeyAndCSR generates a new ECDSA P-256 private key and a PEM-encoded
// Certificate Signing Request (CSR) for the given commonName. The endpointURL is used
// to include the node's real hostname or IP in the CSR SANs so that TLS verification
// succeeds for inter-node communication on real cluster addresses.
// The private key never needs to leave the requesting node.
func GenerateKeyAndCSR(commonName, endpointURL string) (keyPEM, csrPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	extraDNS, extraIPs := endpointSANs(endpointURL)
	template := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"MaxIOFS"},
		},
		DNSNames:    append([]string{commonName, "localhost"}, extraDNS...),
		IPAddresses: append([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, extraIPs...),
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	csrPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return keyPEM, csrPEM, nil
}

// SignCSR signs a PEM-encoded CSR using the given CA certificate and key, returning
// a PEM-encoded signed certificate (1-year validity).
// This is called by the cluster leader when a new node requests to join.
func SignCSR(csrPEM, caCertPEM, caKeyPEM []byte) (certPEM []byte, err error) {
	// Parse CSR
	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil {
		return nil, fmt.Errorf("failed to decode CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature invalid: %w", err)
	}

	// Parse CA cert
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		return nil, fmt.Errorf("failed to decode CA certificate PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	// Parse CA key
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames:    csr.DNSNames,
		IPAddresses: csr.IPAddresses,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign CSR: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), nil
}

// InsecureClusterTLSConfig returns a TLS config that skips server verification.
// Used only during the initial join handshake before the CA cert is available.
func InsecureClusterTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
}

// BuildServerTLSConfig returns a *tls.Config for the cluster server listener using
// the node certificate already stored in the atomic pointer (populated by
// BuildClusterTLSConfig after cluster initialization or join).
// Returns an error if no certificate is available yet.
func BuildServerTLSConfig(currentCert *atomic.Pointer[tls.Certificate]) (*tls.Config, error) {
	if currentCert.Load() == nil {
		return nil, fmt.Errorf("cluster certificates not initialized yet")
	}
	return &tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert := currentCert.Load()
			if cert == nil {
				return nil, fmt.Errorf("cluster certificate not available")
			}
			return cert, nil
		},
		MinVersion: tls.VersionTLS12,
	}, nil
}

func randomSerialNumber() (*big.Int, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}
	return serialNumber, nil
}
