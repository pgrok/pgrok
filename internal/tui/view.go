package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"rsc.io/qr"
)

func (m Model) render() string {
	if m.w < 40 || m.h < 12 {
		return normalize(center(muted("enlarge the terminal to show pgrok"), m.w), m.w, m.h)
	}
	if m.connecting() {
		return normalize(m.viewConnecting(), m.w, m.h)
	}

	bodyH := m.h - 3
	draw := func() string {
		var body string
		switch {
		case m.detail:
			body = m.viewDetail(bodyH)
		case m.tab == tabRequests:
			body = m.viewRequests(bodyH)
		case m.tab == tabLogs:
			body = m.viewLogs(bodyH)
		case m.tab == tabTunnels:
			body = m.viewTunnels(bodyH)
		case m.tab == tabFlow:
			body = m.viewFlow(bodyH)
		default:
			body = m.viewOverview(bodyH)
		}
		return m.header() + "\n" + normalize(body, m.w, bodyH) + "\n" + m.footer()
	}
	if m.modal == "" {
		return draw()
	}

	bg := withDim(0.68, draw)
	lines := strings.Split(bg, "\n")
	lines[len(lines)-1] = m.footer() // the footer stays bright for the modal's hints
	bg = strings.Join(lines, "\n")
	modal := m.helpModal()
	if m.modal == "share" {
		modal = m.shareModal()
	}
	return normalize(lipgloss.NewCompositor(
		lipgloss.NewLayer(bg),
		lipgloss.NewLayer(modal).
			X(max(0, (m.w-lipgloss.Width(modal))/2)).
			Y(max(0, (m.h-lipgloss.Height(modal))/2)).
			Z(1),
	).Render(), m.w, m.h)
}

// ---------- formatting ----------

func comma(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func fmtBytes(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f KB", float64(n)/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.1f MB", float64(n)/1000/1000)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1000/1000/1000)
}

// shortBytes formats n in at most four cells, for chart axes.
func shortBytes(n float64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%.0f", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.0fK", n/1000)
	case n < 1000*1000*1000:
		return fmt.Sprintf("%.0fM", n/1000/1000)
	}
	return fmt.Sprintf("%.0fG", n/1000/1000/1000)
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmtUptime(d)
}

func fmtUptime(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh %02dm %02ds", s/3600, s/60%60, s%60)
}

// shortAddr turns http://localhost:8080 into :8080.
func shortAddr(addr string) string {
	if u, err := url.Parse(addr); err == nil && u.Host != "" {
		addr = u.Host
	}
	host, port, err := net.SplitHostPort(addr)
	if err == nil && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return ":" + port
	}
	return addr
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return p
}

// ---------- header and footer ----------

func (m Model) statusText() string { return m.statusOf(m.snap) }

func (m Model) statusOf(s Snapshot) string {
	switch s.Status {
	case StatusOnline:
		return fg(cGreen).Render("●") + bold(cGreen).Render(" online")
	case StatusReconnecting:
		left := max(0, int(math.Ceil(s.RetryAt.Sub(m.now).Seconds())))
		if s.RetryAt.IsZero() || left == 0 {
			return bold(cYellow).Render("◌ reconnecting…")
		}
		return bold(cYellow).Render("◌ reconnecting") + muted(fmt.Sprintf(" in %ds", left))
	}
	return bold(cYellow).Render("◌ connecting…")
}

func (m Model) header() string {
	s := m.snap
	left := " " + wordmark() + " " + muted(s.Info.Version) + "   "
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
		return m.headerLines(left+bold(c).Render(fmt.Sprintf("● %d/%d online", online, len(m.snaps))), m.tab)
	}
	if m.multi() {
		left += lav("▸ ") + bold(cBright).Render(s.Info.Name) + "   "
	}
	left += m.statusText()
	if m.w >= 100 && s.Status == StatusOnline {
		left += "   " + muted("up ") + text(fmtUptime(m.now.Sub(s.OnlineSince)))
		if s.RTT > 0 {
			left += "   " + muted("rtt ") + text(fmtDur(s.RTT))
		}
	}
	active := m.tab
	if m.detail {
		active = tabRequests
	}
	return m.headerLines(left, active)
}

func (m Model) headerLines(left, active string) string {
	tabs := m.tabs()
	rendered := make([]string, len(tabs))
	for i, n := range tabs {
		if n == active {
			rendered[i] = bold(cLav).Render(" " + n + " ")
		} else {
			rendered[i] = muted(" " + n + " ")
		}
	}
	tabStr := strings.Join(rendered, " ")
	line1 := spread(left, tabStr+" ", m.w)

	// The rule under the header has an accent under the active tab.
	start, aw := m.w-1-width(tabStr), 0
	for i, n := range tabs {
		if n == active {
			aw = width(rendered[i])
			break
		}
		start += width(rendered[i]) + 1
	}
	line2 := fg(cBorder).Render(rep("─", start)) + fg(cLav).Render(rep("━", aw)) + fg(cBorder).Render(rep("─", m.w-start-aw))
	return line1 + "\n" + line2
}

func (m Model) footer() string {
	var pairs []string
	switch {
	case m.modal == "share":
		pairs = []string{"c", "copy", "esc", "close"}
	case m.modal == "help":
		pairs = []string{"esc", "close"}
	case m.detail:
		pairs = []string{"←→", "request · response", "↑↓", "scroll", "r", "replay", "c", "copy as curl", "esc", "back"}
	case m.filtering:
		pairs = []string{"enter", "apply", "esc", "clear", "↑↓", "select"}
	case m.tab == tabTunnels:
		pairs = []string{"↑↓", "select", "enter", "show tunnel", "tab", "views", "?", "help", "q", "quit"}
	case m.tab == tabFlow:
		pairs = []string{"tab", "views", "s", "share", "?", "help", "q", "quit"}
	case m.tab == tabLogs:
		pairs = []string{"d i w e", "levels", "f", "follow", "↑↓", "scroll", "?", "help", "q", "quit"}
	case m.snap.Info.Protocol == "tcp":
		pairs = []string{"tab", "views", "s", "share", "?", "help", "q", "quit"}
	case m.tab == tabRequests:
		pairs = []string{"↑↓", "select", "enter", "inspect", "r", "replay", "c", "copy as curl", "/", "filter", "esc", "clear", "?", "help", "q", "quit"}
	default:
		pairs = []string{"tab", "views", "↑↓", "select", "enter", "inspect", "r", "replay", "/", "filter", "s", "share", "?", "help", "q", "quit"}
	}
	if m.anyReconnecting() && m.modal == "" && !m.detail && !m.filtering {
		pairs = append([]string{"R", "retry now"}, pairs...)
	}
	flash := ""
	if m.flash != "" {
		flash = fg(cPeach).Render("✓ " + m.flash)
	}
	// On narrow terminals, and to make room for a flash message, drop hints
	// from the end, keeping "? help".
	for len(pairs) > 2 && (width(hints(pairs...)) > m.w-2 || flash != "" && width(hints(pairs...))+width(flash)+4 > m.w) {
		i := len(pairs) - 2
		if pairs[i] == "?" {
			i -= 2
		}
		pairs = append(pairs[:i:i], pairs[i+2:]...)
	}
	h := hints(pairs...)
	right := muted(m.snap.Info.RemoteAddr)
	if flash != "" {
		right = flash
	} else if width(h)+width(right)+4 > m.w {
		right = ""
	}
	return spread(" "+h, right+" ", m.w)
}

