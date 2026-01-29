package sshd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
	"github.com/pgrok/pgrok/internal/strutil"
)

// Client is a SSH client that has established a connection.
type Client struct {
	logger          *log.Logger
	db              *database.DB
	serverConn      *ssh.ServerConn
	principal       *database.Principal
	protocol        string
	host            string
	hostReady       context.Context
	hostReadyCancel context.CancelFunc
}

func (c *Client) handleHint(req *ssh.Request) {
	var payload struct {
		Protocol string `json:"protocol"`
	}
	err := json.Unmarshal(req.Payload, &payload)
	if err != nil {
		_ = req.Reply(false, []byte(err.Error()))
		return
	}
	if payload.Protocol != "tcp" && payload.Protocol != "http" {
		_ = req.Reply(false, []byte("unsupported protocol: "+payload.Protocol))
		return
	}
	c.protocol = payload.Protocol
	_ = req.Reply(true, nil)
}

func (c *Client) handleTCPIPForward(
	ctx context.Context,
	cancel context.CancelFunc,
	proxy conf.Proxy,
	req *ssh.Request,
	proxies *reverseproxy.Cluster,
) {
	// RFC 4254 7.1, https://www.rfc-editor.org/rfc/rfc4254#section-7.1
	var forwardRequest struct {
		Addr  string
		Rport uint32
	}
	err := ssh.Unmarshal(req.Payload, &forwardRequest)
	if err != nil {
		c.logger.Error("Failed to unmarshal forward payload",
			"remote", c.serverConn.RemoteAddr(),
			"error", err,
		)
		_ = req.Reply(false, nil)
		return
	}
	if forwardRequest.Rport != 0 {
		_ = req.Reply(false, nil)
		return
	}

	var port int
	var listener net.Listener
	switch c.protocol {
	case "tcp":
		// Attempt to use the same port as the last time
		if c.principal.LastTCPPort >= proxy.TCP.PortStart && c.principal.LastTCPPort < proxy.TCP.PortEnd {
			listener, err = net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(c.principal.LastTCPPort))
			if err == nil {
				port = c.principal.LastTCPPort
				break
			}
		}
		listener, port, err = acquireAvailablePort(proxy.TCP.PortStart, proxy.TCP.PortEnd)

	case "http":
		listener, port, err = acquireAvailablePort(15000, 20000)
	default:
		err = errors.Errorf("unsupported protocol: %s", c.protocol)
	}
	if err != nil {
		_ = req.Reply(false, nil)
		c.logger.Error("Failed to find available port",
			"remote", c.serverConn.RemoteAddr(),
			"error", err,
		)
		return
	}
	defer func() {
		_ = listener.Close()
		c.logger.Info("Reverse tunnel server stopped",
			"remote", c.serverConn.RemoteAddr(),
			"forwardTo", listener.Addr(),
		)
	}()
	c.logger.Info("Reverse tunnel server started",
		"remote", c.serverConn.RemoteAddr(),
		"forwardTo", listener.Addr(),
	)

	type forwardResponse struct {
		Port uint32
	}
	payload := &forwardResponse{Port: uint32(port)}
	_ = req.Reply(true, ssh.Marshal(payload))

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					c.logger.Error("Failed to accept incoming tunnel connection", "error", err)
				}
				return
			}
			c.logger.Debug("Tunneling connection",
				"remote", conn.RemoteAddr(),
				"forwardTo", listener.Addr(),
			)

			go func() {
				defer func() {
					_ = conn.Close()
					c.logger.Debug("Tunneling connection closed",
						"remote", conn.RemoteAddr(),
						"forwardTo", listener.Addr(),
					)
				}()

				host, portStr, _ := net.SplitHostPort(conn.RemoteAddr().String())
				port, _ := strconv.Atoi(portStr)

				// RFC 4254 7.2, , https://www.rfc-editor.org/rfc/rfc4254#section-7.2
				type forwardedPayload struct {
					Addr       string
					Port       uint32
					OriginAddr string
					OriginPort uint32
				}
				payload := &forwardedPayload{
					Addr:       forwardRequest.Addr,
					Port:       payload.Port,
					OriginAddr: host,
					OriginPort: uint32(port),
				}
				stream, reqs, err := c.serverConn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
				if err != nil {
					c.logger.Error("Failed to open tunneling channel",
						"remote", conn.RemoteAddr(),
						"forwardTo", listener.Addr(),
						"error", err,
					)
					return
				}
				defer func() { _ = stream.Close() }()
				go ssh.DiscardRequests(reqs)

				streamCtx, done := context.WithCancel(ctx)
				go func() {
					_, _ = io.Copy(stream, conn)
					done()
				}()
				go func() {
					_, _ = io.Copy(conn, stream)
					done()
				}()
				<-streamCtx.Done()
			}()
		}
	}()
	go func() {
		_ = c.serverConn.Wait()
		cancel()
	}()

	if c.principal.LastTCPPort != port {
		// NOTE: We need to set the last TCP port regardless of the protocol to be
		// compatible with old clients which would not send "demand" requests.
		c.principal.LastTCPPort = port

		// Save the used port for the next time
		err = c.db.UpdatePrincipalLastTCPPort(ctx, c.principal.ID, c.principal.LastTCPPort)
		if err != nil {
			c.logger.Error("Failed to update principal last TCP port",
				"remote", c.serverConn.RemoteAddr(),
				"error", err,
			)
		}
	}

	if c.protocol == "http" {
		maxRetries := 3
		for _, exists := proxies.Get(c.host); exists && maxRetries > 0; maxRetries-- {
			newHost := randomHex(8) + "-" + c.host
			_, exists = proxies.Get(newHost)
			if !exists {
				c.host = newHost
				break
			}
		}
		c.hostReadyCancel() // Prevent race where server-info request reads host before setting it
		c.logger.Warn("Failed to find unused subdomain after %d retries.", maxRetries)
		proxies.Set(c.host, listener.Addr().String())
	}
	<-ctx.Done()
	if c.protocol == "http" {
		proxies.Remove(c.host)
	}
}

