package sshd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/pgrok/pgrok/internal/osutil"
)

// Start starts a SSH server listening on the given port.
func Start(logger log.Logger, port int) error {
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, token []byte) (*ssh.Permissions, error) {
			// TODO: Validate token and get the principal
			return &ssh.Permissions{Extensions: map[string]string{"token": string(token)}}, nil
		},
	}

	hostKeyDir := filepath.Join("data", "sshd")
	err := os.MkdirAll(hostKeyDir, os.ModePerm)
	if err != nil {
		return errors.Wrap(err, "create host key directory")
	}
	hostKeyPath := filepath.Join(hostKeyDir, "ed25519.pem")
	if !osutil.IsExist(hostKeyPath) {
		pem, err := newEd25519PEM()
		if err != nil {
			return errors.Wrap(err, "new ed25519 PEM")
		}
		err = os.WriteFile(hostKeyPath, pem, 0600)
		if err != nil {
			return errors.Wrap(err, "save ed25519 PEM")
		}
	}
	key, err := os.ReadFile(hostKeyPath)
	if err != nil {
		return errors.Wrap(err, "read host key")
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return errors.Wrap(err, "parse host key")
	}
	config.AddHostKey(signer)

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
		go func() {
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

			fmt.Println("token", serverConn.Permissions.Extensions["token"]) // todo delete me

			// The incoming channels and requests must be serviced.
			go func() {
				for newChan := range chans {
					fmt.Println("newChan", newChan.ChannelType())
				}
			}()

			for req := range reqs {
				switch req.Type {
				case "tcpip-forward":
					go handleTCPIPForward(logger, serverConn, req)
				case "cancel-tcpip-forward":
					go handleCancelTCPIPForward(logger, req)
				default:
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
				}
			}
		}()
	}
}

func handleTCPIPForward(logger log.Logger, serverConn *ssh.ServerConn, req *ssh.Request) {
	// RFC 4254 7.1, https://www.rfc-editor.org/rfc/rfc4254#section-7.1
	var forwardRequest struct {
		Addr  string
		Rport uint32
	}
	err := ssh.Unmarshal(req.Payload, &forwardRequest)
	if err != nil {
		logger.Error("Unmarshal forward payload",
			"remote", serverConn.RemoteAddr(),
			"error", err,
		)
		_ = req.Reply(false, nil)
		return
	}

	// todo: do not allow client to specify port, i.e. must be 0
	forwardRequest.Rport = 7777
	address := "127.0.0.1:" + strconv.Itoa(int(forwardRequest.Rport))
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

	type forwardResponse struct {
		Port uint32
	}
	payload := &forwardResponse{Port: 7777} // todo make a random available port
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
					Port:       forwardRequest.Rport,
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

				ctx, done := context.WithCancel(context.Background())
				go func() {
					_, _ = io.Copy(stream, conn)
					done()
				}()
				go func() {
					_, _ = io.Copy(conn, stream)
					done()
				}()
				<-ctx.Done()
			}()
		}
	}()
	_ = serverConn.Wait()
}

func handleCancelTCPIPForward(logger log.Logger, req *ssh.Request) {
	_ = req.Reply(true, nil)
	// todo
}

func newEd25519PEM() ([]byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "generate key")
	}

	data, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, errors.Wrap(err, "marshal private key")
	}

	return pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: data,
		},
	), nil
}