// ---------- connecting ----------

func (m Model) viewConnecting() string {
	const cw = 62
	iw := cw - 4
	lines := []string{""}
	for _, l := range bigLogo() {
		lines = append(lines, center(l, iw))
	}
	lines = append(lines, "", center(muted(m.snap.Info.Version+" · self-hosted tunnels to localhost"), iw), "", "")
	if p := m.snap.Info.ConfigPath; p != "" {
		lines = append(lines, "    "+fg(cGreen).Render("✓")+"  "+text(fit("Loaded config", 18))+lav(tildePath(p)))
	}
	lines = append(lines,
		"    "+m.spin.View()+"  "+bold(cBright).Render(fit("Connecting to", 18))+muted(m.snap.Info.RemoteAddr),
		"", "")
	card := box{W: cw, H: len(lines) + 2, Lines: lines, Border: cLavDeep}.render()
	block := vjoin(card, "", center(muted("ctrl+c quit"), cw))
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, block)
}

// ---------- overview ----------

func (m Model) viewOverview(h int) string {
	var parts []string
	if m.snap.Status == StatusReconnecting {
		parts = append(parts, m.reconnectBanner(m.w))
		h -= 5
	}
	d := 0.0
	if m.snap.Status != StatusOnline {
		d = 0.5
	}
	parts = append(parts, withDim(d, func() string {
		if m.w < 100 {
			return m.overviewCompact(h)
		}
		const top, sw = 12, 50
		row := hjoin(m.sessionBox(sw, top), m.metricsBox(m.w-sw, top))
		if m.snap.Info.Protocol == "tcp" {
			return vjoin(row, m.connsBox(m.snap, m.w, h-top))
		}
		return vjoin(row, m.reqBox(m.w, h-top))
	}))
	return vjoin(parts...)
}

func (m Model) forwardingURL() string { return m.urlOf(m.snap) }

func (m Model) sessionBox(w, h int) string {
	s := m.snap
	l := func(t string) string { return lbl(t, 12) }
	lines := []string{
		l("Forwarding") + m.forwardingURL(),
		sp(12) + muted("→ ") + text(s.Info.ForwardAddr),
	}
	maxRoutes := 3
	if s.Info.InspectURL != "" {
		maxRoutes = 2
	}
	for i, r := range s.Info.Routes {
		if i == maxRoutes {
			lines = append(lines, sp(12)+muted(fmt.Sprintf("+%d more routes", len(s.Info.Routes)-maxRoutes)))
			break
		}
		label := sp(12)
		if i == 0 {
			label = l("Routes")
		}
		lines = append(lines, label+lav(r[0])+muted(" → ")+text(r[1]))
	}
	reconnects := ""
	switch s.Reconnects {
	case 0:
	case 1:
		reconnects = muted(" · 1 reconnect")
	default:
		reconnects = muted(fmt.Sprintf(" · %d reconnects", s.Reconnects))
	}
	if s.Info.InspectURL != "" {
		lines = append(lines, l("Inspector")+lav(s.Info.InspectURL))
	}
	lines = append(lines, "",
		l("Server")+text(s.Info.RemoteAddr),
		l("Started")+text(s.Started.Format("15:04:05"))+reconnects,
	)
	if s.Info.ConfigPath != "" {
		lines = append(lines, l("Config")+text(tildePath(s.Info.ConfigPath)))
	}
	right := fg(cGreen).Render("● " + s.Info.Protocol)
	if s.Status != StatusOnline {
		right = fg(cYellow).Render("◌ offline")
	}
	t := title("Session")
	if s.Info.Name != "" {
		t += muted(" · ") + bold(cBright).Render(s.Info.Name)
	}
	return box{W: w, H: h, Title: t, Right: right, Lines: lines}.render()
}

type tile struct{ v, l, c string }

func tiles(groups [][]tile, iw int) (vals, labs string) {
	tw := iw / max(1, len(groups))
	for gi, g := range groups {
		var v, l string
		if gi > 0 {
			v, l = fg(cBorder).Render("│ "), fg(cBorder).Render("│ ")
		}
		cw := (tw - 2) / len(g)
		for _, t := range g {
			v += bold(t.c).Render(fit(t.v, cw))
			l += muted(fit(t.l, cw))
		}
		vals += fit(v, tw)
		labs += fit(l, tw)
	}
	return vals, labs
}

// percentiles returns p50, p90 and p99 of the finished, non-streaming requests.
func percentiles(rows []Request) (p50, p90, p99 time.Duration, ok bool) {
	var ds []time.Duration
	for _, r := range rows {
		if r.Done && r.Kind == "" {
			ds = append(ds, r.Duration)
		}
	}
	if len(ds) == 0 {
		return 0, 0, 0, false
	}
	slices.Sort(ds)
	at := func(q float64) time.Duration { return ds[int(q*float64(len(ds)-1))] }
	return at(0.5), at(0.9), at(0.99), true
}