func randomHex(n int) string {
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	bytes := make([]byte, n)
	for i := range bytes {
		bytes[i] = byte(r.Intn(256))
	}
	return hex.EncodeToString(bytes)
}

// acquireAvailablePort tries to find an available port in the range [start,
// end) and returns a listener on that port. It returns an error if it fails to
// find an available port after 100 attempts.
func acquireAvailablePort(start, end int) (_ net.Listener, port int, _ error) {
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		port = r.Intn(end-start) + start
		address := fmt.Sprintf("0.0.0.0:%d", port)
		listener, err := net.Listen("tcp", address)
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, errors.New("no luck after 100 attempts")
}

func (c *Client) handleServerInfo(proxy conf.Proxy, req *ssh.Request) {
	if len(req.Payload) > 0 {
		var payload struct {
			Protocol string `json:"protocol"`
		}
		err := json.Unmarshal(req.Payload, &payload)
		if err != nil {
			_ = req.Reply(false, []byte(err.Error()))
			return
		}
		c.protocol = payload.Protocol
	}

	var hostURL string
	switch c.protocol {
	case "tcp":
		host := strutil.Coalesce(proxy.TCP.Domain, proxy.Domain)
		if i := strings.Index(host, ":"); i > 0 {
			host = host[:i]
		}
		hostURL = "tcp://" + host + ":" + strconv.Itoa(c.principal.LastTCPPort)
	case "http":
		<-c.hostReady.Done()
		hostURL = proxy.Scheme + "://" + c.host
	default:
		_ = req.Reply(false, []byte(fmt.Sprintf("unsupported protocol: %s", c.protocol)))
		return
	}

	resp, err := json.Marshal(map[string]string{
		"host_url": hostURL,
	})
	if err != nil {
		c.logger.Error("Failed to marshal server info",
			"remote", c.serverConn.RemoteAddr(),
			"error", err,
		)
		_ = req.Reply(false, []byte("Internal server error"))
		return
	}
	_ = req.Reply(true, resp)
}
