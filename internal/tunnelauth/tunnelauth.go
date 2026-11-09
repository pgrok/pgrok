// Package tunnelauth implements the optional access control of HTTP tunnels:
// basic auth, IP allowlists, and SSO through the identity provider of pgrokd.
package tunnelauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// Policy is the access policy of a tunnel. A nil policy allows everyone.
type Policy struct {
	allowIPs  []netip.Prefix
	basicAuth *[sha256.Size]byte // SHA-256 of "user:password", the password is never kept in plain text
	sso       bool
}

// NewPolicy validates the access control options requested by a client and
// returns the corresponding policy. It returns nil when no option is set.
func NewPolicy(basicAuth string, allowIPs []string, auth string) (*Policy, error) {
	if basicAuth == "" && len(allowIPs) == 0 && auth == "" {
		return nil, nil
	}

	p := &Policy{}
	if basicAuth != "" {
		user, password, ok := strings.Cut(basicAuth, ":")
		if !ok || user == "" || password == "" {
			return nil, errors.New(`invalid basic auth: must be in the form "user:password"`)
		}
		sum := sha256.Sum256([]byte(basicAuth))
		p.basicAuth = &sum
	}

	var err error
	p.allowIPs, err = ParsePrefixes(allowIPs)
	if err != nil {
		return nil, errors.Wrap(err, "invalid allowed IP")
	}

	switch auth {
	case "":
	case "sso":
		p.sso = true
	default:
		return nil, errors.Errorf("unsupported auth %q: only \"sso\" is supported", auth)
	}
	if p.sso && p.basicAuth != nil {
		return nil, errors.New("basic auth and SSO cannot be used together")
	}
	return p, nil
}

// SSO reports whether the policy requires visitors to sign in with SSO.
func (p *Policy) SSO() bool {
	return p != nil && p.sso
}

// ParsePrefixes parses a list of CIDRs or single IP addresses.
func ParsePrefixes(ss []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if addr, err := netip.ParseAddr(s); err == nil {
			addr = addr.Unmap().WithZone("")
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, errors.Errorf("%q is neither an IP address nor a CIDR", s)
		}
		if prefix.Addr().Is4In6() {
			return nil, errors.Errorf("%q: IPv4-mapped IPv6 CIDRs are not supported, use the IPv4 form", s)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func containsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the IP address of the visitor. The X-Forwarded-For header is
// only consulted when the direct peer is one of the trusted proxies, in which
// case the rightmost address that is not a trusted proxy is the client, because
// everything to the left of it may have been forged by the client. It returns
// the zero netip.Addr when the address cannot be determined.
func ClientIP(r *http.Request, trustedProxies []netip.Prefix) netip.Addr {
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := addrPort.Addr().Unmap().WithZone("")
	if !containsAddr(trustedProxies, ip) {
		return ip
	}

	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			// A trusted proxy would not report a malformed address.
			return netip.Addr{}
		}
		ip = hop.Unmap().WithZone("")
		if !containsAddr(trustedProxies, ip) {
			return ip
		}
	}
	// Every hop is a trusted proxy, the leftmost one is the closest to the client.
	return ip
}

// CallbackPath is the path on the tunnel host that completes the SSO handoff.
// It is handled by the proxy server and never forwarded to the upstream.
const CallbackPath = "/_pgrok/sso/callback"

const (
	cookieName = "_pgrok_sso"
	cookieTTL  = 24 * time.Hour
	ticketTTL  = time.Minute
)

type ticket struct {
	host    string
	path    string
	expires time.Time
}

// Guard enforces tunnel access policies on the proxy server, and issues the
// tickets of the SSO handoff on the web server.
//
// SSO works with a redirect handoff because tunnels and the web server live on
// different hosts: the proxy redirects an unauthenticated visitor to the web
// server, which uses (or establishes) the web session and redirects back to the
// CallbackPath on the tunnel host with a short-lived, single-use ticket. The
// proxy redeems the ticket and sets a host-only cookie signed with a
// per-process key, so restarting pgrokd signs everyone out of SSO tunnels.
type Guard struct {
	trustedProxies []netip.Prefix
	tunnelAuthURL  string
	secureCookie   bool
	key            []byte

	ticketsLock sync.Mutex
	tickets     map[string]ticket
}

