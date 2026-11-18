package tui

import (
	"fmt"
	"math"
	"time"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/fig"
	"bloomfx.dev/bloom/fig/fonts"
	"bloomfx.dev/bloom/flex"
	"bloomfx.dev/bloom/fx"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"rsc.io/qr"
)

// layout is the whole screen: the header with its tabs, the active tab, the
// key hints, and the dialogs and toasts over them.
func (m *Model) layout() bloom.Child {
	var base bloom.Child
	if m.connecting() {
		base = m.splash()
	} else {
		body := m.content()
		if m.enter.Animating() {
			body = bloom.Transition(m.enter.Float(), body, fx.Fade{})
		}
		base = bloom.Column(
			bloom.Fixed(1, m.header()),
			bloom.Fixed(1, bloom.Drawable(rule{x: m.ulX.Float(), w: m.ulW.Float()})),
			bloom.Fill(body),
			bloom.Fixed(1, m.footer()),
		)
	}
	return bloom.MinSize(40, 12, bloom.Overlay(base, m.toastLayer(), m.helpLayer(), m.shareLayer()))
}

func (m *Model) content() bloom.Child {
	switch {
	case m.detail:
		return m.viewDetail()
	case m.tab == tabRequests:
		return m.viewRequests()
	case m.tab == tabLogs:
		return m.viewLogs()
	case m.tab == tabTunnels:
		return m.viewTunnels()
	case m.tab == tabFlow:
		return m.viewFlow()
	}
	return m.viewOverview()
}

// ---------- header and footer ----------

func wordmark() string {
	return bold(cPeach).Render("▌") + bold(cLav).Render("pgrok")
}

func (m *Model) spinGlyph() string {
	frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	return string(frames[int(m.spin.Float()*float64(len(frames)))%len(frames)])
}

func (m *Model) statusText(s Snapshot) string {
	switch s.Status {
	case StatusOnline:
		return fg(cGreen).Render("●") + bold(cGreen).Render(" online")
	case StatusReconnecting:
		left := max(0, int(math.Ceil(s.RetryAt.Sub(m.now).Seconds())))
		if s.RetryAt.IsZero() || left == 0 {
			return bold(cYellow).Render(m.spinGlyph() + " reconnecting…")
		}
		return bold(cYellow).Render(m.spinGlyph()+" reconnecting") + muted(fmt.Sprintf(" in %ds", left))
	}
	return bold(cYellow).Render(m.spinGlyph() + " connecting…")
}

// statusNode is the status of a tunnel, with what went wrong as its
// tooltip. It glitches for a moment when the status changes.
func (m *Model) statusNode(s Snapshot) bloom.Child {
	tip := "connected to " + s.Info.RemoteAddr
	if s.Status != StatusOnline && s.LastErr != "" {
		tip = sanitize(s.LastErr)
	}
	node := bloom.Child(bloom.Text(m.statusText(s)).Tooltip(tip, bloom.TipSide(bloom.Bottom)))
	if m.glitch.Animating() {
		node = bloom.Transition(m.glitch.Float(), node, fx.Glitch{Seed: uint32(m.snap.Reconnects)})
	}
	return node
}

