//go:build !prod

package web

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
)

func mountWebAppRoutes(f *flamego.Flame) error {
	viteURL, err := url.Parse("http://localhost:5173")
	if err != nil {
		return errors.Wrap(err, "parse vite URL")
	}
	viteProxy := httputil.NewSingleHostReverseProxy(viteURL)
	f.Get("/{**}", func(w http.ResponseWriter, r *http.Request) {
		viteProxy.ServeHTTP(w, r)
	})
	return nil
}
