package output

// Response is the top-level response structure for all commands
type Response[T any] struct {
	Results []T    `json:"results"`
	Meta    Meta   `json:"meta"`
	Error   *Error `json:"error"`
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
	File       string     `json:"file"`
	Range      Range      `json:"range"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Match      string     `json:"match"`
	Body       string     `json:"body,omitempty"`
	Enclosing  *Enclosing `json:"enclosing,omitempty"`
	Context    *Context   `json:"context,omitempty"`
	EditTarget string     `json:"edit_target"`
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
