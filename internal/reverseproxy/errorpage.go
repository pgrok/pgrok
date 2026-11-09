package reverseproxy

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

//go:embed errorpage.html
var errorPageHTML string

var errorPageTemplate = template.Must(template.New("errorpage").Parse(errorPageHTML))

type errorPage struct {
	Title   string
	Message string
	// Hint is static trusted markup, never derived from the request.
	Hint template.HTML
	// TextFormat is the plain-text body for non-browser clients, formatted with
	// the host.
	TextFormat string
}

var (
	noTunnelPage = errorPage{
		Title:      "This tunnel is offline",
		Message:    "There is no pgrok tunnel connected for this address right now.",
		Hint:       `If this is your tunnel, start it with <code>pgrok http &lt;port&gt;</code> and keep it running. The dashboard shows how to set up the client.`,
		TextFormat: "No reverse proxy is available for the host: %s\n",
	}
	upstreamDownPage = errorPage{
		Title:      "The local service is not responding",
		Message:    "The tunnel is connected but the local service at the other end is not responding.",
		Hint:       `If this is your tunnel, make sure the local service is running and that pgrok forwards to the right address (the port argument, <code>--forward-addr</code>, or <code>forward_addr</code> in pgrok.yml).`,
		TextFormat: "The tunnel for the host %s is connected but the local service at the other end is not responding.\n",
	}
)

// writeErrorPage writes the given error page with 502 status. It responds with
// HTML only when the client accepts it (i.e. browsers), and plain text
// otherwise (i.e. API clients and curl).
func writeErrorPage(w http.ResponseWriter, r *http.Request, page errorPage, dashboardURL string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, page.TextFormat, r.Host)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	_ = errorPageTemplate.Execute(w, struct {
		errorPage
		Host         string
		DashboardURL string
	}{page, r.Host, dashboardURL})
}

// ServeNoTunnel responds to a request whose host has no tunnel connected.
func (c *Cluster) ServeNoTunnel(w http.ResponseWriter, r *http.Request) {
	writeErrorPage(w, r, noTunnelPage, c.dashboardURL)
}
