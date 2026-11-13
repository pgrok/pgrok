package main

import (
	"fmt"
	"net/http"

	"github.com/flamego/flamego"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/reverseproxy"
)

// newProxyHandler returns the handler that forwards requests to the reverse
// proxy of their hosts.
func newProxyHandler(proxies *reverseproxy.Cluster) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy, ok := proxies.Get(r.Host)
		if !ok {
			proxies.ServeNoTunnel(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func newProxyServer(logger *logx.Logger, port int, handler http.Handler) *http.Server {
	f := flamego.New()
	f.Use(flamego.Recovery())
	f.Any("/{**}", handler.ServeHTTP)

	address := fmt.Sprintf("0.0.0.0:%d", port)
	logger.Info("Server listening on", "address", address)
	return &http.Server{
		Addr:    address,
		Handler: f,
	}
}
