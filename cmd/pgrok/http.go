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

	"github.com/charmbracelet/log"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"

	"github.com/pgrok/pgrok/internal/dynamicforward"
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
				Action: func(c *cli.Context, s string) error {
					return c.Set("forward-addr", deriveHTTPForwardAddress(s))
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

func actionHTTP(c *cli.Context) error {
	configPath := c.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", configPath,
			"error", err.Error(),
		)
	}
	log.Debug("Loaded config", "file", configPath)

	defaultForwardAddr := strutil.Coalesce(
		deriveHTTPForwardAddress(c.Args().First()),
		c.String("forward-addr"),
		config.ForwardAddr,
	)
	log.Info("Default forward", "address", defaultForwardAddr)

	dynamicForwardRules := strings.Split(config.DynamicForwards, "\n")
	dynamicForwards := make([]dynamicforward.Forward, 0, len(dynamicForwardRules))
	for _, rule := range dynamicForwardRules {
		if rule == "" {
			continue
		}

		fields := strings.Fields(rule)
		if len(fields) != 2 {
			log.Debug("Skipped invalid dynamic forward rule", "rule", rule)
			continue
		}

		dynamicForwards = append(dynamicForwards,
			dynamicforward.Forward{
				Prefix:  fields[0],
				Address: fields[1],
			},
		)
		log.Debug("Added dynamic forward rule", "pathPrefix", fields[0], "forwardTo", fields[1])
	}
	forwardHandler, err := dynamicforward.New(log.Default(), defaultForwardAddr, dynamicForwards...)
	if err != nil {
		log.Fatal("Failed to create forward handler", "error", err.Error())
	}

	s := httptest.NewServer(forwardHandler)
	log.Debug("Capture server is running on", "url", s.URL)

	surl, _ := url.Parse(s.URL)
	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		err := tryConnect(
			protocolHTTP,
			strutil.Coalesce(c.String("remote-addr"), config.RemoteAddr),
			surl.Host,
			strutil.Coalesce(c.String("token"), config.Token),
		)
		if err != nil {
			if time.Now().After(cooldownAfter) {
				failed = 0
			}
			backoff := time.Duration(2<<(failed/3+1)) * time.Second
			log.Error(
				fmt.Sprintf("Failed to connect to server, will reconnect in %s", backoff.String()),
				"error", err.Error(),
			)
			if strings.Contains(err.Error(), "no supported methods remain") {
				log.Fatal("Please double check your token and try again")
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

func tryConnect(protocol, remoteAddr, forwardAddr, token string) error {
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
		return errors.Wrap(err, "open port on remote connection")
	}
	defer func() { _ = remoteListener.Close() }()

	payload, err := json.Marshal(map[string]string{"protocol": protocol})
	if err != nil {
		return errors.Wrap(err, "marshal server info payload")
	}

	var serverInfo struct {
		HostURL string `json:"host_url"`
	}

	ok, reply, err := client.SendRequest("server-info", true, payload)
	if err != nil {
		return errors.Wrap(err, "query server info")
	} else if ok {
		err = json.Unmarshal(reply, &serverInfo)
		if err != nil {
			return errors.Wrap(err, "unmarshal server info")
		}
	}

	message := "🎉 You're ready to go live!"
	if serverInfo.HostURL != "" {
		message = fmt.Sprintf("🎉 You're ready to go live at %s!", serverInfo.HostURL)
	}
	log.Info(message, "remote", remoteAddr)
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
		log.Debug("Forwarding connection", "remote", remote.RemoteAddr(), "protocol", protocol)

		go func(remote, forward net.Conn) {
			defer func() {
				_ = remote.Close()
				_ = forward.Close()
				log.Debug("Forwarding connection closed", "remote", remote.RemoteAddr(), "protocol", protocol)
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
