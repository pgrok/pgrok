package main

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"

	"github.com/pgrok/pgrok/internal/reverseproxy"
)

func startProxyServer(logger *log.Logger, port int, proxies *reverseproxy.Cluster) {
	logger = logger.WithPrefix("proxy")

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
	err := http.ListenAndServe(address, f)
	if err != nil {
		logger.Fatal("Failed to start server", "error", err)
	}
}
