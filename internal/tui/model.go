package tui

import (
	"context"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/anim"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	tabOverview = "Overview"
	tabRequests = "Requests"
	tabTunnels  = "Tunnels"
	tabFlow     = "Flow"
	tabLogs     = "Logs"
)

// nodeID names the nodes the model finds again, for focus, scrolling and
// where the share dialog opens from.
type nodeID string

const (
	idFilter   nodeID = "filter"
	idOvList   nodeID = "overview-list"
	idReqList  nodeID = "requests-list"
	idTunList  nodeID = "tunnels-list"
	idLogs     nodeID = "logs"
	idBody     nodeID = "body"
	idShareKey nodeID = "share-key"
	idHelpKey  nodeID = "help-key"
	idFlow     nodeID = "flow"
	idURL      nodeID = "url"
	idConnList nodeID = "connections"
	idTunReqs  nodeID = "tunnel-requests"
	idTunConns nodeID = "tunnel-connections"
)

// lists are the lists whose scroll offsets the model keeps.
var lists = []nodeID{idOvList, idReqList, idConnList, idTunConns}

func tabID(name string) nodeID { return nodeID("tab " + name) }

// Model is the Bubble Tea model of the TUI. Use it through a pointer: the
// mouse handlers in its layout change it.
type Model struct {
	ui    *bloom.Root
	focus bloom.Focus[nodeID]

	hubs  []*Hub
	logs  *Logs
	cur   int        // the tunnel shown on the Overview and Requests tabs
	snaps []Snapshot // one per hub
	snap  Snapshot   // snaps[cur]
	now   time.Time

	// view is the last view, shown again while nothing changed, so an idle
	// UI does not lay out its screen ten times a second.
	view  tea.View
	dirty bool

	w, h int

	tab          string
	detail       bool // showing the selected request in full
	detailTab    int  // 0 request, 1 response
	body         *bloom.Buffer
	bodyOf       bodyKey // what body was filled for
	bodyInfo     string
	bodyNumbered bool
	bodyPos      bloom.ViewportPos

	ovSel  int // selected request ID on the overview, 0 follows the newest
	reqSel int // selected request ID on the Requests tab, 0 follows the newest
	tunSel int // selected row on the Tunnels tab
	offs   map[nodeID]int

	filter textinput.Model

	levels     [4]bool // DEBU INFO WARN ERRO
	logBuf     *bloom.Buffer
	logVersion uint64
	logTotal   int // lines ever written when the buffer was last filled
	logLines   []string
	logPos     bloom.ViewportPos

	// throughput holds bytes per second over the last 60 seconds, per tunnel.
	throughput [][]float64
	lastBytes  []int64

	// Motion.
	spin       anim.Sequence // spinners, while a tunnel is not online
	help       anim.Presence
	share      anim.Presence
	shareFrom  image.Point // where the share dialog opens from
	toasts     []*toast
	ulX, ulW   anim.Value // the accent under the active tab
	ulReady    bool
	enter      anim.Value // the body fading in after switching tabs
	roll       anim.Value // the totals turning over
	total      int        // the total shown, updated once a second
	rollFrom   string
	glitch     anim.Value // the status badge after the status changed
	lastStatus Status
	pulse      anim.Sequence // the border of the reconnect banner

	// The Flow tab animates at its own frame rate while packets move.
	fnow      time.Time
	animating bool
	tcpSeen   map[*Conn]tcpSeen
	tcpPk     []tcpPacket
	// What the last frame of the traffic map drew, for the mouse.
	flowHits     []flowHit
	flowHover    image.Point
	flowHovering bool
	flowPackets  int
}

type toast struct {
	text     string
	color    string
	presence anim.Presence
}

type tickMsg time.Time

