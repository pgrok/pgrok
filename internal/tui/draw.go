package tui

import (
	"image/color"
	"math"

	"bloomfx.dev/bloom"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"rsc.io/qr"
)

// Drawables paint cells directly, for charts and codes that are not text.

// colors caches parsed colors, as drawables set many cells on every frame.
// Views run on one goroutine.
var colors = map[string]color.Color{}

func colorOf(h string) color.Color {
	c, ok := colors[h]
	if !ok {
		c = lipgloss.Color(h)
		colors[h] = c
	}
	return c
}

func setCell(scr uv.Screen, x, y int, glyph, fgH, bgH string, strong bool) {
	c := &uv.Cell{Content: glyph, Width: 1}
	if fgH != "" {
		c.Style.Fg = colorOf(fgH)
	}
	if bgH != "" {
		c.Style.Bg = colorOf(bgH)
	}
	if strong {
		c.Style.Attrs = uv.AttrBold
	}
	scr.SetCell(x, y, c)
}

var sparkChars = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// sparkline draws its newest values, one column each, right-aligned and
// scaled to top, as tall as its area. Values at 80% of top or more are hot.
type sparkline struct {
	vals []float64 // oldest first
	top  float64
	hot  bool // color the highest values peach
}

func (s sparkline) Draw(scr uv.Screen, area uv.Rectangle) {
	top := s.top
	if top <= 0 {
		top = 1
	}
	rows := area.Dy()
	vals := s.vals[max(0, len(s.vals)-area.Dx()):]
	x0 := area.Max.X - len(vals)
	for i, v := range vals {
		lvl := int(math.Round(v / top * float64(rows*8)))
		if v > 0 && lvl == 0 {
			lvl = 1
		}
		c := cLav
		if s.hot && s.top > 0 && v >= s.top*0.8 {
			c = cPeach
		}
		for r := range rows {
			n := min(8, max(0, lvl-r*8))
			y := area.Max.Y - 1 - r
			switch {
			case n == 8:
				// Colored spaces tile without the hairline gaps some fonts
				// leave between █ glyphs.
				setCell(scr, x0+i, y, " ", "", c, false)
			case n > 0:
				setCell(scr, x0+i, y, sparkChars[n], c, "", false)
			}
		}
	}
}

// splitBar draws the share of 2xx, 3xx, 4xx and 5xx responses.
type splitBar struct{ counts [4]int }

func (b splitBar) Draw(scr uv.Screen, area uv.Rectangle) {
	w := area.Dx()
	tot := b.counts[0] + b.counts[1] + b.counts[2] + b.counts[3]
	if tot == 0 {
		for x := area.Min.X; x < area.Max.X; x++ {
			setCell(scr, x, area.Min.Y, "─", cTrack, "", false)
		}
		return
	}
	// Give every non-empty class at least one cell, and the rest of the width
	// to the largest class.
	widths, used, largest := [4]int{}, 0, 0
	for i, n := range b.counts {
		if n > 0 {
			widths[i] = max(1, n*w/tot)
		}
		used += widths[i]
		if n > b.counts[largest] {
			largest = i
		}
	}
	widths[largest] += w - used
	x := area.Min.X
	for i, c := range []string{cGreen, cCyan, cYellow, cRed} {
		for range widths[i] {
			setCell(scr, x, area.Min.Y, " ", "", c, false)
			x++
		}
	}
}

// bar fills the part of its width from from to to, fractions of it, and
// draws a track elsewhere.
type bar struct {
	from, to     float64
	fill, track  string
	trackGlyph   string
	minimumCells int // cells filled however short the span
}

func (b bar) Draw(scr uv.Screen, area uv.Rectangle) {
	w := area.Dx()
	cell := func(f float64) int { return max(0, min(w, int(math.Round(f*float64(w))))) }
	a, z := cell(b.from), cell(b.to)
	if z-a < b.minimumCells {
		z = min(w, a+b.minimumCells)
		a = min(a, z-b.minimumCells)
	}
	for i := range w {
		if i >= a && i < z {
			setCell(scr, area.Min.X+i, area.Min.Y, " ", "", b.fill, false)
		} else {
			setCell(scr, area.Min.X+i, area.Min.Y, b.trackGlyph, b.track, "", false)
		}
	}
}

// rule is the line under the header, with an accent under the active tab
// that glides from tab to tab.
type rule struct{ x, w float64 }

func (r rule) Draw(scr uv.Screen, area uv.Rectangle) {
	a := area.Min.X + int(math.Round(r.x))
	z := a + int(math.Round(r.w))
	for x := area.Min.X; x < area.Max.X; x++ {
		if x >= a && x < z {
			setCell(scr, x, area.Min.Y, "━", cLav, "", false)
		} else {
			setCell(scr, x, area.Min.Y, "─", cBorder, "", false)
		}
	}
}

// qrCode draws a QR code with half blocks, two modules per cell, in explicit
// colors so it scans on dark and light terminals alike.
type qrCode struct{ code *qr.Code }

const qrQuiet = 2

func (q qrCode) modules() int { return q.code.Size + 2*qrQuiet }

// Measure implements bloom.Measurer.
func (q qrCode) Measure(bloom.Size) bloom.Size {
	n := q.modules()
	return bloom.Size{W: n, H: (n + 1) / 2}
}

func (q qrCode) Draw(scr uv.Screen, area uv.Rectangle) {
	n := q.modules()
	black := func(x, y int) bool { return q.code.Black(x-qrQuiet, y-qrQuiet) }
	const dark, light = "#101016", "#ECECF4"
	for y := 0; y < n; y += 2 {
		for x := range n {
			top, bottom := light, light
			if black(x, y) {
				top = dark
			}
			if y+1 < n && black(x, y+1) {
				bottom = dark
			}
			setCell(scr, area.Min.X+x, area.Min.Y+y/2, "▀", top, bottom, false)
		}
	}
}
