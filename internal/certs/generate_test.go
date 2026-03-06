package certs

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

func TestGenerateProducesVerifiableCertificateChain(t *testing.T) {
	t.Parallel()

	bundle, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	caCert := parseCertificate(t, bundle.CACertPEM)
	serverCert := parseCertificate(t, bundle.ServerCertPEM)
	clientCert := parseCertificate(t, bundle.ClientCertPEM)

	if !caCert.IsCA {
		t.Fatal("expected CA cert to be a CA")
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	if _, err := serverCert.Verify(x509.VerifyOptions{
		Roots:         roots,
		DNSName:       ServerName,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Intermediates: x509.NewCertPool(),
	}); err != nil {
		t.Fatalf("server cert did not verify: %v", err)
	}

	if _, err := clientCert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("client cert did not verify: %v", err)
	}
}

func TestMaterializeWritesFilesAndCleanupRemovesThem(t *testing.T) {
	t.Parallel()

	bundle, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	materialized, err := Materialize(bundle)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	for _, path := range []string{materialized.CACertPath, materialized.ClientCertPath, materialized.ClientKeyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	if err := materialized.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Stat(materialized.Dir); !os.IsNotExist(err) {
		t.Fatalf("expected temp dir to be removed, got err=%v", err)
	}
}

func parseCertificate(t *testing.T, pemBytes []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("failed to decode PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	return cert
}
