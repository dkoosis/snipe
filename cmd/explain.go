package cmd

import (
	"os"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	explainMode     string
	explainWarnings string
	explainAt       string
)

// degradedExplainFuncOnly marks a pack --depth explain answer for a symbol
// that is not a func or method: the pack fields are there, the walkthrough
// is not.
const degradedExplainFuncOnly = "explain_func_only"

// runExplain is pack --depth explain: the pack for one symbol (runPack's
// single-symbol path, same limits and body), plus query.Explain's purpose
// source, mechanism, caller pattern and warnings (sn-lvub.10).
func runExplain(args []string) error {
	start := time.Now()

	_, _, _, contextLines, withBody, withSiblings := GetOutputConfig()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	if len(args) == 0 && explainAt == "" {
		return w.WriteError(cmdNameExplain, &protocol.Error{
			Code:    protocol.ErrInvalidArgs,
			Message: errProvideSymbolOrAt,
		})
	}

	opts, optErr := explainOptions()
	if optErr != nil {
		return w.WriteError(cmdNameExplain, optErr)
	}

	s, dir, err := OpenStore(w, cmdNameExplain)
	if err != nil {
		return err
	}
	defer s.Close()

	idxState := query.CheckIndexState(s.DB(), dir, Version)

	symbolID, queryInfo, err := resolvePackSymbol(w, s, dir, cmdNameExplain, args, explainAt, idxState)
	if err != nil {
		// resolvePackSymbol already wrote the error; see runPack.
		return nil
	}

	packResult, degraded, allResults, err := buildPackForSymbol(s, symbolID, packOpts{
		withBody:     withBody,
		withSiblings: withSiblings,
		contextLines: contextLines,
	})
	if err != nil {
		return w.WriteErrorWithMeta(cmdNameExplain, protocol.Meta{Query: queryInfo}.PrimaryQueryArg(), nil, idxState, 0, &protocol.Error{
			Code:    protocol.ErrInternal,
			Message: err.Error(),
		})
	}

	result := protocol.ExplainPackResult{PackResult: packResult}
	if kind := packResult.Definition.Kind; kind == protocol.KindFunc || kind == protocol.KindMethod {
		ex, exErr := query.Explain(s.DB(), symbolID, opts)
		if exErr != nil {
			return w.WriteErrorWithMeta(cmdNameExplain, protocol.Meta{Query: queryInfo}.PrimaryQueryArg(), nil, idxState, 0, &protocol.Error{
				Code:    protocol.ErrInternal,
				Message: exErr.Error(),
			})
		}
		mergeExplain(&result, ex, enrichedPurpose(s, symbolID))
	} else {
		degraded = append(degraded, degradedExplainFuncOnly)
	}

	resp := protocol.Response[protocol.ExplainPackResult]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results:  []protocol.ExplainPackResult{result},
		Meta: protocol.Meta{
			Command:       cmdNameExplain,
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    idxState,
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         1,
			TokenEstimate: estimatePackTokens(packResult),
			StaleFiles:    query.CheckFileStaleness(s.DB(), dir, allResults),
		},
	}

	return w.WriteResponse(resp)
}

// explainOptions turns --mode and --warnings into query.ExplainOptions.
func explainOptions() (query.ExplainOptions, *protocol.Error) {
	opts := query.DefaultExplainOptions()

	switch explainMode {
	case "brief":
		opts.Mode = protocol.ExplainBrief
	case "normal":
		opts.Mode = protocol.ExplainNormal
	case "deep":
		opts.Mode = protocol.ExplainDeep
	default:
		return opts, &protocol.Error{
			Code:    protocol.ErrInvalidArgs,
			Message: "invalid --mode: use brief, normal, or deep",
		}
	}

	switch explainWarnings {
	case "none":
		opts.WarningsMode = protocol.WarningsNone
	case "fast":
		opts.WarningsMode = protocol.WarningsFast
	case "full":
		opts.WarningsMode = protocol.WarningsFull
	default:
		return opts, &protocol.Error{
			Code:    protocol.ErrInvalidArgs,
			Message: "invalid --warnings: use none, fast, or full",
		}
	}
	return opts, nil
}

// enrichedPurpose returns the symbol's LLM-enrichment purpose, or "" when the
// index has none.
func enrichedPurpose(s *store.Store, symbolID string) string {
	p, err := s.GetPurpose(symbolID)
	if err != nil {
		return ""
	}
	return p
}

// mergeExplain adds ex's sections to r without repeating what the pack
// already holds (D4). The pack's purpose stands (it prefers enrichment, then
// the doc); explain's fills it only when the pack has none, such as a
// signature template for an undocumented func. PurposeSource names where the
// purpose shown came from. A warning's evidence snippet is dropped when the
// definition body, which holds that line, is already shown.
func mergeExplain(r *protocol.ExplainPackResult, ex *protocol.ExplainResult, enriched string) {
	switch {
	case r.Purpose == "":
		r.Purpose = ex.Purpose
		r.PurposeSource = ex.PurposeSource
	case enriched != "" && r.Purpose == enriched:
		r.PurposeSource = protocol.PurposeFromEnrichment
	default:
		r.PurposeSource = protocol.PurposeFromDoc
	}
	r.Mechanism = ex.Mechanism
	if ex.CallerContext != nil {
		r.CallerPattern = ex.CallerContext.Pattern
	}
	r.Warnings = ex.Warnings
	if r.Definition != nil && r.Definition.Body != "" {
		for i := range r.Warnings {
			r.Warnings[i].Evidence = ""
		}
	}
	docStatus := ex.DocStatus
	r.DocStatus = &docStatus
}
