package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"github.com/pgrok/pgrok/internal/strutil"
)

func commandHTTP(homeDir string) *cli.Command {
	return &cli.Command{
		Name:        "http",
		Description: "Start a HTTP proxy to local endpoints",
		Action:      actionHTTP,
		Flags: append(
			commonFlags(homeDir),
			&cli.StringFlag{
				Name:    "remote-addr",
				Usage:   "The address of the remote SSH server",
				Aliases: []string{"r"},
			},
			&cli.StringFlag{
				Name:    "forward-addr",
				Usage:   "The address to forward requests to",
				Aliases: []string{"f"},
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "The authentication token",
				Aliases: []string{"t"},
			},
		),
	}
}

func actionHTTP(c *cli.Context) error {
	configPath := c.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", configPath,
			"error", err.Error(),
		)
	}

	f := flamego.New()
	f.Use(func(c flamego.Context) {
		started := time.Now()
		c.Next()
		log.Info("Forwarded request",
			"path", c.Request().URL.Path,
			"status", c.ResponseWriter().Status(),
			"duration", time.Since(started),
		)
	})
	rules := strings.Split(config.DynamicForwards, "\n")
	for _, rule := range rules {
		if rule == "" {
			continue
		}

		fields := strings.Fields(rule)
		if len(fields) != 2 {
			log.Debug("Skipped invalid dynamic forward rule", "rule", rule)
			continue
		}
		routePath := fmt.Sprintf("/{*: %s.+/}/{**}", fields[0])
		forward, err := url.Parse(fields[1])
		if err != nil {
			log.Fatal("Failed to parse the forward address",
				"rule", rule,
				"error", err.Error(),
			)
		}
		f.Any(routePath, httputil.NewSingleHostReverseProxy(forward).ServeHTTP)
		log.Debug("Dynamic forward rule added", "path", fields[0], "forwardTo", forward.String())
	}

	forwardAddr := strutil.Coalesce(
		ensureForwardURL(c.Args().First()),
		ensureForwardURL(c.String("forward-addr")),
		config.ForwardAddr,
	)
	defaultForward, err := url.Parse(forwardAddr)
	if err != nil {
		log.Fatal("Failed to parse default forward address", "error", err.Error())
	}
	f.Any("/{**}", httputil.NewSingleHostReverseProxy(defaultForward).ServeHTTP)
	log.Info("Default forward", "address", forwardAddr)

	s := httptest.NewServer(f)
	log.Debug("Capture server is running on", "url", s.URL)

	surl, _ := url.Parse(s.URL)
	var backoff time.Duration
	for failed := 0; ; failed++ {
		err := tryConnect(
			strutil.Coalesce(c.String("remote-addr"), config.RemoteAddr),
			surl.Host,
			strutil.Coalesce(c.String("token"), config.Token),
		)
		if err != nil {
			backoff = 2 << (failed/3 + 1) * time.Second
			log.Error(
				fmt.Sprintf("Failed to connect to server, will reconnect in %s. Press enter to reconnect now.", backoff.String()),
				"error", err.Error(),
			)
			if strings.Contains(err.Error(), "no supported methods remain") {
				log.Fatal("Please double check your token and try again")
			}
		} else {
			failed = 0
		}
		time.Sleep(backoff)
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

func tryConnect(remoteAddr, forwardAddr, token string) error {
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

	remoteListener, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal("Failed to open port on remote connection", "error", err)
	}
	defer func() { _ = remoteListener.Close() }()
	log.Info("Tunneling connection established", "remote", remoteAddr)

	for {
		remote, err := remoteListener.Accept()
		if err != nil {
			return errors.Wrap(err, "accept connection from server")
		}

		forward, err := net.Dial("tcp", forwardAddr)
		if err != nil {
			_ = remote.Close()
			log.Error("Failed to dial local forward", "error", err)
			continue
		}
		log.Debug("Forwarding connection", "remote", remote.RemoteAddr())

		go func() {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				log.Debug("Forwarding connection closed", "remote", remote.RemoteAddr())
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
		}()
	}
}
