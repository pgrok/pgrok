package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/dynamicforward"
)

func commandHTTP(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:  "http",
		Usage: "Start a HTTP proxy to local endpoints",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionHTTP(ctx, cmd, logger)
		},
		// Header values may contain commas, e.g., "Accept: text/html, */*".
		DisableSliceFlagSeparator: true,
		Flags: append(
			commonFlags(homeDir, logger),
			&cli.StringFlag{
				Name:    "remote-addr",
				Usage:   "The address of the remote SSH server",
				Aliases: []string{"r"},
			},
			&cli.StringFlag{
				Name:    "forward-addr",
				Usage:   "The address to forward requests to",
				Aliases: []string{"f"},
				Action: func(_ context.Context, cmd *cli.Command, s string) error {
					return cmd.Set("forward-addr", deriveHTTPForwardAddress(s))
				},
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "The authentication token",
				Aliases: []string{"t"},
			},
			&cli.StringFlag{
				Name:    "subdomain",
				Usage:   "specify a custom subdomain prefix for a deterministic tunnel URL",
				Aliases: []string{"s"},
			},
			&cli.StringFlag{
				Name:  "host-header",
				Usage: `The Host header to forward requests with, "rewrite" to use the host of the forward address`,
			},
			&cli.StringSliceFlag{
				Name:  "request-header",
				Usage: `A header in the form of "Name: value" to set on forwarded requests, can be repeated`,
			},
			&cli.StringSliceFlag{
				Name:  "remove-request-header",
				Usage: "The name of a header to remove from forwarded requests, can be repeated",
			},
			&cli.StringFlag{
				Name:  "basic-auth",
				Usage: `require visitors to provide HTTP basic auth credentials in the form of "user:password"`,
			},
			&cli.StringSliceFlag{
				Name:  "allow-ip",
				Usage: "only allow visitors from the IP address or CIDR, can be repeated",
			},
			&cli.StringFlag{
				Name:  "auth",
				Usage: `require visitors to sign in, only "sso" is supported to require signing in with the identity provider of the server`,
			},
		),
	}
}

func actionHTTP(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	configPath := cmd.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		logger.FatalContext(ctx, "Failed to load config",
			"config", configPath,
			"error", err,
		)
	}
	logger.Debug("Loaded config", "file", configPath)

	defaultForwardAddr := cmp.Or(
		deriveHTTPForwardAddress(cmd.Args().First()),
		cmd.String("forward-addr"),
		config.ForwardAddr,
	)
	logger.Info("Default forward", "address", defaultForwardAddr)

	dynamicForwardRules := strings.Split(config.DynamicForwards, "\n")
	dynamicForwards := make([]dynamicforward.Forward, 0, len(dynamicForwardRules))
	for _, rule := range dynamicForwardRules {
		if rule == "" {
			continue
		}

		fields := strings.Fields(rule)
		if len(fields) != 2 {
			logger.Debug("Skipped invalid dynamic forward rule", "rule", rule)
			continue
		}

		dynamicForwards = append(dynamicForwards,
			dynamicforward.Forward{
				Prefix:  fields[0],
				Address: fields[1],
			},
		)
		logger.Debug("Added dynamic forward rule", "pathPrefix", fields[0], "forwardTo", fields[1])
	}
	forwardHandler, err := dynamicforward.New(
		logger,
		defaultForwardAddr,
		dynamicforward.Options{
			HostHeader:           cmp.Or(cmd.String("host-header"), config.HostHeader),
			RequestHeaders:       append(config.RequestHeaders, cmd.StringSlice("request-header")...),
			RemoveRequestHeaders: append(config.RemoveRequestHeaders, cmd.StringSlice("remove-request-header")...),
		},
		dynamicForwards...,
	)
	if err != nil {
		logger.FatalContext(ctx, "Failed to create forward handler", "error", err)
	}

	s := httptest.NewServer(forwardHandler)
	logger.Debug("Capture server is running on", "url", s.URL)

	surl, _ := url.Parse(s.URL)
	allowIPs := cmd.StringSlice("allow-ip")
	if len(allowIPs) == 0 {
		allowIPs = config.AllowIPs
	}
	onReady := newReadyHook(logger)
	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		err := tryConnect(
			logger,
			protocolHTTP,
			cmp.Or(cmd.String("remote-addr"), config.RemoteAddr),
			surl.Host,
			cmp.Or(cmd.String("token"), config.Token),
			cmd.String("subdomain"),
			accessOptions{
				BasicAuth: cmp.Or(cmd.String("basic-auth"), config.BasicAuth),
				AllowIPs:  allowIPs,
				Auth:      cmp.Or(cmd.String("auth"), config.Auth),
			},
			onReady,
		)
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
				logger.FatalContext(ctx, "Please double check your token and try again")
			}
			time.Sleep(backoff)
			cooldownAfter = time.Now().Add(time.Minute)
		}
	}
}

