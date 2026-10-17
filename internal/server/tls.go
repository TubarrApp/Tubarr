package server

import (
	"cmp"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/spf13/viper"
)

const (
	selfSignedFile    = "tls-self-signed.pem"
	selfSignedLife    = 2 * 365 * 24 * time.Hour
	selfSignedRenewAt = 30 * 24 * time.Hour // Regenerate when this close to expiry.
)

// httpsSetting reports whether HTTPS is on, and whether --web-https or TUBARR_WEB_HTTPS decides it rather than the saved setting.
func httpsSetting(tss contracts.TubarrSettingsStore) (on, locked bool) {
	if viper.IsSet(keys.WebHTTPS) {
		return viper.GetBool(keys.WebHTTPS), true
	}
	if env := strings.TrimSpace(os.Getenv(consts.EnvWebHTTPS)); env != "" {
		on, _ = strconv.ParseBool(env)
		return on, true
	}
	if tss == nil {
		return false, false
	}
	if val, found, err := tss.GetSetting(keys.WebHTTPS); err != nil {
		logger.Pl.W("Failed to load setting %q from DB: %v", keys.WebHTTPS, err)
	} else if found {
		on, _ = strconv.ParseBool(val)
	}
	return on, false
}

// tlsPaths returns the custom certificate and key paths, from --web-tls-cert/--web-tls-key, TUBARR_WEB_TLS_CERT/TUBARR_WEB_TLS_KEY,
// or else the settings table. locked is true when a flag or env var sets them.
func tlsPaths(tss contracts.TubarrSettingsStore) (certPath, keyPath string, locked bool) {
	if certPath = strings.TrimSpace(viper.GetString(keys.WebTLSCert)); certPath != "" {
		return certPath, strings.TrimSpace(viper.GetString(keys.WebTLSKey)), true
	}
	if certPath = strings.TrimSpace(os.Getenv(consts.EnvWebTLSCert)); certPath != "" {
		return certPath, strings.TrimSpace(os.Getenv(consts.EnvWebTLSKey)), true
	}
	if tss == nil {
		return "", "", false
	}
	for k, dst := range map[string]*string{keys.WebTLSCert: &certPath, keys.WebTLSKey: &keyPath} {
		if val, found, err := tss.GetSetting(k); err != nil {
			logger.Pl.W("Failed to load setting %q from DB: %v", k, err)
		} else if found {
			*dst = val
		}
	}
	return certPath, keyPath, false
}

// loadKeyPair loads a certificate and key, or a combined PEM holding both when keyPath is empty.
func loadKeyPair(certPath, keyPath string) (tls.Certificate, error) {
	if keyPath != "" {
		return tls.LoadX509KeyPair(certPath, keyPath)
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(data, data)
}

// checkKeyPair loads a certificate and key and rejects expired certificates.
func checkKeyPair(certPath, keyPath string) error {
	cert, err := loadKeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	if cert.Leaf != nil && time.Now().After(cert.Leaf.NotAfter) {
		return fmt.Errorf("certificate expired on %s", cert.Leaf.NotAfter.Format(time.DateOnly))
	}
	return nil
}

// tlsCerts serves the user's certificate when set and loadable, else a saved self-signed one.
type tlsCerts struct {
	mu         sync.Mutex
	certPath   string
	keyPath    string
	custom     *tls.Certificate
	loadedMod  time.Time // Newest modification time of the custom files when last loaded (or failed).
	selfSigned *tls.Certificate
}

// newTLSCerts loads or creates the self-signed certificate and sets the custom certificate paths.
func newTLSCerts(certPath, keyPath string) (*tlsCerts, error) {
	selfSigned, err := loadOrCreateSelfSigned(filepath.Join(paths.HomeTubarrDir, selfSignedFile))
	if err != nil {
		return nil, err
	}
	c := &tlsCerts{selfSigned: selfSigned}
	c.setPaths(certPath, keyPath)
	return c, nil
}

// setPaths swaps the custom certificate paths, taking effect on the next connection.
func (c *tlsCerts) setPaths(certPath, keyPath string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.certPath, c.keyPath = certPath, keyPath
	c.custom, c.loadedMod = nil, time.Time{}
	if certPath == "" {
		logger.Pl.I("HTTPS using self-signed certificate %s (browsers warn until it is trusted)", filepath.Join(paths.HomeTubarrDir, selfSignedFile))
		return
	}
	c.current() // Load now so problems are logged straight away.
}

// active reports which certificate is being served: "custom" or "self-signed".
func (c *tlsCerts) active() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current() == c.selfSigned {
		return "self-signed"
	}
	return "custom"
}

