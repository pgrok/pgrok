package reverseproxy

import (
	"net/http"
	"net/http/httputil"

	"github.com/pkg/errors"
	"github.com/puzpuzpuz/xsync/v2"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

// Cluster contains a list of proxies identified by their hosts.
type Cluster struct {
	logger       *logx.Logger
	dashboardURL string
	proxies      map[string]*httputil.ReverseProxy
	proxiesLock  xsync.RBMutex
}

// NewCluster returns a new Cluster. The dashboardURL is linked from the error
// pages.
func NewCluster(logger *logx.Logger, dashboardURL string) *Cluster {
	return &Cluster{
		logger:       logger,
		dashboardURL: dashboardURL,
		proxies:      make(map[string]*httputil.ReverseProxy),
	}
}

// Get returns the proxy by the given host. It returns a boolean to indicate
// whether such proxy exists.
func (c *Cluster) Get(host string) (*httputil.ReverseProxy, bool) {
	t := c.proxiesLock.RLock()
	defer c.proxiesLock.RUnlock(t)

	proxy, ok := c.proxies[host]
	return proxy, ok
}

// Set atomically sets a proxy for the given host. If the host is already taken,
// it tries the alternative host. It returns the actually used host or an error
// if both are taken.
func (c *Cluster) Set(host, alternative, forward string) (string, error) {
	c.proxiesLock.Lock()
	defer c.proxiesLock.Unlock()

	if _, exists := c.proxies[host]; !exists {
		c.proxies[host] = c.newProxy(forward)
		return host, nil
	}
	if _, exists := c.proxies[alternative]; !exists {
		c.proxies[alternative] = c.newProxy(forward)
		return alternative, nil
	}
	return "", errors.New("host and alternative are both taken")
}

var errUpstreamDown = errors.New("local service is not responding")

func (c *Cluster) newProxy(forward string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			r.Out.URL.Host = forward
			r.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.Header.Get(dynamicforward.UpstreamErrorHeader) != "" {
				return errUpstreamDown
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// The error may contain internal addresses, so only log it.
			c.logger.Debug("Failed to proxy request", "host", r.Host, "error", err)
			writeErrorPage(w, r, upstreamDownPage, c.dashboardURL)
		},
	}
}

// Remove removes the proxy with given host from the cluster.
func (c *Cluster) Remove(host string) {
	c.proxiesLock.Lock()
	defer c.proxiesLock.Unlock()
	delete(c.proxies, host)
}
