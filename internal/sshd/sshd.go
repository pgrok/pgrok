package sshd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/database"
)

// Start starts a SSH server listening on the given port.
func Start(
	logger *log.Logger,
	port int,
	proxy conf.Proxy,
	db *database.DB,
	getHostByToken func(token string) (host string, _ error),
	newProxy func(host, forward string),
	removeProxy func(host string),
) error {
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, token []byte) (*ssh.Permissions, error) {
			host, err := getHostByToken(string(token))
			if err != nil {
				return nil, err
			}
			return &ssh.Permissions{Extensions: map[string]string{"host": host}}, nil
		},
	}

	signers, err := ensureHostKeys(context.Background(), db)
	if err != nil {
		return errors.Wrap(err, "ensure host keys")
	}
	for _, signer := range signers {
		config.AddHostKey(signer)
	}

	address := "0.0.0.0:" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.Wrap(err, "listen")
	}
	defer func() { _ = listener.Close() }()
	logger.Info("Server started", "address", address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Failed to accept incoming connection", "error", err)
			continue
		}

		// A handshake must be performed on the incoming net.Conn before using it. It
		// must be handled in a separate goroutine, otherwise one user could easily
		// block entire loop. For example, user could be asked to trust server key
		// fingerprint and hangs.
		go func(conn net.Conn) {
			defer func() { _ = conn.Close() }()

			logger.Debug("Handshaking", "remote", conn.RemoteAddr())
			serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
			if err != nil {
				if err == io.EOF || errors.Is(err, syscall.ECONNRESET) {
					logger.Debug("Handshake terminated unexpectedly",
						"remote", conn.RemoteAddr(),
						"error", err,
					)
				} else {
					logger.Error("Failed to handshake",
						"remote", conn.RemoteAddr(),
						"error", err,
					)
				}
				return
			}
			logger.Debug("Serving connection",
				"remote", serverConn.RemoteAddr(),
				"clientVersion", string(serverConn.ClientVersion()),
			)

			// The incoming channels and requests must be serviced.
			go func() {
				for newChan := range chans {
					logger.Debug("Discarded channel", "type", newChan.ChannelType())
				}
			}()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			for req := range reqs {
				switch req.Type {
				case "tcpip-forward":
					go handleTCPIPForward(
						ctx,
						cancel,
						logger,
						serverConn,
						req,
						func(forward string) { newProxy(serverConn.Permissions.Extensions["host"], forward) },
						func() { removeProxy(serverConn.Permissions.Extensions["host"]) },
					)
				case "cancel-tcpip-forward":
					go func(req *ssh.Request) {
						logger.Debug("Forward cancel request", "remote", serverConn.RemoteAddr())
						cancel()
						_ = req.Reply(true, nil)
					}(req)
				case "server-info":
					protocol := "http"
					if len(req.Payload) > 0 {
						var payload struct {
							Protocol string `json:"protocol"`
						}
						err := json.Unmarshal(req.Payload, &payload)
						if err != nil {
							_ = req.Reply(false, []byte(err.Error()))
							return
						}
						protocol = payload.Protocol
					}

					var hostURL string
					if protocol == "tcp" {
						host := proxy.Domain
						if i := strings.Index(host, ":"); i > 0 {
							host = host[:i]
						}
						hostURL = "tcp://" + host + ":" + serverConn.Permissions.Extensions["tcp-port"]
					} else {
						hostURL = proxy.Scheme + "://" + serverConn.Permissions.Extensions["host"]
					}

					resp, err := json.Marshal(map[string]string{
						"host_url": hostURL,
					})
					if err != nil {
						logger.Error("Failed to marshal server info",
							"remote", serverConn.RemoteAddr(),
							"error", err,
						)
						_ = req.Reply(false, []byte("Internal server error"))
						return
					}
					_ = req.Reply(true, resp)
				default:
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
				}
			}
		}(conn)
	}
}