// getCertificate is the tls.Config hook, called for every new connection.
func (c *tlsCerts) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current(), nil
}

// current returns the certificate to serve, reloading the custom files when they change on disk (caller holds mu).
func (c *tlsCerts) current() *tls.Certificate {
	if c.certPath == "" {
		return c.selfSigned
	}
	mod, err := newestModTime(c.certPath, c.keyPath)
	if err != nil {
		if c.custom != nil || c.loadedMod.IsZero() {
			logger.Pl.W("Failed to read TLS certificate %q, serving self-signed certificate: %v", c.certPath, err)
		}
		c.custom, c.loadedMod = nil, time.Unix(0, 1)
		return c.selfSigned
	}
	if mod.Equal(c.loadedMod) {
		return cmp.Or(c.custom, c.selfSigned)
	}

	// New or changed files, (re)load them once.
	c.loadedMod = mod
	cert, err := loadKeyPair(c.certPath, c.keyPath)
	if err != nil {
		logger.Pl.W("Failed to load TLS certificate %q, serving self-signed certificate: %v", c.certPath, err)
		c.custom = nil
		return c.selfSigned
	}
	if cert.Leaf != nil && time.Now().After(cert.Leaf.NotAfter) {
		logger.Pl.W("TLS certificate %q expired on %s", c.certPath, cert.Leaf.NotAfter.Format(time.DateOnly))
	}
	logger.Pl.I("HTTPS using certificate %q", c.certPath)
	c.custom = &cert
	return c.custom
}

// newestModTime returns the latest modification time of the given files, skipping empty paths.
func newestModTime(files ...string) (time.Time, error) {
	var newest time.Time
	for _, f := range files {
		if f == "" {
			continue
		}
		info, err := os.Stat(f)
		if err != nil {
			return time.Time{}, err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest, nil
}

// loadOrCreateSelfSigned reuses the saved self-signed certificate, regenerating it near expiry or when the hostname changes.
func loadOrCreateSelfSigned(path string) (*tls.Certificate, error) {
	host, _ := os.Hostname()
	if data, err := os.ReadFile(path); err == nil {
		cert, err := tls.X509KeyPair(data, data)
		if err == nil && cert.Leaf != nil &&
			time.Until(cert.Leaf.NotAfter) > selfSignedRenewAt &&
			(host == "" || cert.Leaf.VerifyHostname(host) == nil) {
			return &cert, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read self-signed certificate: %w", err)
	}

	logger.Pl.I("Generating self-signed TLS certificate at %s", path)
	data, err := generateSelfSigned(host)
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}
	if err := os.WriteFile(path, data, sharedconsts.PermsPrivateFile); err != nil {
		return nil, fmt.Errorf("failed to save self-signed certificate: %w", err)
	}
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// generateSelfSigned returns a combined PEM certificate and key for localhost, host, and this machine's addresses.
func generateSelfSigned(host string) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	// Names the certificate is valid for.
	dnsNames := []string{"localhost"}
	if host != "" && host != "localhost" {
		dnsNames = append(dnsNames, host)
	}
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && !ipNet.IP.IsLinkLocalUnicast() {
				ips = append(ips, ipNet.IP)
			}
		}
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Tubarr", Organization: []string{"Tubarr self-signed"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(selfSignedLife),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}

	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...), nil
}
