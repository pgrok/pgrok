package reverseproxy

import (
	"net/http"
	"net/http/httputil"

	"github.com/pkg/errors"
	"github.com/puzpuzpuz/xsync/v2"
)

// Cluster contains a list of proxies identified by their hosts.
type Cluster struct {
	proxies     map[string]*httputil.ReverseProxy
	proxiesLock xsync.RBMutex
}

// NewCluster returns a new Cluster.
func NewCluster() *Cluster {
	return &Cluster{proxies: make(map[string]*httputil.ReverseProxy)}
}

// Get returns the proxy by the given host. It returns a boolean to indicate
// whether such proxy exists.
func (c *Cluster) Get(host string) (*httputil.ReverseProxy, bool) {
	t := c.proxiesLock.RLock()
	defer c.proxiesLock.RUnlock(t)

	proxy, ok := c.proxies[host]
	return proxy, ok
}

// Set creates a new proxy pointing to the forward address for the given host.
func (c *Cluster) Set(host, forward string) {
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = forward
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(errors.Cause(err).Error()))
		},
	}

	c.proxiesLock.Lock()
	defer c.proxiesLock.Unlock()

	c.proxies[host] = proxy
}

// Remove removes the proxy with given host from the cluster.
func (c *Cluster) Remove(host string) {
	c.proxiesLock.Lock()
	defer c.proxiesLock.Unlock()
	delete(c.proxies, host)
}
