package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
)

// dim blends every color toward the background while content is drawn behind
// a modal or while the tunnel is offline. Views run on one goroutine.
var dim float64

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

func col(h string) color.Color {
	if dim > 0 {
		h = mixHex(h, cBg, dim)
	}
	return lipgloss.Color(h)
}

func withDim(d float64, f func() string) string {
	old := dim
	dim = math.Max(old, d)
	defer func() { dim = old }()
	return f()
}

func fg(h string) lipgloss.Style   { return lipgloss.NewStyle().Foreground(col(h)) }
func bold(h string) lipgloss.Style { return fg(h).Bold(true) }

// chip renders text in the background color on a colored badge.
func chip(bgH string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(col(cBg)).Background(col(bgH)).Bold(true)
}

func muted(s string) string { return fg(cMuted).Render(s) }
func text(s string) string  { return fg(cText).Render(s) }
func lav(s string) string   { return fg(cLav).Render(s) }
func title(s string) string { return bold(cLav).Render(s) }
func lbl(s string, w int) string {
	return fg(cMuted).Render(fit(s, w))
}

func width(s string) int { return lipgloss.Width(s) }
func sp(n int) string    { return strings.Repeat(" ", max(0, n)) }
func rep(s string, n int) string {
	return strings.Repeat(s, max(0, n))
}

// fit truncates (with …) or pads s to exactly w cells.
func fit(s string, w int) string {
	if width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + sp(w-width(s))
}

// fitR right-aligns s in w cells.
func fitR(s string, w int) string {
	if width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return sp(w-width(s)) + s
}

func center(s string, w int) string {
	return fit(sp((w-width(s))/2)+s, w)
}

// spread puts l on the left and r on the right of a w-wide line, truncating l
// when both do not fit.
func spread(l, r string, w int) string {
	gap := w - width(l) - width(r)
	if gap < 1 {
		l = ansi.Truncate(l, max(0, w-width(r)-1), "…")
		gap = w - width(l) - width(r)
	}
	return fit(l+sp(gap)+r, w)
}

// normalize forces s to exactly h lines of exactly w cells.
func normalize(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out[i] = fit(l, w)
	}
	return strings.Join(out, "\n")
}

func vjoin(parts ...string) string { return lipgloss.JoinVertical(lipgloss.Left, parts...) }
func hjoin(parts ...string) string { return lipgloss.JoinHorizontal(lipgloss.Top, parts...) }

// paintBg gives an already styled line a background, re-applying it after
// every SGR reset so nested styles do not punch holes into it.
func paintBg(s, hex string) string {
	if dim > 0 {
		hex = mixHex(hex, cBg, dim)
	}
	r, g, b := parseHex(hex)
	seq := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[m")
	return seq + strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+seq) + "\x1b[m"
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

// box is a rounded panel with a title and an optional right title in its top
// border, and an optional footer in its bottom border.
type box struct {
	W, H         int
	Title, Right string
	Foot         string
	Lines        []string
	Border       string
}

func (b box) inner() int { return b.W - 4 }

func (b box) render() string {
	bc := b.Border
	if bc == "" {
		bc = cBorder
	}
	bs := fg(bc)
	var sb strings.Builder
	left := bs.Render("╭─")
	if b.Title != "" {
		left += " " + b.Title + " "
	}
	right := bs.Render("╮")
	if b.Right != "" {
		right = " " + b.Right + " " + bs.Render("─╮")
	}
	if width(left)+width(right) > b.W {
		right = bs.Render("╮")
	}
	sb.WriteString(left + bs.Render(rep("─", b.W-width(left)-width(right))) + right + "\n")
	for i := 0; i < b.H-2; i++ {
		l := ""
		if i < len(b.Lines) {
			l = b.Lines[i]
		}
		sb.WriteString(bs.Render("│") + " " + fit(l, b.inner()) + " " + bs.Render("│") + "\n")
	}
	bl, br := bs.Render("╰"), bs.Render("╯")
	if b.Foot != "" {
		br = " " + b.Foot + " " + bs.Render("─╯")
	}
	sb.WriteString(bl + bs.Render(rep("─", b.W-width(bl)-width(br))) + br)
	return sb.String()
}

// hints renders key hints: key, description, key, description...
func hints(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, bold(cLav).Render(pairs[i])+" "+muted(pairs[i+1]))
	}
	return strings.Join(parts, fg(cBorder).Render("  ·  "))
}

