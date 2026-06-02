package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	charmlog "charm.land/log/v2"
	"github.com/adrg/xdg"
	"github.com/urfave/cli/v3"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/osutil"
)

var version = "0.0.0+dev"

func commonFlags(homeDir string, logger *logx.Logger) []cli.Flag {
	configPath := filepath.Join(homeDir, ".pgrok", "pgrok.yml")
	if !osutil.IsExist(configPath) {
		xdgConfigPath, err := xdg.ConfigFile(filepath.Join("pgrok", "pgrok.yml"))
		if err == nil {
			configPath = xdgConfigPath
		}
	}

	return []cli.Flag{
		&cli.StringFlag{
			Name:    "config",
			Usage:   "The path to the config file",
			Value:   configPath,
			Aliases: []string{"c"},
		},
		&cli.BoolFlag{
			Name:    "debug",
			Usage:   "Whether to enable debug mode",
			Aliases: []string{"d"},
			Action: func(_ context.Context, _ *cli.Command, b bool) error {
				if b {
					if h, ok := logger.Handler().(*charmlog.Logger); ok {
						h.SetLevel(charmlog.DebugLevel)
					}
				}
				return nil
			},
		},
	}
}

func main() {
	logger := logx.New(
		charmlog.NewWithOptions(
			os.Stderr,
			charmlog.Options{
				TimeFormat:      time.DateTime,
				ReportTimestamp: true,
			},
		),
	)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.FatalContext(context.Background(), "Failed to get home directory", "error", err)
	}

	app := &cli.Command{
		Name:           "pgrok",
		Usage:          "Poor man's ngrok",
		Version:        version,
		DefaultCommand: "http",
		Commands: []*cli.Command{
			commandInit(homeDir, logger),
			commandHTTP(homeDir, logger),
			commandTCP(homeDir, logger),
		},
		Flags: commonFlags(homeDir, logger),
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		logger.FatalContext(context.Background(), "Failed to run pgrok", "error", err)
	}
}
