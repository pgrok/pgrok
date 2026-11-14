// Package certs provides the certificates of the HTTPS server, either from
// files on disk or obtained and renewed through ACME DNS-01 challenges.
package certs

import (
	"context"
	"crypto/tls"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/pkg/errors"
	"unknwon.dev/x/logx"
)

// Config is where the certificates come from, read from the [tls] section of
// the config. Exactly one of CertFiles and ACMEDNSProvider is set when HTTPS is
// enabled.
type Config struct {
	// CertFiles and KeyFiles are the certificate files and their keys, in the
	// same order.
	CertFiles []string `ini:"CERT_FILES"`
	KeyFiles  []string `ini:"KEY_FILES"`

	ACMEEmail       string `ini:"ACME_EMAIL"`
	ACMECA          string `ini:"ACME_CA"`
	ACMEStoragePath string `ini:"ACME_STORAGE_PATH"`
	ACMEDNSProvider string `ini:"ACME_DNS_PROVIDER"`
	// ACMEDNSSettings are the settings of the DNS provider, read from the
	// [tls.acme_dns] section, keyed by the upper snake case of their names.
	ACMEDNSSettings map[string]string `ini:"-"`
}

// Enabled returns whether HTTPS is configured.
func (c *Config) Enabled() bool {
	return len(c.CertFiles) > 0 || c.ACMEDNSProvider != ""
}

// Validate returns an error when the config is invalid. It must only be called
// when the config is enabled.
func (c *Config) Validate() error {
	if len(c.CertFiles) > 0 && c.ACMEDNSProvider != "" {
		return errors.New("only one of CERT_FILES and ACME_DNS_PROVIDER can be set")
	}
	if len(c.CertFiles) != len(c.KeyFiles) {
		return errors.New("CERT_FILES and KEY_FILES must have the same number of files")
	}
	if c.ACMEDNSProvider != "" && c.ACMEStoragePath == "" {
		return errors.New("ACME_STORAGE_PATH is required")
	}
	return nil
}

// Start returns the TLS config serving certificates for the given host names,
// and keeps the certificates up to date until ctx is canceled.
func Start(ctx context.Context, logger *logx.Logger, config *Config, hosts []string) (*tls.Config, error) {
	if config.ACMEDNSProvider != "" {
		return startACME(ctx, logger, config, hosts)
	}

	files := &fileCertificates{certFiles: config.CertFiles, keyFiles: config.KeyFiles}
	if _, err := files.reload(); err != nil {
		return nil, err
	}
	for _, host := range hosts {
		if _, ok := files.match(host); !ok {
			logger.Warn("No certificate covers the host", "host", host)
		}
	}
	go files.watch(ctx, logger)
	return &tls.Config{
		GetCertificate: files.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
	}, nil
}

// fileCertificates serves certificates loaded from files, and reloads them
// when the files change, e.g. after certbot renewed them.
type fileCertificates struct {
	certFiles []string
	keyFiles  []string

	mu       sync.RWMutex
	certs    []*tls.Certificate
	modTimes []time.Time
}

// reload loads the certificates when any of the files changed since the last
// load. It returns whether the certificates were loaded.
func (f *fileCertificates) reload() (bool, error) {
	var modTimes []time.Time
	for _, name := range slices.Concat(f.certFiles, f.keyFiles) {
		fi, err := os.Stat(name)
		if err != nil {
			return false, errors.Wrap(err, "stat")
		}
		modTimes = append(modTimes, fi.ModTime())
	}

	f.mu.RLock()
	unchanged := len(f.modTimes) == len(modTimes)
	for i := 0; unchanged && i < len(modTimes); i++ {
		unchanged = f.modTimes[i].Equal(modTimes[i])
	}
	f.mu.RUnlock()
	if unchanged {
		return false, nil
	}

	certs := make([]*tls.Certificate, 0, len(f.certFiles))
	for i := range f.certFiles {
		cert, err := tls.LoadX509KeyPair(f.certFiles[i], f.keyFiles[i])
		if err != nil {
			return false, errors.Wrapf(err, "load certificate %q", f.certFiles[i])
		}
		certs = append(certs, &cert)
	}

	f.mu.Lock()
	f.certs = certs
	f.modTimes = modTimes
	f.mu.Unlock()
	return true, nil
}

func (f *fileCertificates) watch(ctx context.Context, logger *logx.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		reloaded, err := f.reload()
		if err != nil {
			logger.Error("Failed to reload certificates, keep serving the previous ones", "error", err)
		} else if reloaded {
			logger.Info("Reloaded certificates")
		}
	}
}

// match returns the first certificate that is valid for the host.
func (f *fileCertificates) match(host string) (*tls.Certificate, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	for _, cert := range f.certs {
		if cert.Leaf.VerifyHostname(host) == nil {
			return cert, true
		}
	}
	return f.certs[0], false
}

// GetCertificate returns the first certificate that is valid for the server
// name, or the first certificate when none is.
func (f *fileCertificates) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert, _ := f.match(hello.ServerName)
	return cert, nil
}