// blk renders n cells filled with a background color. Colored spaces tile
// without the hairline gaps some fonts leave between █ glyphs.
func blk(hex string, n int) string {
	if n <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Background(col(hex)).Render(sp(n))
}

var sparkChars = []rune(" ▁▂▃▄▅▆▇█")

// spark renders vals as a rows-tall sparkline scaled to top, one column per
// value, using colorOf for each column.
func spark(vals []float64, top float64, rows int, colorOf func(v float64) string) []string {
	out := make([]string, rows)
	if top <= 0 {
		top = 1
	}
	levels := float64(rows * 8)
	for r := 0; r < rows; r++ {
		var sb, run strings.Builder
		runKey, runCol, runFull, runN := "", "", false, 0
		flush := func() {
			switch {
			case runN == 0:
			case runFull:
				sb.WriteString(blk(runCol, runN))
			case runCol == "":
				sb.WriteString(run.String())
			default:
				sb.WriteString(fg(runCol).Render(run.String()))
			}
			run.Reset()
			runN = 0
		}
		base := (rows - 1 - r) * 8
		for _, v := range vals {
			lvl := int(math.Round(v / top * levels))
			if v > 0 && lvl == 0 {
				lvl = 1
			}
			n := min(8, max(0, lvl-base))
			c := colorOf(v)
			if n == 0 {
				c = ""
			}
			key := fmt.Sprint(c, n == 8)
			if key != runKey {
				flush()
				runKey, runCol, runFull = key, c, n == 8
			}
			run.WriteRune(sparkChars[n])
			runN++
		}
		flush()
		out[r] = sb.String()
	}
	return out
}

// bar renders a w-wide horizontal bar filled to frac (0..1).
func bar(frac float64, w int, fill, track string) string {
	frac = math.Max(0, math.Min(1, frac))
	eighths := int(math.Round(frac * float64(w*8)))
	full, part := eighths/8, eighths%8
	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	s := blk(fill, full) + fg(fill).Render(partials[part])
	used := full
	if part > 0 {
		used++
	}
	return s + fg(track).Render(rep("─", w-used))
}

func wordmark() string {
	return bold(cPeach).Render("▌") + bold(cLav).Render("pgrok")
}

// glyphs is a pixel font for the startup wordmark: 6 cells wide, 8 rows.
var glyphs = map[rune][]string{
	'p': {"      ", "      ", "######", "##  ##", "##  ##", "######", "##    ", "##    "},
	'g': {"      ", "      ", "######", "##  ##", "##  ##", "######", "    ##", "######"},
	'r': {"      ", "      ", "######", "##    ", "##    ", "##    ", "      ", "      "},
	'o': {"      ", "      ", "######", "##  ##", "##  ##", "######", "      ", "      "},
	'k': {"##    ", "##    ", "##  ##", "## ## ", "####  ", "##  ##", "      ", "      "},
}

// bigLogo draws the wordmark with a deep-lavender drop shadow like the banner.
func bigLogo() []string {
	const gw, gap, gh = 6, 2, 8
	word := "pgrok"
	w := len(word)*gw + (len(word)-1)*gap + 1
	h := gh + 1
	owner := make([][]int, h) // index+1 of the letter painting a cell
	for y := range owner {
		owner[y] = make([]int, w)
	}
	for i, ch := range word {
		for y, row := range glyphs[ch] {
			for x, c := range row {
				if c == '#' {
					owner[y][i*(gw+gap)+x] = i + 1
				}
			}
		}
	}
	out := make([]string, h)
	for y := 0; y < h; y++ {
		t := float64(y) / float64(gh-1)
		var sb strings.Builder
		for x := 0; x < w; x++ {
			switch o := owner[y][x]; {
			case o == 1:
				sb.WriteString(blk(mixHex("#F8C0AC", "#E8846A", t), 1))
			case o > 1:
				sb.WriteString(blk(mixHex(cLavLight, "#9A7CD6", t), 1))
			case y > 0 && x > 0 && owner[y-1][x-1] > 0:
				sb.WriteString(blk("#33284F", 1))
			default:
				sb.WriteByte(' ')
			}
		}
		out[y] = sb.String()
	}
	return out
}
