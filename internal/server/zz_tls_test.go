package server

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
)

// TestSelfSignedReused checks the self-signed certificate is saved once and reused, so browser exceptions stick.
func TestSelfSignedReused(t *testing.T) {
	logger.Pl.Console = io.Discard
	path := filepath.Join(t.TempDir(), selfSignedFile)
	first, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("self-signed file mode = %v, want 0600", info.Mode().Perm())
	}
	second, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Certificate[0], second.Certificate[0]) {
		t.Error("self-signed certificate was regenerated instead of reused")
	}
	if err := first.Leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("self-signed certificate not valid for localhost: %v", err)
	}
}

// TestSelfSignedKeptAfterHostnameChange checks a certificate made under another hostname (e.g. a recreated container) is kept.
func TestSelfSignedKeptAfterHostnameChange(t *testing.T) {
	logger.Pl.Console = io.Discard
	path := filepath.Join(t.TempDir(), selfSignedFile)
	old, err := generateSelfSigned("old-container-hostname.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}

	cert, err := loadOrCreateSelfSigned(path)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, old) {
		t.Error("self-signed certificate was regenerated after a hostname change")
	}
	if cert.Leaf.VerifyHostname("old-container-hostname.invalid") != nil {
		t.Error("loaded certificate isn't the saved one")
	}
}

// TestCustomCertificateFallbackAndReload checks a combined PEM is served, missing files fall back, and changes reload.
func TestCustomCertificateFallbackAndReload(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	paths.HomeTubarrDir = dir
	certFile := filepath.Join(dir, "custom.pem")

	writeCert := func(host string, mod time.Time) []byte {
		t.Helper()
		data, err := generateSelfSigned(host)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certFile, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(certFile, mod, mod); err != nil {
			t.Fatal(err)
		}
		return data
	}
	served := func(c *tlsCerts) []byte {
		t.Helper()
		cert, err := c.getCertificate(nil)
		if err != nil {
			t.Fatal(err)
		}
		return cert.Certificate[0]
	}

	writeCert("first.test", time.Now().Add(-time.Hour))
	if err := checkKeyPair(certFile, ""); err != nil {
		t.Fatalf("combined PEM rejected: %v", err)
	}
	c, err := newTLSCerts(certFile, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.active(); got != "custom" {
		t.Fatalf("active = %q, want custom", got)
	}
	first := served(c)

	// Changed on disk: reloaded without a restart.
	writeCert("second.test", time.Now())
	if bytes.Equal(served(c), first) {
		t.Error("changed certificate file was not reloaded")
	}

	// Missing file: falls back to self-signed.
	if err := os.Remove(certFile); err != nil {
		t.Fatal(err)
	}
	if got := c.active(); got != "self-signed" {
		t.Errorf("active after removal = %q, want self-signed", got)
	}

	// Cleared path: self-signed.
	c.setPaths("", "")
	if !bytes.Equal(served(c), c.selfSigned.Certificate[0]) {
		t.Error("cleared path did not serve the self-signed certificate")
	}
}
