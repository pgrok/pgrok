package tui

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bloomfx.dev/bloom"
	"charm.land/lipgloss/v2"
)

// Colors from the pgrok banner: lavender wordmark and peach mascot.
const (
	cLav      = "#B9A3E8"
	cLavDeep  = "#7B5BB5"
	cLavLight = "#D4C6F4"
	cPeach    = "#F4A48C"
	cGreen    = "#9ECE6A"
	cCyan     = "#7DCFFF"
	cYellow   = "#E0AF68"
	cRed      = "#F7768E"
	cPurple   = "#BB9AF7"
	cMuted    = "#6B7089"
	cBorder   = "#3B3F55"
	cText     = "#C8C8D8"
	cBright   = "#ECECF4"
	cBg       = "#16161E"
	cSel      = "#2A2740"
	cTrack    = "#2A2C3C"
	cRaised   = "#1F1F2B"
)

func parseHex(h string) (r, g, b uint8) {
	_, _ = fmt.Sscanf(h, "#%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func mixHex(a, b string, t float64) string {
	ar, ag, ab := parseHex(a)
	br, bg, bb := parseHex(b)
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return fmt.Sprintf("#%02x%02x%02x", m(ar, br), m(ag, bg), m(ab, bb))
}

func fg(h string) lipgloss.Style   { return lipgloss.NewStyle().Foreground(lipgloss.Color(h)) }
func bold(h string) lipgloss.Style { return fg(h).Bold(true) }

// chip renders text in the background color on a colored badge.
func chip(bgH string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(cBg)).Background(lipgloss.Color(bgH)).Bold(true)
}

// Spans of styled text, joined into the strings of text nodes.
func muted(s string) string { return fg(cMuted).Render(s) }
func text(s string) string  { return fg(cText).Render(s) }
func lav(s string) string   { return fg(cLav).Render(s) }
func title(s string) string { return bold(cLav).Render(s) }

var (
	panelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cBorder)).Padding(0, 1)
	hoverPanel = panelStyle.BorderForeground(lipgloss.Color(cLavDeep))
	selStyle   = lipgloss.NewStyle().Background(lipgloss.Color(cSel))
	hoverRow   = lipgloss.NewStyle().Background(lipgloss.Color(cRaised))
	headStyle  = bold(cMuted)
)

// panel is a rounded box with a title on its top border, and right on the
// right of it when not empty.
func panel(titleS, right string, body bloom.Child) bloom.BoxNode {
	return panelIn(panelStyle, titleS, right, body)
}

func panelIn(st lipgloss.Style, titleS, right string, body bloom.Child) bloom.BoxNode {
	b := bloom.Box(st, body).Edge(bloom.Top, bloom.Start, bloom.Text(" "+titleS+" "))
	if right != "" {
		b = b.Edge(bloom.Top, bloom.End, bloom.Text(" "+right+" "))
	}
	return b
}

// line is one line of styled spans, cut with … when it does not fit.
func line(spans ...string) bloom.TextNode { return bloom.Text(strings.Join(spans, "")).Truncate() }

// labeled is a line with a muted label in a column of w cells.
func labeled(label string, w int, spans ...string) bloom.Child {
	return bloom.Row(bloom.Fixed(w, bloom.Text(muted(label))), bloom.Fill(line(spans...)))
}

// spread puts l on the left and r on the right of a line, cutting l short
// when both do not fit.
func spread(l, r string) bloom.Child {
	return bloom.Row(bloom.Fill(line(l)), bloom.Auto(bloom.Text(r)))
}

// keep is styled text that keeps its width in a row.
func keep(s string) bloom.SlotNode { return bloom.Auto(bloom.Text(s)).Min(lipgloss.Width(s)) }

// alignR is styled text against the right edge of its space.
func alignR(s string) bloom.Child { return bloom.Row(bloom.Space(), bloom.Auto(bloom.Text(s))) }

// muted centered note, for empty lists.
func note(s string) bloom.Child {
	return bloom.Column(bloom.Fixed(1, nil), bloom.Centered(bloom.Text(muted(s))))
}

func statusHex(code int) string {
	switch {
	case code == 0:
		return cMuted
	case code >= 500:
		return cRed
	case code >= 400:
		return cYellow
	case code >= 300 || code == 101:
		return cCyan
	default:
		return cGreen
	}
}

func methodHex(m string) string {
	switch m {
	case "POST":
		return cPeach
	case "PUT", "PATCH":
		return cPurple
	case "DELETE":
		return cRed
	case "OPTIONS", "HEAD":
		return "#8A8FA8"
	}
	return cLav
}

func statusPill(code int, txt string) string {
	h := statusHex(code)
	return chip(h).Render(fmt.Sprintf(" %d ", code)) + " " + fg(h).Render(txt)
}

func replayBadge() string { return chip(cPeach).Render(" ↻ replay ") }

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

func fmtRate(r float64) string {
	if r >= 10 || r == 0 {
		return fmt.Sprintf("%.0f", r)
	}
	return fmt.Sprintf("%.1f", r)
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

// scrollHover grows a scrollbar's thumb under the pointer, to show it can be
// dragged.
var scrollHover = bloom.ScrollbarStyle{Thumb: "█", ThumbStyle: fg(cLav)}
