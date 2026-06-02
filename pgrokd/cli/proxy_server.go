package main

import (
	"fmt"
	"net/http"

	"github.com/flamego/flamego"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/reverseproxy"
)

func newProxyServer(logger *logx.Logger, port int, proxies *reverseproxy.Cluster) *http.Server {
	f := flamego.New()
	f.Use(flamego.Recovery())
	f.Any("/{**}", func(w http.ResponseWriter, r *http.Request) {
		proxy, ok := proxies.Get(r.Host)
		if !ok {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("No reverse proxy is available for the host: " + r.Host))
			return
		}
		proxy.ServeHTTP(w, r)
	})

	address := fmt.Sprintf("0.0.0.0:%d", port)
	logger.Info("Server listening on", "address", address)
	return &http.Server{
		Addr:    address,
		Handler: f,
	}
}
