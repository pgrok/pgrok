package main

import (
	"io"
	"os"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/mdp/qrterminal/v3"
	"golang.org/x/term"
	"unknwon.dev/x/logx"
)

// newReadyHook returns the callback for tryConnect to invoke whenever the
// tunnel is up, which prints the QR code of the URL when stdout is a terminal.
func newReadyHook(logger *logx.Logger) func(hostURL string) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		logger.Debug("Skipped printing QR code because stdout is not a terminal")
		return func(string) {}
	}
	// The QR code is colored with ANSI sequences, which the legacy Windows
	// console only renders once enabled. This is a no-op on other platforms.
	lipgloss.EnableLegacyWindowsANSI(os.Stdout)
	return onNewURL(os.Stdout)
}

// qrMu serializes printing QR codes, because the QR code is written line by
// line and the tunnels of the "start" subcommand become ready concurrently.
var qrMu sync.Mutex

// onNewURL returns a callback that prints the QR code of a non-empty host URL
// only when it differs from the last printed one, so reconnects that keep the
// same URL do not print it again.
func onNewURL(w io.Writer) func(hostURL string) {
	var last string
	return func(hostURL string) {
		if hostURL == "" || hostURL == last {
			return
		}
		last = hostURL
		// Force white on black so the QR code scans the same on light and dark
		// terminal themes.
		colored := func(s string) string { return "\x1b[97;40m" + s + "\x1b[0m" }
		qrMu.Lock()
		defer qrMu.Unlock()
		qrterminal.GenerateWithConfig(hostURL, qrterminal.Config{
			Level:          qrterminal.L,
			Writer:         w,
			HalfBlocks:     true,
			BlackChar:      colored(qrterminal.BLACK_BLACK),
			BlackWhiteChar: colored(qrterminal.BLACK_WHITE),
			WhiteChar:      colored(qrterminal.WHITE_WHITE),
			WhiteBlackChar: colored(qrterminal.WHITE_BLACK),
			QuietZone:      2,
		})
	}
}