func handleTCPIPForward(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *log.Logger,
	serverConn *ssh.ServerConn,
	req *ssh.Request,
	newProxy func(forward string),
	removeProxy func(),
) {
	// RFC 4254 7.1, https://www.rfc-editor.org/rfc/rfc4254#section-7.1
	var forwardRequest struct {
		Addr  string
		Rport uint32
	}
	err := ssh.Unmarshal(req.Payload, &forwardRequest)
	if err != nil {
		logger.Error("Failed to unmarshal forward payload",
			"remote", serverConn.RemoteAddr(),
			"error", err,
		)
		_ = req.Reply(false, nil)
		return
	}
	if forwardRequest.Rport != 0 {
		_ = req.Reply(false, nil)
		return
	}

	port, err := findAvailablePort()
	if err != nil {
		_ = req.Reply(false, nil)
		logger.Error("Failed to find available port",
			"remote", serverConn.RemoteAddr(),
			"error", err,
		)
		return
	}
	address := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.Error("Failed to listen on reverse tunnel address",
			"remote", serverConn.RemoteAddr(),
			"forwardTo", address,
			"error", err,
		)
		_ = req.Reply(false, nil)
		return
	}
	defer func() {
		_ = listener.Close()
		logger.Info("Reverse tunnel server stopped",
			"remote", serverConn.RemoteAddr(),
			"forwardTo", address,
		)
	}()
	logger.Info("Reverse tunnel server started",
		"remote", serverConn.RemoteAddr(),
		"forwardTo", address,
	)
	serverConn.Permissions.Extensions["tcp-port"] = fmt.Sprintf("%d", port)

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
					logger.Error("Failed to accept incoming tunnel connection", "error", err)
				}
				return
			}
			logger.Debug("Tunneling connection",
				"remote", conn.RemoteAddr(),
				"forwardTo", address,
			)

			go func() {
				defer func() {
					_ = conn.Close()
					logger.Debug("Tunneling connection closed",
						"remote", conn.RemoteAddr(),
						"forwardTo", address,
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
				stream, reqs, err := serverConn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
				if err != nil {
					logger.Error("Failed to open tunneling channel",
						"remote", conn.RemoteAddr(),
						"forwardTo", address,
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
		_ = serverConn.Wait()
		cancel()
	}()

	newProxy(address)
	<-ctx.Done()
	removeProxy()
}

func ensureHostKeys(ctx context.Context, db *database.DB) ([]ssh.Signer, error) {
	var signers []ssh.Signer
	for algorithm, keygen := range map[cryptoutil.KeyAlgorithm]func() ([]byte, error){
		cryptoutil.KeyAlgorithmRSA:     cryptoutil.NewRSAPEM,
		cryptoutil.KeyAlgorithmEd25519: cryptoutil.NewEd25519PEM,
		cryptoutil.KeyAlgorithmECDSA:   cryptoutil.NewECDSAPEM,
	} {
		hostKey, err := db.GetHostKeyByAlgorithm(ctx, algorithm)
		if err != nil {
			if err != database.ErrHostKeyNotExists {
				return nil, errors.Wrapf(err, "get host key with algorithm %q", algorithm)
			}

			pem, err := keygen()
			if err != nil {
				return nil, errors.Wrapf(err, "generate %q PEM", algorithm)
			}
			hostKey, err = db.CreateHostKey(ctx, algorithm, pem)
			if err != nil {
				return nil, errors.Wrapf(err, "create host key with algorithm %q", algorithm)
			}
		}

		signer, err := ssh.ParsePrivateKey(hostKey.PEM)
		if err != nil {
			return nil, errors.Wrap(err, "parse host key")
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

// findAvailablePort returns a random port between 10000-20000 that is available
// for use.
func findAvailablePort() (int, error) {
	r := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		port := r.Intn(10000) + 10000
		address := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", address)
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}
	return 0, errors.New("no luck after 100 iterations")
}