type Config struct {
	RemoteAddr      string `yaml:"remote_addr"`
	ForwardAddr     string `yaml:"forward_addr"`
	Token           string `yaml:"token"`
	DynamicForwards string `yaml:"dynamic_forwards"`

	HostHeader           string   `yaml:"host_header"`
	RequestHeaders       []string `yaml:"request_headers"`
	RemoveRequestHeaders []string `yaml:"remove_request_headers"`

	// Optional access control of HTTP tunnels.
	BasicAuth string   `yaml:"basic_auth"`
	AllowIPs  []string `yaml:"allow_ips"`
	Auth      string   `yaml:"auth"`
}

// accessOptions are the access control options of a tunnel sent to the server.
// Only HTTP tunnels support them, the server rejects them for TCP tunnels.
type accessOptions struct {
	BasicAuth string   `json:"basic_auth,omitempty"`
	AllowIPs  []string `json:"allow_ips,omitempty"`
	Auth      string   `json:"auth,omitempty"`
}

func loadConfig(configPath string) (*Config, error) {
	p, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}

	var config Config
	err = yaml.Unmarshal(p, &config)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal")
	}
	return &config, nil
}

const (
	protocolHTTP string = "http"
	protocolTCP  string = "tcp"
)

// tryConnect establishes the tunnel and blocks until it fails. The onReady
// callback is invoked with the public URL reported by the server (which may be
// empty) every time the tunnel is up.
func tryConnect(logger *logx.Logger, protocol, remoteAddr, forwardAddr, token, subdomain string, access accessOptions, onReady func(hostURL string)) error {
	client, err := ssh.Dial(
		"tcp",
		remoteAddr,
		&ssh.ClientConfig{
			User: "pgrok",
			Auth: []ssh.AuthMethod{
				ssh.Password(token),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
	)
	if err != nil {
		return errors.Wrap(err, "dial remote server")
	}

	// Hint the server before establishing the reverse tunnel
	payload, err := json.Marshal(struct {
		Protocol  string `json:"protocol"`
		Subdomain string `json:"subdomain"`
		accessOptions
	}{protocol, subdomain, access})
	if err != nil {
		return errors.Wrap(err, "marshal payload")
	}
	ok, reply, err := client.SendRequest("hint", true, payload)
	if err != nil {
		return errors.Wrap(err, "hint server")
	}
	if !ok {
		return errors.Errorf("server rejected hint: %s", reply)
	}
	if access.BasicAuth != "" || len(access.AllowIPs) > 0 || access.Auth != "" {
		// Servers without the support of access control silently ignore the
		// options, which would expose the tunnel to everyone.
		var hintReply struct {
			AccessControl bool `json:"access_control"`
		}
		_ = json.Unmarshal(reply, &hintReply)
		if !hintReply.AccessControl {
			return errors.New("server does not support access control, refusing to open an unprotected tunnel")
		}
	}

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "open port on remote connection")
	}
	defer func() { _ = remoteListener.Close() }()

	// Query the server info after the reverse tunnel is established
	var serverInfo struct {
		HostURL string `json:"host_url"`
	}
	ok, reply, err = client.SendRequest("server-info", true, payload)
	if err != nil {
		return errors.Wrap(err, "query server info")
	} else if !ok {
		return errors.Errorf("query server info rejected: %s", reply)
	}
	err = json.Unmarshal(reply, &serverInfo)
	if err != nil {
		return errors.Wrap(err, "unmarshal server info")
	}

	message := "🎉 You're ready to go live!"
	if serverInfo.HostURL != "" {
		message = fmt.Sprintf("🎉 You're ready to go live at %s!", serverInfo.HostURL)
	}
	logger.Info(message, "remote", remoteAddr)
	onReady(serverInfo.HostURL)
	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			return errors.Wrap(err, "accept connection from server")
		}

		forward, err := net.Dial("tcp", forwardAddr)
		if err != nil {
			_ = remote.Close()
			logger.Error("Failed to dial local forward", "error", err)
			continue
		}
		logger.Debug("Forwarding connection", "remote", remote.RemoteAddr(), "protocol", protocol)

		go func(remote, forward net.Conn) {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				logger.Debug("Forwarding connection closed", "remote", remote.RemoteAddr(), "protocol", protocol)
			}()

			ctx, done := context.WithCancel(context.Background())
			go func() {
				_, _ = io.Copy(forward, remote)
				done()
			}()
			go func() {
				_, _ = io.Copy(remote, forward)
				done()
			}()
			<-ctx.Done()
		}(remote, forward)
	}
}
