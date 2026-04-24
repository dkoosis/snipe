package output

import (
	"bytes"
	"strings"
	"testing"
)

// withTTY overrides isStdoutTTY for the duration of a test.
func withTTY(t *testing.T, v bool) {
	t.Helper()
	prev := isStdoutTTY
	isStdoutTTY = func() bool { return v }
	t.Cleanup(func() { isStdoutTTY = prev })
}

func humanResp() Response[Result] {
	return Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results: []Result{
			{
				Name:  "ProcessOrder",
				File:  "internal/order/order.go",
				Range: Range{Start: Position{Line: 42, Col: 1}},
				Kind:  "func",
			},
			{
				Name:     "Start",
				Receiver: "*Server",
				File:     "internal/server/server.go",
				Range:    Range{Start: Position{Line: 10, Col: 1}},
				Kind:     "method",
			},
		},
		Meta: Meta{Command: "def", Total: 2},
	}
}

func TestHumanFormat_NonTTY_NoAnsi(t *testing.T) {
	withTTY(t, false)
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputHuman)
	if err := w.WriteResponse(humanResp()); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY output contains ANSI escape: %q", got)
	}
	wantLines := []string{
		"ProcessOrder  internal/order/order.go:42  func\n",
		"(*Server).Start  internal/server/server.go:10  method\n",
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line) {
			t.Errorf("missing line %q in output:\n%s", line, got)
		}
	}
}

func TestHumanFormat_TTY_HasAnsi(t *testing.T) {
	withTTY(t, true)
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputHuman)
	if err := w.WriteResponse(humanResp()); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("TTY output missing ANSI escape: %q", got)
	}
	// Names and kinds should still be present even with color codes mixed in.
	if !strings.Contains(got, "ProcessOrder") || !strings.Contains(got, "func") {
		t.Errorf("TTY output missing expected tokens: %q", got)
	}
	// Reset code appears at least once.
	if !strings.Contains(got, ansiReset) {
		t.Errorf("TTY output missing reset code: %q", got)
	}
}

func TestHumanFormat_EmptyResults(t *testing.T) {
	withTTY(t, false)
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputHuman)
	resp := Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results:  nil,
		Meta:     Meta{Command: "search"},
	}
	if err := w.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	if !strings.Contains(buf.String(), "no results") {
		t.Errorf("expected 'no results', got %q", buf.String())
	}
}

func TestHumanFormat_ErrorRendering(t *testing.T) {
	withTTY(t, false)
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputHuman)
	err := &Error{
		Code:    ErrNotFound,
		Message: "Symbol not found: Foo",
		Candidates: []Candidate{
			{Name: "Foo", File: "a.go", Kind: "func"},
		},
	}
	if werr := w.WriteError("def", err); werr != nil {
		t.Fatalf("WriteError: %v", werr)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "error:") {
		t.Errorf("expected 'error:' prefix, got %q", out)
	}
	if !strings.Contains(out, "Foo  a.go  func") {
		t.Errorf("expected candidate line, got %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("non-TTY error output should not contain ANSI: %q", out)
	}
}

// Composite result types should fall back to the Claude renderer so human
// format doesn't silently drop information for pack/explain/etc.
func TestHumanFormat_CompositeFallback(t *testing.T) {
	withTTY(t, false)
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputHuman)
	resp := Response[PackResult]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results: []PackResult{
			{Definition: &Result{Name: "Thing", File: "x.go", Range: Range{Start: Position{Line: 1}}, Kind: "type"}},
		},
		Meta: Meta{Command: "pack"},
	}
	if err := w.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	// Claude header format uses '# ' prefix.
	if !strings.Contains(buf.String(), "# Thing") {
		t.Errorf("expected Claude fallback for PackResult, got %q", buf.String())
	}
}
