package sshd

import (
	"context"
	"io"
	"net"
	"strconv"
	"syscall"

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"

	"github.com/pgrok/pgrok/internal/conf"
	"github.com/pgrok/pgrok/internal/cryptoutil"
	"github.com/pgrok/pgrok/internal/database"
	"github.com/pgrok/pgrok/internal/reverseproxy"
)

// Start starts a SSH server listening on the given port.
func Start(
	logger *log.Logger,
	port int,
	proxy conf.Proxy,
	db *database.DB,
	proxies *reverseproxy.Cluster,
) error {
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, token []byte) (*ssh.Permissions, error) {
			principal, err := db.GetPrincipalByToken(context.Background(), string(token))
			if err != nil {
				return nil, err
			}
			return &ssh.Permissions{
				Extensions: map[string]string{
					"principal-id": strconv.FormatInt(principal.ID, 10),
				},
			}, nil
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

			principalID, _ := strconv.ParseInt(serverConn.Permissions.Extensions["principal-id"], 10, 64)
			principal, err := db.GetPrincipalByID(ctx, principalID)
			if err != nil {
				logger.Error("Failed to get principal", "error", err)
				return
			}

			client := &Client{
				logger:     logger,
				db:         db,
				serverConn: serverConn,
				principal:  principal,
				protocol:   "http",
				host:       principal.Subdomain + "." + proxy.Domain,
			}
			for req := range reqs {
				switch req.Type {
				case "hint":
					client.handleHint(req)
				case "tcpip-forward":
					go client.handleTCPIPForward(
						ctx,
						cancel,
						proxy,
						req,
						proxies,
					)
				case "cancel-tcpip-forward":
					go func(req *ssh.Request) {
						logger.Debug("Forward cancel request", "remote", serverConn.RemoteAddr())
						cancel()
						_ = req.Reply(true, nil)
					}(req)
				case "server-info":
					client.handleServerInfo(proxy, req)
				default:
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
				}
			}
		}(conn)
	}
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
