package cmd

import (
	"math"
	"testing"
)

// totalArea sums box areas (in percent-of-canvas units) — should equal the
// full 100x100 canvas regardless of how many items or how skewed their sizes.
func totalArea(boxes []treemapBox) float64 {
	var sum float64
	for _, b := range boxes {
		sum += b.WPct * b.HPct
	}
	return sum
}

func TestLayoutTreemapEmpty(t *testing.T) {
	if boxes := layoutTreemap(nil); boxes != nil {
		t.Fatalf("layoutTreemap(nil) = %v, want nil", boxes)
	}
	if boxes := layoutTreemap([]treemapItem{}); boxes != nil {
		t.Fatalf("layoutTreemap([]) = %v, want nil", boxes)
	}
}

func TestLayoutTreemapSingleItemFillsCanvas(t *testing.T) {
	boxes := layoutTreemap([]treemapItem{{Path: "a.go", Size: 42, Complexity: 5}})
	if len(boxes) != 1 {
		t.Fatalf("got %d boxes, want 1", len(boxes))
	}
	b := boxes[0]
	if b.WPct != 100 || b.HPct != 100 || b.XPct != 0 || b.YPct != 0 {
		t.Errorf("single item should fill the whole canvas, got %+v", b)
	}
}

func TestLayoutTreemapCoversFullArea(t *testing.T) {
	items := []treemapItem{
		{Path: "huge.go", Size: 500, Complexity: 90},
		{Path: "big.go", Size: 200, Complexity: 40},
		{Path: "medium.go", Size: 80, Complexity: 20},
		{Path: "small.go", Size: 10, Complexity: 2},
		{Path: "tiny.go", Size: 1, Complexity: 1},
	}
	boxes := layoutTreemap(items)
	if len(boxes) != len(items) {
		t.Fatalf("got %d boxes, want %d", len(boxes), len(items))
	}
	area := totalArea(boxes)
	if math.Abs(area-10000) > 0.01 { // 100 x 100 canvas
		t.Errorf("total area = %.4f, want ~10000 (full canvas)", area)
	}
	for _, b := range boxes {
		if b.WPct <= 0 || b.HPct <= 0 {
			t.Errorf("box %s has non-positive dimension: %+v", b.Path, b)
		}
		if b.XPct < 0 || b.YPct < 0 || b.XPct+b.WPct > 100.0001 || b.YPct+b.HPct > 100.0001 {
			t.Errorf("box %s escapes canvas bounds: %+v", b.Path, b)
		}
	}
}

// TestLayoutTreemapProportional checks that a box twice the size of another
// (same total weight class) gets roughly twice the area — the whole point of
// sizing by func-count/complexity is that bigger files read as bigger boxes.
func TestLayoutTreemapProportional(t *testing.T) {
	items := []treemapItem{
		{Path: "a.go", Size: 20},
		{Path: "b.go", Size: 10},
	}
	boxes := layoutTreemap(items)
	byPath := map[string]treemapBox{}
	for _, b := range boxes {
		byPath[b.Path] = b
	}
	areaA := byPath["a.go"].WPct * byPath["a.go"].HPct
	areaB := byPath["b.go"].WPct * byPath["b.go"].HPct
	ratio := areaA / areaB
	if math.Abs(ratio-2.0) > 0.01 {
		t.Errorf("area ratio = %.4f, want ~2.0 (a.go is 2x b.go's size)", ratio)
	}
}

func TestLayoutTreemapZeroSizesFallBackToEqualWeight(t *testing.T) {
	items := []treemapItem{
		{Path: "a.go", Size: 0},
		{Path: "b.go", Size: 0},
	}
	boxes := layoutTreemap(items)
	if len(boxes) != 2 {
		t.Fatalf("got %d boxes, want 2", len(boxes))
	}
	area := totalArea(boxes)
	if math.Abs(area-10000) > 0.01 {
		t.Errorf("degenerate all-zero input should still fill the canvas, area=%.4f", area)
	}
}

func TestWorstRatioPerfectSquareIsOne(t *testing.T) {
	// A single item whose area exactly matches a square side gives ratio 1.
	row := []treemapItem{{Size: 100}}
	got := worstRatio(row, 10) // side=10 -> area 100 -> perfect square
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("worstRatio = %.6f, want 1.0 for a perfect square", got)
	}
}

func TestWorstRatioEmptyRowIsInfinite(t *testing.T) {
	if got := worstRatio(nil, 10); !math.IsInf(got, 1) {
		t.Errorf("worstRatio(nil, 10) = %v, want +Inf", got)
	}
	if got := worstRatio([]treemapItem{{Size: 5}}, 0); !math.IsInf(got, 1) {
		t.Errorf("worstRatio(_, 0) = %v, want +Inf", got)
	}
}

func TestColorForComplexityBounds(t *testing.T) {
	if c := colorForComplexity(0); c != "#4ade80" {
		t.Errorf("colorForComplexity(0) = %s, want green stop #4ade80", c)
	}
	if c := colorForComplexity(1); c != "#ef4444" {
		t.Errorf("colorForComplexity(1) = %s, want red stop #ef4444", c)
	}
	// Out-of-range inputs clamp rather than extrapolate into invalid hex.
	if c := colorForComplexity(-5); c != "#4ade80" {
		t.Errorf("colorForComplexity(-5) = %s, want clamped to green", c)
	}
	if c := colorForComplexity(5); c != "#ef4444" {
		t.Errorf("colorForComplexity(5) = %s, want clamped to red", c)
	}
}

func TestColorForComplexityMidpointIsYellow(t *testing.T) {
	if c := colorForComplexity(0.5); c != "#facc15" {
		t.Errorf("colorForComplexity(0.5) = %s, want yellow stop #facc15", c)
	}
}

func TestHexRGBRoundTrip(t *testing.T) {
	for _, hex := range []string{"#4ade80", "#facc15", "#ef4444", "#000000", "#ffffff"} {
		r, g, b := hexToRGB(hex)
		got := rgbToHex(r, g, b)
		if got != hex {
			t.Errorf("hexToRGB/rgbToHex round trip: %s -> %s", hex, got)
		}
	}
}
