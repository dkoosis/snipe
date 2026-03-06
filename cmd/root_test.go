package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dkoosis/snipe/internal/output"
)

func TestRoot_IsKnownSubcommandOrFlag_ReturnsExpectedValue_When_InputVaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want bool
	}{
		{
			name: "flag input is always treated as known",
			arg:  "--help",
			want: true,
		},
		{
			name: "registered subcommand is treated as known",
			arg:  "def",
			want: true,
		},
		{
			name: "unknown bare argument is treated as symbol",
			arg:  "ProbablyASymbol",
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isKnownSubcommandOrFlag(tc.arg)
			if got != tc.want {
				t.Fatalf("isKnownSubcommandOrFlag(%q) = %v, want %v", tc.arg, got, tc.want)
			}
		})
	}
}

func TestRoot_ApplyFormatOverrides_ReturnsExpectedConfig_When_FormatModeChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		format      ResponseFormat
		baseBody    bool
		baseSibling bool
		baseContext int
		wantBody    bool
		wantSibling bool
		wantContext int
	}{
		{
			name:        "concise disables body siblings and context",
			format:      FormatConcise,
			baseBody:    true,
			baseSibling: true,
			baseContext: 7,
			wantBody:    false,
			wantSibling: false,
			wantContext: 0,
		},
		{
			name:        "detailed enables body and siblings and keeps context",
			format:      FormatDetailed,
			baseBody:    false,
			baseSibling: false,
			baseContext: 9,
			wantBody:    true,
			wantSibling: true,
			wantContext: 9,
		},
		{
			name:        "summary disables body siblings and context",
			format:      FormatSummary,
			baseBody:    true,
			baseSibling: true,
			baseContext: 5,
			wantBody:    false,
			wantSibling: false,
			wantContext: 0,
		},
		{
			name:        "default preserves base settings",
			format:      FormatDefault,
			baseBody:    true,
			baseSibling: false,
			baseContext: 3,
			wantBody:    true,
			wantSibling: false,
			wantContext: 3,
		},
		{
			name:        "unrecognized format preserves base settings",
			format:      ResponseFormat("mystery"),
			baseBody:    false,
			baseSibling: true,
			baseContext: 11,
			wantBody:    false,
			wantSibling: true,
			wantContext: 11,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotBody, gotSiblings, gotContext := ApplyFormatOverrides(tc.format, tc.baseBody, tc.baseSibling, tc.baseContext)
			if gotBody != tc.wantBody || gotSiblings != tc.wantSibling || gotContext != tc.wantContext {
				t.Fatalf("ApplyFormatOverrides(%q) = (%v, %v, %d), want (%v, %v, %d)", tc.format, gotBody, gotSiblings, gotContext, tc.wantBody, tc.wantSibling, tc.wantContext)
			}
		})
	}
}

func TestRoot_UniqueStrings_PreservesOrderAndUniqueness_When_InputContainsDuplicates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty input returns nil",
			input: []string{},
			want:  nil,
		},
		{
			name:  "duplicates are removed while first-seen order is preserved",
			input: []string{"a", "b", "a", "c", "b", "d", "d"},
			want:  []string{"a", "b", "c", "d"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := uniqueStrings(tc.input)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("uniqueStrings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRoot_ApplySelection_ReturnsExpectedWindow_When_SelectModeVaries(t *testing.T) {
	originalSelectMode := selectMode
	t.Cleanup(func() {
		selectMode = originalSelectMode
	})

	results := []output.Result{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}, {ID: "6"}}

	tests := []struct {
		name   string
		mode   string
		input  []output.Result
		wantID []string
	}{
		{
			name:   "best returns first result only",
			mode:   "best",
			input:  results,
			wantID: []string{"1"},
		},
		{
			name:   "top3 returns first three results",
			mode:   "top3",
			input:  results,
			wantID: []string{"1", "2", "3"},
		},
		{
			name:   "top5 returns first five results",
			mode:   "top5",
			input:  results,
			wantID: []string{"1", "2", "3", "4", "5"},
		},
		{
			name:   "all keeps every result",
			mode:   "all",
			input:  results,
			wantID: []string{"1", "2", "3", "4", "5", "6"},
		},
		{
			name:   "unknown mode falls back to all",
			mode:   "mystery",
			input:  results,
			wantID: []string{"1", "2", "3", "4", "5", "6"},
		},
		{
			name:   "nil input remains nil",
			mode:   "top3",
			input:  nil,
			wantID: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			selectMode = tc.mode
			got := ApplySelection(tc.input)
			var gotIDs []string
			if got != nil {
				gotIDs = make([]string, 0, len(got))
				for _, r := range got {
					gotIDs = append(gotIDs, r.ID)
				}
			}
			if diff := cmp.Diff(tc.wantID, gotIDs); diff != "" {
				t.Fatalf("ApplySelection() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRoot_GetOutputConfig_DisablesBodyAndContext_When_SignatureOnlyEnabled(t *testing.T) {
	originalSignatureOnly := signatureOnly
	originalAutoCompact := autoCompact
	originalLimit := limit
	originalOffset := offset
	originalContext := contextLines
	originalNoBody := noBody
	originalNoSiblings := noSiblings
	t.Cleanup(func() {
		signatureOnly = originalSignatureOnly
		autoCompact = originalAutoCompact
		limit = originalLimit
		offset = originalOffset
		contextLines = originalContext
		noBody = originalNoBody
		noSiblings = originalNoSiblings
	})

	autoCompact = true
	limit = 42
	offset = 4
	contextLines = 9
	noBody = false
	noSiblings = false

	tests := []struct {
		name          string
		signatureOnly bool
		wantContext   int
		wantBody      bool
		wantSiblings  bool
	}{
		{
			name:          "signature only strips context body and siblings",
			signatureOnly: true,
			wantContext:   0,
			wantBody:      false,
			wantSiblings:  false,
		},
		{
			name:          "normal mode keeps configured body and siblings",
			signatureOnly: false,
			wantContext:   9,
			wantBody:      true,
			wantSiblings:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			signatureOnly = tc.signatureOnly

			compact, gotLimit, gotOffset, gotContext, gotBody, gotSiblings := GetOutputConfig()
			if !compact || gotLimit != 42 || gotOffset != 4 {
				t.Fatalf("GetOutputConfig() invariant mismatch, got compact=%v limit=%d offset=%d", compact, gotLimit, gotOffset)
			}
			if gotContext != tc.wantContext || gotBody != tc.wantBody || gotSiblings != tc.wantSiblings {
				t.Fatalf("GetOutputConfig() = context=%d body=%v siblings=%v, want context=%d body=%v siblings=%v", gotContext, gotBody, gotSiblings, tc.wantContext, tc.wantBody, tc.wantSiblings)
			}
		})
	}
}

func TestRoot_GetContext_ReturnsCommandContext_When_Configured(t *testing.T) {
	originalCtx := cmdCtx
	originalCancel := cmdCancel
	t.Cleanup(func() {
		cmdCtx = originalCtx
		cmdCancel = originalCancel
	})

	t.Run("error path returns background when command context is unset", func(t *testing.T) {
		cmdCtx = nil
		got := GetContext()
		if got == nil {
			t.Fatal("GetContext() returned nil, want non-nil background context")
		}
		if err := got.Err(); err != nil {
			t.Fatalf("background context should not be canceled, got err=%v", err)
		}
	})

	t.Run("configured context is returned unchanged", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		t.Cleanup(cancel)
		cmdCtx = ctx
		got := GetContext()
		if got != ctx {
			t.Fatal("GetContext() did not return the configured command context")
		}
	})
}
