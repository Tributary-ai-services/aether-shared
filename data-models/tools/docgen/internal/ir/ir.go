// Package ir defines the normalized intermediate representation that every
// extractor emits and the renderer consumes.
//
// Extractors live in different languages (Go AST walker, Python introspector,
// TypeScript compiler-API caller, YAML CRD reader). They all serialize their
// output to JSON in this shape so the renderer stays language-agnostic.
package ir

// Document is the top-level payload emitted by an extractor: zero or more
// models discovered from one or more source files in a single service.
type Document struct {
	Service    string  `json:"service"`              // e.g. "aether-be"
	SourceKind string  `json:"source_kind"`          // "go-struct" | "pydantic" | "redux-slice" | "k8s-crd" | "proto"
	Generator  string  `json:"generator"`            // tool name + version
	Generated  string  `json:"generated"`            // RFC3339 timestamp
	Models     []Model `json:"models"`
}

// Model represents a single named type — a Go struct, a Pydantic class, a
// Redux slice's state shape, a CRD spec, etc.
type Model struct {
	Name        string   `json:"name"`                    // Type name as declared in source
	Kind        string   `json:"kind"`                    // "node" | "request" | "response" | "value" | "crd" | "slice"
	SourceFile  string   `json:"source_file"`             // Repo-relative path
	SourceLine  int      `json:"source_line"`             // 1-based line of declaration
	DocComment  string   `json:"doc_comment,omitempty"`   // Leading // comment, if any
	Fields      []Field  `json:"fields"`
	Constraints []string `json:"constraints,omitempty"`   // Free-form constraint hints (e.g. "oneof: active inactive")
}

// Field describes a single member of a Model.
type Field struct {
	Name        string            `json:"name"`                  // Source-language name (Go: Go identifier; Pydantic: snake_case)
	JSONName    string            `json:"json_name,omitempty"`   // Serialized name if different (e.g. json:"avatar_url")
	Type        string            `json:"type"`                  // Human-readable type ("string", "*time.Time", "map[string]any", "UUID")
	Optional    bool              `json:"optional"`              // omitempty OR pointer OR not-required-by-validator
	Required    bool              `json:"required"`              // Explicitly validate:"required" or schema-required
	Section     string            `json:"section,omitempty"`     // Group header derived from preceding source comment
	DocComment  string            `json:"doc_comment,omitempty"` // Trailing or leading comment for the field
	Validators  []string          `json:"validators,omitempty"`  // Raw validator rules (e.g. "min=3", "max=50", "oneof=a b c")
	Tags        map[string]string `json:"tags,omitempty"`        // Other struct tags (gorm, neo4j, etc.) keyed by tag name
}
