package main

import (
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	charmlog "charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/tui"
)

var tuiFlag = &cli.BoolFlag{
	Name:  "tui",
	Usage: "Whether to show the full-screen terminal UI when running in a terminal",
	Value: true,
}

// tunnelInfo describes t to the terminal UI. name is empty for the tunnel of
// "pgrok http" and "pgrok tcp".
func tunnelInfo(name, configPath string, t tunnel) tui.Info {
	return tui.Info{
		Name:        name,
		Version:     "v" + version,
		Protocol:    t.protocol,
		RemoteAddr:  t.remoteAddr,
		ForwardAddr: t.forwardAddr,
		Subdomain:   t.subdomain,
		ConfigPath:  configPath,
	}
}

// runWithTUI calls run with one hub per tunnel in infos, showing the terminal
// UI while it runs when the "tui" flag is set and stdin and stdout are
// terminals. It passes run nil hubs otherwise, and the client logs as usual.
//
// While the UI is on screen, log lines go to its Logs view instead of stderr.
// Quitting the UI returns nil without waiting for run.
func runWithTUI(cmd *cli.Command, logger *logx.Logger, infos []tui.Info, run func(hubs []*tui.Hub) error) error {
	hubs := make([]*tui.Hub, len(infos))
	handler, ok := logger.Handler().(*charmlog.Logger)
	if !ok || !cmd.Bool("tui") || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return run(hubs)
	}

	for i, info := range infos {
		hubs[i] = tui.NewHub(info)
	}
	logs := &tui.Logs{}
	handler.SetOutput(logs)
	handler.SetColorProfile(colorprofile.TrueColor) // the UI downsamples to the terminal's profile
	// The reverse proxy reports errors through the standard logger.
	log.SetOutput(handler.StandardLog(charmlog.StandardLogOptions{ForceLevel: charmlog.ErrorLevel}).Writer())
	log.SetFlags(0)
	defer func() {
		handler.SetOutput(os.Stderr)
		handler.SetColorProfile(colorprofile.Detect(os.Stderr, os.Environ()))
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	}()

	p := tea.NewProgram(tui.New(hubs, logs))
	runErr := make(chan error, 1)
	go func() {
		runErr <- run(hubs)
		p.Quit()
	}()
	if _, err := p.Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return errors.Wrap(err, "run terminal UI")
	}
	select {
	case err := <-runErr:
		return err
	default:
		return nil // the user quit
	}
}