// NewGuard returns a new Guard. The externalURL is the external URL of the web
// server, and secureCookie should be true when tunnels are served over HTTPS.
func NewGuard(trustedProxies []netip.Prefix, externalURL string, secureCookie bool) *Guard {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return &Guard{
		trustedProxies: trustedProxies,
		tunnelAuthURL:  externalURL + "/-/tunnel-auth",
		secureCookie:   secureCookie,
		key:            key,
		tickets:        make(map[string]ticket),
	}
}

// Authorize reports whether the request may be forwarded to the upstream. When
// it returns false, the response has been written. Credentials that belong to
// pgrok are removed from the request before it is forwarded.
func (g *Guard) Authorize(w http.ResponseWriter, r *http.Request, p *Policy) bool {
	if p == nil {
		return true
	}

	if len(p.allowIPs) > 0 {
		ip := ClientIP(r, g.trustedProxies)
		if !containsAddr(p.allowIPs, ip) {
			http.Error(w, "Access denied: your IP address ("+ip.String()+") is not allowed to access this tunnel.", http.StatusForbidden)
			return false
		}
	}

	if p.basicAuth != nil {
		user, password, ok := r.BasicAuth()
		sum := sha256.Sum256([]byte(user + ":" + password))
		if !ok || subtle.ConstantTimeCompare(sum[:], p.basicAuth[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="pgrok", charset="UTF-8"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return false
		}
		r.Header.Del("Authorization")
	}

	if p.sso {
		return g.authorizeSSO(w, r)
	}
	return true
}

func (g *Guard) authorizeSSO(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == CallbackPath {
		path, ok := g.redeemTicket(r.URL.Query().Get("ticket"), r.Host)
		if !ok {
			http.Error(w, "The sign-in ticket is invalid or has expired, please open the tunnel URL again.", http.StatusForbidden)
			return false
		}
		expires := time.Now().Add(cookieTTL)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    g.signCookie(r.Host, expires.Unix()),
			Path:     "/",
			Expires:  expires,
			Secure:   g.secureCookie,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Location", path)
		w.WriteHeader(http.StatusFound)
		return false
	}

	if g.verifyCookie(r) {
		cookies := r.Cookies()
		r.Header.Del("Cookie")
		for _, c := range cookies {
			if c.Name != cookieName {
				r.AddCookie(c)
			}
		}
		return true
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Unauthorized: sign in by opening the tunnel URL in a browser.", http.StatusUnauthorized)
		return false
	}
	query := url.Values{"host": {r.Host}, "path": {r.URL.RequestURI()}}
	http.Redirect(w, r, g.tunnelAuthURL+"?"+query.Encode(), http.StatusFound)
	return false
}

func (g *Guard) signCookie(host string, expires int64) string {
	exp := strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, g.key)
	_, _ = mac.Write([]byte(host + "\n" + exp))
	return exp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (g *Guard) verifyCookie(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	exp, _, _ := strings.Cut(c.Value, ".")
	expires, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(g.signCookie(r.Host, expires)))
}

// IssueTicket issues a single-use ticket for the SSO handoff that redirects the
// visitor to the given path of the tunnel host once redeemed. The caller must
// have verified that the host is an active tunnel that requires SSO.
func (g *Guard) IssueTicket(host, path string) string {
	// Only allow local paths so the callback cannot redirect to another host,
	// browsers treat "/\" the same as "//".
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.HasPrefix(path, `/\`) {
		path = "/"
	}

	id := rand.Text()
	now := time.Now()

	g.ticketsLock.Lock()
	defer g.ticketsLock.Unlock()
	for k, t := range g.tickets {
		if now.After(t.expires) {
			delete(g.tickets, k)
		}
	}
	g.tickets[id] = ticket{host: host, path: path, expires: now.Add(ticketTTL)}
	return id
}

func (g *Guard) redeemTicket(id, host string) (path string, ok bool) {
	g.ticketsLock.Lock()
	defer g.ticketsLock.Unlock()

	t, ok := g.tickets[id]
	delete(g.tickets, id)
	if !ok || t.host != host || time.Now().After(t.expires) {
		return "", false
	}
	return t.path, true
}