func (m Model) metricsBox(w, h int) string {
	s := m.snap
	iw := w - 4
	var groups [][]tile
	var vals []float64
	var caption, unit string
	if s.Info.Protocol == "tcp" {
		groups = [][]tile{
			{{comma(s.ConnTotal), "total", cLav}, {comma(s.ConnOpen), "open", cBright}},
			{{fmtBytes(s.BytesIn), "↓ received", cPeach}, {fmtBytes(s.BytesOut), "↑ sent", cLav}},
		}
		vals = m.throughput[m.cur]
		caption, unit = "throughput · last 60s", "/s"
	} else {
		inflight := 0
		for _, r := range s.Requests {
			if !r.Done {
				inflight++
			}
		}
		sum := 0
		vals = make([]float64, len(s.PerSec))
		for i, n := range s.PerSec {
			vals[i] = float64(n)
			sum += n
		}
		pt := []tile{{"—", "p50", cBright}, {"—", "p90", cBright}, {"—", "p99", cBright}}
		if p50, p90, p99, ok := percentiles(s.Requests); ok {
			pt = []tile{{fmtDur(p50), "p50", cBright}, {fmtDur(p90), "p90", cBright}, {fmtDur(p99), "p99", cYellow}}
		}
		groups = [][]tile{
			{{comma(s.ReqTotal), "requests", cLav}, {comma(inflight), "in flight", cBright}},
			{{fmt.Sprintf("%.2f", float64(sum)/60), "req/s 1m", cBright}, {comma(s.ConnOpen), "conns", cBright}},
			pt,
		}
		caption = "requests/sec · last 60s"
	}
	v, l := tiles(groups, iw)

	top := 0.0
	for _, x := range vals {
		top = math.Max(top, x)
	}
	now := vals[len(vals)-1]
	fmtV := func(x float64) string {
		if unit != "" {
			return fmtBytes(int64(x)) + unit
		}
		return fmt.Sprintf("%.0f", x)
	}
	lines := []string{v, l, "", spread(muted(caption), muted("peak ")+fg(cPeach).Render(fmtV(top))+muted("   now ")+text(fmtV(now)), iw)}
	chart := spark(vals, top, 3, func(x float64) string {
		if top > 0 && x >= top*0.8 {
			return cPeach
		}
		return cLav
	})
	axis := []string{"", "", "0"}
	switch {
	case top == 0:
	case unit != "":
		axis[0] = shortBytes(top)
	default:
		axis[0] = fmt.Sprintf("%.0f", top)
	}
	const aw = 4
	for i, c := range chart {
		lines = append(lines, muted(fitR(axis[i], aw))+fg(cBorder).Render("│")+c)
	}
	lines = append(lines, sp(aw)+fg(cBorder).Render("└"+rep("─", len(vals))))
	if s.Info.Protocol == "http" {
		lines = append(lines, m.statusBar(iw), m.splitLegend(iw))
	}
	return box{W: w, H: h, Title: title("Metrics"), Lines: lines}.render()
}

func (m Model) split() [4]int {
	var out [4]int
	for _, r := range m.snap.Requests {
		switch {
		case r.Status == 0:
		case r.Status >= 500:
			out[3]++
		case r.Status >= 400:
			out[2]++
		case r.Status >= 300 || r.Status == 101:
			out[1]++
		default:
			out[0]++
		}
	}
	return out
}

func (m Model) statusBar(w int) string {
	split := m.split()
	tot := split[0] + split[1] + split[2] + split[3]
	if tot == 0 {
		return fg(cTrack).Render(rep("─", w))
	}
	// Give every non-empty class at least one cell, and the rest of the width
	// to the largest class.
	widths, used, largest := [4]int{}, 0, 0
	for i, n := range split {
		if n > 0 {
			widths[i] = max(1, n*w/tot)
		}
		used += widths[i]
		if n > split[largest] {
			largest = i
		}
	}
	widths[largest] += w - used
	var s string
	for i, c := range []string{cGreen, cCyan, cYellow, cRed} {
		s += blk(c, widths[i])
	}
	return s
}

func (m Model) splitLegend(w int) string {
	split := m.split()
	cols := []string{cGreen, cCyan, cYellow, cRed}
	names := []string{"2xx", "3xx", "4xx", "5xx"}
	parts := make([]string, 4)
	for i := range cols {
		parts[i] = fg(cols[i]).Render("■") + " " + muted(names[i]) + " " + text(comma(split[i]))
	}
	return spread(strings.Join(parts, "   "), muted(fmt.Sprintf("last %s", comma(len(m.snap.Requests)))), w)
}

func (m Model) overviewCompact(h int) string {
	s := m.snap
	iw := m.w - 4
	l1 := spread(m.forwardingURL()+muted(" → ")+text(shortAddr(s.Info.ForwardAddr)), muted("up ")+text(fmtUptime(m.now.Sub(s.OnlineSince))), iw)
	if s.Status != StatusOnline {
		l1 = m.forwardingURL() + muted(" → ") + text(shortAddr(s.Info.ForwardAddr))
	}
	var routes []string
	for _, r := range s.Info.Routes {
		routes = append(routes, lav(r[0])+muted(" → ")+text(shortAddr(r[1])))
	}
	l2 := muted("server ") + text(s.Info.RemoteAddr)
	if len(routes) > 0 {
		l2 = strings.Join(routes, "   ")
	}
	mt := func(k, v string) string { return muted(k+" ") + bold(cBright).Render(v) }
	var l3 string
	if s.Info.Protocol == "tcp" {
		l3 = strings.Join([]string{mt("total", comma(s.ConnTotal)), mt("open", comma(s.ConnOpen)),
			mt("↓", fmtBytes(s.BytesIn)), mt("↑", fmtBytes(s.BytesOut))}, "   ")
	} else {
		parts := []string{mt("requests", comma(s.ReqTotal))}
		if p50, p90, _, ok := percentiles(s.Requests); ok {
			parts = append(parts, mt("p50", fmtDur(p50)), mt("p90", fmtDur(p90)))
		}
		l3 = strings.Join(parts, "   ")
	}
	sess := box{W: m.w, H: 5, Title: title("Session"), Right: fg(cGreen).Render("● " + s.Info.Protocol), Lines: []string{l1, l2, l3}}.render()
	if s.Info.Protocol == "tcp" {
		return vjoin(sess, m.connsBox(m.snap, m.w, h-5))
	}
	return vjoin(sess, m.reqBox(m.w, h-5))
}

func (m Model) reconnectBanner(w int) string {
	s := m.snap
	iw := w - 4
	lost := "connection lost: "
	if s.LastOnline.IsZero() {
		lost = "cannot connect: "
	}
	right := ""
	if !s.LastOnline.IsZero() {
		right = muted("last online ") + text(s.LastOnline.Format("15:04:05"))
	}
	l1 := spread(fg(cRed).Render(lost)+text(s.LastErr), right, iw)
	left := max(0, s.RetryAt.Sub(m.now).Seconds())
	frac := 0.0
	if s.Backoff > 0 {
		frac = left / s.Backoff.Seconds()
	}
	l2 := muted("retrying in ") + bold(cYellow).Render(fmt.Sprintf("%ds", int(math.Ceil(left)))) + "  " + bar(frac, 30, cYellow, cBorder) +
		"     " + muted("attempt ") + text(fmt.Sprint(s.Attempt)) + muted("   backoff ") + text(s.Backoff.String())
	l3 := muted("showing last known state · ") + bold(cLav).Render("R") + muted(" retry now")
	return box{W: w, H: 5, Title: m.spin.View() + " " + bold(cYellow).Render("Reconnecting"), Right: fg(cRed).Render("● offline"),
		Lines: []string{l1, l2, l3}, Border: cYellow}.render()
}

// ---------- request table ----------

type cols struct {
	compact bool
	path    int
}

func newCols(iw int) cols {
	if iw < 96 {
		return cols{compact: true, path: iw - 2 - 8 - 2 - 8 - 2 - 3 - 2 - 7 - 2 - 8}
	}
	return cols{path: iw - 2 - 12 - 2 - 8 - 2 - 24 - 2 - 9 - 2 - 9 - 3 - 8}
}

