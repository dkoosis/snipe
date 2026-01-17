package output

// Response is the top-level response structure for all commands
type Response[T any] struct {
	Results     []T          `json:"results"`
	Meta        Meta         `json:"meta"`
	Error       *Error       `json:"error"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

// Suggestion provides actionable next steps for LLM consumers
type Suggestion struct {
	Command     string `json:"command"`               // The suggested snipe command
	Description string `json:"description"`           // Why this command might be useful
	Priority    int    `json:"priority,omitempty"`    // 1=high, 2=medium, 3=low
	Condition   string `json:"condition,omitempty"`   // When this suggestion applies
}

// Meta contains metadata about the query execution
type Meta struct {
	Command       string            `json:"command"`
	Query         map[string]string `json:"query,omitempty"`
	RepoRoot      string            `json:"repo_root,omitempty"`
	IndexState    IndexState        `json:"index_state"`
	Degraded      []string          `json:"degraded,omitempty"`
	Ms            int64             `json:"ms"`
	Total         int               `json:"total"`
	Offset        int               `json:"offset,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	Truncated     bool              `json:"truncated"`
	TokenEstimate int               `json:"token_estimate,omitempty"`
}

// IndexState represents the state of the index
type IndexState string

const (
	IndexFresh   IndexState = "fresh"
	IndexStale   IndexState = "stale"
	IndexMissing IndexState = "missing"
)

// Error represents an error response
type Error struct {
	Code       string      `json:"code"`
	Message    string      `json:"message"`
	Next       *NextAction `json:"next,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

// NextAction suggests the next command to run
type NextAction struct {
	Command string `json:"command"`
}

// Candidate represents an ambiguous symbol match
type Candidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	File string `json:"file"`
	Kind string `json:"kind"`
}

// Result represents a single navigation result
type Result struct {
	ID         string     `json:"id"`
	File       string     `json:"file"`                // Relative path (for output)
	FileAbs    string     `json:"-"`                   // Absolute path (for file operations, not exported)
	Range      Range      `json:"range"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Match      string     `json:"match"`
	Body       string     `json:"body,omitempty"`
	Score      float64    `json:"score,omitempty"`
	Enclosing  *Enclosing `json:"enclosing,omitempty"`
	Context    *Context   `json:"context,omitempty"`
	Siblings   []Sibling  `json:"siblings,omitempty"`
	EditTarget string     `json:"edit_target"`
}

// Sibling represents another declaration of the same kind in the same file
type Sibling struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Line int    `json:"line"`
}

// Range represents a source code range
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position represents a line:col position
type Position struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// Enclosing represents the enclosing scope of a result
type Enclosing struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature,omitempty"`
	Range     *Range `json:"range,omitempty"`
}

// Context provides surrounding lines for a result
type Context struct {
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// Summary provides a condensed view of results grouped by file
type Summary struct {
	Total int            `json:"total"`
	Files []FileSummary  `json:"files"`
	Kinds map[string]int `json:"kinds,omitempty"`
}

// FileSummary shows the count of results in a single file
type FileSummary struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

// Error codes
const (
	ErrNotFound        = "NOT_FOUND"
	ErrAmbiguousSymbol = "AMBIGUOUS_SYMBOL"
	ErrMissingIndex    = "MISSING_INDEX"
	ErrStaleIndex      = "STALE_INDEX"
	ErrRgNotFound      = "RG_NOT_FOUND"
	ErrInternal        = "INTERNAL_ERROR"
)

// NewNotFoundError creates a NOT_FOUND error
func NewNotFoundError(symbol string) *Error {
	return &Error{
		Code:    ErrNotFound,
		Message: "Symbol not found: " + symbol,
	}
}

// NewAmbiguousError creates an AMBIGUOUS_SYMBOL error
func NewAmbiguousError(symbol string, candidates []Candidate) *Error {
	return &Error{
		Code:       ErrAmbiguousSymbol,
		Message:    "Multiple definitions found for '" + symbol + "'",
		Candidates: candidates,
	}
}

// NewMissingIndexError creates a MISSING_INDEX error
func NewMissingIndexError() *Error {
	return &Error{
		Code:    ErrMissingIndex,
		Message: "No index found. Run: snipe index",
		Next:    &NextAction{Command: "snipe index"},
	}
}

// SuggestionsForDef generates suggestions after a def command
func SuggestionsForDef(result *Result) []Suggestion {
	if result == nil {
		return nil
	}

	suggestions := []Suggestion{
		{
			Command:     "snipe refs " + result.Name,
			Description: "Find all usages of this symbol",
			Priority:    1,
		},
	}

	// If it's a function/method, suggest callers
	if result.Kind == "func" || result.Kind == "method" {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe callers " + result.Name,
			Description: "Find functions that call this",
			Priority:    2,
		})
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe callees " + result.Name,
			Description: "Find functions called by this",
			Priority:    2,
		})
	}

	return suggestions
}

// SuggestionsForRefs generates suggestions after a refs command
func SuggestionsForRefs(symbol string, resultCount int) []Suggestion {
	suggestions := []Suggestion{
		{
			Command:     "snipe def " + symbol,
			Description: "Jump to the definition",
			Priority:    1,
		},
	}

	if resultCount > 10 {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe refs " + symbol + " --summary",
			Description: "Get summary grouped by file",
			Priority:    2,
			Condition:   "many results",
		})
	}

	return suggestions
}

// SuggestionsForSearch generates suggestions after a search command
func SuggestionsForSearch(pattern string, resultCount int) []Suggestion {
	var suggestions []Suggestion

	if resultCount == 0 {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe search \"" + pattern + "\" --context 5",
			Description: "Try with more context lines",
			Priority:    2,
		})
	}

	if resultCount > 20 {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe search \"" + pattern + "\" --summary",
			Description: "Get summary grouped by file",
			Priority:    2,
			Condition:   "many results",
		})
	}

	return suggestions
}

// SuggestionsForCallers generates suggestions after a callers command
func SuggestionsForCallers(symbol string, resultCount int) []Suggestion {
	suggestions := []Suggestion{
		{
			Command:     "snipe def " + symbol,
			Description: "View the function definition",
			Priority:    1,
		},
		{
			Command:     "snipe callees " + symbol,
			Description: "See what this function calls",
			Priority:    2,
		},
	}

	return suggestions
}

// SuggestionsForAmbiguous generates suggestions when symbol is ambiguous
func SuggestionsForAmbiguous(candidates []Candidate) []Suggestion {
	if len(candidates) == 0 {
		return nil
	}

	suggestions := make([]Suggestion, 0, len(candidates))
	for i, c := range candidates {
		if i >= 3 {
			break // Limit to 3 suggestions
		}
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe def --at " + c.File + ":1:1",
			Description: "Use position to specify " + c.Name + " in " + c.File,
			Priority:    1,
		})
	}

	return suggestions
}
