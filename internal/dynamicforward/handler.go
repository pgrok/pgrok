package dynamicforward

import (
	"cmp"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"unknwon.dev/x/logx"
)

// Forward represents a dynamic forward rule.
type Forward struct {
	Prefix  string
	Address string
}

// New creates a new http.Handler for dynamic forwarding.
func New(logger *logx.Logger, defaultForwardAddr string, forwards ...Forward) (http.Handler, error) {
	defaultForwardURL, err := url.Parse(defaultForwardAddr)
	if err != nil {
		return nil, errors.Wrap(err, "parse default forward address")
	}

	defaultProxy := httputil.NewSingleHostReverseProxy(defaultForwardURL)

	type prefixProxy struct {
		prefix string
		proxy  *httputil.ReverseProxy
	}
	proxies := make([]prefixProxy, 0, len(forwards))
	for _, forward := range forwards {
		forwardURL, err := url.Parse(forward.Address)
		if err != nil {
			return nil, errors.Wrapf(err, "parse forward address %q", forward.Address)
		}
		proxies = append(proxies, prefixProxy{
			prefix: forward.Prefix,
			proxy:  httputil.NewSingleHostReverseProxy(forwardURL),
		})
	}
	// Match the longest prefix first so that overlapping rules (e.g. "/api" and
	// "/api/v2") are resolved deterministically to the most specific one.
	slices.SortStableFunc(proxies, func(a, b prefixProxy) int {
		return cmp.Compare(len(b.prefix), len(a.prefix))
	})

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
		for _, p := range proxies {
			if strings.HasPrefix(r.URL.Path, p.prefix) {
				p.proxy.ServeHTTP(w, r)
				return
			}
		}
		defaultProxy.ServeHTTP(w, r)
	})
	return f, nil
}