type replayDoneMsg struct {
	what string // the method and URI of the replayed request
	err  error
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// New returns the TUI model showing hubs, one per tunnel, and logs.
func New(hubs []*Hub, logs *Logs) *Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type to filter…  e.g. status:>=400 method:POST /api"
	st := textinput.DefaultDarkStyles()
	st.Focused.Text = fg(cBright)
	st.Blurred.Text = fg(cText)
	st.Focused.Placeholder = fg(cMuted)
	st.Blurred.Placeholder = st.Focused.Placeholder
	ti.SetStyles(st)
	ti.SetVirtualCursor(false) // bloom places the terminal's own cursor
	ti.SetWidth(200)

	spring := []anim.Option{anim.Spring(16, 0.85)}
	m := &Model{
		hubs:       hubs,
		logs:       logs,
		now:        time.Now(),
		w:          120,
		h:          36,
		tab:        tabOverview,
		filter:     ti,
		levels:     [4]bool{true, true, true, true},
		logBuf:     bloom.NewBuffer(""),
		logPos:     bloom.ViewportEnd,
		body:       bloom.NewBuffer(""),
		offs:       map[nodeID]int{},
		throughput: make([][]float64, len(hubs)),
		lastBytes:  make([]int64, len(hubs)),
		tcpSeen:    map[*Conn]tcpSeen{},
		dirty:      true,

		spin:   anim.NewLoop(800*time.Millisecond, anim.FPS(12)),
		help:   anim.NewPresence(anim.Duration(200 * time.Millisecond)),
		share:  anim.NewPresence(anim.Duration(320 * time.Millisecond)),
		ulX:    anim.NewValue(0, spring...),
		ulW:    anim.NewValue(0, spring...),
		enter:  anim.NewValue(1, anim.Duration(180*time.Millisecond)),
		roll:   anim.NewValue(1, anim.Duration(600*time.Millisecond), anim.Ease(anim.Linear), anim.FPS(30)),
		glitch: anim.NewValue(1, anim.Duration(500*time.Millisecond), anim.Ease(anim.Linear), anim.FPS(30)),
		pulse:  anim.NewLoop(1600*time.Millisecond, anim.FPS(20)),
	}
	m.ui = bloom.New(
		bloom.WithColors(lipgloss.Color(cText), lipgloss.Color(cBg)),
		bloom.WithFocus(&m.focus),
		bloom.WithTooltips(bloom.Tooltips{
			Style: lipgloss.NewStyle().Foreground(lipgloss.Color(cBright)).Background(lipgloss.Color(cSel)).Padding(0, 1),
		}),
	)
	for i := range m.throughput {
		m.throughput[i] = make([]float64, 60)
	}
	if m.multi() {
		m.tab = tabTunnels
	}
	m.refresh()
	m.total = m.totalCount()
	m.lastStatus = m.snap.Status
	return m
}

// refresh takes new snapshots of every hub and the logs.
func (m *Model) refresh() {
	m.snaps = make([]Snapshot, len(m.hubs))
	for i, h := range m.hubs {
		m.snaps[i] = h.Snapshot(m.now)
	}
	m.snap = m.snaps[m.cur]
	v, total, lines := m.logs.snapshot()
	if v == m.logVersion {
		return
	}
	added := min(total-m.logTotal, len(lines))
	m.logVersion, m.logTotal, m.logLines = v, total, lines
	// Append the new lines, so what is selected in the log stays selected.
	// The buffer is filled again only once it is twice as long as the log.
	if m.logBuf.Len()+added > 2*maxLogLines {
		m.fillLogs()
		return
	}
	m.logBuf.Append(shownLogs(lines[len(lines)-added:], m.levels)...)
}

// shownLogs returns the lines of the levels shown.
func shownLogs(lines []string, levels [4]bool) []string {
	shown := make([]string, 0, len(lines))
	for _, l := range lines {
		if levels[logLevel(l)] {
			shown = append(shown, l)
		}
	}
	return shown
}

// fillLogs puts the log lines of the shown levels in the log buffer.
func (m *Model) fillLogs() { m.logBuf.SetLines(shownLogs(m.logLines, m.levels)...) }

// stale reports whether any hub or the logs changed since the last refresh.
func (m *Model) stale() bool {
	for i, h := range m.hubs {
		if h.Version() != m.snaps[i].Version {
			return true
		}
	}
	return m.logs.currentVersion() != m.logVersion
}

func (m *Model) multi() bool { return len(m.hubs) > 1 }

func (m *Model) tabs() []string {
	tabs := []string{tabOverview}
	if m.snap.Info.Protocol == "http" {
		tabs = append(tabs, tabRequests)
	}
	tabs = append(tabs, tabFlow)
	if m.multi() {
		tabs = append(tabs, tabTunnels)
	}
	return append(tabs, tabLogs)
}

