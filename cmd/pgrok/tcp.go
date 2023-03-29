package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"

	"github.com/pgrok/pgrok/internal/strutil"
)

func commandTCP(homeDir string) *cli.Command {
	return &cli.Command{
		Name:        "tcp",
		Description: "Start a TCP proxy to a local address",
		Action:      actionTCP,
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
					return c.Set("forward-addr", deriveTCPForwardAddress(s))
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

// deriveTCPForwardAddress tries to be smart about deriving the full TCP address
// from incomplete forward host and port information.
func deriveTCPForwardAddress(addr string) string {
	if addr == "" {
		return ""
	}

	// Check if it's just a port number
	port, err := strconv.Atoi(addr)
	if err == nil {
		return fmt.Sprintf("localhost:%d", port)
	}

	// Check if it's omitting the hostname
	port, err = strconv.Atoi(addr[1:])
	if err == nil {
		return fmt.Sprintf("localhost:%d", port)
	}
	return addr
}

func actionTCP(c *cli.Context) error {
	configPath := c.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatal("Failed to load config",
			"config", configPath,
			"error", err.Error(),
		)
	}
	log.Debug("Loaded config", "file", configPath)

	forwardAddr := strutil.Coalesce(
		deriveTCPForwardAddress(c.Args().First()),
		c.String("forward-addr"),
		config.ForwardAddr,
	)
	log.Info("Forward", "address", forwardAddr)

	cooldownAfter := time.Now().Add(time.Minute)
	for failed := 0; ; failed++ {
		err := tryConnect(
			protocolTCP,
			strutil.Coalesce(c.String("remote-addr"), config.RemoteAddr),
			forwardAddr,
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