func (c cols) header() string {
	h := bold(cMuted)
	if c.compact {
		return "  " + h.Render(fit("TIME", 8)) + "  " + h.Render(fit("METHOD", 8)) + h.Render(fit("PATH", c.path)) + "  " +
			h.Render("STA") + "  " + h.Render(fitR("DUR", 7)) + "  " + h.Render(fitR("SIZE", 8))
	}
	return "  " + h.Render(fit("TIME", 12)) + "  " + h.Render(fit("METHOD", 8)) + h.Render(fit("PATH", c.path)) + "  " +
		h.Render(fit("STATUS", 24)) + "  " + h.Render(fitR("DURATION", 9)) + "  " + h.Render(fitR("SIZE", 9)) + "   " + h.Render(fit("UPSTREAM", 8))
}

func (m Model) reqDuration(r Request, compact bool) string {
	switch {
	case !r.Done && r.Kind == "sse":
		if compact {
			return fg(cCyan).Italic(true).Render("stream")
		}
		return fg(cCyan).Italic(true).Render("streaming")
	case !r.Done && r.Kind == "ws":
		return fg(cCyan).Italic(true).Render(fmtUptime(m.now.Sub(r.Start)))
	case !r.Done:
		return fg(cMuted).Italic(true).Render(fmtDur(m.now.Sub(r.Start)))
	case r.Duration >= 500*time.Millisecond:
		return fg(cRed).Render(fmtDur(r.Duration))
	case r.Duration >= 100*time.Millisecond:
		return fg(cYellow).Render(fmtDur(r.Duration))
	}
	return text(fmtDur(r.Duration))
}

func (m Model) reqRow(r Request, c cols, iw int, sel bool) string {
	mark := "  "
	if sel {
		mark = bold(cPeach).Render("▌") + " "
	}
	var badge string
	switch {
	case r.Kind == "ws" && !r.Done:
		badge = chip(cCyan).Render(" WS ") + " " + fg(cGreen).Render("● live")
	case r.Kind == "ws":
		badge = chip(cCyan).Render(" WS ")
	case r.Kind == "sse" && !r.Done:
		badge = chip(cPurple).Render(" SSE ") + " " + fg(cGreen).Render("● open")
	case r.Kind == "sse":
		badge = chip(cPurple).Render(" SSE ")
	}
	if r.ReplayOf != 0 {
		badge = strings.TrimSpace(replayBadge() + " " + badge)
	}
	pc := cText
	if sel {
		pc = cBright
	}
	path := r.URI
	avail := c.path - 1
	if badge != "" {
		avail -= width(badge) + 2
	}
	pathCell := fg(pc).Render(ansi.Truncate(path, max(1, avail), "…"))
	if badge != "" {
		pathCell += "  " + badge
	}
	pathCell = fit(pathCell, c.path)

	size := fmtBytes(r.RespSize)
	if r.Kind != "" || !r.Done && r.RespSize == 0 {
		size = "—"
	}
	sh := statusHex(r.Status)
	code := fmt.Sprint(r.Status)
	if r.Status == 0 {
		code = "···"
	}
	tc := cMuted
	if sel {
		tc = cText
	}
	method := bold(methodHex(r.Method))
	var line string
	if c.compact {
		line = mark + fg(tc).Render(r.Start.Format("15:04:05")) + "  " + method.Render(fit(r.Method, 8)) + pathCell + "  " +
			bold(sh).Render(code) + "  " + fitR(m.reqDuration(r, true), 7) + "  " + fitR(muted(size), 8)
	} else {
		status := bold(sh).Render(code)
		if r.Status != 0 {
			status += " " + fg(mixHex(sh, cText, 0.35)).Render(http.StatusText(r.Status))
		} else {
			status += " " + muted("pending")
		}
		line = mark + fg(tc).Render(r.Start.Format("15:04:05.000")) + "  " + method.Render(fit(r.Method, 8)) + pathCell + "  " +
			fit(status, 24) + "  " + fitR(m.reqDuration(r, false), 9) + "  " + fitR(muted(size), 9) + "   " +
			muted("→ ") + text(fit(shortAddr(r.Upstream), 6))
	}
	line = fit(line, iw)
	if sel {
		line = paintBg(line, cSel)
	}
	return line
}

// reqLines renders the header and as many rows as fit in n lines, scrolled so
// the selection stays visible.
func (m Model) reqLines(rows []Request, iw, n int, showSel bool) []string {
	c := newCols(iw)
	out := []string{c.header()}
	n--
	if len(rows) == 0 {
		msg := "no requests yet · open " + m.snap.HostURL
		if m.snap.HostURL == "" || m.tab == tabRequests && m.filter.Value() != "" {
			msg = "no requests"
		}
		return append(out, "", center(muted(msg), iw))
	}
	cur := selIndex(rows, m.selID())
	off := 0
	if showSel && cur >= n {
		off = cur - n + 1
	}
	for i := off; i < len(rows) && i < off+n; i++ {
		out = append(out, m.reqRow(rows[i], c, iw, showSel && i == cur))
	}
	return out
}

func (m Model) liveBadge() string {
	s := muted(comma(m.snap.ReqTotal) + " total · ")
	if m.snap.Status != StatusOnline {
		return s + fg(cYellow).Render("‖ paused")
	}
	return s + fg(cGreen).Render("● live")
}

func (m Model) reqBox(w, h int) string {
	lines := m.reqLines(m.snap.Requests, w-4, h-2, m.snap.Status == StatusOnline || len(m.snap.Requests) > 0)
	return box{W: w, H: h, Title: title("Live requests"), Right: m.liveBadge(), Lines: lines}.render()
}

// ---------- TCP connections ----------

func (m Model) connsBox(s Snapshot, w, h int) string {
	iw := w - 4
	hd := bold(cMuted)
	lines := []string{"    " + hd.Render(fit("REMOTE", 24)) + hd.Render(fit("OPENED", 10)) + hd.Render(fit("DURATION", 12)) +
		hd.Render(fitR("↓ RECEIVED", 12)) + hd.Render(fitR("↑ SENT", 12))}
	if len(s.Conns) == 0 {
		lines = append(lines, "", center(muted("no connections yet"), iw))
	}
	// Open connections first, then closed ones, each newest first.
	conns := slices.Clone(s.Conns)
	slices.SortStableFunc(conns, func(a, b *Conn) int {
		return cmpBool(a.Closed().IsZero(), b.Closed().IsZero())
	})
	for _, c := range conns {
		if len(lines) >= h-2 {
			break
		}
		closed := c.Closed()
		row := func() string {
			icon := fg(cGreen).Render("●")
			end := m.now
			if !closed.IsZero() {
				icon, end = muted("○"), closed
			}
			return "  " + icon + " " + text(fit(c.Remote, 24)) + muted(fit(c.Opened.Format("15:04:05"), 10)) +
				text(fit(fmtUptime(end.Sub(c.Opened)), 12)) + fitR(fg(cPeach).Render(fmtBytes(c.In())), 12) + fitR(lav(fmtBytes(c.Out())), 12)
		}
		if closed.IsZero() {
			lines = append(lines, row())
		} else {
			lines = append(lines, withDim(0.45, row))
		}
	}
	right := text(comma(s.ConnOpen)) + muted(" open · ") + text(comma(s.ConnTotal)) + muted(" total")
	return box{W: w, H: h, Title: title("Connections"), Right: right, Lines: lines}.render()
}

