package main

import (
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
	"unknwon.dev/x/anyx"
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

	defaultForwardAddr := anyx.Coalesce(
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
	forwardHandler, err := dynamicforward.New(logger, defaultForwardAddr, dynamicForwards...)
	if err != nil {
		logger.FatalContext(ctx, "Failed to create forward handler", "error", err)
	}

	s := httptest.NewServer(forwardHandler)
	logger.Debug("Capture server is running on", "url", s.URL)

	surl, _ := url.Parse(s.URL)
	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		err := tryConnect(
			logger,
			protocolHTTP,
			anyx.Coalesce(cmd.String("remote-addr"), config.RemoteAddr),
			surl.Host,
			anyx.Coalesce(cmd.String("token"), config.Token),
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

func tryConnect(logger *logx.Logger, protocol, remoteAddr, forwardAddr, token string) error {
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
	payload, err := json.Marshal(map[string]string{"protocol": protocol})
	if err != nil {
		return errors.Wrap(err, "marshal payload")
	}
	_, _, err = client.SendRequest("hint", true, payload)
	if err != nil {
		return errors.Wrap(err, "hint server")
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
	ok, reply, err := client.SendRequest("server-info", true, payload)
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
