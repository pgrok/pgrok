package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/fx"
	"bloomfx.dev/bloom/on"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------- overview ----------

func (m *Model) viewOverview() bloom.Child {
	var parts []bloom.Child
	if m.snap.Status == StatusReconnecting {
		parts = append(parts, bloom.Fixed(5, m.reconnectBanner()))
	}
	var content bloom.Child
	if m.w < 100 {
		content = m.overviewCompact()
	} else {
		content = bloom.Column(
			bloom.Fixed(12, bloom.Row(bloom.Fixed(50, m.sessionBox()), bloom.Fill(m.metricsBox()))),
			bloom.Fill(m.liveBox()),
		)
	}
	if m.snap.Status != StatusOnline {
		// The last known state, faded while the tunnel is down.
		content = bloom.Transition(1, content, fx.Desaturate{Amount: 0.6}, fx.Dim{Amount: 0.35})
	}
	return bloom.Column(append(parts, bloom.Fill(content))...)
}

func (m *Model) liveBox() bloom.Child {
	if m.snap.Info.Protocol == "tcp" {
		return m.connsBox(m.snap, idConnList)
	}
	return m.reqBox()
}

// urlNode is the public URL of s, which opens the share dialog when clicked.
func (m *Model) urlNode(s Snapshot, id nodeID) bloom.Child {
	if s.HostURL == "" {
		return bloom.Text(muted("waiting for the server…"))
	}
	return bloom.Text(s.HostURL).Style(bold(cPeach)).HoverStyle(bold(cPeach).Underline(true)).Truncate().
		Tooltip("click to share", bloom.TipSide(bloom.Bottom)).
		ID(id).
		On(on.Click(func() tea.Cmd { return m.openShare(id) }))
}

func (m *Model) sessionBox() bloom.Child {
	s := m.snap
	l := func(label string, spans ...string) bloom.Child { return labeled(label, 12, spans...) }
	rows := []bloom.Child{
		bloom.Row(bloom.Fixed(12, bloom.Text(muted("Forwarding"))), bloom.Fill(m.urlNode(s, idURL))),
		l("", muted("→ "), text(s.Info.ForwardAddr)),
	}
	maxRoutes := 3
	if s.Info.InspectURL != "" {
		maxRoutes = 2
	}
	for i, r := range s.Info.Routes {
		if i == maxRoutes {
			rows = append(rows, l("", muted(fmt.Sprintf("+%d more routes", len(s.Info.Routes)-maxRoutes))))
			break
		}
		label := ""
		if i == 0 {
			label = "Routes"
		}
		rows = append(rows, l(label, lav(r[0]), muted(" → "), text(r[1])))
	}
	if s.Info.InspectURL != "" {
		rows = append(rows, l("Inspector", lav(s.Info.InspectURL)))
	}
	reconnects := ""
	switch s.Reconnects {
	case 0:
	case 1:
		reconnects = muted(" · 1 reconnect")
	default:
		reconnects = muted(fmt.Sprintf(" · %d reconnects", s.Reconnects))
	}
	rows = append(rows, bloom.Fixed(1, nil),
		l("Server", text(s.Info.RemoteAddr)),
		l("Started", text(s.Started.Format("15:04:05")), reconnects),
	)
	if s.Info.ConfigPath != "" {
		rows = append(rows, l("Config", text(tildePath(s.Info.ConfigPath))))
	}
	right := fg(cGreen).Render("● " + s.Info.Protocol)
	if s.Status != StatusOnline {
		right = fg(cYellow).Render("◌ offline")
	}
	t := title("Session")
	if s.Info.Name != "" {
		t += muted(" · ") + bold(cBright).Render(s.Info.Name)
	}
	return panel(t, right, bloom.Column(rows...))
}

type tile struct {
	v bloom.Child
	l string
}

