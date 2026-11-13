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
	"golang.org/x/net/http/httpguts"
	"unknwon.dev/x/logx"
)

// UpstreamErrorHeader is set on the response when the client fails to reach the
// local service, so that pgrokd can tell it apart from a 502 returned by the
// local service itself and render a friendly error page.
const UpstreamErrorHeader = "X-Pgrok-Upstream-Error"

// Forward represents a dynamic forward rule.
type Forward struct {
	Prefix  string
	Address string
}

// HostHeaderRewrite is the special value of Options.HostHeader that sets the
// Host header to the host of the address a request is forwarded to.
const HostHeaderRewrite = "rewrite"

// Options contains options for rewriting forwarded requests.
type Options struct {
	// HostHeader overrides the Host header of forwarded requests and moves the
	// original one to the X-Forwarded-Host header. HostHeaderRewrite uses the
	// host of the forward address, and any other non-empty value is used
	// literally. The original Host header is kept when empty.
	HostHeader string
	// RequestHeaders are headers in the form of "Name: value" to be set on
	// forwarded requests, overwriting existing values. They are set after
	// RemoveRequestHeaders are removed.
	//
	// The reverse proxy still appends the local address to X-Forwarded-For and
	// drops hop-by-hop headers (e.g., Connection).
	RequestHeaders []string
	// RemoveRequestHeaders are names of headers to be removed from forwarded
	// requests.
	RemoveRequestHeaders []string
}

// New creates a new http.Handler for dynamic forwarding.
func New(logger *logx.Logger, defaultForwardAddr string, opts Options, forwards ...Forward) (http.Handler, error) {
	setHeaders := make(http.Header, len(opts.RequestHeaders))
	for _, h := range opts.RequestHeaders {
		name, value, ok := strings.Cut(h, ":")
		name = strings.TrimSpace(name)
		if !ok || !httpguts.ValidHeaderFieldName(name) {
			return nil, errors.Errorf(`invalid request header %q, want "Name: value"`, h)
		}
		if http.CanonicalHeaderKey(name) == "Host" {
			return nil, errors.New(`use the host header option to set the "Host" header`)
		}
		setHeaders.Set(name, strings.TrimSpace(value))
	}
	rewriteHeaders := opts.HostHeader != "" || len(setHeaders) > 0 || len(opts.RemoveRequestHeaders) > 0
	serve := func(w http.ResponseWriter, r *http.Request, proxy *httputil.ReverseProxy, forwardHost string) {
		if rewriteHeaders {
			r = r.Clone(r.Context())
			if opts.HostHeader != "" {
				r.Header.Set("X-Forwarded-Host", r.Host)
				r.Host = opts.HostHeader
				if opts.HostHeader == HostHeaderRewrite {
					r.Host = forwardHost
				}
			}
			for _, name := range opts.RemoveRequestHeaders {
				// Keep the key with a nil value instead of deleting it, otherwise the
				// reverse proxy adds X-Forwarded-For back.
				r.Header[http.CanonicalHeaderKey(name)] = nil
			}
			for name := range setHeaders {
				r.Header.Set(name, setHeaders.Get(name))
			}
		}
		proxy.ServeHTTP(w, r)
	}

	defaultForwardURL, err := url.Parse(defaultForwardAddr)
	if err != nil {
		return nil, errors.Wrap(err, "parse default forward address")
	}

	defaultProxy := newProxy(logger, defaultForwardURL)

	type prefixProxy struct {
		prefix string
		host   string
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
			host:   forwardURL.Host,
			proxy:  newProxy(logger, forwardURL),
		})
	}
	// Match the longest prefix first so that overlapping rules (e.g., "/api" and
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
				serve(w, r, p.proxy, p.host)
				return
			}
		}
		serve(w, r, defaultProxy, defaultForwardURL.Host)
	})
	return f, nil
}

// newProxy returns a reverse proxy to the target that marks its own failures
// to reach the target with UpstreamErrorHeader.
func newProxy(logger *logx.Logger, target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// The local service being down is not an error of pgrok, and the request
		// is already logged with its 502 status, so keep the details at debug level.
		logger.Debug("Failed to forward request", "path", r.URL.Path, "forwardTo", target.Host, "error", err)
		w.Header().Set(UpstreamErrorHeader, "1")
		w.WriteHeader(http.StatusBadGateway)
	}
	return proxy
}
