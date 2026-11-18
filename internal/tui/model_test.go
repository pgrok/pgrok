package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

func TestLogSelectionSurvivesNewLines(t *testing.T) {
	logs := &Logs{}
	_, _ = logs.Write([]byte("2026-11-18 17:50:58 INFO first line\n2026-11-18 17:50:58 INFO second line\n"))
	hub := NewHub(Info{Protocol: "http", ForwardAddr: "http://localhost:3001"})
	hub.SetOnline("https://unknwon.pgrok.dev")
	m := New([]*Hub{hub}, logs)
	m.w, m.h = 120, 36
	m.tab = tabLogs

	// Drag across the log, as the terminal reports it: each event goes to
	// the view's mouse handler, then to Update.
	mouse := func(msg tea.MouseMsg) {
		if v := m.View(); v.OnMouse != nil {
			v.OnMouse(msg)
		}
		m.Update(msg)
	}
	m.View()
	r, ok := m.ui.Frame().Rect(idLogs)
	require.True(t, ok)
	mouse(tea.MouseClickMsg{X: r.Min.X, Y: r.Min.Y, Button: tea.MouseLeft})
	mouse(tea.MouseMotionMsg{X: r.Min.X + 30, Y: r.Min.Y + 1, Button: tea.MouseLeft})
	mouse(tea.MouseReleaseMsg{X: r.Min.X + 30, Y: r.Min.Y + 1, Button: tea.MouseLeft})
	want, ok := m.ui.Selection()
	require.True(t, ok)
	require.Contains(t, want, "first line")

	// Traffic keeps logging while the user goes to copy what they selected.
	_, _ = logs.Write([]byte("2026-11-18 17:50:59 INFO third line\n"))
	m.Update(tickMsg(time.Now().Add(time.Second)))
	m.View()
	got, ok := m.ui.Selection()
	require.True(t, ok, "the selection was lost when a line was logged")
	require.Equal(t, want, got)
}
