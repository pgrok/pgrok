package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestViewFlow(t *testing.T) {
	newModel := func(reqs ...*Request) Model {
		hub := NewHub(Info{
			Protocol:    "http",
			ForwardAddr: "http://localhost:3001",
			Routes:      [][2]string{{"/api", "http://localhost:8080"}},
		})
		for _, r := range reqs {
			hub.addRequest(r)
		}
		m := New([]*Hub{hub}, &Logs{})
		m.w, m.h = 120, 36
		m.fnow = m.now
		return m
	}
	render := func(m Model) string { return ansi.Strip(m.viewFlow(m.h - 3)) }

	t.Run("requests in flight and responses on their way back", func(t *testing.T) {
		now := time.Now()
		m := newModel(
			// Reached the local address and still waiting for its answer.
			&Request{Method: "GET", URI: "/api/slow", Route: "/api", Start: now.Add(-flowOut - 500*time.Millisecond)},
			// The local address was unreachable; the 502 is on its way back.
			&Request{Method: "POST", URI: "/legacy", Start: now.Add(-flowOut - 300*time.Millisecond),
				Status: 502, TTFB: time.Millisecond, Duration: time.Millisecond, Done: true,
				Trace: Trace{ConnectErr: "connection refused"}},
		)
		out := render(m)
		require.Contains(t, out, "1 active")
		// One ✕ in the legend, and one for the 502 on the map.
		require.Equal(t, 2, strings.Count(out, "✕"), out)
		require.True(t, m.flowBusy(m.fnow))
	})

	t.Run("idle once every response is back", func(t *testing.T) {
		now := time.Now()
		m := newModel(&Request{Method: "GET", URI: "/", Start: now.Add(-10 * time.Second),
			Status: 200, Duration: time.Millisecond, Done: true})
		out := render(m)
		require.Contains(t, out, "waiting for traffic")
		require.Contains(t, out, "idle")
		require.False(t, m.flowBusy(m.fnow), "frames should stop while nothing moves")
	})
}