// cmpBool orders true before false.
func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

// ---------- Requests tab ----------

func (m Model) viewRequests(h int) string {
	rows := m.list()
	fb := cBorder
	if m.filtering {
		fb = cLav
	}
	count := text(comma(len(rows))) + muted(" of ") + text(comma(len(m.snap.Requests)))
	if m.filter.Value() == "" {
		count = text(comma(len(rows))) + muted(" requests")
	}
	fl := spread(bold(cLav).Render("/ ")+m.filter.View(), count, m.w-4)
	filterBox := box{W: m.w, H: 3, Title: title("Filter"), Right: muted("status:>=400 · method:POST · /path"), Lines: []string{fl}, Border: fb}.render()

	pvH := 10
	if h < 28 {
		pvH = 0
	}
	listH := h - 3 - pvH
	list := box{W: m.w, H: listH, Title: title("Requests"), Right: m.liveBadge(), Lines: m.reqLines(rows, m.w-4, listH-2, true)}.render()
	if pvH == 0 {
		return vjoin(filterBox, list)
	}
	pv := box{W: m.w, H: pvH, Title: title("Preview"), Lines: []string{"", center(muted("no request selected"), m.w-4)}}.render()
	if r, ok := m.selected(); ok {
		pv = m.previewBox(r, m.w, pvH)
	}
	return vjoin(filterBox, list, pv)
}

func (m Model) routeText(r Request) string {
	if r.Route == "" {
		return text(r.Upstream) + muted("  (default)")
	}
	return text(r.Upstream) + muted("  (route ") + lav(r.Route) + muted(")")
}

func (m Model) previewBox(r Request, w, h int) string {
	l := func(s string) string { return lbl(s, 12) }
	head := bold(methodHex(r.Method)).Render(r.Method) + " " + bold(cBright).Render(r.URI)
	if r.Status != 0 {
		head += "   " + statusPill(r.Status, http.StatusText(r.Status))
	}
	head = spread(head, m.reqDuration(r, false)+muted(" · "+r.Start.Format("15:04:05.000")), w-4)
	ua := r.ReqHeader.Get("User-Agent")
	client := text(r.Client)
	if ua != "" {
		client += muted(" · ") + text(ua)
	}
	bodyInfo := func(size int64, h http.Header) string {
		if size == 0 {
			return muted("no body")
		}
		s := text(fmtBytes(size))
		if ct := h.Get("Content-Type"); ct != "" {
			s += muted(" · ") + text(ct)
		}
		return s
	}
	lines := []string{
		head, "",
		l("Upstream") + m.routeText(r),
		l("Client") + client,
		l("Request") + bodyInfo(r.ReqSize, r.ReqHeader),
		l("Response") + bodyInfo(r.RespSize, r.RespHeader),
		"",
		hints("enter", "inspect", "r", "replay", "c", "copy as curl"),
	}
	return box{W: w, H: h, Title: title("Preview"), Lines: lines}.render()
}

// ---------- request detail ----------

func (m Model) viewDetail(h int) string {
	w := m.w
	r, ok := m.selected()
	if !ok {
		return center(muted("no request selected"), w)
	}
	left := " " + bold(cLav).Render("‹") + " " + muted("Requests") + "   " + bold(methodHex(r.Method)).Render(r.Method) + " " + bold(cBright).Render(r.URI)
	if r.Status != 0 {
		left += "   " + statusPill(r.Status, http.StatusText(r.Status))
	}
	if r.ReplayOf != 0 {
		left += "   " + replayBadge()
		for _, o := range m.snap.Requests {
			if o.ID == r.ReplayOf {
				left += muted(" of ") + text(o.Start.Format("15:04:05.000"))
				break
			}
		}
	}
	left += "   " + muted(fmtBytes(r.RespSize)+" · "+r.Start.Format("15:04:05.000"))
	l1 := spread(left, muted("→ ")+text(shortAddr(r.Upstream))+" ", w)

	tabs := make([]string, 2)
	for i, n := range []string{"Request", "Response"} {
		if i == m.detailTab {
			tabs[i] = chip(cLav).Render(" " + n + " ")
		} else {
			tabs[i] = muted(" " + n + " ")
		}
	}
	l3 := " " + strings.Join(tabs, fg(cBorder).Render(" │ ")) + "   " + muted("←/→ switch")

	timLines := m.timing(r, w-4)
	timH := len(timLines) + 2
	boxH := h - 3 - timH
	lw := min(56, w/2)
	hdr, body, size := r.ReqHeader, r.ReqBody, r.ReqSize
	extra := []string{"", divider("Connection", lw-4),
		lbl("Client", 14) + text(r.Client),
		lbl("Protocol", 14) + text(r.Proto),
		lbl("Upstream", 14) + text(shortAddr(r.Upstream)) + routeSuffix(r),
		lbl("Received", 14) + text(r.Start.Format("2006-01-02 15:04:05.000")),
	}
	bodyTitle := "Request body"
	if m.detailTab == 1 {
		hdr, body, size = r.RespHeader, r.RespBody, r.RespSize
		bodyTitle = "Response body"
		extra = []string{"", divider("Response", lw-4)}
		if r.Status != 0 {
			extra = append(extra, lbl("Status", 14)+statusPill(r.Status, http.StatusText(r.Status)))
		}
		if r.Done {
			extra = append(extra, lbl("Completed", 14)+text(r.Start.Add(r.Duration).Format("15:04:05.000")))
		}
	}
	hl := append(headerLines(hdr, lw-4), extra...)
	lb := box{W: lw, H: boxH, Title: title("Headers"), Right: muted(fmt.Sprint(len(hdr))), Lines: hl}.render()

	bl, info := bodyLines(r, hdr, body, size)
	if m.detailTab == 0 && r.Done && !r.ReqRead {
		note := muted("not captured: the local address did not read it all")
		if size == 0 {
			bl = []string{note}
		} else {
			bl = append(bl, "", note)
		}
	}
	scroll := min(m.scroll, max(0, len(bl)-(boxH-2)))
	rb := box{W: w - lw, H: boxH, Title: title(bodyTitle), Right: muted(info), Lines: bl[scroll:]}.render()

	tim := box{W: w, H: timH, Title: title("Timing"), Right: muted("upstream round trip, as seen by the pgrok client"), Lines: timLines}.render()
	return vjoin(l1, "", l3, hjoin(lb, rb), tim)
}

