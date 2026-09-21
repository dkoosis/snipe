package cmd

import (
	"sort"
	"strings"

	snipectx "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/index"
)

// embedMaxCallers caps how many caller names are folded into a symbol's
// embedded text. Callers describe how a symbol is used; a handful is enough
// signal and keeps the per-symbol token cost bounded.
const embedMaxCallers = 3

// embedContext is the cross-symbol context folded into each symbol's embedded
// text alongside its own name, signature and doc (sn-6wv). Vague-intent
// queries ("where do we persist X") match the package's stated purpose and how
// a symbol is used, which the bare signature does not carry.
type embedContext struct {
	pkgNarrative map[string]string   // pkg path -> first sentence of the package doc
	callers      map[string][]string // callee symbol ID -> distinct non-test caller names (sorted)
}

// buildEmbedContext derives the package narratives and caller lists from data
// the indexer already holds in memory. Test-file callers and self-recursion
// are excluded: they say how a symbol is verified, not how it is used.
func buildEmbedContext(symbols []index.Symbol, edges []index.CallEdge, pkgDocs []index.PackageDoc) embedContext {
	ec := embedContext{
		pkgNarrative: make(map[string]string, len(pkgDocs)),
		callers:      make(map[string][]string),
	}
	for _, d := range pkgDocs {
		if s := snipectx.ExtractFirstSentence(d.Doc); s != "" {
			ec.pkgNarrative[d.PkgPath] = s
		}
	}

	byID := make(map[string]*index.Symbol, len(symbols))
	for i := range symbols {
		byID[symbols[i].ID] = &symbols[i]
	}
	seen := make(map[string]map[string]bool)
	for _, e := range edges {
		if e.CallerID == e.CalleeID {
			continue
		}
		caller := byID[e.CallerID]
		if caller == nil || caller.Name == "" || strings.HasSuffix(caller.FilePath, "_test.go") {
			continue
		}
		if seen[e.CalleeID] == nil {
			seen[e.CalleeID] = make(map[string]bool)
		}
		if seen[e.CalleeID][caller.Name] {
			continue
		}
		seen[e.CalleeID][caller.Name] = true
		ec.callers[e.CalleeID] = append(ec.callers[e.CalleeID], caller.Name)
	}
	for id := range ec.callers {
		sort.Strings(ec.callers[id])
		if len(ec.callers[id]) > embedMaxCallers {
			ec.callers[id] = ec.callers[id][:embedMaxCallers]
		}
	}
	return ec
}

// shortPkgPath keeps the last two path segments ("internal/store") — enough to
// place a symbol without spending tokens on the module prefix.
func shortPkgPath(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

// composeEmbedText builds the text a symbol is embedded as: name, signature
// and doc (the original composition, kept as the prefix), then package
// location + narrative and caller names when known.
func composeEmbedText(sym *index.Symbol, ec embedContext) string {
	text := sym.Name
	if sym.Signature != "" {
		text += " " + sym.Signature
	}
	if sym.Doc != "" {
		text += " " + sym.Doc
	}
	if sym.PkgPath != "" {
		text += "\npackage " + shortPkgPath(sym.PkgPath)
		if n := ec.pkgNarrative[sym.PkgPath]; n != "" {
			text += ": " + n
		}
	}
	if callers := ec.callers[sym.ID]; len(callers) > 0 {
		text += "\ncalled by: " + strings.Join(callers, ", ")
	}
	return text
}

// filterEmbeddableSymbols returns symbols suitable for embedding (functions, methods,
// types with signatures or docs) as SymbolText with combined text for the embedding model.
func filterEmbeddableSymbols(symbols []index.Symbol, ec embedContext) []embed.SymbolText {
	var result []embed.SymbolText
	for i := range symbols {
		sym := &symbols[i]
		switch sym.Kind {
		case index.KindFunc, index.KindMethod, index.KindType, index.KindInterface, index.KindStruct:
			if sym.Signature != "" || sym.Doc != "" {
				result = append(result, embed.SymbolText{
					ID:   sym.ID,
					Text: composeEmbedText(sym, ec),
				})
			}
		case index.KindVar, index.KindConst, index.KindField:
			// Skip - these typically don't have meaningful signatures for embedding
		}
	}
	return result
}
