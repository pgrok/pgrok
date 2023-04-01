package dynamicforward

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
)

// Forward represents a dynamic forward rule.
type Forward struct {
	Prefix  string
	Address string
}

// New creates a new http.Handler for dynamic forwarding.
func New(logger *log.Logger, defaultForwardAddr string, forwards ...Forward) (http.Handler, error) {
	defaultForwardURL, err := url.Parse(defaultForwardAddr)
	if err != nil {
		return nil, errors.Wrap(err, "parse default forward address")
	}

	defaultProxy := httputil.NewSingleHostReverseProxy(defaultForwardURL)
	proxies := make(map[string]*httputil.ReverseProxy)
	for _, forward := range forwards {
		forwardURL, err := url.Parse(forward.Address)
		if err != nil {
			return nil, errors.Wrapf(err, "parse forward address %q", forward.Address)
		}
		proxies[forward.Prefix] = httputil.NewSingleHostReverseProxy(forwardURL)
	}

	f := flamego.New()
	f.Use(func(c flamego.Context) {
		started := time.Now()
		c.Next()
		logger.Info("Forwarded request",
			"path", c.Request().URL.Path,
			"status", c.ResponseWriter().Status(),
			"duration", time.Since(started),
		)
	})
	f.Any("/{**}", func(w http.ResponseWriter, r *http.Request) {
		for prefix, proxy := range proxies {
			if strings.HasPrefix(r.URL.Path, prefix) {
				proxy.ServeHTTP(w, r)
				return
			}
		}
		defaultProxy.ServeHTTP(w, r)
	})
	return f, nil
}