func routeSuffix(r Request) string {
	if r.Route == "" {
		return muted(" (default)")
	}
	return muted(" (route ") + lav(r.Route) + muted(")")
}

func divider(label string, w int) string {
	return fg(cBorder).Render("── ") + bold(cMuted).Render(label) + " " + fg(cBorder).Render(rep("─", w-width(label)-4))
}

func headerLines(h http.Header, iw int) []string {
	if len(h) == 0 {
		return []string{muted("no headers")}
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kw := 19
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, lav(fit(k, kw-1))+" "+text(fit(sanitize(strings.Join(h[k], ", ")), iw-kw)))
	}
	return out
}

// sanitize drops control characters, so request data cannot move the cursor
// or change colors.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0:
			return -1
		}
		return r
	}, s)
}

func bodyLines(r Request, h http.Header, body []byte, size int64) ([]string, string) {
	ct := h.Get("Content-Type")
	mediaType, _, _ := strings.Cut(ct, ";")
	info := strings.TrimSpace(mediaType)
	if info == "" {
		info = "no content type"
	}
	info += " · " + fmtBytes(size)
	switch {
	case r.Kind == "sse":
		return []string{muted("event stream: the body is not captured")}, info
	case r.Kind == "ws":
		return []string{muted("protocol upgrade: the connection is not captured")}, info
	case size == 0:
		return []string{muted("no body")}, info
	}
	if enc := h.Get("Content-Encoding"); enc != "" {
		return []string{muted(fmt.Sprintf("%s-encoded body (%s)", enc, fmtBytes(size)))}, info
	}
	truncated := int64(len(body)) < size
	var out []string
	switch {
	case !truncated && json.Valid(body):
		var buf bytes.Buffer
		_ = json.Indent(&buf, body, "", "  ")
		out = hlJSON(buf.String())
		info += " · pretty"
	case utf8.Valid(body) && !strings.ContainsRune(string(body), 0):
		for i, l := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			out = append(out, fg(cBorder).Render(fitR(fmt.Sprint(i+1), 3))+"  "+text(sanitize(l)))
		}
	default:
		return []string{muted(fmt.Sprintf("binary body (%s)", fmtBytes(size)))}, info
	}
	if truncated {
		out = append(out, "", muted(fmt.Sprintf("… showing the first %s of %s", fmtBytes(int64(len(body))), fmtBytes(size))))
	}
	return out, info
}

func hlJSON(s string) []string {
	var out []string
	for n, line := range strings.Split(s, "\n") {
		var sb strings.Builder
		sb.WriteString(fg(cBorder).Render(fitR(fmt.Sprint(n+1), 3)) + "  ")
		i := 0
		for i < len(line) && line[i] == ' ' {
			i++
		}
		sb.WriteString(line[:i])
		for i < len(line) {
			c := line[i]
			switch {
			case c == '"':
				j := i + 1
				for j < len(line) && line[j] != '"' {
					if line[j] == '\\' {
						j++
					}
					j++
				}
				end := min(j+1, len(line))
				tok := sanitize(line[i:end])
				if strings.HasPrefix(strings.TrimLeft(line[end:], " "), ":") {
					sb.WriteString(lav(tok))
				} else {
					sb.WriteString(fg(cGreen).Render(tok))
				}
				i = end
			case c == '-' || c >= '0' && c <= '9':
				j := i
				for j < len(line) && strings.IndexByte("-+.eE0123456789", line[j]) >= 0 {
					j++
				}
				sb.WriteString(fg(cPeach).Render(line[i:j]))
				i = j
			case c >= 'a' && c <= 'z':
				j := i
				for j < len(line) && line[j] >= 'a' && line[j] <= 'z' {
					j++
				}
				sb.WriteString(fg(cCyan).Render(line[i:j]))
				i = j
			case c == ' ':
				sb.WriteByte(' ')
				i++
			default:
				sb.WriteString(muted(string(c)))
				i++
			}
		}
		out = append(out, sb.String())
	}
	return out
}

func replayBadge() string { return chip(cPeach).Render(" ↻ replay ") }

// timing draws the phases of the upstream round trip as a waterfall.
func (m Model) timing(r Request, iw int) []string {
	total := r.Duration
	if !r.Done {
		total = m.now.Sub(r.Start)
	}
	const labW, valW = 22, 10
	bw := iw - labW - valW
	cell := func(d time.Duration) int {
		if total <= 0 {
			return 0
		}
		return max(0, min(bw, int(math.Round(float64(d)/float64(total)*float64(bw)))))
	}
	track := func(n int) string { return fg(cTrack).Render(rep("╌", n)) }
	row := func(name string, from, to time.Duration, c string, strong bool) string {
		a := cell(from)
		n := max(1, cell(to)-a)
		a = min(a, bw-n)
		ls := muted(fit(name, labW))
		if strong {
			ls = bold(cBright).Render(fit(name, labW))
		}
		return ls + track(a) + blk(c, n) + track(bw-a-n) + fitR(text(fmtDur(to-from)), valW)
	}
	// until returns d, or the elapsed time for a phase that has not ended.
	until := func(d time.Duration) time.Duration {
		if d == 0 {
			return total
		}
		return d
	}

	t := r.Trace
	var out []string
	switch {
	case t.Reused:
		out = append(out, row("reused connection", 0, t.GotConn, cMuted, false))
	case t.ConnectErr != "":
		out = append(out, row("connect failed", t.ConnectStart, until(t.ConnectDone), cRed, false))
	case t.ConnectStart > 0:
		out = append(out, row("connect", t.ConnectStart, until(t.ConnectDone), cCyan, false))
	}
	if t.ConnectErr != "" {
		return append(out, fg(cRed).Render(fit("  "+sanitize(t.ConnectErr), iw)))
	}
	if t.GotConn > 0 {
		out = append(out, row("send request", t.GotConn, until(t.WroteRequest), cPurple, false))
	}
	// The response may start before the request is fully written.
	if t.WroteRequest > 0 {
		out = append(out, row("wait for response", t.WroteRequest, max(t.WroteRequest, until(t.FirstByte)), cLav, false))
	}
	if t.FirstByte > 0 {
		out = append(out, row("response", t.FirstByte, total, cPeach, false))
	}
	switch {
	case len(out) == 0 && r.Status == 0:
		return []string{row("waiting for upstream", 0, total, cMuted, false)}
	case len(out) == 0:
		// No upstream round trip was traced, such as for a protocol upgrade.
		out = []string{
			row("response header", 0, r.TTFB, cLav, false),
			row("body", r.TTFB, total, cPeach, false),
		}
	}
	totalName := "total"
	if !r.Done {
		totalName = "elapsed (in progress)"
	}
	return append(out, row(totalName, 0, total, cLavDeep, true))
}