// connecting reports whether every tunnel is still on its first attempt.
func (m *Model) connecting() bool {
	for _, s := range m.snaps {
		if s.Status != StatusConnecting || s.Attempt > 0 {
			return false
		}
	}
	return true
}

func (m *Model) allOnline() bool {
	for _, s := range m.snaps {
		if s.Status != StatusOnline {
			return false
		}
	}
	return true
}

func (m *Model) anyReconnecting() bool {
	for _, s := range m.snaps {
		if s.Status == StatusReconnecting {
			return true
		}
	}
	return false
}

func (m *Model) totalCount() int {
	if m.snap.Info.Protocol == "tcp" {
		return m.snap.ConnTotal
	}
	return m.snap.ReqTotal
}

func (m *Model) filtering() bool { return m.focus.Is(idFilter) }

func (m *Model) modalOpen() bool { return m.help.Shown() || m.share.Shown() }

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.ui.Init(), tick(), m.spin.Loop())
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := []tea.Cmd{m.ui.Update(msg)}
	switch msg := msg.(type) {
	case tickMsg:
		cmds = append(cmds, m.onTick(time.Time(msg)))
	case frameMsg:
		cmds = append(cmds, m.onFrame(time.Time(msg)))
	default:
		// Keys, the mouse, animation frames and the terminal's replies can
		// all change what is on screen.
		m.dirty = true
	}

	// Lists scroll from where the last frame left them.
	for _, id := range lists {
		if v, ok := m.ui.Frame().List(id); ok {
			m.offs[id] = v.First
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case replayDoneMsg:
		if msg.err != nil {
			cmds = append(cmds, m.notify("replay failed: "+msg.err.Error(), cRed))
		} else {
			cmds = append(cmds, m.notify("replayed "+msg.what, cGreen))
		}
	case tea.KeyPressMsg:
		cmds = append(cmds, m.onKey(msg))
	case tea.PasteMsg:
		if m.filtering() {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	// A click on the filter focuses it, and a click elsewhere blurs it.
	if m.filtering() && !m.filter.Focused() {
		cmds = append(cmds, m.filter.Focus())
	} else if !m.filtering() && m.filter.Focused() {
		m.filter.Blur()
	}

	for _, t := range m.toasts {
		cmds = append(cmds, t.presence.Update(msg))
	}
	m.toasts = slices.DeleteFunc(m.toasts, func(t *toast) bool { return !t.presence.Visible() })
	if m.detail {
		m.syncBody()
	}
	cmds = append(cmds,
		anim.Update(msg, &m.spin, &m.help, &m.share, &m.ulX, &m.ulW, &m.enter, &m.roll, &m.glitch, &m.pulse),
		m.syncUnderline(),
	)
	if m.tab == tabFlow && !m.animating && m.flowBusy(m.now) {
		m.animating = true
		cmds = append(cmds, frame())
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) onTick(now time.Time) tea.Cmd {
	newSecond := now.Unix() != m.now.Unix()
	if !newSecond && !m.stale() {
		return tick()
	}
	m.dirty = true
	m.now = now
	m.refresh()
	cmds := []tea.Cmd{tick()}
	if m.tab == tabFlow {
		m.flowSample(now)
	}
	if newSecond {
		for i, s := range m.snaps {
			total := s.BytesIn + s.BytesOut
			m.throughput[i] = append(m.throughput[i][1:], float64(max(0, total-m.lastBytes[i])))
			m.lastBytes[i] = total
		}
		if t := m.totalCount(); t != m.total {
			m.rollFrom, m.total = comma(m.total), t
			cmds = append(cmds, m.roll.Play(0, 1))
		}
	}
	if s := m.snap.Status; s != m.lastStatus {
		m.lastStatus = s
		cmds = append(cmds, m.glitch.Play(0, 1))
	}
	// Spinners and the reconnect banner move only while a tunnel is down.
	switch {
	case m.allOnline():
		m.spin.Stop()
		m.pulse.Stop()
	default:
		if !m.spin.Animating() {
			cmds = append(cmds, m.spin.Loop())
		}
		if m.snap.Status == StatusReconnecting && !m.pulse.Animating() {
			cmds = append(cmds, m.pulse.Loop())
		}
	}
	return tea.Batch(cmds...)
}

// syncUnderline sends the accent under the tabs to the active tab, from
// where the last frame drew it.
func (m *Model) syncUnderline() tea.Cmd {
	f := m.ui.Frame()
	if f == nil {
		return nil
	}
	r, ok := f.Rect(tabID(m.activeTab()))
	if !ok {
		return nil
	}
	x, w := float64(r.Min.X), float64(r.Dx())
	if m.ulReady && m.ulX.Target() == x && m.ulW.Target() == w {
		return nil
	}
	if !m.ulReady {
		m.ulReady, m.dirty = true, true
		m.ulX.Set(x)
		m.ulW.Set(w)
		return nil
	}
	return tea.Batch(m.ulX.To(x), m.ulW.To(w))
}

func (m *Model) activeTab() string {
	if m.detail {
		return tabRequests
	}
	return m.tab
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	if !m.dirty {
		return m.view
	}
	m.dirty = false
	v := m.ui.View(m.w, m.h, m.layout())
	v.AltScreen = true
	v.ReportFocus = true
	v.WindowTitle = "pgrok"
	if m.snap.HostURL != "" {
		v.WindowTitle = "pgrok · " + m.snap.HostURL
	}
	m.view = v
	return v
}

// notify shows a toast for a few seconds.
func (m *Model) notify(text, color string) tea.Cmd {
	t := &toast{text: text, color: color, presence: anim.NewPresence(anim.Duration(250 * time.Millisecond))}
	m.toasts = append(m.toasts, t)
	if len(m.toasts) > 3 {
		m.toasts = m.toasts[len(m.toasts)-3:]
	}
	return t.presence.ShowFor(3 * time.Second)
}

func (m *Model) setTab(t string) tea.Cmd {
	if t == m.tab && !m.detail {
		return nil
	}
	m.tab, m.detail = t, false
	m.focus.Clear()
	return m.enter.Play(0, 1)
}

// setCur shows tunnel i on the Overview and Requests tabs.
func (m *Model) setCur(i int) tea.Cmd {
	m.cur, m.snap = i, m.snaps[i]
	m.ovSel, m.reqSel, m.detail = 0, 0, false
	m.focus.Clear()
	m.filter.SetValue("")
	m.total = m.totalCount()
	m.lastStatus = m.snap.Status
	return m.setTab(tabOverview)
}

// openShare opens the share dialog, growing from the node with ID from.
func (m *Model) openShare(from nodeID) tea.Cmd {
	if m.snap.HostURL == "" {
		return nil
	}
	m.shareFrom = image.Point{}
	if r, ok := m.ui.Frame().Rect(from); ok {
		m.shareFrom = image.Pt((r.Min.X+r.Max.X)/2, r.Min.Y)
	}
	return tea.Batch(m.help.Hide(), m.share.Show())
}

func (m *Model) openRequest(id int) tea.Cmd {
	m.setSel(id)
	m.detail, m.detailTab, m.bodyPos = true, 0, bloom.ViewportPos{}
	m.focus.Clear()
	return m.enter.Play(0, 1)
}

// replay sends the selected request again.
func (m *Model) replay() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if !r.Replayable() {
		return m.notify("this request cannot be replayed", cYellow)
	}
	hub := m.hubs[m.cur]
	what := r.Method + " " + r.URI
	return tea.Batch(m.notify("replaying "+what+"…", cPeach), func() tea.Msg {
		return replayDoneMsg{what: what, err: hub.Replay(context.Background(), r.ID)}
	})
}

func (m *Model) copyCurl() tea.Cmd {
	if r, ok := m.selected(); ok {
		return tea.Batch(m.notify("copied curl command", cGreen), tea.SetClipboard(curlCommand(m.snap.HostURL, r)))
	}
	return nil
}

func (m *Model) copyURL() tea.Cmd {
	if m.snap.HostURL == "" {
		return nil
	}
	return tea.Batch(m.notify("copied "+m.snap.HostURL, cGreen), tea.SetClipboard(m.snap.HostURL))
}

func (m *Model) retry() tea.Cmd {
	retried := false
	for i, s := range m.snaps {
		if s.Status == StatusReconnecting {
			m.hubs[i].requestRetry()
			retried = true
		}
	}
	if retried {
		return m.notify("reconnecting now", cYellow)
	}
	return nil
}

func (m *Model) toggleLevel(i int) {
	m.levels[i] = !m.levels[i]
	m.fillLogs()
}

func (m *Model) onKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.filtering() {
		switch key {
		case "esc":
			m.focus.Clear()
			m.filter.SetValue("")
			m.reqSel = 0
			return nil
		case "enter":
			m.focus.Clear()
			return nil
		case "up", "down":
			m.move(key)
			return nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		return cmd
	}
	return m.press(key)
}

// press runs what a key does, for key presses and for clicks on key hints.
func (m *Model) press(key string) tea.Cmd {
	if m.modalOpen() {
		switch {
		case key == "c" && m.share.Shown():
			return tea.Batch(m.share.Hide(), m.copyURL())
		case key == "esc" || key == "q" || key == "enter",
			key == "?" && m.help.Shown(),
			key == "s" && m.share.Shown():
			return tea.Batch(m.help.Hide(), m.share.Hide())
		}
		return nil
	}

	// Copy what the mouse selected, in the logs or a request's body.
	if key == "c" || key == "y" {
		if cmd := m.ui.CopySelection(); cmd != nil {
			return tea.Batch(cmd, m.notify("copied the selection", cGreen))
		}
	}

	if m.detail {
		switch key {
		case "esc", "backspace", "q":
			m.detail = false
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.detailTab = 1 - m.detailTab
			m.bodyPos = bloom.ViewportPos{}
		case "up", "k":
			m.scrollBody(-1)
		case "down", "j":
			m.scrollBody(1)
		case "pgup":
			m.scrollBody(-10)
		case "pgdown", "space":
			m.scrollBody(10)
		case "c":
			return m.copyCurl()
		case "r":
			return m.replay()
		}
		return nil
	}

	tabs := m.tabs()
	idx := slices.Index(tabs, m.tab)
	isHTTPList := (m.tab == tabOverview || m.tab == tabRequests) && m.snap.Info.Protocol == "http"
	switch key {
	case "q":
		return tea.Quit
	case "tab":
		return m.setTab(tabs[(idx+1)%len(tabs)])
	case "shift+tab":
		return m.setTab(tabs[(idx+len(tabs)-1)%len(tabs)])
	case "1", "2", "3", "4", "5":
		if i, _ := strconv.Atoi(key); i <= len(tabs) {
			return m.setTab(tabs[i-1])
		}
	case "?":
		return m.help.Show()
	case "s":
		return m.openShare(idShareKey)
	case "R":
		return m.retry()
	case "/":
		if m.snap.Info.Protocol == "http" {
			cmd := m.setTab(tabRequests)
			m.focus.Set(idFilter)
			return tea.Batch(cmd, m.filter.Focus())
		}
	case "esc":
		if m.tab == tabRequests && m.filter.Value() != "" {
			m.filter.SetValue("")
			m.reqSel = 0
		}
	case "up", "k", "down", "j":
		m.move(key)
	case "pgup", "pgdown":
		if m.tab == tabLogs {
			m.scrollLogs(map[string]int{"pgup": -10, "pgdown": 10}[key])
		}
	case "enter":
		switch {
		case m.tab == tabTunnels:
			return m.setCur(m.tunSel)
		case isHTTPList:
			if r, ok := m.selected(); ok {
				// Pin the selection so new requests do not replace it.
				return m.openRequest(r.ID)
			}
		}
	case "r":
		if isHTTPList {
			return m.replay()
		}
	case "c":
		if isHTTPList {
			return m.copyCurl()
		}
	case "d", "i", "w", "e":
		if m.tab == tabLogs {
			m.toggleLevel(strings.Index("diwe", key))
		}
	case "f":
		if m.tab == tabLogs {
			m.toggleFollow()
		}
	}
	return nil
}

func (m *Model) toggleFollow() {
	if m.logPos == bloom.ViewportEnd {
		if v, ok := m.ui.Frame().Viewport(idLogs); ok {
			m.logPos = v.Pos
			if m.logPos == bloom.ViewportEnd {
				m.logPos = bloom.ViewportPos{Line: v.First, Row: v.Row}
			}
		} else {
			m.logPos = bloom.ViewportPos{}
		}
		return
	}
	m.logPos = bloom.ViewportEnd
}

func (m *Model) scrollLogs(n int) {
	v, ok := m.ui.Frame().Viewport(idLogs)
	if !ok {
		return
	}
	switch {
	case n < 0:
		m.logPos = v.Up(-n)
	default:
		m.logPos = v.Down(n)
		if m.logPos.AtEnd() || v.AtBottom() && n > 0 {
			m.logPos = bloom.ViewportEnd // back at the end: follow again
		}
	}
}

func (m *Model) scrollBody(n int) {
	v, ok := m.ui.Frame().Viewport(idBody)
	if !ok {
		return
	}
	if n < 0 {
		m.bodyPos = v.Up(-n)
	} else {
		m.bodyPos = v.Down(n)
	}
}

// list returns the requests shown on the current tab, newest first.
func (m *Model) list() []Request {
	if m.tab == tabRequests {
		return filterRequests(m.snap.Requests, m.filter.Value())
	}
	return m.snap.Requests
}

func (m *Model) selID() int {
	if m.tab == tabRequests {
		return m.reqSel
	}
	return m.ovSel
}

func (m *Model) setSel(id int) {
	if m.tab == tabRequests {
		m.reqSel = id
	} else {
		m.ovSel = id
	}
}

// selIndex returns the index of the selected request in rows.
func selIndex(rows []Request, id int) int {
	for i, r := range rows {
		if r.ID == id {
			return i
		}
	}
	return 0
}

func (m *Model) selected() (Request, bool) {
	rows := m.list()
	if len(rows) == 0 {
		return Request{}, false
	}
	return rows[selIndex(rows, m.selID())], true
}

// selectIndex selects row i of the requests on the current tab.
func (m *Model) selectIndex(rows []Request, i int) {
	if i <= 0 {
		m.setSel(0) // back at the top: follow new requests again
		return
	}
	m.setSel(rows[min(i, len(rows)-1)].ID)
}

func (m *Model) move(key string) {
	d := 1
	if key == "up" || key == "k" {
		d = -1
	}
	m.moveBy(d)
}

func (m *Model) moveBy(d int) {
	switch m.tab {
	case tabLogs:
		m.scrollLogs(d)
		return
	case tabTunnels:
		m.tunSel = max(0, min(len(m.hubs)-1, m.tunSel+d))
		return
	}
	rows := m.list()
	if len(rows) == 0 {
		return
	}
	m.selectIndex(rows, max(0, min(len(rows)-1, selIndex(rows, m.selID())+d)))
}

// filterRequests keeps the requests matching every space-separated term of q:
// status:>=400, status:4xx, status:404, method:POST, or text in the path.
func filterRequests(rows []Request, q string) []Request {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return rows
	}
	var out []Request
	for _, r := range rows {
		if matchAll(r, terms) {
			out = append(out, r)
		}
	}
	return out
}

func matchAll(r Request, terms []string) bool {
	for _, t := range terms {
		switch {
		case strings.HasPrefix(t, "status:"):
			if !matchStatus(r.Status, strings.TrimPrefix(t, "status:")) {
				return false
			}
		case strings.HasPrefix(t, "method:"):
			if !strings.EqualFold(r.Method, strings.TrimPrefix(t, "method:")) {
				return false
			}
		default:
			if !strings.Contains(strings.ToLower(r.URI), strings.ToLower(t)) {
				return false
			}
		}
	}
	return true
}

func matchStatus(status int, e string) bool {
	for _, op := range []string{">=", "<=", ">", "<", "="} {
		if !strings.HasPrefix(e, op) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(e, op))
		if err != nil {
			return true // still typing: do not hide everything
		}
		switch op {
		case ">=":
			return status >= n
		case "<=":
			return status <= n
		case ">":
			return status > n
		case "<":
			return status < n
		}
		return status == n
	}
	if len(e) == 3 && strings.HasSuffix(strings.ToLower(e), "xx") {
		return status/100 == int(e[0]-'0')
	}
	if n, err := strconv.Atoi(e); err == nil {
		return status == n
	}
	return true
}
