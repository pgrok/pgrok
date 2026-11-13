package tui

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The Flow tab draws the path of every request as a map: visitors, the
// pgrokd server, the SSH tunnel, this client and the local addresses, with
// requests and responses moving along the links as they happen.
//
// Moving through the tunnel takes microseconds, far too fast to see, so a
// packet takes flowOut to reach the local address and flowBack to return. It
// waits at the local address for as long as the local address really took to
// answer, so slow endpoints hold their packets visibly longer.
const (
	flowOut   = 1100 * time.Millisecond
	flowBack  = 1100 * time.Millisecond
	flowFrame = 50 * time.Millisecond
	flowFlash = 250 * time.Millisecond
)

type frameMsg time.Time

func frame() tea.Cmd {
	return tea.Tick(flowFrame, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// ---------- canvas ----------

type pt struct{ x, y int }

const (
	bN uint8 = 1 << iota
	bE
	bS
	bW
)

var trackGlyph = map[uint8]rune{
	bE | bW: '─', bE: '─', bW: '─', bN | bS: '│', bN: '│', bS: '│',
	bS | bE: '╭', bS | bW: '╮', bN | bE: '╰', bN | bW: '╯',
	bN | bS | bE: '├', bN | bS | bW: '┤', bE | bW | bS: '┬', bE | bW | bN: '┴', bN | bE | bS | bW: '┼',
}

type cell struct {
	r    rune  // 0 draws the track glyph of bits
	bits uint8 // track connections
	c    string
	bold bool
}

// canvas is a grid of cells, for drawing what does not fit in boxes and rows.
// Every rune must be one cell wide.
type canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *canvas { return &canvas{w: w, h: h, cells: make([]cell, w*h)} }

func (cv *canvas) at(p pt) *cell {
	if p.x < 0 || p.y < 0 || p.x >= cv.w || p.y >= cv.h {
		return nil
	}
	return &cv.cells[p.y*cv.w+p.x]
}

func (cv *canvas) put(x, y int, r rune, c string, b bool) {
	if p := cv.at(pt{x, y}); p != nil {
		p.r, p.c, p.bold = r, c, b
	}
}

type span struct {
	s, c string
	b    bool
}

// spans writes spans from (x, y), cut with an ellipsis at maxW cells.
func (cv *canvas) spans(x, y, maxW int, ss ...span) {
	n := 0
	for _, s := range ss {
		n += len([]rune(s.s))
	}
	end := x + maxW
	for _, s := range ss {
		for _, r := range s.s {
			switch {
			case x >= end:
				return
			case n > maxW && x == end-1:
				r = '…'
			}
			cv.put(x, y, r, s.c, s.b)
			x++
		}
	}
}

// link draws a track along path in color c.
func (cv *canvas) link(path []pt, c string) {
	for i, p := range path {
		cl := cv.at(p)
		if cl == nil {
			continue
		}
		cl.c = c
		if i == 0 {
			continue
		}
		q := path[i-1]
		ql := cv.at(q)
		if ql == nil {
			continue
		}
		switch {
		case p.x == q.x+1:
			ql.bits |= bE
			cl.bits |= bW
		case p.x == q.x-1:
			ql.bits |= bW
			cl.bits |= bE
		case p.y == q.y+1:
			ql.bits |= bS
			cl.bits |= bN
		case p.y == q.y-1:
			ql.bits |= bN
			cl.bits |= bS
		}
	}
}

// box draws a rounded box with a title in its top border.
func (cv *canvas) box(x, y, w, h int, border string, ttl ...span) {
	for i := 1; i < w-1; i++ {
		cv.put(x+i, y, '─', border, false)
		cv.put(x+i, y+h-1, '─', border, false)
	}
	for j := 1; j < h-1; j++ {
		cv.put(x, y+j, '│', border, false)
		cv.put(x+w-1, y+j, '│', border, false)
		for i := 1; i < w-1; i++ {
			cv.put(x+i, y+j, ' ', "", false)
		}
	}
	cv.put(x, y, '╭', border, false)
	cv.put(x+w-1, y, '╮', border, false)
	cv.put(x, y+h-1, '╰', border, false)
	cv.put(x+w-1, y+h-1, '╯', border, false)
	if len(ttl) > 0 {
		cv.put(x+1, y, ' ', "", false)
		n := 0
		for _, s := range ttl {
			n += len([]rune(s.s))
		}
		n = min(n, w-5)
		cv.spans(x+2, y, n, ttl...)
		cv.put(x+2+n, y, ' ', "", false)
	}
}

func (cv *canvas) lines() []string {
	out := make([]string, cv.h)
	for y := 0; y < cv.h; y++ {
		var sb, run strings.Builder
		rc, rb := "", false
		flush := func() {
			switch {
			case run.Len() == 0:
			case rc == "":
				sb.WriteString(run.String())
			case rb:
				sb.WriteString(bold(rc).Render(run.String()))
			default:
				sb.WriteString(fg(rc).Render(run.String()))
			}
			run.Reset()
		}
		for x := 0; x < cv.w; x++ {
			c := cv.cells[y*cv.w+x]
			r := c.r
			if r == 0 && c.bits != 0 {
				r = trackGlyph[c.bits]
			}
			if r == 0 || r == ' ' {
				r, c.c, c.bold = ' ', "", false
			}
			if c.c != rc || c.bold != rb {
				flush()
				rc, rb = c.c, c.bold
			}
			run.WriteRune(r)
		}
		flush()
		out[y] = sb.String()
	}
	return out
}

func hseg(x0, x1, y int) []pt {
	step := 1
	if x1 < x0 {
		step = -1
	}
	var out []pt
	for x := x0; ; x += step {
		out = append(out, pt{x, y})
		if x == x1 {
			return out
		}
	}
}

func vseg(x, y0, y1 int) []pt {
	step := 1
	if y1 < y0 {
		step = -1
	}
	var out []pt
	for y := y0; ; y += step {
		out = append(out, pt{x, y})
		if y == y1 {
			return out
		}
	}
}

// chain joins segments that share their end points.
func chain(segs ...[]pt) []pt {
	var out []pt
	for _, s := range segs {
		if len(out) > 0 && len(s) > 0 && out[len(out)-1] == s[0] {
			s = s[1:]
		}
		out = append(out, s...)
	}
	return out
}

func reversed(p []pt) []pt {
	out := slices.Clone(p)
	slices.Reverse(out)
	return out
}

// packet is a request or response on its way along path.
type packet struct {
	path []pt
	frac float64
	c    string
	head rune
}

func (cv *canvas) tails(p packet) {
	i := int(math.Round(p.frac * float64(len(p.path)-1)))
	for k, fade := range []float64{0.3, 0.55, 0.75} {
		if j := i - k - 1; j >= 0 {
			if cl := cv.at(p.path[j]); cl != nil && cl.r == 0 {
				cl.c, cl.bold = mixHex(p.c, cBg, fade), true
			}
		}
	}
}

func (cv *canvas) head(p packet) {
	i := int(math.Round(p.frac * float64(len(p.path)-1)))
	cv.put(p.path[i].x, p.path[i].y, p.head, p.c, true)
}

// ---------- model ----------

// tcpSeen is the byte counts of a TCP connection at its last packet.
type tcpSeen struct {
	in, out int64
	at      time.Time
}

type tcpPacket struct {
	born time.Time
	hub  int
	out  bool // toward the local address
}

// flowSample turns the bytes TCP connections moved since the last call into
// packets.
func (m Model) flowSample(now time.Time) Model {
	keep := m.tcpPk[:0]
	for _, p := range m.tcpPk {
		if now.Sub(p.born) < flowOut {
			keep = append(keep, p)
		}
	}
	m.tcpPk = keep
	for i, s := range m.snaps {
		if s.Info.Protocol != "tcp" {
			continue
		}
		for _, c := range s.Conns {
			seen, ok := m.tcpSeen[c]
			in, out := c.In(), c.Out()
			if !ok {
				m.tcpSeen[c] = tcpSeen{in: in, out: out, at: now}
				if in > 0 {
					m.tcpPk = append(m.tcpPk, tcpPacket{born: now, hub: i, out: true})
				}
				continue
			}
			if now.Sub(seen.at) < 200*time.Millisecond || in == seen.in && out == seen.out {
				continue
			}
			if in > seen.in {
				m.tcpPk = append(m.tcpPk, tcpPacket{born: now, hub: i, out: true})
			}
			if out > seen.out {
				m.tcpPk = append(m.tcpPk, tcpPacket{born: now, hub: i})
			}
			m.tcpSeen[c] = tcpSeen{in: in, out: out, at: now}
		}
	}
	return m
}

// flowBusy reports whether anything moves on the Flow tab at now, so frames
// stop while there is no traffic.
func (m Model) flowBusy(now time.Time) bool {
	if len(m.tcpPk) > 0 {
		return true
	}
	for _, s := range m.snaps {
		if s.Info.Protocol == "tcp" && s.ConnOpen > 0 {
			return true
		}
		for _, r := range s.Requests {
			age := now.Sub(r.Start)
			if !r.Done || age < flowOut+r.Duration+flowBack {
				return true
			}
			if age > 2*time.Minute {
				break
			}
		}
	}
	return false
}

// ---------- view ----------

type flowUp struct {
	hub    int
	route  string // "" for the default address
	addr   string
	path   []pt // from the client node to this address
	cy     int
	active int     // requests waiting for an answer, or open connections
	rate   float64 // requests per second over the last 10 seconds
	durs   []time.Duration
	flash  string // the color of a packet that just arrived
	more   int    // addresses folded into this one for lack of room
}

func pathOf(p string) string {
	if u, err := url.ParseRequestURI(p); err == nil {
		return u.Path
	}
	path, _, _ := strings.Cut(p, "?")
	return path
}

func fmtRate(r float64) string {
	if r >= 10 || r == 0 {
		return fmt.Sprintf("%.0f", r)
	}
	return fmt.Sprintf("%.1f", r)
}

func (m Model) viewFlow(h int) string {
	now := m.fnow
	if now.Before(m.now) {
		now = m.now
	}
	bottomH := 0
	if h >= 30 {
		bottomH = 11
	}
	gbH := h - bottomH
	iw := m.w - 4
	gh := gbH - 3
	cv := newCanvas(iw, gh)

	// The local addresses of every tunnel, and the requests going to each.
	var ups []flowUp
	index := map[string]int{}
	for i, s := range m.snaps {
		index[fmt.Sprintf("%d ", i)] = len(ups)
		ups = append(ups, flowUp{hub: i, addr: s.Info.ForwardAddr})
		for _, r := range s.Info.Routes {
			index[fmt.Sprintf("%d %s", i, r[0])] = len(ups)
			ups = append(ups, flowUp{hub: i, route: r[0], addr: r[1]})
		}
	}
	fit := max(1, (gh+1)/5)
	if len(ups) > fit {
		ups[fit-1].more = len(ups) - fit + 1
	}
	slot := func(i int) int { return min(i, fit-1) }
	shown := min(len(ups), fit)

	// Layout: four columns of nodes with links between them.
	nw, uw := 19, 26
	narrow := iw < 100
	perSec := " req/s"
	if narrow {
		nw, uw, perSec = 14, 18, "/s"
	}
	gaps := iw - 3*nw - uw
	e1 := gaps * 32 / 100
	e2 := gaps * 32 / 100
	vx := 0
	px := vx + nw + e1
	cx := px + nw + e2
	ux := iw - uw
	stack := shown*4 + (shown-1)*1
	y0 := max(0, (gh-stack)/2)
	for i := 0; i < shown; i++ {
		ups[i].cy = y0 + i*5 + 1
	}
	mid := (ups[0].cy + ups[shown-1].cy) / 2
	mid = max(2, min(gh-3, mid))
	ny := mid - 2

	// Traffic of the last seconds, per local address.
	var total, totalRate float64
	clients := map[string]bool{}
	var packets []packet
	laneOut1 := hseg(vx+nw, px-1, mid-1)
	laneOut2 := hseg(px+nw, cx-1, mid-1)
	laneBack1 := hseg(px-1, vx+nw, mid+1)
	laneBack2 := hseg(cx-1, px+nw, mid+1)
	bx := cx + nw + max(2, (ux-cx-nw)/3)
	for i := 0; i < shown; i++ {
		ups[i].path = chain(hseg(cx+nw, bx, mid), vseg(bx, mid, ups[i].cy), hseg(bx, ux-1, ups[i].cy))
	}
	var visitorFlash, clientFlash string

	for hi, s := range m.snaps {
		for _, r := range s.Requests {
			age := now.Sub(r.Start)
			if age > 2*time.Minute && r.Done {
				break
			}
			ui, ok := index[fmt.Sprintf("%d %s", hi, r.Route)]
			if !ok {
				continue
			}
			u := &ups[slot(ui)]
			if age < 10*time.Second {
				u.rate += 0.1
				total += 0.1
			}
			if age < time.Minute {
				if r.ReplayOf == 0 {
					clients[r.Client] = true
				}
				if r.Done && r.Kind == "" {
					u.durs = append(u.durs, r.Duration)
				}
			}
			replay := r.ReplayOf != 0
			out := chain(laneOut1, laneOut2, u.path)
			back := chain(reversed(u.path), laneBack2, laneBack1)
			if replay {
				out, back = u.path, reversed(u.path)
			}
			c := methodHex(r.Method)
			if replay {
				c = cPeach
			}
			if age < flowOut {
				packets = append(packets, packet{path: out, frac: float64(age) / float64(flowOut), c: c, head: '●'})
				if age < flowOut/3 && !replay {
					visitorFlash = c
				}
				continue
			}
			wait := r.Duration
			if r.Kind != "" || !r.Done {
				wait = r.TTFB
			}
			t := age - flowOut
			if t < flowFlash {
				u.flash = c
			}
			switch {
			case r.Kind != "" && !r.Done:
				// An open stream: events keep flowing back.
				u.active++
				phase := t % (flowBack + 400*time.Millisecond)
				if phase < flowBack {
					packets = append(packets, packet{path: back, frac: float64(phase) / float64(flowBack), c: cCyan, head: '•'})
				}
				continue
			case r.Status == 0 || t < wait:
				u.active++
				continue
			}
			tb := t - wait
			if tb >= flowBack {
				continue
			}
			head := '●'
			switch {
			case r.Trace.ConnectErr != "":
				head = '✕'
			case r.RespSize >= 50_000:
				head = '◆'
			}
			sc := statusHex(r.Status)
			packets = append(packets, packet{path: back, frac: float64(tb) / float64(flowBack), c: sc, head: head})
			if tb > flowBack*3/4 {
				if replay {
					clientFlash = sc
				} else {
					visitorFlash = sc
				}
			}
		}
		if s.Info.Protocol == "tcp" {
			u := &ups[slot(index[fmt.Sprintf("%d ", hi)])]
			u.active = s.ConnOpen
		}
	}
	for _, p := range m.tcpPk {
		u := &ups[slot(index[fmt.Sprintf("%d ", p.hub)])]
		age := now.Sub(p.born)
		if p.out {
			packets = append(packets, packet{path: chain(laneOut1, laneOut2, u.path), frac: float64(age) / float64(flowOut), c: cPeach, head: '■'})
		} else {
			packets = append(packets, packet{path: chain(reversed(u.path), laneBack2, laneBack1), frac: float64(age) / float64(flowOut), c: cLav, head: '■'})
		}
	}
	totalRate = total

	// Links, colored by load.
	load := func(r float64) string { return mixHex(cBorder, cLavDeep, math.Min(1, r/8)) }
	cv.link(laneOut1, load(totalRate))
	cv.link(laneOut2, load(totalRate))
	cv.link(laneBack1, load(totalRate))
	cv.link(laneBack2, load(totalRate))
	for i := 0; i < shown; i++ {
		cv.link(ups[i].path, load(ups[i].rate))
	}
	arrow := func(x, y int, r rune) { cv.put(x, y, r, cMuted, false) }
	arrow(px-1, mid-1, '▸')
	arrow(cx-1, mid-1, '▸')
	arrow(vx+nw, mid+1, '◂')
	arrow(px+nw, mid+1, '◂')
	for i := 0; i < shown; i++ {
		arrow(ux-1, ups[i].cy, '▸')
	}

	// Edge labels between the lanes.
	label := func(x0, x1 int, ss ...span) {
		n := 0
		for _, s := range ss {
			n += len([]rune(s.s))
		}
		cv.spans(x0+max(0, (x1-x0-n)/2), mid, x1-x0, ss...)
	}
	label(vx+nw, px, span{fmtRate(totalRate), cBright, true}, span{perSec, cMuted, false})
	bytesNow := 0.0
	for i := range m.snaps {
		if t := m.throughput[i]; len(t) > 0 {
			bytesNow += t[len(t)-1]
		}
	}
	label(px+nw, cx, span{shortBytes(bytesNow) + "B/s", cText, false})
	// Name the links above them.
	above := func(x0, x1 int, names ...string) {
		for _, s := range names {
			if len(s) <= x1-x0 {
				cv.spans(x0+(x1-x0-len(s))/2, mid-2, x1-x0, span{s, cMuted, false})
				return
			}
		}
	}
	above(vx+nw, px, "public")
	above(px+nw, cx, "ssh tunnel", "ssh")

	// Nodes.
	border := func(flash string) string {
		if flash != "" {
			return flash
		}
		return cLavDeep
	}
	inner := nw - 4
	cv.box(vx, ny, nw, 5, border(visitorFlash), span{"Internet", cBright, true})
	visitors := " visitors"
	if len(clients) == 1 {
		visitors = " visitor"
	}
	cv.spans(vx+2, ny+1, inner, span{fmt.Sprint(len(clients)), cBright, true}, span{visitors, cMuted, false})
	cv.spans(vx+2, ny+2, inner, span{fmtRate(totalRate), cBright, true}, span{" req/s", cMuted, false})
	cv.spans(vx+2, ny+3, inner, span{"last 60s", cMuted, false})

	online := 0
	var rtt time.Duration
	for _, s := range m.snaps {
		if s.Status == StatusOnline {
			online++
			rtt = max(rtt, s.RTT)
		}
	}
	srvBorder := cLavDeep
	status := span{"● online", cGreen, false}
	if online < len(m.snaps) {
		status = span{fmt.Sprintf("◌ %d/%d online", online, len(m.snaps)), cYellow, false}
		srvBorder = cYellow
	}
	cv.box(px, ny, nw, 5, srvBorder, span{"pgrokd", cBright, true})
	cv.spans(px+2, ny+1, inner, span{m.snap.Info.RemoteAddr, cText, false})
	cv.spans(px+2, ny+2, inner, status)
	if rtt > 0 {
		cv.spans(px+2, ny+3, inner, span{"rtt ", cMuted, false}, span{fmtDur(rtt), cText, false})
	}

	active := 0
	for _, u := range ups {
		active += u.active
	}
	cv.box(cx, ny, nw, 5, border(clientFlash), span{"pgrok", cBright, true})
	cv.spans(cx+2, ny+1, inner, span{m.snap.Info.Version, cMuted, false})
	tunnels := m.snap.Info.Protocol + " tunnel"
	if m.multi() {
		tunnels = fmt.Sprintf("%d tunnels", len(m.snaps))
	}
	cv.spans(cx+2, ny+2, inner, span{tunnels, cText, false})
	spin := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")[now.UnixMilli()/80%10]
	if active > 0 {
		cv.spans(cx+2, ny+3, inner, span{string(spin) + " ", cPeach, true}, span{fmt.Sprint(active), cBright, true}, span{" active", cMuted, false})
	} else {
		cv.spans(cx+2, ny+3, inner, span{"idle", cMuted, false})
	}

	for i := 0; i < shown; i++ {
		u := ups[i]
		s := m.snaps[u.hub]
		ttl := []span{{shortAddr(u.addr), cBright, true}}
		if m.multi() {
			ttl = append(ttl, span{" · " + s.Info.Name, cLav, false})
		}
		if u.more > 1 {
			ttl = []span{{fmt.Sprintf("+%d more", u.more), cBright, true}}
		}
		cv.box(ux, u.cy-1, uw, 4, border(u.flash), ttl...)
		route := u.route
		switch {
		case u.more > 1:
			route = "others"
		case s.Info.Protocol == "tcp":
			route = "tcp"
		case route == "":
			route = "default"
		}
		if s.Info.Protocol == "tcp" {
			bps := 0.0
			if t := m.throughput[u.hub]; len(t) > 0 {
				bps = t[len(t)-1]
			}
			cv.spans(ux+2, u.cy, uw-4, span{route, cLav, false}, span{"  " + fmtBytes(int64(bps)), cBright, true}, span{"/s", cMuted, false})
			cv.spans(ux+2, u.cy+1, uw-4, span{fmt.Sprint(u.active), cBright, true}, span{" open", cMuted, false})
			continue
		}
		cv.spans(ux+2, u.cy, uw-4, span{route, cLav, false}, span{"  " + fmtRate(u.rate), cBright, true}, span{perSec, cMuted, false})
		var ss []span
		if len(u.durs) > 0 {
			slices.Sort(u.durs)
			ss = append(ss, span{"p50 ", cMuted, false}, span{fmtDur(u.durs[len(u.durs)/2]), cText, false})
		}
		if u.active > 0 {
			if len(ss) > 0 {
				ss = append(ss, span{" · ", cMuted, false})
			}
			ss = append(ss, span{string(spin) + " ", cPeach, true}, span{fmt.Sprint(u.active), cBright, true})
		}
		if len(ss) == 0 {
			ss = []span{{"idle", cMuted, false}}
		}
		cv.spans(ux+2, u.cy+1, uw-4, ss...)
	}

	// Packets: every tail first, so no tail hides another packet's head.
	for _, p := range packets {
		p.frac = math.Max(0, math.Min(1, p.frac))
		cv.tails(p)
	}
	for _, p := range packets {
		p.frac = math.Max(0, math.Min(1, p.frac))
		cv.head(p)
	}

	legend := muted("→ ") + fg(cLav).Render("●") + muted(" GET ") + fg(cPeach).Render("●") + muted(" POST ") +
		fg(cPurple).Render("●") + muted(" PUT/PATCH ") + fg(cRed).Render("●") + muted(" DELETE") +
		muted("    ← ") + fg(cGreen).Render("●") + muted(" 2xx ") + fg(cCyan).Render("●") + muted(" 3xx ") +
		fg(cYellow).Render("●") + muted(" 4xx ") + fg(cRed).Render("●") + muted(" 5xx ") + fg(cRed).Render("✕") + muted(" unreachable") +
		muted("    ") + fg(cPeach).Render("●") + muted(" replay ") + fg(cCyan).Render("•") + muted(" stream")
	lines := append(cv.lines(), legend)
	right := muted(fmt.Sprintf("%d in flight · ", len(packets))) + fg(cGreen).Render("● live")
	if !m.flowBusy(now) {
		right = muted("waiting for traffic")
	}
	g := box{W: m.w, H: gbH, Title: title("Traffic map"), Right: right, Lines: lines}.render()
	if bottomH == 0 {
		return g
	}
	lw := m.w * 2 / 5
	return vjoin(g, hjoin(m.flowRateBox(lw, bottomH), m.flowPathsBox(now, m.w-lw, bottomH)))
}

func (m Model) flowRateBox(w, h int) string {
	iw := w - 4
	vals := make([]float64, 60)
	for _, s := range m.snaps {
		for i, n := range s.PerSec {
			vals[i] += float64(n)
		}
	}
	vals = vals[max(0, len(vals)-iw):]
	top := slices.Max(vals)
	rows := h - 5
	chart := spark(vals, top, rows, func(x float64) string {
		if top > 0 && x >= top*0.8 {
			return cPeach
		}
		return cLav
	})
	var in, out int64
	for _, s := range m.snaps {
		in += s.BytesIn
		out += s.BytesOut
	}
	lines := []string{spread(muted("requests/sec · last 60s"), muted("peak ")+fg(cPeach).Render(fmt.Sprintf("%.0f", top)), iw)}
	lines = append(lines, chart...)
	lines = append(lines, "", muted("through the tunnel  ")+fg(cPeach).Render("↓ "+fmtBytes(in))+"   "+lav("↑ "+fmtBytes(out)))
	return box{W: w, H: h, Title: title("Throughput"), Lines: lines}.render()
}

func (m Model) flowPathsBox(now time.Time, w, h int) string {
	type hot struct {
		method, path string
		n, done      int
		dur          time.Duration
		errs         int
	}
	byKey := map[string]*hot{}
	for _, s := range m.snaps {
		for _, r := range s.Requests {
			if now.Sub(r.Start) > time.Minute {
				break
			}
			k := r.Method + " " + pathOf(r.URI)
			e := byKey[k]
			if e == nil {
				e = &hot{method: r.Method, path: pathOf(r.URI)}
				byKey[k] = e
			}
			e.n++
			if r.Done {
				e.done++
				e.dur += r.Duration
			}
			if r.Status >= 400 {
				e.errs++
			}
		}
	}
	list := make([]*hot, 0, len(byKey))
	for _, e := range byKey {
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b *hot) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.path, b.path)
	})
	iw := w - 4
	const methodW, countW, durW = 8, 6, 8
	barW := max(4, iw/4)
	pathW := iw - methodW - barW - countW - durW - 2
	var lines []string
	if len(list) == 0 {
		lines = []string{"", center(muted("no requests in the last minute"), iw)}
	}
	most := 1
	if len(list) > 0 {
		most = list[0].n
	}
	for _, e := range list {
		if len(lines) >= h-2 {
			break
		}
		n := max(1, e.n*barW/most)
		bc := cLav
		if e.errs*2 > e.n {
			bc = cYellow
		}
		avg := "—"
		if e.done > 0 {
			avg = fmtDur(e.dur / time.Duration(e.done))
		}
		lines = append(lines, bold(methodHex(e.method)).Render(fit(e.method, methodW))+text(fit(sanitize(e.path), pathW))+"  "+
			blk(bc, n)+sp(barW-n)+fitR(text(comma(e.n)), countW)+fitR(muted(avg), durW))
	}
	return box{W: w, H: h, Title: title("Top paths"), Right: muted("last 60s · avg"), Lines: lines}.render()
}