// curlCommand returns a curl command that repeats r against the public URL.
func curlCommand(hostURL string, r Request) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	parts := []string{"curl"}
	if r.Method != http.MethodGet {
		parts = append(parts, "-X", r.Method)
	}
	parts = append(parts, quote(strings.TrimSuffix(hostURL, "/")+r.URI))
	keys := make([]string, 0, len(r.ReqHeader))
	for k := range r.ReqHeader {
		switch k {
		case "Accept-Encoding", "Connection", "Content-Length", "Upgrade", "Te", "Trailer", "Transfer-Encoding":
			continue
		}
		if strings.HasPrefix(k, "X-Forwarded-") || k == "X-Real-Ip" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range r.ReqHeader[k] {
			parts = append(parts, "-H", quote(k+": "+v))
		}
	}
	if r.ReqSize > 0 && int64(len(r.ReqBody)) == r.ReqSize {
		parts = append(parts, "--data-raw", quote(string(r.ReqBody)))
	}
	return strings.Join(parts, " ")
}

// ---------- tunnels ----------

func (m Model) anyReconnecting() bool {
	for _, s := range m.snaps {
		if s.Status == StatusReconnecting {
			return true
		}
	}
	return false
}

func (m Model) viewTunnels(h int) string {
	iw := m.w - 4
	const nameW, protoW, statusW, trafficW, sparkW = 14, 6, 18, 11, 16
	urlW := max(10, iw-2-nameW-protoW-statusW-trafficW-2-sparkW)
	hd := bold(cMuted)
	lines := []string{"  " + hd.Render(fit("NAME", nameW)) + hd.Render(fit("TYPE", protoW)) + hd.Render(fit("FORWARDING", urlW)) +
		hd.Render(fit("STATUS", statusW)) + hd.Render(fitR("TRAFFIC", trafficW)) + "  " + hd.Render(fit("LAST 60S", sparkW))}
	for i, s := range m.snaps {
		sel := i == m.tunSel
		mark := "  "
		if sel {
			mark = bold(cPeach).Render("▌") + " "
		}
		url := muted("waiting…")
		if s.HostURL != "" {
			url = fg(cPeach).Render(s.HostURL)
		}
		url += muted(" → ") + text(shortAddr(s.Info.ForwardAddr))
		traffic := comma(s.ReqTotal) + " req"
		vals := make([]float64, len(s.PerSec))
		for j, n := range s.PerSec {
			vals[j] = float64(n)
		}
		if s.Info.Protocol == "tcp" {
			traffic = comma(s.ConnOpen) + "/" + comma(s.ConnTotal) + " conn"
			vals = m.throughput[i]
		}
		vals = vals[max(0, len(vals)-sparkW):]
		top := 0.0
		for _, v := range vals {
			top = math.Max(top, v)
		}
		chart := spark(vals, top, 1, func(float64) string { return cLav })[0]
		name := bold(cBright).Render(fit(s.Info.Name, nameW-1)) + " "
		line := mark + name + lav(fit(s.Info.Protocol, protoW)) + fit(url, urlW-1) + " " + fit(m.statusOf(s), statusW) +
			fitR(text(traffic), trafficW) + "  " + chart
		line = fit(line, iw)
		if sel {
			line = paintBg(line, cSel)
		}
		lines = append(lines, line)
	}
	listH := min(h-8, len(m.snaps)+4)
	right := muted(fmt.Sprintf("%d tunnels · from ", len(m.snaps))) + text(tildePath(m.snap.Info.ConfigPath))
	if m.snap.Info.ConfigPath == "" {
		right = muted(fmt.Sprintf("%d tunnels", len(m.snaps)))
	}
	list := box{W: m.w, H: listH, Title: title("Tunnels"), Right: right, Lines: lines}.render()
	return vjoin(list, m.tunnelDetails(m.snaps[m.tunSel], m.w, h-listH))
}

func (m Model) tunnelDetails(s Snapshot, w, h int) string {
	l := func(t string) string { return lbl(t, 12) }
	lines := []string{
		l("Forwarding") + m.urlOf(s) + muted(" → ") + text(s.Info.ForwardAddr),
	}
	for i, r := range s.Info.Routes {
		label := sp(12)
		if i == 0 {
			label = l("Routes")
		}
		lines = append(lines, label+lav(r[0])+muted(" → ")+text(r[1]))
	}
	if s.Info.InspectURL != "" {
		lines = append(lines, l("Inspector")+lav(s.Info.InspectURL))
	}
	lines = append(lines, "")
	switch s.Status {
	case StatusOnline:
		lines = append(lines, l("Status")+m.statusOf(s)+muted(" for ")+text(fmtUptime(m.now.Sub(s.OnlineSince))))
	default:
		lines = append(lines, l("Status")+m.statusOf(s))
		if s.LastErr != "" {
			lines = append(lines, l("Last error")+fg(cRed).Render(sanitize(s.LastErr)))
		}
	}
	reconnects := ""
	if s.Reconnects > 0 {
		reconnects = muted(fmt.Sprintf(" · %d reconnects", s.Reconnects))
	}
	lines = append(lines, l("Started")+text(s.Started.Format("15:04:05"))+reconnects)
	if s.Info.Protocol == "tcp" {
		lines = append(lines, l("Traffic")+text(comma(s.ConnTotal))+muted(" connections · ")+
			fg(cPeach).Render("↓ "+fmtBytes(s.BytesIn))+"  "+lav("↑ "+fmtBytes(s.BytesOut)))
	} else {
		t := text(comma(s.ReqTotal)) + muted(" requests")
		if p50, _, p99, ok := percentiles(s.Requests); ok {
			t += muted(" · p50 ") + text(fmtDur(p50)) + muted(" · p99 ") + text(fmtDur(p99))
		}
		lines = append(lines, l("Traffic")+t)
	}
	t := title("Tunnel") + muted(" · ") + bold(cBright).Render(s.Info.Name)
	detH := len(lines) + 2
	det := box{W: w, H: detH, Title: t, Right: hints("enter", "show on Overview"), Lines: lines}.render()
	if h-detH < 5 {
		return det
	}
	if s.Info.Protocol == "tcp" {
		return vjoin(det, m.connsBox(s, w, h-detH))
	}
	iw := w - 4
	c := newCols(iw)
	rows := []string{c.header()}
	for _, r := range s.Requests {
		if len(rows) >= h-detH-2 {
			break
		}
		rows = append(rows, m.reqRow(r, c, iw, false))
	}
	if len(s.Requests) == 0 {
		rows = append(rows, "", center(muted("no requests yet"), iw))
	}
	right := muted(comma(s.ReqTotal) + " total")
	return vjoin(det, box{W: w, H: h - detH, Title: title("Latest requests"), Right: right, Lines: rows}.render())
}

