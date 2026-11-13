package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	tabOverview = "Overview"
	tabRequests = "Requests"
	tabTunnels  = "Tunnels"
	tabLogs     = "Logs"
)

// Model is the Bubble Tea model of the TUI.
type Model struct {
	hubs  []*Hub
	logs  *Logs
	cur   int        // the tunnel shown on the Overview and Requests tabs
	snaps []Snapshot // one per hub
	snap  Snapshot   // snaps[cur]
	now   time.Time
	out   string // the rendered screen, refreshed only when something changed

	logLines   []string
	logVersion uint64

	w, h int

	tab       string
	detail    bool // showing the selected request in full
	detailTab int  // 0 request, 1 response
	scroll    int  // body scroll in the detail view
	modal     string

	ovSel  int // selected request ID on the overview, 0 follows the newest
	reqSel int // selected request ID on the Requests tab, 0 follows the newest
	tunSel int // selected row on the Tunnels tab

	filter    textinput.Model
	filtering bool

	levels    [4]bool // DEBU INFO WARN ERRO
	follow    bool
	logScroll int

	// throughput holds bytes per second over the last 60 seconds, per tunnel.
	throughput [][]float64
	lastBytes  []int64

	spin     spinner.Model
	spinning bool

	flash      string
	flashUntil time.Time
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
func New(hubs []*Hub, logs *Logs) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type to filter…  e.g. status:>=400 method:POST /api"
	st := textinput.DefaultDarkStyles()
	st.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(cBright))
	st.Blurred.Text = lipgloss.NewStyle().Foreground(lipgloss.Color(cText))
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(cMuted))
	st.Blurred.Placeholder = st.Focused.Placeholder
	st.Cursor.Color = lipgloss.Color(cPeach)
	ti.SetStyles(st)
	ti.SetWidth(60)

	m := Model{
		hubs:       hubs,
		logs:       logs,
		now:        time.Now(),
		w:          120,
		h:          36,
		tab:        tabOverview,
		filter:     ti,
		levels:     [4]bool{true, true, true, true},
		follow:     true,
		throughput: make([][]float64, len(hubs)),
		lastBytes:  make([]int64, len(hubs)),
		spin: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(cPeach))),
		),
	}
	for i := range m.throughput {
		m.throughput[i] = make([]float64, 60)
	}
	if m.multi() {
		m.tab = tabTunnels
	}
	m.refresh()
	m.out = m.render()
	return m
}

// refresh takes new snapshots of every hub and the logs.
func (m *Model) refresh() {
	m.snaps = make([]Snapshot, len(m.hubs))
	for i, h := range m.hubs {
		m.snaps[i] = h.Snapshot(m.now)
	}
	m.snap = m.snaps[m.cur]
	m.logVersion, m.logLines = m.logs.snapshot()
}

// stale reports whether any hub or the logs changed since the last refresh.
func (m Model) stale() bool {
	for i, h := range m.hubs {
		if h.Version() != m.snaps[i].Version {
			return true
		}
	}
	return m.logs.currentVersion() != m.logVersion
}

func (m Model) multi() bool { return len(m.hubs) > 1 }

func (m Model) tabs() []string {
	tabs := []string{tabOverview}
	if m.snap.Info.Protocol == "http" {
		tabs = append(tabs, tabRequests)
	}
	if m.multi() {
		tabs = append(tabs, tabTunnels)
	}
	return append(tabs, tabLogs)
}

// connecting reports whether every tunnel is still on its first attempt.
func (m Model) connecting() bool {
	for _, s := range m.snaps {
		if s.Status != StatusConnecting || s.Attempt > 0 {
			return false
		}
	}
	return true
}

func (m Model) needsSpinner() bool {
	for _, s := range m.snaps {
		if s.Status != StatusOnline {
			return true
		}
	}
	return false
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.spin.Tick)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd, dirty := m.update(msg)
	if dirty {
		nm.out = nm.render()
	}
	return nm, cmd
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.out)
	v.AltScreen = true
	v.WindowTitle = "pgrok"
	if m.snap.HostURL != "" {
		v.WindowTitle = "pgrok · " + m.snap.HostURL
	}
	return v
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.filter.SetWidth(max(10, m.w-50))
		return m, nil, true

	case tickMsg:
		now := time.Time(msg)
		newSecond := now.Unix() != m.now.Unix()
		if !newSecond && !m.stale() {
			return m, tick(), false
		}
		m.now = now
		m.refresh()
		if newSecond {
			for i, s := range m.snaps {
				total := s.BytesIn + s.BytesOut
				m.throughput[i] = append(m.throughput[i][1:], float64(max(0, total-m.lastBytes[i])))
				m.lastBytes[i] = total
			}
			if !m.flashUntil.IsZero() && now.After(m.flashUntil) {
				m.flash, m.flashUntil = "", time.Time{}
			}
		}
		cmds := []tea.Cmd{tick()}
		if m.needsSpinner() && !m.spinning {
			m.spinning = true
			cmds = append(cmds, m.spin.Tick)
		}
		return m, tea.Batch(cmds...), true

	case spinner.TickMsg:
		// Let the tick chain stop while no spinner is on screen, so an idle
		// UI does not redraw several times a second.
		if !m.needsSpinner() {
			m.spinning = false
			return m, nil, false
		}
		m.spinning = true
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd, true

	case replayDoneMsg:
		if msg.err != nil {
			return m.setFlash("replay failed: " + msg.err.Error()), nil, true
		}
		return m.setFlash("replayed " + msg.what), nil, true

	case tea.KeyPressMsg:
		return m.onKey(msg)
	}

	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd, true
	}
	return m, nil, false
}

