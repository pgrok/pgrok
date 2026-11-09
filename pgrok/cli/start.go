package main

import (
	"cmp"
	"context"
	"slices"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"
	"unknwon.dev/x/logx"
)

func commandStart(homeDir string, logger *logx.Logger) *cli.Command {
	return &cli.Command{
		Name:      "start",
		Usage:     "Start tunnels defined in the config file by name",
		ArgsUsage: "<name>...",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return actionStart(ctx, cmd, logger)
		},
		Flags: append(
			commonFlags(homeDir, logger),
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Start all tunnels defined in the config file",
			},
			&cli.StringFlag{
				Name:    "remote-addr",
				Usage:   "The address of the remote SSH server",
				Aliases: []string{"r"},
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "The authentication token",
				Aliases: []string{"t"},
			},
		),
	}
}

func actionStart(ctx context.Context, cmd *cli.Command, logger *logx.Logger) error {
	configPath := cmd.String("config")
	config, err := loadConfig(configPath)
	if err != nil {
		logger.FatalContext(ctx, "Failed to load config",
			"config", configPath,
			"error", err,
		)
	}
	logger.Debug("Loaded config", "file", configPath)

	var names []string
	if cmd.Bool("all") {
		if cmd.Args().Present() {
			return errors.New("tunnel names cannot be used together with --all")
		}
		for name := range config.Tunnels {
			names = append(names, name)
		}
		if len(names) == 0 {
			return errors.Errorf("no tunnels defined in %q", configPath)
		}
		slices.Sort(names)
	} else {
		for _, name := range cmd.Args().Slice() {
			if _, ok := config.Tunnels[name]; !ok {
				return errors.Errorf("tunnel %q is not defined in %q", name, configPath)
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return errors.New("specify the names of tunnels to start, or use --all")
		}
	}

	type tunnelError struct {
		name string
		err  error
	}
	errs := make(chan tunnelError)
	for _, name := range names {
		c := config.Tunnels[name]
		t := tunnel{
			protocol:   c.Protocol,
			remoteAddr: cmp.Or(cmd.String("remote-addr"), config.RemoteAddr),
			token:      cmp.Or(cmd.String("token"), config.Token),
		}
		if c.Protocol == protocolHTTP {
			t.forwardAddr = deriveHTTPForwardAddress(c.ForwardAddr)
			t.subdomain = c.Subdomain
			t.dynamicForwards = c.DynamicForwards
		} else {
			t.forwardAddr = deriveTCPForwardAddress(c.ForwardAddr)
		}
		go func() {
			// Each tunnel reconnects on its own, so runTunnel only returns on
			// unrecoverable errors, e.g. the token is rejected.
			errs <- tunnelError{name: name, err: runTunnel(logger.Scoped(name), t)}
		}()
	}

	// Stop the whole command on the first fatal error.
	e := <-errs
	fatalTunnelError(ctx, logger.Scoped(e.name), e.err)
	return nil
}
