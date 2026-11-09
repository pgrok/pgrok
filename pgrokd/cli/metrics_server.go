package main

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"unknwon.dev/x/logx"
)

// newMetricsServer returns a server that exposes Prometheus metrics on
// "/metrics". It is served on its own port because metric labels contain
// principal subdomains, which should not be exposed via the public web or proxy
// servers.
func newMetricsServer(logger *logx.Logger, port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	address := fmt.Sprintf("0.0.0.0:%d", port)
	logger.Info("Server listening on", "address", address)
	return &http.Server{
		Addr:    address,
		Handler: mux,
	}
}
