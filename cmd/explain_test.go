package cmd

import (
	"testing"

	"github.com/dkoosis/snipe/internal/protocol"
)

// TestMergeExplain pins how pack --depth explain folds query.Explain into the
// pack: the pack's purpose stands and PurposeSource names its origin, explain
// fills an empty purpose, only the caller pattern is kept, and a warning's
// evidence is dropped when the body already holds that line (D4).
func TestMergeExplain(t *testing.T) {
	ex := &protocol.ExplainResult{
		Purpose:       "Reply calls across.",
		PurposeSource: protocol.PurposeFromDoc,
		Mechanism:     []protocol.MechanismStep{{Action: "calls", Target: "Greet"}},
		CallerContext: &protocol.CallerContext{Count: 1, Pattern: "all from . (1 callers)", TopCallers: []string{"Run"}},
		Warnings:      []protocol.Warning{{Line: 3, Message: "m", Evidence: "x, _ := f()"}},
		DocStatus:     protocol.DocStatus{Status: protocol.DocFresh},
		KeyDeps:       []string{"Greet"},
	}
	tests := []struct {
		name, packPurpose, enriched, body string
		wantPurpose                       string
		wantSource                        protocol.PurposeSource
		wantEvidence                      string
	}{
		{"pack purpose from doc", "Reply calls across.", "", "func Reply() {}", "Reply calls across.", protocol.PurposeFromDoc, ""},
		{"pack purpose from enrichment", "Sends a reply.", "Sends a reply.", "", "Sends a reply.", protocol.PurposeFromEnrichment, "x, _ := f()"},
		{"pack has none", "", "", "", "Reply calls across.", protocol.PurposeFromDoc, "x, _ := f()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exCopy := *ex
			exCopy.Warnings = append([]protocol.Warning(nil), ex.Warnings...)
			r := protocol.ExplainPackResult{PackResult: protocol.PackResult{
				Definition: &protocol.Result{Body: tt.body},
				Purpose:    tt.packPurpose,
			}}
			mergeExplain(&r, &exCopy, tt.enriched)
			if r.Purpose != tt.wantPurpose || r.PurposeSource != tt.wantSource {
				t.Errorf("purpose = %q (%s), want %q (%s)", r.Purpose, r.PurposeSource, tt.wantPurpose, tt.wantSource)
			}
			if r.CallerPattern != "all from . (1 callers)" {
				t.Errorf("caller pattern = %q", r.CallerPattern)
			}
			if len(r.Mechanism) != 1 || r.DocStatus == nil || r.DocStatus.Status != protocol.DocFresh {
				t.Errorf("mechanism or doc status not carried: %+v", r)
			}
			if got := r.Warnings[0].Evidence; got != tt.wantEvidence {
				t.Errorf("evidence = %q, want %q", got, tt.wantEvidence)
			}
		})
	}
}
