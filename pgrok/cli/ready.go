package main

import (
	"io"
	"os"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/mdp/qrterminal/v3"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
	"unknwon.dev/x/logx"
)

// newReadyHook returns the callback for tryConnect to invoke whenever the
// tunnel is up, which prints the QR code and/or copies the URL to the clipboard
// as requested by the "--qr" and "--copy" flags (or the config file).
func newReadyHook(cmd *cli.Command, config *Config, logger *logx.Logger) func(hostURL string) {
	printQR := config.QR
	if cmd.IsSet("qr") {
		printQR = cmd.Bool("qr")
	}
	if printQR && !term.IsTerminal(int(os.Stdout.Fd())) {
		logger.Debug("Skipped printing QR code because stdout is not a terminal")
		printQR = false
	}

	copyURL := config.Copy
	if cmd.IsSet("copy") {
		copyURL = cmd.Bool("copy")
	}
	return onFirstReady(logger, os.Stdout, printQR, copyURL)
}

// onFirstReady returns a callback that only acts on the first call with a
// non-empty host URL, so reconnects do not print the QR code or overwrite the
// clipboard again.
func onFirstReady(logger *logx.Logger, w io.Writer, printQR, copyURL bool) func(hostURL string) {
	var once sync.Once
	return func(hostURL string) {
		if hostURL == "" {
			return
		}
		once.Do(func() {
			if printQR {
				qrterminal.GenerateWithConfig(hostURL, qrterminal.Config{
					Level:      qrterminal.L,
					Writer:     w,
					HalfBlocks: true,
					QuietZone:  2,
				})
			}
			if copyURL {
				if err := clipboard.WriteAll(hostURL); err != nil {
					logger.Warn("Failed to copy URL to clipboard", "error", err)
				} else {
					logger.Info("📋 Copied URL to clipboard", "url", hostURL)
				}
			}
		})
	}
}