func (m *Model) header() bloom.Child {
	s := m.snap
	left := []bloom.Child{keep(" " + wordmark() + " " + muted(s.Info.Version) + "   ")}
	if m.multi() && (m.tab == tabTunnels || m.tab == tabFlow) && !m.detail {
		online := 0
		for _, t := range m.snaps {
			if t.Status == StatusOnline {
				online++
			}
		}
		c := cGreen
		if online < len(m.snaps) {
			c = cYellow
		}
		left = append(left, keep(bold(c).Render(fmt.Sprintf("● %d/%d online", online, len(m.snaps)))))
	} else {
		if m.multi() {
			left = append(left, keep(lav("▸ ")+bold(cBright).Render(s.Info.Name)+"   "))
		}
		left = append(left, bloom.Auto(m.statusNode(s)).Min(lipgloss.Width(m.statusText(s))))
		if s.Status == StatusOnline {
			// Uptime and round trip give way first when the header is
			// crowded.
			up := "   " + muted("up ") + text(fmtUptime(m.now.Sub(s.OnlineSince)))
			left = append(left, bloom.Auto(bloom.Text(up)).Min(lipgloss.Width(up)).Collapsible(2))
			if s.RTT > 0 {
				rtt := "   " + muted("rtt ") + text(fmtDur(s.RTT))
				left = append(left, bloom.Auto(bloom.Text(rtt).Tooltip("round trip to pgrokd over SSH", bloom.TipSide(bloom.Bottom))).
					Min(lipgloss.Width(rtt)).Collapsible(1))
			}
		}
	}
	items := append(left, bloom.Space())
	active := m.activeTab()
	for i, n := range m.tabs() {
		st := fg(cMuted)
		if n == active {
			st = bold(cLav)
		}
		items = append(items, bloom.Auto(bloom.Text(" "+n+" ").Style(st).HoverStyle(bold(cLavLight)).
			Tooltip(fmt.Sprintf("press %d", i+1), bloom.TipSide(bloom.Bottom), bloom.TipDelay(800*time.Millisecond)).
			ID(tabID(n)).
			On(on.Click(func() tea.Cmd { return m.setTab(n) }))).Min(len(n)+2))
	}
	items = append(items, bloom.Fixed(1, nil))
	return bloom.Row(items...).Gap(0)
}

// hint is a key hint in the footer. A click on it presses key, when set.
type hint struct {
	label, desc, key string
	id               nodeID
}

func (m *Model) hints() []hint {
	h := func(label, desc string) hint { return hint{label: label, desc: desc, key: label} }
	show := func(label, desc string) hint { return hint{label: label, desc: desc} }
	help := hint{label: "?", desc: "help", key: "?", id: idHelpKey}
	share := hint{label: "s", desc: "share", key: "s", id: idShareKey}
	quit := h("q", "quit")
	var hs []hint
	switch {
	case m.detail:
		hs = []hint{show("←→", "request · response"), show("↑↓", "scroll"), h("r", "replay"), h("c", "copy as curl"), h("esc", "back")}
	case m.filtering():
		hs = []hint{show("enter", "apply"), show("esc", "clear"), show("↑↓", "select")}
	case m.tab == tabTunnels:
		hs = []hint{show("↑↓", "select"), h("enter", "show tunnel"), h("tab", "views"), help, quit}
	case m.tab == tabFlow:
		hs = []hint{h("tab", "views"), show("click", "a node to drill in"), share, help, quit}
	case m.tab == tabLogs:
		hs = []hint{show("d i w e", "levels"), h("f", "follow"), show("↑↓", "scroll"), show("drag", "select"), help, quit}
	case m.snap.Info.Protocol == "tcp":
		hs = []hint{h("tab", "views"), share, help, quit}
	case m.tab == tabRequests:
		hs = []hint{show("↑↓", "select"), h("enter", "inspect"), h("r", "replay"), h("c", "copy as curl"), h("/", "filter"), h("esc", "clear"), help, quit}
	default:
		hs = []hint{h("tab", "views"), show("↑↓", "select"), h("enter", "inspect"), h("r", "replay"), h("/", "filter"), share, help, quit}
	}
	if m.anyReconnecting() && !m.detail && !m.filtering() {
		hs = append([]hint{h("R", "retry now")}, hs...)
	}
	return hs
}

func (m *Model) footer() bloom.Child {
	hs := m.hints()
	items := []bloom.Child{bloom.Fixed(1, nil)}
	for i, h := range hs {
		s := bold(cLav).Render(h.label) + " " + muted(h.desc)
		if i > 0 {
			s = fg(cBorder).Render("  ·  ") + s
		}
		var node bloom.Child = bloom.Text(s)
		if h.key != "" {
			node = bloom.Box(lipgloss.NewStyle(), bloom.Text(s)).HoverStyle(selStyle).
				ID(h.id).
				On(on.Click(func() tea.Cmd { return m.press(h.key) }))
		}
		// Hints drop from the end as the terminal narrows, keeping help.
		prio := 10 + len(hs) - i
		if h.key == "?" {
			prio = 100
		}
		items = append(items, bloom.Auto(node).Min(lipgloss.Width(s)).Collapsible(prio))
	}
	addr := muted(m.snap.Info.RemoteAddr) + " "
	items = append(items, bloom.Space(), bloom.Auto(bloom.Text(addr)).Min(lipgloss.Width(addr)).Collapsible(1))
	return bloom.Row(items...)
}

