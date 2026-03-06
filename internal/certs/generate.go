package certs

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const ServerName = "forja-builder"

type Bundle struct {
	CACertPEM     []byte
	ServerCertPEM []byte
	ServerKeyPEM  []byte
	ClientCertPEM []byte
	ClientKeyPEM  []byte
}

type Materialized struct {
	Bundle
	Dir            string
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
}

func Generate() (*Bundle, error) {
	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}
	serverPub, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate server key: %w", err)
	}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate client key: %w", err)
	}

	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "forja-build-ca"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(2 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPub, caPriv)
	if err != nil {
		return nil, fmt.Errorf("create ca cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parse ca cert: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + 1),
		Subject:      pkix.Name{CommonName: ServerName},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(2 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     []string{ServerName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, serverPub, caPriv)
	if err != nil {
		return nil, fmt.Errorf("create server cert: %w", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano() + 2),
		Subject:      pkix.Name{CommonName: "forja-client"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(2 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, clientPub, caPriv)
	if err != nil {
		return nil, fmt.Errorf("create client cert: %w", err)
	}
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverPriv)
	if err != nil {
		return nil, fmt.Errorf("marshal server key: %w", err)
	}
	clientKeyDER, err := x509.MarshalPKCS8PrivateKey(clientPriv)
	if err != nil {
		return nil, fmt.Errorf("marshal client key: %w", err)
	}

	return &Bundle{
		CACertPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		ServerCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		ServerKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}),
		ClientCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}),
		ClientKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: clientKeyDER}),
	}, nil
}

func Materialize(bundle *Bundle) (*Materialized, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle is nil")
	}
	dir, err := os.MkdirTemp("", "forja-certs-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	write := func(name string, data []byte) (string, error) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return "", err
		}
		return path, nil
	}
	caPath, err := write("ca-cert.pem", bundle.CACertPEM)
	if err != nil {
		return nil, err
	}
	clientCertPath, err := write("client-cert.pem", bundle.ClientCertPEM)
	if err != nil {
		return nil, err
	}
	clientKeyPath, err := write("client-key.pem", bundle.ClientKeyPEM)
	if err != nil {
		return nil, err
	}
	return &Materialized{
		Bundle:         *bundle,
		Dir:            dir,
		CACertPath:     caPath,
		ClientCertPath: clientCertPath,
		ClientKeyPath:  clientKeyPath,
	}, nil
}

func (m *Materialized) Cleanup() error {
	if m == nil || m.Dir == "" {
		return nil
	}
	return os.RemoveAll(m.Dir)
}
