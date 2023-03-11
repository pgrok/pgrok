package main

import (
	"os"
	"time"

	"github.com/charmbracelet/log"
	"github.com/urfave/cli/v2"
)

var version = "0.0.0+dev"

var commonFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "config",
		Usage:   "The path to the config file",
		Value:   "pgrok.yml",
		Aliases: []string{"c"},
	},
	&cli.BoolFlag{
		Name:    "debug",
		Usage:   "Whether to enable debug mode",
		Aliases: []string{"d"},
		Action: func(c *cli.Context, b bool) error {
			if b {
				log.SetLevel(log.DebugLevel)
			}
			return nil
		},
	},
}

func main() {
	log.SetTimeFormat(time.DateTime)

	app := cli.NewApp()
	app.Name = "pgrok"
	app.Usage = "Poor man's ngrok"
	app.Version = version
	app.DefaultCommand = "http"
	app.Commands = []*cli.Command{
		commandHTTP,
		commandInit,
	}
	app.Flags = commonFlags
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