// ---------- toasts and dialogs ----------

func (m *Model) toastLayer() bloom.LayerNode {
	stack := make([]bloom.Child, len(m.toasts))
	for i, t := range m.toasts {
		icon := "✓ "
		if t.color != cGreen {
			icon = "● "
		}
		st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(t.color)).
			Background(lipgloss.Color(cRaised)).BorderBackground(lipgloss.Color(cRaised)).Padding(0, 1)
		box := bloom.Box(st, bloom.Text(fg(t.color).Render(icon)+fg(cBright).Render(sanitize(t.text))).Truncate())
		stack[i] = bloom.Present(t.presence, box, fx.Slide{From: bloom.Right}, fx.Fade{})
	}
	return bloom.Layer(bloom.Column(stack...).Align(bloom.End)).
		Anchor(bloom.End, bloom.End).
		Margin(2)
}

// dialog is the frame of a dialog, with a title and right on its top border,
// and the hints that close it on its bottom border.
func dialog(titleS, right string, body bloom.Child, keys ...bloom.Child) bloom.BoxNode {
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cLav)).
		Background(lipgloss.Color(cBg)).BorderBackground(lipgloss.Color(cBg)).Padding(1, 2)
	b := panelIn(st, titleS, right, body)
	if len(keys) > 0 {
		b = b.Edge(bloom.Bottom, bloom.End, bloom.Row(keys...).Gap(1))
	}
	return b
}

// keyButton is a key hint on a dialog's border that presses its key.
func (m *Model) keyButton(key, desc string) bloom.Child {
	s := " " + bold(cLav).Render(key) + " " + muted(desc) + " "
	return bloom.Box(lipgloss.NewStyle(), bloom.Text(s)).HoverStyle(selStyle).
		On(on.Click(func() tea.Cmd { return m.press(key) }))
}

func (m *Model) modal(l bloom.LayerNode) bloom.LayerNode {
	return l.Modal().
		On(on.ClickOutside(func() tea.Cmd { return m.press("esc") })).
		Backdrop(fx.Dim{Amount: 0.6}, fx.Desaturate{Amount: 0.5}).
		Shadow(2, 1, fx.Dim{Amount: 0.5})
}

func (m *Model) helpLayer() bloom.LayerNode {
	sec := func(s string) bloom.Child { return bloom.Text(bold(cPeach).Render(s)) }
	row := func(k, d string) bloom.Child {
		return bloom.Row(bloom.Fixed(13, bloom.Text(bold(cLav).Render(k))), bloom.Fill(bloom.Text(text(d)).Truncate()))
	}
	left := bloom.Column(
		sec("Navigation"),
		row("tab ⇧tab", "next / previous view"),
		row("1 – 5", "jump to view"),
		row("↑ ↓  k j", "move selection"),
		row("enter", "inspect request · show tunnel"),
		row("esc", "back · close · clear"),
		bloom.Fixed(1, nil),
		sec("Requests"),
		row("/", "filter requests"),
		row("← →", "request · response"),
		row("r", "replay to the local address"),
		row("c", "copy as curl"),
	)
	right := bloom.Column(
		sec("Global"),
		row("s", "share URL + QR code"),
		row("R", "reconnect now"),
		row("?", "toggle this help"),
		row("q ctrl+c", "quit"),
		bloom.Fixed(1, nil),
		sec("Mouse"),
		row("click", "tabs, rows, keys, nodes"),
		row("double click", "inspect a request"),
		row("wheel", "scroll lists and bodies"),
		row("drag · c", "select text · copy it"),
	)
	syntax := text("Filters  ") + fg(cGreen).Render("status:>=400") + "  " + fg(cGreen).Render("status:5xx") + "  " +
		fg(cGreen).Render("method:POST") + "  " + fg(cGreen).Render("/api/users") + muted("  (terms are ANDed)")
	body := bloom.MinSize(0, 0, bloom.Column(
		bloom.Row(bloom.Fill(left).Max(44), bloom.Fill(right).Max(40)).Gap(2),
		bloom.Fixed(1, nil),
		bloom.Text(syntax),
	))
	box := dialog(title("Keyboard and mouse"), muted("pgrok "+m.snap.Info.Version), body, m.keyButton("esc", "close"))
	return m.modal(bloom.Layer(box)).Presence(m.help, fx.Fade{}, fx.Slide{From: bloom.Bottom})
}