func (m Model) setFlash(s string) Model {
	m.flash, m.flashUntil = s, m.now.Add(3*time.Second)
	return m
}

// setCur shows tunnel i on the Overview and Requests tabs.
func (m Model) setCur(i int) Model {
	m.cur, m.snap = i, m.snaps[i]
	m.ovSel, m.reqSel, m.detail = 0, 0, false
	m.filtering = false
	m.filter.Blur()
	m.filter.SetValue("")
	return m
}

// replay sends the selected request again.
func (m Model) replay() (Model, tea.Cmd, bool) {
	r, ok := m.selected()
	if !ok {
		return m, nil, false
	}
	if !r.Replayable() {
		return m.setFlash("this request cannot be replayed"), nil, true
	}
	hub := m.hubs[m.cur]
	what := r.Method + " " + r.URI
	return m.setFlash("replaying " + what + "…"), func() tea.Msg {
		return replayDoneMsg{what: what, err: hub.Replay(context.Background(), r.ID)}
	}, true
}

func (m Model) copyCurl() (Model, tea.Cmd, bool) {
	if r, ok := m.selected(); ok {
		return m.setFlash("copied curl command"), tea.SetClipboard(curlCommand(m.snap.HostURL, r)), true
	}
	return m, nil, false
}

func (m Model) onKey(k tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit, false
	}

	if m.modal != "" {
		switch key {
		case "esc", "q", "enter", "?", "s":
			if key == "s" && m.modal != "share" || key == "?" && m.modal != "help" {
				return m, nil, false
			}
			m.modal = ""
		case "c":
			if m.modal == "share" {
				m.modal = ""
				return m.setFlash("copied " + m.snap.HostURL), tea.SetClipboard(m.snap.HostURL), true
			}
		}
		return m, nil, true
	}

	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.reqSel = 0
			return m, nil, true
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil, true
		case "up", "down":
			return m.move(key), nil, true
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		return m, cmd, true
	}

	if m.detail {
		switch key {
		case "esc", "backspace", "q":
			m.detail = false
		case "left", "right", "h", "l", "tab", "shift+tab":
			m.detailTab = 1 - m.detailTab
			m.scroll = 0
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		case "c":
			return m.copyCurl()
		case "r":
			return m.replay()
		}
		return m, nil, true
	}

	tabs := m.tabs()
	idx := 0
	for i, t := range tabs {
		if t == m.tab {
			idx = i
		}
	}
	isHTTPList := (m.tab == tabOverview || m.tab == tabRequests) && m.snap.Info.Protocol == "http"
	switch key {
	case "q":
		return m, tea.Quit, false
	case "tab":
		m.tab = tabs[(idx+1)%len(tabs)]
	case "shift+tab":
		m.tab = tabs[(idx+len(tabs)-1)%len(tabs)]
	case "1", "2", "3", "4":
		if i, _ := strconv.Atoi(key); i <= len(tabs) {
			m.tab = tabs[i-1]
		}
	case "?":
		m.modal = "help"
	case "s":
		if m.snap.HostURL != "" {
			m.modal = "share"
		}
	case "R":
		retried := false
		for i, s := range m.snaps {
			if s.Status == StatusReconnecting {
				m.hubs[i].requestRetry()
				retried = true
			}
		}
		if retried {
			return m.setFlash("reconnecting now"), nil, true
		}
	case "/":
		if m.snap.Info.Protocol == "http" {
			m.tab = tabRequests
			m.filtering = true
			return m, m.filter.Focus(), true
		}
	case "esc":
		if m.tab == tabRequests && m.filter.Value() != "" {
			m.filter.SetValue("")
			m.reqSel = 0
		}
	case "up", "k", "down", "j":
		return m.move(key), nil, true
	case "enter":
		switch {
		case m.tab == tabTunnels:
			m = m.setCur(m.tunSel)
			m.tab = tabOverview
		case isHTTPList:
			if r, ok := m.selected(); ok {
				// Pin the selection so new requests do not replace it.
				m.setSel(r.ID)
				m.detail, m.detailTab, m.scroll = true, 0, 0
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
			i := strings.Index("diwe", key)
			m.levels[i] = !m.levels[i]
		}
	case "f":
		if m.tab == tabLogs {
			m.follow = !m.follow
			m.logScroll = 0
		}
	}
	return m, nil, true
}

// list returns the requests shown on the current tab, newest first.
func (m Model) list() []Request {
	if m.tab == tabRequests {
		return filterRequests(m.snap.Requests, m.filter.Value())
	}
	return m.snap.Requests
}

func (m Model) selID() int {
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

func (m Model) selected() (Request, bool) {
	rows := m.list()
	if len(rows) == 0 {
		return Request{}, false
	}
	return rows[selIndex(rows, m.selID())], true
}

func (m Model) move(key string) Model {
	d := 1
	if key == "up" || key == "k" {
		d = -1
	}
	switch m.tab {
	case tabLogs:
		m.follow = false
		m.logScroll = max(0, m.logScroll-d)
		return m
	case tabTunnels:
		m.tunSel = max(0, min(len(m.hubs)-1, m.tunSel+d))
		return m
	}
	rows := m.list()
	if len(rows) == 0 {
		return m
	}
	i := max(0, min(len(rows)-1, selIndex(rows, m.selID())+d))
	if i == 0 {
		m.setSel(0) // back at the top: follow new requests again
	} else {
		m.setSel(rows[i].ID)
	}
	return m
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
