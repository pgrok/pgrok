//go:build !prod

package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
)

// setupWebAssets proxies all non-backend URLs to the live Vite dev server. The
// web app is not embedded in dev builds, so `pgrokd-web:dev` must be running.
func setupWebAssets(f *flamego.Flame) {
	viteURL, err := url.Parse("http://localhost:5173")
	if err != nil {
		log.Fatal("Failed to parse vite URL", "error", err.Error())
		return
	}
	viteProxy := httputil.NewSingleHostReverseProxy(viteURL)
	f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
		viteProxy.ServeHTTP(w, r)
	})
}
