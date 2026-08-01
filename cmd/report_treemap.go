package cmd

import (
	"math"
	"sort"
)

// treemapItem is one file going into the hotspot treemap: Size drives box
// area (v1: function count — cheap, always available from the index, unlike
// filesystem LOC which would require a second disk pass), Complexity drives
// box color (summed McCabe complexity, matching hotspotRow.Cyclo).
type treemapItem struct {
	Path       string
	Size       float64
	Complexity float64
}

// treemapBox is one placed rectangle, in percent-of-canvas coordinates
// (0..100) so the renderer can emit plain width/height/left/top CSS with no
// further math — a hand-rolled treemap needs no charting library (sn-qnq3).
type treemapBox struct {
	Path             string
	XPct, YPct       float64
	WPct, HPct       float64
	Size, Complexity float64
	Color            string
}

// layoutTreemap arranges items into non-overlapping boxes filling a 100x100
// percentage canvas using the squarified treemap algorithm (Bruls, Huizing &
// van Wijk, "Squarified Treemaps", 2000): items are sorted by size descending
// and packed into rows chosen to keep each box's aspect ratio close to 1,
// which reads far better than naively slicing proportional strips when file
// sizes vary widely (a few huge files next to a long tail of tiny ones).
func layoutTreemap(items []treemapItem) []treemapBox {
	if len(items) == 0 {
		return nil
	}
	sorted := make([]treemapItem, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Size != sorted[j].Size {
			return sorted[i].Size > sorted[j].Size
		}
		return sorted[i].Path < sorted[j].Path
	})

	var total float64
	for _, it := range sorted {
		total += it.Size
	}
	if total <= 0 {
		// Degenerate input (all-zero sizes): fall back to equal weighting so
		// the canvas still fills instead of collapsing to zero-area boxes.
		for i := range sorted {
			sorted[i].Size = 1
		}
		total = float64(len(sorted))
	}

	const canvasW, canvasH = 100.0, 100.0
	area := canvasW * canvasH
	scaled := make([]treemapItem, len(sorted))
	for i, it := range sorted {
		scaled[i] = it
		scaled[i].Size = it.Size / total * area
	}

	return squarify(scaled, canvasW, canvasH)
}

// squarify iteratively grows a "row" of items one at a time as long as doing
// so improves (or does not worsen) the worst aspect ratio in the row, then
// lays that row out along the shorter side of the remaining rectangle and
// recurses on what's left — the standard greedy squarify loop, written
// iteratively (rather than the paper's recursion) to keep it easy to read
// and to unit test in isolation.
func squarify(items []treemapItem, w, h float64) []treemapBox {
	var boxes []treemapBox
	remaining := items
	x, y, rw, rh := 0.0, 0.0, w, h

	for len(remaining) > 0 {
		shortSide := math.Min(rw, rh)
		row := []treemapItem{remaining[0]}
		best := worstRatio(row, shortSide)
		i := 1
		for i < len(remaining) {
			candidate := append(append([]treemapItem{}, row...), remaining[i])
			ratio := worstRatio(candidate, shortSide)
			if ratio > best {
				break
			}
			row = candidate
			best = ratio
			i++
		}
		remaining = remaining[i:]

		rowBoxes, nx, ny, nw, nh := layoutRow(row, x, y, rw, rh)
		boxes = append(boxes, rowBoxes...)
		x, y, rw, rh = nx, ny, nw, nh
	}
	return boxes
}

// worstRatio computes the worst (largest) box aspect ratio that would result
// from laying `row` out along a band of thickness `side` — the formula from
// the squarified-treemaps paper: max(side²·maxArea/sum², sum²/(side²·minArea)).
// Lower is better (1.0 = every box in the row is a perfect square).
func worstRatio(row []treemapItem, side float64) float64 {
	if len(row) == 0 || side <= 0 {
		return math.Inf(1)
	}
	var sum, maxV float64
	minV := math.Inf(1)
	for _, it := range row {
		sum += it.Size
		if it.Size > maxV {
			maxV = it.Size
		}
		if it.Size < minV {
			minV = it.Size
		}
	}
	if sum <= 0 || minV <= 0 {
		return math.Inf(1)
	}
	s2 := side * side
	sum2 := sum * sum
	return math.Max(s2*maxV/sum2, sum2/(s2*minV))
}