// tileRow is groups of values over their labels, the groups split by rules.
func tileRow(groups ...[]tile) bloom.Child {
	var items []bloom.Child
	for gi, g := range groups {
		if gi > 0 {
			items = append(items, bloom.Fixed(2, bloom.Text(fg(cBorder).Render("│")+"\n"+fg(cBorder).Render("│"))))
		}
		for _, t := range g {
			items = append(items, bloom.Grow(1, bloom.Column(t.v, bloom.Text(muted(t.l)).Truncate())).Min(lipgloss.Width(t.l)+2))
		}
	}
	return bloom.Row(items...)
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

func (m *Model) metricsBox() bloom.Child {
	s := m.snap
	val := func(v, c string) bloom.Child { return bloom.Text(v).Style(bold(c)).Truncate() }
	// The total turns over like an odometer when it changes.
	total := val(comma(m.total), cLav)
	if m.roll.Animating() {
		total = bloom.Transition(m.roll.Float(), total, fx.Roll{From: m.rollFrom})
	}
	var groups [][]tile
	var vals []float64
	var caption, unit string
	if s.Info.Protocol == "tcp" {
		groups = [][]tile{
			{{total, "total"}, {val(comma(s.ConnOpen), cBright), "open"}},
			{{val(fmtBytes(s.BytesIn), cPeach), "↓ received"}, {val(fmtBytes(s.BytesOut), cLav), "↑ sent"}},
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
		pt := []tile{{val("—", cBright), "p50"}, {val("—", cBright), "p90"}, {val("—", cBright), "p99"}}
		if p50, p90, p99, ok := percentiles(s.Requests); ok {
			pt = []tile{{val(fmtDur(p50), cBright), "p50"}, {val(fmtDur(p90), cBright), "p90"}, {val(fmtDur(p99), cYellow), "p99"}}
		}
		groups = [][]tile{
			{{total, "requests"}, {val(comma(inflight), cBright), "in flight"}},
			{{val(fmt.Sprintf("%.2f", float64(sum)/60), cBright), "req/s 1m"}, {val(comma(s.ConnOpen), cBright), "conns"}},
			pt,
		}
		caption = "requests/sec · last 60s"
	}

	top := slices.Max(vals)
	fmtV := func(x float64) string {
		if unit != "" {
			return fmtBytes(int64(x)) + unit
		}
		return fmt.Sprintf("%.0f", x)
	}
	axis := ""
	switch {
	case top == 0:
	case unit != "":
		axis = shortBytes(top)
	default:
		axis = fmt.Sprintf("%.0f", top)
	}
	rows := []bloom.Child{
		bloom.Fixed(2, tileRow(groups...)),
		bloom.Fixed(1, nil),
		spread(muted(caption), muted("peak ")+fg(cPeach).Render(fmtV(top))+muted("   now ")+text(fmtV(vals[len(vals)-1]))),
		bloom.Fixed(3, bloom.Row(
			bloom.Fixed(4, bloom.Column(alignR(muted(axis)), bloom.Space(), alignR(muted("0")))),
			bloom.Fixed(1, bloom.Text(strings.Repeat(fg(cBorder).Render("│")+"\n", 2)+fg(cBorder).Render("│"))),
			bloom.Fixed(len(vals), bloom.Drawable(sparkline{vals: vals, top: top, hot: true})),
		)),
		bloom.Text(strings.Repeat(" ", 4) + fg(cBorder).Render("└"+strings.Repeat("─", len(vals)))),
	}
	if s.Info.Protocol == "http" {
		rows = append(rows, bloom.Fixed(1, bloom.Drawable(splitBar{m.split()})), m.splitLegend())
	}
	return panel(title("Metrics"), "", bloom.Column(rows...))
}

func (m *Model) split() [4]int {
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

func (m *Model) splitLegend() bloom.Child {
	split := m.split()
	cols := []string{cGreen, cCyan, cYellow, cRed}
	names := []string{"2xx", "3xx", "4xx", "5xx"}
	parts := make([]string, 4)
	for i := range cols {
		parts[i] = fg(cols[i]).Render("■") + " " + muted(names[i]) + " " + text(comma(split[i]))
	}
	return spread(strings.Join(parts, "   "), muted(fmt.Sprintf("last %s", comma(len(m.snap.Requests)))))
}

func (m *Model) overviewCompact() bloom.Child {
	s := m.snap
	l1 := bloom.Row(bloom.Auto(m.urlNode(s, idURL)), bloom.Fill(line(muted(" → "), text(shortAddr(s.Info.ForwardAddr)))))
	if s.Status == StatusOnline {
		l1 = bloom.Row(bloom.Fill(l1), bloom.Auto(bloom.Text(muted("up ")+text(fmtUptime(m.now.Sub(s.OnlineSince))))))
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
	sess := panel(title("Session"), fg(cGreen).Render("● "+s.Info.Protocol), bloom.Column(l1, line(l2), line(l3)))
	return bloom.Column(bloom.Fixed(5, sess), bloom.Fill(m.liveBox()))
}

func (m *Model) reconnectBanner() bloom.Child {
	s := m.snap
	lost := "connection lost: "
	if s.LastOnline.IsZero() {
		lost = "cannot connect: "
	}
	l1 := []bloom.Child{bloom.Fill(line(fg(cRed).Render(lost), text(sanitize(s.LastErr))))}
	if !s.LastOnline.IsZero() {
		l1 = append(l1, bloom.Auto(bloom.Text(muted("last online ")+text(s.LastOnline.Format("15:04:05")))))
	}
	left := max(0, s.RetryAt.Sub(m.now).Seconds())
	frac := 0.0
	if s.Backoff > 0 {
		frac = left / s.Backoff.Seconds()
	}
	l2 := bloom.Row(
		bloom.Auto(bloom.Text(muted("retrying in ")+bold(cYellow).Render(fmt.Sprintf("%3ds", int(math.Ceil(left))))+"  ")),
		bloom.Fixed(30, bloom.Drawable(bar{to: frac, fill: cYellow, track: cBorder, trackGlyph: "─"})),
		bloom.Fill(line("     ", muted("attempt "), text(fmt.Sprint(s.Attempt)), muted("   backoff "), text(s.Backoff.String()))),
	)
	l3 := bloom.Row(bloom.Auto(bloom.Text(muted("showing last known state ·"))), bloom.Auto(m.keyButton("R", "retry now")))
	b := panelIn(panelStyle.BorderForeground(lipgloss.Color(cYellow)),
		fg(cYellow).Render(m.spinGlyph())+" "+bold(cYellow).Render("Reconnecting"), fg(cRed).Render("● offline"),
		bloom.Column(bloom.Row(l1...), l2, l3))
	if m.pulse.Animating() {
		b = b.BorderFx(m.pulse.Float(), fx.Pulse{Color: lipgloss.Color(cPeach)})
	}
	return b
}

// ---------- request table ----------

// reqCols lays out the cells of a request row, or of the header. Every row
// is given the same slots, so the columns line up, and drop the same columns
// as the table narrows: the upstream first, then the status text and size.
func reqCols(mark, at, method, path, code, status, dur, size, upstream bloom.Child) bloom.Child {
	return bloom.Row(
		bloom.Fixed(2, mark),
		bloom.Fixed(14, at),
		bloom.Fixed(8, method),
		bloom.Fill(path).Min(16),
		bloom.Fixed(1, nil),
		bloom.Fixed(5, code),
		bloom.Fixed(21, status).Collapsible(2),
		bloom.Fixed(11, dur),
		bloom.Fixed(11, size).Collapsible(3),
		bloom.Fixed(11, upstream).Collapsible(1),
	)
}

func reqHeader() bloom.Child {
	h := func(s string) bloom.Child { return bloom.Text(headStyle.Render(s)) }
	return reqCols(h(""), h("TIME"), h("METHOD"), h("PATH"), h("STA"), h("STATUS"), alignR(headStyle.Render("DURATION")),
		alignR(headStyle.Render("SIZE")), h("   UPSTREAM"))
}

func (m *Model) reqDuration(r Request) string {
	switch {
	case !r.Done && r.Kind == "sse":
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

func reqBadge(r Request) string {
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
	return badge
}

// reqRow is a row of the request table. When interactive, a click selects
// it and a double click opens it.
func (m *Model) reqRow(rows []Request, i int, sel, interactive bool) bloom.Child {
	r := rows[i]
	mark := ""
	if sel {
		mark = bold(cPeach).Render("▌")
	}
	pc, tc := cText, cMuted
	if sel {
		pc, tc = cBright, cText
	}
	var path bloom.Child = bloom.Text(sanitize(r.URI)).Style(fg(pc)).Truncate()
	if b := reqBadge(r); b != "" {
		path = bloom.Row(bloom.Fill(path), bloom.Auto(bloom.Text("  "+b)), bloom.Fixed(1, nil))
	}
	code, status := muted("···"), muted("pending")
	if r.Status != 0 {
		sh := statusHex(r.Status)
		code = bold(sh).Render(fmt.Sprint(r.Status))
		status = fg(mixHex(sh, cText, 0.35)).Render(http.StatusText(r.Status))
	}
	size := fmtBytes(r.RespSize)
	if r.Kind != "" || !r.Done && r.RespSize == 0 {
		size = "—"
	}
	row := reqCols(
		bloom.Text(mark),
		bloom.Text(fg(tc).Render(r.Start.Format("15:04:05.000"))),
		bloom.Text(bold(methodHex(r.Method)).Render(r.Method)),
		path,
		bloom.Text(code),
		bloom.Text(status).Truncate(),
		alignR(m.reqDuration(r)),
		alignR(muted(size)),
		bloom.Text("   "+muted("→ ")+text(shortAddr(r.Upstream))),
	)
	st, hover := lipgloss.NewStyle(), hoverRow
	if sel {
		st, hover = selStyle, selStyle
	}
	box := bloom.Box(st, row)
	if !interactive {
		return box
	}
	return box.HoverStyle(hover).On(
		on.Click(func() tea.Cmd { m.focus.Clear(); m.selectIndex(rows, i); return nil }),
		on.DoubleClick(func() tea.Cmd { return m.openRequest(r.ID) }),
	)
}

// reqTable is the header and the rows of requests, as many as fit. The
// selection, when shown, stays in sight.
func (m *Model) reqTable(rows []Request, id nodeID, showSel, interactive bool) bloom.Child {
	if len(rows) == 0 {
		msg := "no requests yet · open " + m.snap.HostURL
		if m.snap.HostURL == "" || id == idReqList && m.filter.Value() != "" || !interactive {
			msg = "no requests"
		}
		return bloom.Column(reqHeader(), note(msg))
	}
	cur := selIndex(rows, m.selID())
	list := bloom.List(len(rows), func(i int) bloom.Child {
		return m.reqRow(rows, i, showSel && i == cur, interactive)
	}).Offset(m.offs[id]).ID(id)
	if showSel {
		list = list.Keep(cur)
	}
	if interactive {
		list = list.On(on.Scroll(func(_, dy int) tea.Cmd { m.moveBy(dy); return nil }))
	}
	return bloom.Column(reqHeader(), bloom.Fill(list))
}

func (m *Model) liveBadge() string {
	s := muted(comma(m.snap.ReqTotal) + " total · ")
	if m.snap.Status != StatusOnline {
		return s + fg(cYellow).Render("‖ paused")
	}
	return s + fg(cGreen).Render("● live")
}

// listScrollbar is a scrollbar for the list with ID id, on the right border
// of its box. Dragging it scrolls the list and keeps the selection in sight.
func (m *Model) listScrollbar(id nodeID) bloom.ScrollbarNode {
	return bloom.ScrollbarOf(id).OnScroll(func(first int) tea.Cmd {
		m.offs[id] = first
		if v, ok := m.ui.Frame().List(id); ok && (id == idOvList || id == idReqList) {
			rows := m.list()
			if i := selIndex(rows, m.selID()); i < first || i >= first+v.Visible {
				m.selectIndex(rows, min(max(i, first), first+v.Visible-1))
			}
		}
		return nil
	}).HoverStyle(scrollHover)
}

func (m *Model) reqBox() bloom.Child {
	show := m.snap.Status == StatusOnline || len(m.snap.Requests) > 0
	return panel(title("Live requests"), m.liveBadge(), m.reqTable(m.snap.Requests, idOvList, show, true)).
		Edge(bloom.Right, bloom.Stretch, m.listScrollbar(idOvList))
}

// ---------- TCP connections ----------

func (m *Model) connsBox(s Snapshot, id nodeID) bloom.Child {
	cols := func(icon, remote, opened, dur, in, out bloom.Child) bloom.Child {
		return bloom.Row(bloom.Fixed(4, icon), bloom.Fill(remote).Min(16), bloom.Fixed(10, opened), bloom.Fixed(12, dur),
			bloom.Fixed(12, in), bloom.Fixed(12, out), bloom.Fixed(2, nil))
	}
	h := func(s string) string { return headStyle.Render(s) }
	header := cols(bloom.Text(""), bloom.Text(h("REMOTE")), bloom.Text(h("OPENED")), bloom.Text(h("DURATION")),
		alignR(h("↓ RECEIVED")), alignR(h("↑ SENT")))
	right := text(comma(s.ConnOpen)) + muted(" open · ") + text(comma(s.ConnTotal)) + muted(" total")
	if len(s.Conns) == 0 {
		return panel(title("Connections"), right, bloom.Column(header, note("no connections yet")))
	}
	// Open connections first, then closed ones, each newest first.
	conns := slices.Clone(s.Conns)
	slices.SortStableFunc(conns, func(a, b *Conn) int {
		return cmpBool(a.Closed().IsZero(), b.Closed().IsZero())
	})
	list := bloom.List(len(conns), func(i int) bloom.Child {
		c := conns[i]
		closed := c.Closed()
		icon, end := fg(cGreen).Render("  ●"), m.now
		if !closed.IsZero() {
			icon, end = muted("  ○"), closed
		}
		row := cols(bloom.Text(icon), bloom.Text(text(c.Remote)).Truncate(), bloom.Text(muted(c.Opened.Format("15:04:05"))),
			bloom.Text(text(fmtUptime(end.Sub(c.Opened)))), alignR(fg(cPeach).Render(fmtBytes(c.In()))), alignR(lav(fmtBytes(c.Out()))))
		if !closed.IsZero() {
			return bloom.Transition(1, row, fx.Dim{Amount: 0.45})
		}
		return row
	}).Offset(m.offs[id]).ID(id).On(on.Scroll(func(_, dy int) tea.Cmd {
		m.offs[id] = max(0, m.offs[id]+dy)
		return nil
	}))
	return panel(title("Connections"), right, bloom.Column(header, bloom.Fill(list))).
		Edge(bloom.Right, bloom.Stretch, m.listScrollbar(id))
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

func (m *Model) viewRequests() bloom.Child {
	rows := m.list()
	count := text(comma(len(rows))) + muted(" of ") + text(comma(len(m.snap.Requests)))
	if m.filter.Value() == "" {
		count = text(comma(len(rows))) + muted(" requests")
	}
	st := panelStyle
	if m.filtering() {
		st = panelStyle.BorderForeground(lipgloss.Color(cLav))
	}
	input := bloom.Text(m.filter.View()).Cursor(m.filter.Cursor())
	filterBox := panelIn(st, title("Filter"), muted("status:>=400 · method:POST · /path"), bloom.Row(
		bloom.Fixed(2, bloom.Text(bold(cLav).Render("/ "))),
		bloom.Fill(input),
		bloom.Auto(bloom.Text(" "+count)),
	)).HoverStyle(hoverPanel.BorderForeground(lipgloss.Color(cLav))).ID(idFilter).Focusable()

	list := panel(title("Requests"), m.liveBadge(), m.reqTable(rows, idReqList, true, true)).
		Edge(bloom.Right, bloom.Stretch, m.listScrollbar(idReqList))
	var pv bloom.Child = panel(title("Preview"), "", note("no request selected"))
	if r, ok := m.selected(); ok {
		pv = m.previewBox(r)
	}
	// The preview gives way to the list on short terminals.
	return bloom.Column(
		bloom.Fixed(3, filterBox),
		bloom.Fill(list).Min(12),
		bloom.Fixed(10, pv).Collapsible(1),
	)
}

func routeSuffix(r Request) string {
	if r.Route == "" {
		return muted("  (default)")
	}
	return muted("  (route ") + lav(r.Route) + muted(")")
}

func (m *Model) previewBox(r Request) bloom.Child {
	l := func(label string, spans ...string) bloom.Child { return labeled(label, 12, spans...) }
	head := bold(methodHex(r.Method)).Render(r.Method) + " " + bold(cBright).Render(sanitize(r.URI))
	if r.Status != 0 {
		head += "   " + statusPill(r.Status, http.StatusText(r.Status))
	}
	client := text(r.Client)
	if ua := r.ReqHeader.Get("User-Agent"); ua != "" {
		client += muted(" · ") + text(sanitize(ua))
	}
	bodyInfo := func(size int64, h http.Header) string {
		if size == 0 {
			return muted("no body")
		}
		s := text(fmtBytes(size))
		if ct := h.Get("Content-Type"); ct != "" {
			s += muted(" · ") + text(sanitize(ct))
		}
		return s
	}
	body := bloom.Column(
		spread(head, m.reqDuration(r)+muted(" · "+r.Start.Format("15:04:05.000"))),
		bloom.Fixed(1, nil),
		l("Upstream", text(r.Upstream), routeSuffix(r)),
		l("Client", client),
		l("Request", bodyInfo(r.ReqSize, r.ReqHeader)),
		l("Response", bodyInfo(r.RespSize, r.RespHeader)),
		bloom.Fixed(1, nil),
		bloom.Row(m.keyButton("enter", "inspect"), m.keyButton("r", "replay"), m.keyButton("c", "copy as curl")),
	).Selectable()
	return panel(title("Preview"), "", body)
}

// ---------- request detail ----------

type bodyKey struct {
	id, tab, n int
	done       bool
}

// syncBody fills the body buffer with the body of the request shown in full,
// when it or its tab changed.
func (m *Model) syncBody() {
	r, ok := m.selected()
	if !ok {
		return
	}
	hdr, body, size := r.ReqHeader, r.ReqBody, r.ReqSize
	if m.detailTab == 1 {
		hdr, body, size = r.RespHeader, r.RespBody, r.RespSize
	}
	k := bodyKey{r.ID, m.detailTab, len(body), r.Done}
	if k == m.bodyOf {
		return
	}
	m.bodyOf = k
	lines, info, numbered := bodyLines(r, hdr, body, size)
	if m.detailTab == 0 && r.Done && !r.ReqRead {
		const missing = "not captured: the local address did not read it all"
		if size == 0 {
			lines, numbered = []string{muted(missing)}, false
		} else {
			lines = append(lines, "", muted(missing))
		}
	}
	m.body.SetLines(lines...)
	m.bodyInfo, m.bodyNumbered = info, numbered
}

func (m *Model) viewDetail() bloom.Child {
	r, ok := m.selected()
	if !ok {
		return note("no request selected")
	}
	back := bloom.Box(lipgloss.NewStyle(), bloom.Text(" "+bold(cLav).Render("‹")+" "+muted("Requests")+" ")).
		HoverStyle(selStyle).
		On(on.Click(func() tea.Cmd { return m.press("esc") }))
	what := []string{bold(methodHex(r.Method)).Render(r.Method) + " " + bold(cBright).Render(sanitize(r.URI))}
	if r.Status != 0 {
		what = append(what, "   "+statusPill(r.Status, http.StatusText(r.Status)))
	}
	if r.ReplayOf != 0 {
		what = append(what, "   "+replayBadge())
		for _, o := range m.snap.Requests {
			if o.ID == r.ReplayOf {
				what = append(what, muted(" of ")+text(o.Start.Format("15:04:05.000")))
				break
			}
		}
	}
	what = append(what, "   "+muted(fmtBytes(r.RespSize)+" · "+r.Start.Format("15:04:05.000")))
	head := bloom.Row(
		bloom.Auto(back),
		bloom.Fixed(2, nil),
		bloom.Fill(line(what...)),
		bloom.Auto(bloom.Text(muted("→ ")+text(shortAddr(r.Upstream))+" ")),
	)

	tabs := []bloom.Child{bloom.Fixed(1, nil)}
	for i, n := range []string{"Request", "Response"} {
		st, hover := fg(cMuted), fg(cLavLight)
		if i == m.detailTab {
			st, hover = chip(cLav), chip(cLav)
		}
		if i > 0 {
			tabs = append(tabs, bloom.Auto(bloom.Text(fg(cBorder).Render(" │ "))))
		}
		tabs = append(tabs, bloom.Auto(bloom.Text(" "+n+" ").Style(st).HoverStyle(hover).
			On(on.Click(func() tea.Cmd {
				if m.detailTab != i {
					m.detailTab, m.bodyPos = i, bloom.ViewportPos{}
				}
				return nil
			}))))
	}
	tabs = append(tabs, bloom.Auto(bloom.Text(muted("   ←/→ switch"))))

	hdr := r.ReqHeader
	bodyTitle := "Request body"
	extra := []bloom.Child{bloom.Fixed(1, nil), divider("Connection"),
		labeled("Client", 14, text(r.Client)),
		labeled("Protocol", 14, text(r.Proto)),
		labeled("Upstream", 14, text(shortAddr(r.Upstream)), routeSuffix(r)),
		labeled("Received", 14, text(r.Start.Format("2006-01-02 15:04:05.000"))),
	}
	if m.detailTab == 1 {
		hdr = r.RespHeader
		bodyTitle = "Response body"
		extra = []bloom.Child{bloom.Fixed(1, nil), divider("Response")}
		if r.Status != 0 {
			extra = append(extra, labeled("Status", 14, statusPill(r.Status, http.StatusText(r.Status))))
		}
		if r.Done {
			extra = append(extra, labeled("Completed", 14, text(r.Start.Add(r.Duration).Format("15:04:05.000"))))
		}
	}
	headers := panel(title("Headers"), muted(fmt.Sprint(len(hdr))), bloom.Column(append(headerRows(hdr), extra...)...).Selectable())

	vp := bloom.Viewport(m.body).At(m.bodyPos).ID(idBody).Selectable().
		OnScroll(func(p bloom.ViewportPos) tea.Cmd { m.bodyPos = p; return nil })
	if m.bodyNumbered {
		vp = vp.LineNumbers(fg(cBorder))
	}
	body := panel(title(bodyTitle), muted(m.bodyInfo), vp).
		Edge(bloom.Right, bloom.Stretch, bloom.ScrollbarOf(idBody).HoverStyle(scrollHover))

	tim := m.timing(r)
	timBox := panel(title("Timing"), muted("upstream round trip, as seen by the pgrok client"), bloom.Column(tim...))
	return bloom.Column(
		bloom.Fixed(1, head),
		bloom.Fixed(1, nil),
		bloom.Fixed(1, bloom.Row(tabs...)),
		bloom.Fill(bloom.Row(bloom.Percent(45, headers).Max(56), bloom.Fill(body))),
		bloom.Fixed(len(tim)+2, timBox),
	)
}

func divider(label string) bloom.Child {
	return bloom.Text(fg(cBorder).Render("── ") + bold(cMuted).Render(label) + " " + fg(cBorder).Render(strings.Repeat("─", 200)))
}

func headerRows(h http.Header) []bloom.Child {
	if len(h) == 0 {
		return []bloom.Child{bloom.Text(muted("no headers"))}
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]bloom.Child, 0, len(keys))
	for _, k := range keys {
		out = append(out, bloom.Row(
			bloom.Fixed(19, bloom.Text(lav(k)).Truncate()),
			bloom.Fill(bloom.Text(text(sanitize(strings.Join(h[k], ", ")))).Truncate()),
		))
	}
	return out
}

// bodyLines returns the lines of a body, highlighted, what it is, and whether
// it is content worth numbering rather than a note.
func bodyLines(r Request, h http.Header, body []byte, size int64) ([]string, string, bool) {
	ct := h.Get("Content-Type")
	mediaType, _, _ := strings.Cut(ct, ";")
	info := strings.TrimSpace(mediaType)
	if info == "" {
		info = "no content type"
	}
	info += " · " + fmtBytes(size)
	switch {
	case r.Kind == "sse":
		return []string{muted("event stream: the body is not captured")}, info, false
	case r.Kind == "ws":
		return []string{muted("protocol upgrade: the connection is not captured")}, info, false
	case size == 0:
		return []string{muted("no body")}, info, false
	}
	if enc := h.Get("Content-Encoding"); enc != "" {
		return []string{muted(fmt.Sprintf("%s-encoded body (%s)", sanitize(enc), fmtBytes(size)))}, info, false
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
		for _, l := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
			out = append(out, text(sanitize(l)))
		}
	default:
		return []string{muted(fmt.Sprintf("binary body (%s)", fmtBytes(size)))}, info, false
	}
	if truncated {
		out = append(out, "", muted(fmt.Sprintf("… showing the first %s of %s", fmtBytes(int64(len(body))), fmtBytes(size))))
	}
	return out, info, true
}

func hlJSON(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		var sb strings.Builder
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

// timing draws the phases of the upstream round trip as a waterfall.
func (m *Model) timing(r Request) []bloom.Child {
	total := r.Duration
	if !r.Done {
		total = m.now.Sub(r.Start)
	}
	frac := func(d time.Duration) float64 {
		if total <= 0 {
			return 0
		}
		return float64(d) / float64(total)
	}
	row := func(name string, from, to time.Duration, c string, strong bool) bloom.Child {
		label := muted(name)
		if strong {
			label = bold(cBright).Render(name)
		}
		return bloom.Row(
			bloom.Fixed(22, bloom.Text(label)),
			bloom.Fill(bloom.Drawable(bar{from: frac(from), to: frac(to), fill: c, track: cTrack, trackGlyph: "╌", minimumCells: 1})),
			bloom.Fixed(10, alignR(text(fmtDur(to-from)))),
		)
	}
	// until returns d, or the elapsed time for a phase that has not ended.
	until := func(d time.Duration) time.Duration {
		if d == 0 {
			return total
		}
		return d
	}

	t := r.Trace
	var out []bloom.Child
	switch {
	case t.Reused:
		out = append(out, row("reused connection", 0, t.GotConn, cMuted, false))
	case t.ConnectErr != "":
		out = append(out, row("connect failed", t.ConnectStart, until(t.ConnectDone), cRed, false))
	case t.ConnectStart > 0:
		out = append(out, row("connect", t.ConnectStart, until(t.ConnectDone), cCyan, false))
	}
	if t.ConnectErr != "" {
		return append(out, line(fg(cRed).Render("  "+sanitize(t.ConnectErr))))
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
		return []bloom.Child{row("waiting for upstream", 0, total, cMuted, false)}
	case len(out) == 0:
		// No upstream round trip was traced, such as for a protocol upgrade.
		out = []bloom.Child{
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

func (m *Model) viewTunnels() bloom.Child {
	cols := func(mark, name, kind, url, status, traffic, chart bloom.Child) bloom.Child {
		return bloom.Row(bloom.Fixed(2, mark), bloom.Fixed(14, name), bloom.Fixed(6, kind), bloom.Fill(url).Min(20),
			bloom.Fixed(20, status), bloom.Fixed(11, traffic), bloom.Fixed(2, nil), bloom.Fixed(16, chart).Collapsible(1))
	}
	h := func(s string) bloom.Child { return bloom.Text(headStyle.Render(s)) }
	header := cols(h(""), h("NAME"), h("TYPE"), h("FORWARDING"), h("STATUS"), alignR(headStyle.Render("TRAFFIC")), h("LAST 60S"))
	list := bloom.List(len(m.snaps), func(i int) bloom.Child {
		s := m.snaps[i]
		sel := i == m.tunSel
		mark := ""
		if sel {
			mark = bold(cPeach).Render("▌")
		}
		url := muted("waiting…")
		if s.HostURL != "" {
			url = fg(cPeach).Render(s.HostURL)
		}
		traffic := comma(s.ReqTotal) + " req"
		vals := make([]float64, len(s.PerSec))
		for j, n := range s.PerSec {
			vals[j] = float64(n)
		}
		if s.Info.Protocol == "tcp" {
			traffic = comma(s.ConnOpen) + "/" + comma(s.ConnTotal) + " conn"
			vals = m.throughput[i]
		}
		row := cols(bloom.Text(mark), bloom.Text(bold(cBright).Render(s.Info.Name)).Truncate(), bloom.Text(lav(s.Info.Protocol)),
			line(url, muted(" → "), text(shortAddr(s.Info.ForwardAddr))), m.statusNode(s), alignR(text(traffic)),
			bloom.Drawable(sparkline{vals: vals, top: slices.Max(vals)}))
		st, hover := lipgloss.NewStyle(), hoverRow
		if sel {
			st, hover = selStyle, selStyle
		}
		return bloom.Box(st, row).HoverStyle(hover).On(
			on.Click(func() tea.Cmd { m.tunSel = i; return nil }),
			on.DoubleClick(func() tea.Cmd { return m.setCur(i) }),
		)
	}).Keep(m.tunSel).ID(idTunList).On(on.Scroll(func(_, dy int) tea.Cmd { m.moveBy(dy); return nil }))

	right := muted(fmt.Sprintf("%d tunnels · from ", len(m.snaps))) + text(tildePath(m.snap.Info.ConfigPath))
	if m.snap.Info.ConfigPath == "" {
		right = muted(fmt.Sprintf("%d tunnels", len(m.snaps)))
	}
	tl := panel(title("Tunnels"), right, bloom.Column(header, bloom.Fill(list)))
	s := m.snaps[m.tunSel]
	var latest bloom.Child
	if s.Info.Protocol == "tcp" {
		latest = m.connsBox(s, idTunConns)
	} else {
		latest = panel(title("Latest requests"), muted(comma(s.ReqTotal)+" total"), m.reqTable(s.Requests, idTunReqs, false, false))
	}
	return bloom.Column(
		bloom.Fixed(len(m.snaps)+3, tl).Collapsible(2).Min(4),
		bloom.Auto(m.tunnelDetails(s)),
		bloom.Fill(latest).Min(5).Collapsible(1),
	)
}

func (m *Model) tunnelDetails(s Snapshot) bloom.Child {
	l := func(label string, spans ...string) bloom.Child { return labeled(label, 12, spans...) }
	rows := []bloom.Child{
		bloom.Row(bloom.Fixed(12, bloom.Text(muted("Forwarding"))), bloom.Auto(m.urlNode(s, nodeID("tunnel-url"))),
			bloom.Fill(line(muted(" → "), text(s.Info.ForwardAddr)))),
	}
	for i, r := range s.Info.Routes {
		label := ""
		if i == 0 {
			label = "Routes"
		}
		rows = append(rows, l(label, lav(r[0]), muted(" → "), text(r[1])))
	}
	if s.Info.InspectURL != "" {
		rows = append(rows, l("Inspector", lav(s.Info.InspectURL)))
	}
	rows = append(rows, bloom.Fixed(1, nil))
	switch s.Status {
	case StatusOnline:
		rows = append(rows, l("Status", m.statusText(s), muted(" for "), text(fmtUptime(m.now.Sub(s.OnlineSince)))))
	default:
		rows = append(rows, l("Status", m.statusText(s)))
		if s.LastErr != "" {
			rows = append(rows, l("Last error", fg(cRed).Render(sanitize(s.LastErr))))
		}
	}
	reconnects := ""
	if s.Reconnects > 0 {
		reconnects = muted(fmt.Sprintf(" · %d reconnects", s.Reconnects))
	}
	rows = append(rows, l("Started", text(s.Started.Format("15:04:05")), reconnects))
	if s.Info.Protocol == "tcp" {
		rows = append(rows, l("Traffic", text(comma(s.ConnTotal)), muted(" connections · "),
			fg(cPeach).Render("↓ "+fmtBytes(s.BytesIn)), "  ", lav("↑ "+fmtBytes(s.BytesOut))))
	} else {
		t := text(comma(s.ReqTotal)) + muted(" requests")
		if p50, _, p99, ok := percentiles(s.Requests); ok {
			t += muted(" · p50 ") + text(fmtDur(p50)) + muted(" · p99 ") + text(fmtDur(p99))
		}
		rows = append(rows, l("Traffic", t))
	}
	t := title("Tunnel") + muted(" · ") + bold(cBright).Render(s.Info.Name)
	return panel(t, "", bloom.Column(rows...)).Edge(bloom.Top, bloom.End, m.keyButton("enter", "show on Overview"))
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

func (m *Model) viewLogs() bloom.Child {
	items := []bloom.Child{bloom.Fixed(1, nil), keep(muted("Levels  "))}
	for i, n := range levelNames {
		st := chip(levelHex[i])
		if !m.levels[i] {
			st = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted)).Background(lipgloss.Color(cTrack)).Strikethrough(true)
		}
		items = append(items, bloom.Fixed(6, bloom.Text(" "+n+" ").Style(st).
			Tooltip(fmt.Sprintf("show or hide %s lines · %c", n, "diwe"[i]), bloom.TipSide(bloom.Bottom)).
			On(on.Click(func() tea.Cmd { m.toggleLevel(i); return nil }))), bloom.Fixed(1, nil))
	}
	hintS := muted("  d i w e to toggle")
	items = append(items, bloom.Auto(bloom.Text(hintS)).Min(lipgloss.Width(hintS)).Collapsible(1), bloom.Space())
	follow := fg(cGreen).Render("● following")
	if m.logPos != bloom.ViewportEnd {
		follow = fg(cYellow).Render("‖ paused") + muted(" · f to follow")
	}
	count := muted(fmt.Sprintf("   %s lines ", comma(m.logBuf.Len())))
	items = append(items,
		bloom.Auto(bloom.Text(follow).On(on.Click(func() tea.Cmd { m.toggleFollow(); return nil }))).Min(lipgloss.Width(follow)),
		bloom.Auto(bloom.Text(count)).Min(lipgloss.Width(count)).Collapsible(2),
	)

	body := note("no log lines")
	if m.logBuf.Len() > 0 {
		body = bloom.Viewport(m.logBuf).At(m.logPos).Follow().ID(idLogs).Selectable().
			OnScroll(func(p bloom.ViewportPos) tea.Cmd {
				m.logPos = p
				if v, ok := m.ui.Frame().Viewport(idLogs); ok && p == v.Bottom() {
					m.logPos = bloom.ViewportEnd // back at the end: follow again
				}
				return nil
			})
	}
	foot := muted("end of log") + "  " + fg(cGreen).Render("▼")
	if v, ok := m.ui.Frame().Viewport(idLogs); ok {
		if below := v.TotalRows - v.Row - v.Rows; below > 0 && m.logPos != bloom.ViewportEnd {
			foot = muted(fmt.Sprintf("%d more below", below)) + "  " + fg(cYellow).Render("▼")
		}
	}
	box := panel(title("Logs"), "", body).
		Edge(bloom.Right, bloom.Stretch, bloom.ScrollbarOf(idLogs).OnScroll(func(row int) tea.Cmd {
			if v, ok := m.ui.Frame().Viewport(idLogs); ok {
				m.logPos = v.AtRow(row)
			}
			return nil
		}).HoverStyle(scrollHover)).
		Edge(bloom.Bottom, bloom.End, bloom.Text(" "+foot+" "))
	return bloom.Column(bloom.Fixed(1, bloom.Row(items...)), bloom.Fixed(1, nil), bloom.Fill(box))
}
