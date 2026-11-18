package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/errors"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
	"github.com/pgrok/pgrok/internal/inspector"
	"github.com/pgrok/pgrok/internal/tui"
)

// tunnel contains everything needed to run a single tunnel.
type tunnel struct {
	protocol        string
	remoteAddr      string
	forwardAddr     string
	token           string
	subdomain       string                 // HTTP only
	dynamicForwards string                 // HTTP only
	headers         dynamicforward.Options // HTTP only
	// inspectAddr is the local address to serve the request inspector on, HTTP
	// only. Empty or "false" disables it.
	inspectAddr string
}

// errTokenRejected is returned by runTunnel when the server rejects the token,
// in which case reconnecting would never succeed.
var errTokenRejected = errors.New("token rejected by the server")

// runTunnel connects the tunnel to the remote server and keeps reconnecting with
// backoff whenever the connection fails, reporting to hub, which may be nil. It
// only returns on unrecoverable errors, e.g. errTokenRejected.
func runTunnel(logger *logx.Logger, t tunnel, hub *tui.Hub) error {
	localAddr := t.forwardAddr
	var inspectURL string
	if t.protocol == protocolHTTP {
		logger.Info("Default forward", "address", t.forwardAddr)
		forwards := parseDynamicForwards(logger, t.dynamicForwards)
		forwardHandler, err := dynamicforward.New(logger, t.forwardAddr, t.headers, forwards...)
		if err != nil {
			return errors.Wrap(err, "create forward handler")
		}
		// The terminal UI captures requests next to the reverse proxy, so that
		// it sees the requests the inspector replays too.
		forwardHandler = hub.Capture(forwardHandler, func(path string) (prefix, addr string) {
			for _, f := range forwards {
				if strings.HasPrefix(path, f.Prefix) && len(f.Prefix) > len(prefix) {
					prefix, addr = f.Prefix, f.Address
				}
			}
			if addr == "" {
				addr = t.forwardAddr
			}
			return prefix, addr
		})

		if t.inspectAddr != "" && t.inspectAddr != "false" {
			ln, err := net.Listen("tcp", t.inspectAddr)
			if err != nil {
				logger.Warn("Failed to start request inspector, continuing without it", "address", t.inspectAddr, "error", err)
			} else {
				defer func() { _ = ln.Close() }()
				ins := inspector.New(forwardHandler)
				forwardHandler = ins
				inspectURL = "http://" + ln.Addr().String()
				go func() {
					err := http.Serve(ln, ins.Handler())
					logger.Error("Request inspector stopped", "error", err)
				}()
			}
		}

		routes := make([][2]string, 0, len(forwards))
		for _, f := range forwards {
			routes = append(routes, [2]string{f.Prefix, f.Address})
		}
		hub.SetHTTP(routes, inspectURL, forwardHandler)

		s := httptest.NewServer(forwardHandler)
		defer s.Close()
		logger.Debug("Capture server is running on", "url", s.URL)

		surl, _ := url.Parse(s.URL)
		localAddr = surl.Host
	} else {
		logger.Info("Forward", "address", t.forwardAddr)
	}

	// Each tunnel has its own hook, so it only compares against its own last URL.
	// The terminal UI has its own share modal, and printing to stdout would
	// corrupt its screen.
	onReady := func(string) {}
	if hub == nil {
		onReady = newReadyHook(logger)
	}
	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		hub.SetConnecting()
		err := tryConnect(logger, hub, t.protocol, t.remoteAddr, localAddr, t.token, t.subdomain, inspectURL, onReady)
		if err != nil {
			if time.Now().After(cooldownAfter) {
				failed = 0
			}
			backoff := time.Duration(2<<(failed/3+1)) * time.Second
			logger.Error(
				fmt.Sprintf("Failed to connect to server, will reconnect in %s", backoff.String()),
				"error", err.Error(),
			)
			if strings.Contains(err.Error(), "no supported methods remain") {
				return errTokenRejected
			}
			hub.SetDisconnected(err, backoff)
			select {
			case <-time.After(backoff):
			case <-hub.Retry():
			}
			cooldownAfter = time.Now().Add(time.Minute)
		}
	}
}

// fatalTunnelError terminates the process with a message for the error returned
// by runTunnel.
func fatalTunnelError(ctx context.Context, logger *logx.Logger, err error) {
	if errors.Is(err, errTokenRejected) {
		logger.FatalContext(ctx, "Please double check your token and try again")
	}
	logger.FatalContext(ctx, "Failed to run tunnel", "error", err)
}

func parseDynamicForwards(logger *logx.Logger, rules string) []dynamicforward.Forward {
	var forwards []dynamicforward.Forward
	for _, rule := range strings.Split(rules, "\n") {
		if rule == "" {
			continue
		}

		fields := strings.Fields(rule)
		if len(fields) != 2 {
			logger.Debug("Skipped invalid dynamic forward rule", "rule", rule)
			continue
		}

		forwards = append(forwards,
			dynamicforward.Forward{
				Prefix:  fields[0],
				Address: fields[1],
			},
		)
		logger.Debug("Added dynamic forward rule", "pathPrefix", fields[0], "forwardTo", fields[1])
	}
	return forwards
}
