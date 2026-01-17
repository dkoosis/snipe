package mcp

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/search"
	"github.com/dkoosis/snipe/internal/store"
)

// Server wraps the MCP server with snipe-specific tools
type Server struct {
	server *mcp.Server
	dir    string
}

// NewServer creates a new MCP server for snipe
func NewServer(version string) *Server {
	impl := &mcp.Implementation{
		Name:    "snipe",
		Version: version,
	}

	server := mcp.NewServer(impl, nil)
	s := &Server{
		server: server,
	}

	// Get working directory
	dir, _ := os.Getwd()
	s.dir = dir

	// Register tools
	s.registerDefTool()
	s.registerRefsTool()
	s.registerSearchTool()
	s.registerCallersTool()
	s.registerCalleesTool()

	return s
}

// Run starts the MCP server on stdio transport
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &mcp.StdioTransport{})
}

// DefArgs are the arguments for the def tool
type DefArgs struct {
	Symbol string `json:"symbol,omitempty" jsonschema:"Symbol name to look up"`
	At     string `json:"at,omitempty" jsonschema:"Position (file:line:col) to look up"`
}

func (s *Server) registerDefTool() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "snipe_def",
		Description: "Find symbol definition by name or position. Returns file path, range, and signature.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DefArgs) (*mcp.CallToolResult, any, error) {
		if args.Symbol == "" && args.At == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: provide symbol or at position"},
				},
				IsError: true,
			}, nil, nil
		}

		dbPath := store.DefaultIndexPath(s.dir)
		if !store.Exists(dbPath) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: No index found. Run: snipe index"},
				},
				IsError: true,
			}, nil, nil
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		defer st.Close()

		var symbol *query.SymbolRow

		if args.At != "" {
			pos, err := query.ParsePosition(args.At)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
			symbolID, err := query.ResolvePosition(st.DB(), pos)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
			symbol, err = query.LookupByID(st.DB(), symbolID)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
		} else {
			symbols, err := query.LookupByName(st.DB(), args.Symbol)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
			if len(symbols) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Symbol not found: " + args.Symbol},
					},
				}, nil, nil
			}
			if len(symbols) > 1 {
				var candidates string
				for _, sym := range symbols {
					candidates += sym.Name + " (" + sym.Kind + ") in " + sym.FilePath + "\n"
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Ambiguous symbol. Candidates:\n" + candidates},
					},
				}, nil, nil
			}
			symbol = &symbols[0]
		}

		result := symbol.ToResult()
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: formatResult(&result),
				},
			},
		}, nil, nil
	})
}

// RefsArgs are the arguments for the refs tool
type RefsArgs struct {
	Symbol string `json:"symbol,omitempty" jsonschema:"Symbol name to find references for"`
	At     string `json:"at,omitempty" jsonschema:"Position (file:line:col) to find references for"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

func (s *Server) registerRefsTool() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "snipe_refs",
		Description: "Find all references to a symbol by name or position.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args RefsArgs) (*mcp.CallToolResult, any, error) {
		if args.Symbol == "" && args.At == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: provide symbol or at position"},
				},
				IsError: true,
			}, nil, nil
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		dbPath := store.DefaultIndexPath(s.dir)
		if !store.Exists(dbPath) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: No index found. Run: snipe index"},
				},
				IsError: true,
			}, nil, nil
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		defer st.Close()

		var symbolID string

		if args.At != "" {
			pos, err := query.ParsePosition(args.At)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
			symbolID, err = query.ResolvePosition(st.DB(), pos)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
		} else {
			symbols, err := query.LookupByName(st.DB(), args.Symbol)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil, nil
			}
			if len(symbols) == 0 {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Symbol not found: " + args.Symbol},
					},
				}, nil, nil
			}
			if len(symbols) > 1 {
				var candidates string
				for _, sym := range symbols {
					candidates += sym.Name + " (" + sym.Kind + ") in " + sym.FilePath + "\n"
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Ambiguous symbol. Candidates:\n" + candidates},
					},
				}, nil, nil
			}
			symbolID = symbols[0].ID
		}

		refs, err := query.FindRefs(st.DB(), symbolID, limit, 0)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}

		var text string
		for _, ref := range refs {
			text += formatRefRow(&ref) + "\n"
		}
		if text == "" {
			text = "No references found"
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})
}

// SearchArgs are the arguments for the search tool
type SearchArgs struct {
	Pattern string `json:"pattern" jsonschema:"Pattern to search for (regex supported)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

func (s *Server) registerSearchTool() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "snipe_search",
		Description: "Text search across the codebase using ripgrep.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args SearchArgs) (*mcp.CallToolResult, any, error) {
		if args.Pattern == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: pattern required"},
				},
				IsError: true,
			}, nil, nil
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		results, err := search.Search(s.dir, args.Pattern, limit, 0)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}

		var text string
		for _, r := range results {
			text += formatResult(&r) + "\n"
		}
		if text == "" {
			text = "No matches found"
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})
}

// CallersArgs are the arguments for the callers tool
type CallersArgs struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name to find callers for"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

func (s *Server) registerCallersTool() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "snipe_callers",
		Description: "Find all functions that call a given symbol.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CallersArgs) (*mcp.CallToolResult, any, error) {
		if args.Symbol == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: symbol required"},
				},
				IsError: true,
			}, nil, nil
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		dbPath := store.DefaultIndexPath(s.dir)
		if !store.Exists(dbPath) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: No index found. Run: snipe index"},
				},
				IsError: true,
			}, nil, nil
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		defer st.Close()

		symbols, err := query.LookupByName(st.DB(), args.Symbol)
		if err != nil || len(symbols) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Symbol not found: " + args.Symbol},
				},
			}, nil, nil
		}

		callers, err := query.FindCallers(st.DB(), symbols[0].ID, limit, 0)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}

		var text string
		for _, c := range callers {
			text += fmt.Sprintf("%s (%s:%d)\n", c.CallerName, c.CallerFile, c.CallLine)
		}
		if text == "" {
			text = "No callers found"
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})
}

// CalleesArgs are the arguments for the callees tool
type CalleesArgs struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name to find callees for"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 20)"`
}

func (s *Server) registerCalleesTool() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "snipe_callees",
		Description: "Find all functions called by a given symbol.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CalleesArgs) (*mcp.CallToolResult, any, error) {
		if args.Symbol == "" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: symbol required"},
				},
				IsError: true,
			}, nil, nil
		}

		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}

		dbPath := store.DefaultIndexPath(s.dir)
		if !store.Exists(dbPath) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: No index found. Run: snipe index"},
				},
				IsError: true,
			}, nil, nil
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		defer st.Close()

		symbols, err := query.LookupByName(st.DB(), args.Symbol)
		if err != nil || len(symbols) == 0 {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Symbol not found: " + args.Symbol},
				},
			}, nil, nil
		}

		callees, err := query.FindCallees(st.DB(), symbols[0].ID, limit, 0)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Error: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}

		var text string
		for _, c := range callees {
			text += c.CalleeName + " (" + c.CalleeFile + ")\n"
		}
		if text == "" {
			text = "No callees found"
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil, nil
	})
}

// formatResult formats an output.Result for text display
func formatResult(r *output.Result) string {
	return fmt.Sprintf("%s:%d:%d %s - %s", r.File, r.Range.Start.Line, r.Range.Start.Col, r.Name, r.Match)
}

// formatRefRow formats a query.RefRow for text display
func formatRefRow(r *query.RefRow) string {
	return fmt.Sprintf("%s:%d:%d %s", r.FilePath, r.Line, r.Col, r.Snippet)
}
