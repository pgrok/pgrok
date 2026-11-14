package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/certs"
)

// hostname returns the host without the port.
func hostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// newTLSServers returns the HTTPS server that serves the web handler for the
// host of the external URL and the proxy handler for all other hosts, and the
// server that redirects HTTP to HTTPS, which is nil when not configured.
func newTLSServers(
	ctx context.Context,
	logger *logx.Logger,
	config *Config,
	web, proxy http.Handler,
) (_ *http.Server, redirect *http.Server, _ error) {
	externalURL, err := url.Parse(config.Server.ExternalURL)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parse external URL")
	}
	webHost := externalURL.Hostname()

	// All tunnels are exactly one label under the proxy domain, so a single
	// wildcard certificate covers them, which also covers the web host if it is
	// one of them.
	proxyDomain := hostname(config.Proxy.Domain)
	hosts := []string{"*." + proxyDomain}
	if _, parent, _ := strings.Cut(webHost, "."); parent != proxyDomain {
		hosts = append(hosts, webHost)
	}

	tlsConfig, err := certs.Start(ctx, logger.Scoped("certs"), &config.TLS.Config, hosts)
	if err != nil {
		return nil, nil, errors.Wrap(err, "start certificates")
	}

	address := fmt.Sprintf("0.0.0.0:%d", config.TLS.Port)
	logger.Info("Server listening on", "address", address, "hosts", hosts)
	server := &http.Server{
		Addr:      address,
		TLSConfig: tlsConfig,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.EqualFold(hostname(r.Host), webHost) {
				web.ServeHTTP(w, r)
				return
			}
			proxy.ServeHTTP(w, r)
		}),
	}

	if config.TLS.HTTPRedirectPort > 0 {
		port := ""
		if config.TLS.Port != 443 {
			port = ":" + strconv.Itoa(config.TLS.Port)
		}
		address := fmt.Sprintf("0.0.0.0:%d", config.TLS.HTTPRedirectPort)
		logger.Info("Redirect server listening on", "address", address)
		redirect = &http.Server{
			Addr: address,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "https://"+hostname(r.Host)+port+r.URL.RequestURI(), http.StatusPermanentRedirect)
			}),
		}
	}
	return server, redirect, nil
}
