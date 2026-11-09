// Package tunnels keeps track of the tunnels that are currently established.
package tunnels

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Tunnel is an established tunnel.
type Tunnel struct {
	ID          string
	PrincipalID int64     // The ID of the principal who owns the tunnel.
	Protocol    string    // Either "http" or "tcp".
	URL         string    // The public URL, as returned to the client in "server-info".
	Host        string    // The host routed by the HTTP proxy, empty for TCP tunnels.
	RemoteAddr  string    // The remote address of the SSH client.
	ConnectedAt time.Time // The time when the tunnel was established.

	close func() error
}

// Close closes the tunnel by terminating its underlying connection. The tunnel
// is removed from the registry once the connection is torn down.
func (t Tunnel) Close() error {
	return t.close()
}

// Registry contains the established tunnels identified by their IDs.
type Registry struct {
	mu      sync.RWMutex
	tunnels map[string]Tunnel
	hosts   map[string]string // Host -> ID of HTTP tunnels
}

// NewRegistry returns a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		tunnels: make(map[string]Tunnel),
		hosts:   make(map[string]string),
	}
}

// Add assigns a new ID to the tunnel and adds it to the registry. The closer is
// called to close the tunnel. It returns a function to remove the tunnel from
// the registry, which must be called once the tunnel has ended.
func (r *Registry) Add(t Tunnel, closer func() error) (remove func()) {
	t.ID = uuid.NewString()
	t.close = closer

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tunnels[t.ID] = t
	if t.Host != "" {
		r.hosts[t.Host] = t.ID
	}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.tunnels, t.ID)
		// The host may have been taken over by a newer tunnel already.
		if t.Host != "" && r.hosts[t.Host] == t.ID {
			delete(r.hosts, t.Host)
		}
	}
}

// Get returns the tunnel by the given ID. It returns a boolean to indicate
// whether such tunnel exists.
func (r *Registry) Get(id string) (Tunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tunnels[id]
	return t, ok
}

// GetByHost returns the HTTP tunnel serving the given host. It returns a
// boolean to indicate whether such tunnel exists.
func (r *Registry) GetByHost(host string) (Tunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tunnels[r.hosts[host]]
	return t, ok
}

// List returns all tunnels ordered by their connected time, oldest first.
func (r *Registry) List() []Tunnel {
	return r.list(func(Tunnel) bool { return true })
}

// ListByPrincipal returns tunnels owned by the given principal ordered by
// their connected time, oldest first.
func (r *Registry) ListByPrincipal(principalID int64) []Tunnel {
	return r.list(func(t Tunnel) bool { return t.PrincipalID == principalID })
}

func (r *Registry) list(match func(Tunnel) bool) []Tunnel {
	r.mu.RLock()
	tunnels := make([]Tunnel, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		if match(t) {
			tunnels = append(tunnels, t)
		}
	}
	r.mu.RUnlock()

	slices.SortFunc(tunnels, func(a, b Tunnel) int {
		return cmp.Or(a.ConnectedAt.Compare(b.ConnectedAt), cmp.Compare(a.ID, b.ID))
	})
	return tunnels
}