func (m Model) urlOf(s Snapshot) string {
	if s.HostURL == "" {
		return muted("waiting for the server…")
	}
	return bold(cPeach).Render(s.HostURL)
}

// ---------- logs ----------

var (
	levelNames = []string{"DEBU", "INFO", "WARN", "ERRO"}
	levelHex   = []string{"#8B8FD8", "#73DACA", cYellow, cRed}
)

// logLevel returns the index of a charm/log line's level, which follows the
// date and time.
func logLevel(line string) int {
	f := strings.Fields(ansi.Strip(line))
	for i := 0; i < len(f) && i < 3; i++ {
		if f[i] == "FATA" {
			return 3
		}
		for j, n := range levelNames {
			if f[i] == n {
				return j
			}
		}
	}
	return 1
}

func (m Model) viewLogs(h int) string {
	var logs []string
	for _, l := range m.logLines {
		if m.levels[logLevel(l)] {
			logs = append(logs, l)
		}
	}
	chips := make([]string, len(levelNames))
	for i, n := range levelNames {
		if m.levels[i] {
			chips[i] = chip(levelHex[i]).Render(" " + n + " ")
		} else {
			chips[i] = lipgloss.NewStyle().Foreground(col(cMuted)).Background(col(cTrack)).Strikethrough(true).Render(" " + n + " ")
		}
	}
	left := " " + muted("Levels  ") + strings.Join(chips, " ") + "   " + muted("d i w e to toggle")
	follow := fg(cGreen).Render("● following")
	if !m.follow {
		follow = fg(cYellow).Render("‖ paused") + muted(" · f to follow")
	}
	l1 := spread(left, follow+muted(fmt.Sprintf("   %d of %d lines", len(logs), len(m.logLines)))+" ", m.w)

	bh := h - 2
	n := bh - 2
	off := 0
	if len(logs) > n {
		off = len(logs) - n
		if !m.follow {
			off = max(0, off-m.logScroll)
		}
	}
	lines := logs[off:min(len(logs), off+n)]
	if len(logs) == 0 {
		lines = []string{"", center(muted("no log lines"), m.w-4)}
	}
	foot := muted("end of log") + "  " + fg(cGreen).Render("▼")
	if off+n < len(logs) {
		foot = muted(fmt.Sprintf("%d more below", len(logs)-off-n)) + "  " + fg(cYellow).Render("▼")
	}
	b := box{W: m.w, H: bh, Title: title("Logs"), Foot: foot, Lines: lines}.render()
	return vjoin(l1, "", b)
}

// ---------- modals ----------

func (m Model) helpModal() string {
	sec := func(s string) string { return bold(cPeach).Render(s) }
	row := func(k, d string) string { return bold(cLav).Render(fit(k, 13)) + text(d) }
	left := []string{
		sec("Navigation"),
		row("tab ⇧tab", "next / previous view"),
		row("1 – 5", "jump to view"),
		row("↑ ↓  k j", "move selection"),
		row("enter", "inspect request · show tunnel"),
		row("esc", "back · close · clear"),
		"",
		sec("Requests"),
		row("/", "filter requests"),
		row("← →", "request · response"),
		row("r", "replay to the local address"),
		row("c", "copy as curl"),
	}
	right := []string{
		sec("Global"),
		row("s", "share URL + QR code"),
		row("R", "reconnect now"),
		row("?", "toggle this help"),
		row("q ctrl+c", "quit"),
		"",
		sec("Logs"),
		row("d i w e", "toggle DEBU INFO WARN ERRO"),
		row("f", "follow the tail"),
		row("↑ ↓", "scroll"),
	}
	const cw = 40
	lines := []string{""}
	for i := 0; i < max(len(left), len(right)); i++ {
		var a, b string
		if i < len(left) {
			a = left[i]
		}
		if i < len(right) {
			b = right[i]
		}
		lines = append(lines, " "+fit(a, cw)+"  "+b)
	}
	lines = append(lines, "", " "+divider("Filter syntax", 2*cw+1),
		" "+fg(cGreen).Render("status:>=400")+"  "+fg(cGreen).Render("status:5xx")+"  "+fg(cGreen).Render("method:POST")+"  "+
			fg(cGreen).Render("/api/users")+muted("  (terms are ANDed)"), "")
	return box{W: 2*cw + 10, H: len(lines) + 2, Title: title("Keyboard shortcuts"), Right: muted("pgrok " + m.snap.Info.Version),
		Foot: bold(cLav).Render("esc") + muted(" close"), Lines: lines, Border: cLav}.render()
}

// qrLines draws the QR code of s with half blocks, two modules per cell, in
// explicit colors so it scans on dark and light terminals alike.
func qrLines(s string) []string {
	c, err := qr.Encode(s, qr.L)
	if err != nil {
		return []string{muted(err.Error())}
	}
	const quiet = 2
	n := c.Size + 2*quiet
	black := func(x, y int) bool { return c.Black(x-quiet, y-quiet) }
	dark, light := "16;16;22", "236;236;244"
	out := make([]string, 0, (n+1)/2)
	for y := 0; y < n; y += 2 {
		var sb strings.Builder
		for x := 0; x < n; x++ {
			top, bottom := light, light
			if black(x, y) {
				top = dark
			}
			if y+1 < n && black(x, y+1) {
				bottom = dark
			}
			fmt.Fprintf(&sb, "\x1b[38;2;%sm\x1b[48;2;%sm▀", top, bottom)
		}
		sb.WriteString("\x1b[m")
		out = append(out, sb.String())
	}
	return out
}

func (m Model) shareModal() string {
	s := m.snap
	code := qrLines(s.HostURL)
	w := max(52, width(code[0])+8)
	iw := w - 4
	lines := []string{""}
	if len(code)+11 <= m.h {
		for _, l := range code {
			lines = append(lines, center(l, iw))
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, center(muted("enlarge the terminal to show the QR code"), iw), "")
	}
	lines = append(lines,
		center(bold(cPeach).Underline(true).Render(s.HostURL), iw),
		center(muted("→ "+s.Info.ForwardAddr), iw),
		"",
		center(muted("scan to open on your phone"), iw),
		"",
		center(hints("c", "copy", "esc", "close"), iw))
	right := fg(cGreen).Render("● online")
	if s.Status != StatusOnline {
		right = fg(cYellow).Render("◌ offline")
	}
	return box{W: w, H: len(lines) + 2, Title: title("Share tunnel"), Right: right, Lines: lines, Border: cLav}.render()
}
