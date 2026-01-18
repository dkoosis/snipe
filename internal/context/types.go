// Package context generates Claude-optimized project context from snipe index.
package context

// ProjectContext is the top-level output structure for snipe context command.
type ProjectContext struct {
	Project      Project      `json:"project" yaml:"project"`
	Architecture Architecture `json:"architecture" yaml:"architecture"`
	Files        Files        `json:"files,omitempty" yaml:"files,omitempty"`
	Symbols      Symbols      `json:"symbols" yaml:"symbols"`
	Meta         Meta         `json:"meta" yaml:"meta"`
}

// BootContext is a minimal context for LLM boot sequences (~2000 tokens).
type BootContext struct {
	Project     string      `json:"project" yaml:"project"`
	Lang        string      `json:"lang" yaml:"lang"`
	Build       string      `json:"build" yaml:"build"`
	Test        string      `json:"test" yaml:"test"`
	EntryPoints []string    `json:"entry_points" yaml:"entry_points"`
	KeySymbols  []SymbolRef `json:"key_symbols" yaml:"key_symbols"`
	Commit      string      `json:"commit" yaml:"commit"`
}

// Project contains basic project information.
type Project struct {
	Name  string   `json:"name" yaml:"name"`
	Root  string   `json:"root" yaml:"root"`
	Lang  []string `json:"lang" yaml:"lang"`
	Build string   `json:"build,omitempty" yaml:"build,omitempty"`
	Test  string   `json:"test,omitempty" yaml:"test,omitempty"`
}

// Architecture describes high-level code structure.
type Architecture struct {
	Components []Component `json:"components" yaml:"components"`
	DataFlows  []DataFlow  `json:"data_flows,omitempty" yaml:"data_flows,omitempty"`
	Boundaries []Boundary  `json:"boundaries,omitempty" yaml:"boundaries,omitempty"`
}

// DataFlow describes a data path between components.
type DataFlow struct {
	From   string `json:"from" yaml:"from"`
	To     string `json:"to" yaml:"to"`
	Via    string `json:"via,omitempty" yaml:"via,omitempty"` // Mechanism: "import", "call", "channel"
	Weight int    `json:"weight,omitempty" yaml:"weight,omitempty"`
}

// Boundary describes ownership of a package/directory.
type Boundary struct {
	Package string   `json:"package" yaml:"package"`
	Owns    []string `json:"owns" yaml:"owns"`                           // What this package is responsible for
	Exports []string `json:"exports,omitempty" yaml:"exports,omitempty"` // Key exported symbols
}

// Component represents a logical grouping of code.
type Component struct {
	Name     string   `json:"name" yaml:"name"`
	Purpose  string   `json:"purpose" yaml:"purpose"`
	Entry    string   `json:"entry,omitempty" yaml:"entry,omitempty"`
	KeyFiles []string `json:"key_files,omitempty" yaml:"key_files,omitempty"`
}

// Files organizes files by concern/purpose.
type Files struct {
	ByConcern map[string][]FileInfo `json:"by_concern" yaml:"by_concern"`
}

// FileInfo describes a file with its purpose and key exports.
type FileInfo struct {
	Path        string   `json:"path" yaml:"path"`
	Description string   `json:"description" yaml:"description"`
	Source      string   `json:"source,omitempty" yaml:"source,omitempty"` // "doc" or "inferred"
	Exports     []string `json:"exports,omitempty" yaml:"exports,omitempty"`
}

// Symbols lists key types and functions.
type Symbols struct {
	Types           []SymbolRef      `json:"types,omitempty" yaml:"types,omitempty"`
	Functions       []SymbolRef      `json:"functions,omitempty" yaml:"functions,omitempty"`
	ExtensionPoints []ExtensionPoint `json:"extension_points,omitempty" yaml:"extension_points,omitempty"`
}

// ExtensionPoint describes a high-centrality symbol suitable for extension.
type ExtensionPoint struct {
	Name        string `json:"name" yaml:"name"`
	Kind        string `json:"kind" yaml:"kind"` // interface, func, type
	File        string `json:"file" yaml:"file"`
	Line        int    `json:"line" yaml:"line"`
	RefCount    int    `json:"ref_count" yaml:"ref_count"`                           // How often it's referenced
	CallerCount int    `json:"caller_count,omitempty" yaml:"caller_count,omitempty"` // For funcs: how often called
	Purpose     string `json:"purpose,omitempty" yaml:"purpose,omitempty"`           // From doc comment
}

// SymbolRef is a lightweight reference to a symbol.
type SymbolRef struct {
	Name string `json:"name" yaml:"name"`
	File string `json:"file" yaml:"file"`
	Line int    `json:"line" yaml:"line"`
}

// Meta contains generation metadata.
type Meta struct {
	GeneratedAt      string `json:"generated_at" yaml:"generated_at"`
	GitCommit        string `json:"git_commit,omitempty" yaml:"git_commit,omitempty"`
	IndexFingerprint string `json:"index_fingerprint,omitempty" yaml:"index_fingerprint,omitempty"`
}