func (m *Model) shareLayer() bloom.LayerNode {
	s := m.snap
	var code bloom.Child = bloom.Text(muted("enlarge the terminal to show the QR code"))
	if c, err := qr.Encode(s.HostURL, qr.L); err == nil && (c.Size+2*qrQuiet+1)/2+12 <= m.h {
		code = bloom.Drawable(qrCode{c})
	}
	right := fg(cGreen).Render("● online")
	if s.Status != StatusOnline {
		right = fg(cYellow).Render("◌ offline")
	}
	body := bloom.Column(
		bloom.Centered(code),
		bloom.Fixed(1, nil),
		bloom.Centered(bloom.Text(bold(cPeach).Underline(true).Render(s.HostURL)).
			Tooltip("click to copy", bloom.TipSide(bloom.Bottom)).
			On(on.Click(func() tea.Cmd { return m.press("c") }))),
		bloom.Centered(bloom.Text(muted("→ "+s.Info.ForwardAddr))),
		bloom.Fixed(1, nil),
		bloom.Centered(bloom.Text(muted("scan to open on your phone"))),
	).Align(bloom.Center)
	box := dialog(title("Share tunnel"), right, body, m.keyButton("c", "copy"), m.keyButton("esc", "close"))
	return m.modal(bloom.Layer(box)).Presence(m.share, fx.Iris{At: m.shareFrom}, fx.Fade{})
}

// ---------- connecting ----------

// logoPaint colors the wordmark like the banner: a peach p, lavender letters
// and a deep lavender shadow, each fading down the letters.
var logoPaint = fig.PaintFunc(func(f *fig.Figure, c fig.Cell) uv.Style {
	_, h := f.Size()
	t := float64(c.Y) / float64(max(1, h-1))
	switch {
	case c.Content != "█" && c.Content != "▀" && c.Content != "▄":
		return uv.Style{Fg: lipgloss.Color("#4A3A70")}
	case c.Glyph == 0:
		return uv.Style{Fg: lipgloss.Color(mixHex("#F8C0AC", "#E8846A", t))}
	}
	return uv.Style{Fg: lipgloss.Color(mixHex(cLavLight, "#9A7CD6", t))}
})

func (m *Model) splash() bloom.Child {
	logo := bloom.Child(fig.Text("pgrok").Font(fonts.LinguaholicShadow3D(), fonts.SmBlock()).Paint(logoPaint).
		Fallback(bloom.Text(wordmark())))
	step := func(icon, label, value string) bloom.Child {
		return bloom.Row(bloom.Fixed(4, nil), bloom.Fixed(3, bloom.Text(icon)), bloom.Fixed(18, bloom.Text(label)), bloom.Fill(line(value)))
	}
	var steps []bloom.Child
	if p := m.snap.Info.ConfigPath; p != "" {
		steps = append(steps, step(fg(cGreen).Render("✓"), text("Loaded config"), lav(tildePath(p))))
	}
	steps = append(steps, step(fg(cPeach).Render(m.spinGlyph()), bold(cBright).Render("Connecting to"), muted(m.snap.Info.RemoteAddr)))
	card := bloom.Box(
		panelStyle.BorderForeground(lipgloss.Color(cLavDeep)).Padding(1, 2),
		bloom.Column(
			bloom.Centered(logo),
			bloom.Fixed(1, nil),
			bloom.Centered(bloom.Text(muted(m.snap.Info.Version+" · self-hosted tunnels to localhost"))),
			bloom.Fixed(1, nil),
			bloom.Column(steps...),
		),
	)
	return bloom.Column(
		bloom.Space(),
		bloom.Row(bloom.Space(), bloom.Fixed(62, card), bloom.Space()).Justify(flex.Center),
		bloom.Fixed(1, nil),
		bloom.Centered(bloom.Text(muted("ctrl+c quit"))),
		bloom.Space(),
	)
}