// layoutRow places one completed row into rectangle (x,y,w,h), banding along
// whichever side is shorter (so boxes trend toward square, not sliver-thin),
// and returns the placed boxes plus the rectangle still remaining afterward.
func layoutRow(row []treemapItem, x, y, w, h float64) ([]treemapBox, float64, float64, float64, float64) {
	var rowArea float64
	for _, it := range row {
		rowArea += it.Size
	}
	boxes := make([]treemapBox, 0, len(row))

	if w >= h {
		// Band is a column on the left, full height h, stacking items top-to-bottom.
		colW := 0.0
		if h > 0 {
			colW = rowArea / h
		}
		yy := y
		for _, it := range row {
			itemH := 0.0
			if colW > 0 {
				itemH = it.Size / colW
			}
			boxes = append(boxes, treemapBox{
				Path: it.Path, XPct: x, YPct: yy, WPct: colW, HPct: itemH,
				Size: it.Size, Complexity: it.Complexity,
			})
			yy += itemH
		}
		return boxes, x + colW, y, w - colW, h
	}

	// Band is a row along the top, full width w, placing items left-to-right.
	rowH := 0.0
	if w > 0 {
		rowH = rowArea / w
	}
	xx := x
	for _, it := range row {
		itemW := 0.0
		if rowH > 0 {
			itemW = it.Size / rowH
		}
		boxes = append(boxes, treemapBox{
			Path: it.Path, XPct: xx, YPct: y, WPct: itemW, HPct: rowH,
			Size: it.Size, Complexity: it.Complexity,
		})
		xx += itemW
	}
	return boxes, x, y + rowH, w, h - rowH
}

// treemapColorScale is a green→yellow→red 3-stop scale (low→high complexity),
// picked for the same reason `snipe hotspots`' ⚑ markers are terse: a glance
// should show risk concentration without a legend lookup.
var treemapColorScale = []struct {
	stop  float64
	color string
}{
	{0.0, "#4ade80"}, // green  — low complexity
	{0.5, "#facc15"}, // yellow — medium
	{1.0, "#ef4444"}, // red    — high
}

// colorForComplexity maps a normalized complexity value (0..1, this file's
// share of the hottest file in the report) to a hex color by linear
// interpolation between the nearest two scale stops.
func colorForComplexity(norm float64) string {
	if norm < 0 {
		norm = 0
	}
	if norm > 1 {
		norm = 1
	}
	for i := 0; i < len(treemapColorScale)-1; i++ {
		lo, hi := treemapColorScale[i], treemapColorScale[i+1]
		if norm >= lo.stop && norm <= hi.stop {
			span := hi.stop - lo.stop
			t := 0.0
			if span > 0 {
				t = (norm - lo.stop) / span
			}
			return lerpHexColor(lo.color, hi.color, t)
		}
	}
	return treemapColorScale[len(treemapColorScale)-1].color
}

// lerpHexColor linearly interpolates two "#rrggbb" colors at t in [0,1].
func lerpHexColor(a, b string, t float64) string {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	r := lerpByte(ar, br, t)
	g := lerpByte(ag, bg, t)
	bl := lerpByte(ab, bb, t)
	return rgbToHex(r, g, bl)
}

func lerpByte(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}

func hexToRGB(s string) (r, g, b uint8) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0
	}
	parse := func(h string) uint8 {
		var v int
		for _, c := range h {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= int(c - '0')
			case c >= 'a' && c <= 'f':
				v |= int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v |= int(c-'A') + 10
			}
		}
		return uint8(v)
	}
	return parse(s[1:3]), parse(s[3:5]), parse(s[5:7])
}

func rgbToHex(r, g, b uint8) string {
	const hexDigits = "0123456789abcdef"
	buf := [7]byte{'#'}
	buf[1] = hexDigits[r>>4]
	buf[2] = hexDigits[r&0xf]
	buf[3] = hexDigits[g>>4]
	buf[4] = hexDigits[g&0xf]
	buf[5] = hexDigits[b>>4]
	buf[6] = hexDigits[b&0xf]
	return string(buf[:])
}
