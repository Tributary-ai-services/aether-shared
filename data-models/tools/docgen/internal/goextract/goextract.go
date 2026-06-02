// Package goextract walks a directory of Go source files, finds exported
// struct type declarations, and emits them as IR Documents.
//
// Section detection: when an inline `//` comment appears on its own line (not
// attached to a field) inside a struct body, that comment text becomes the
// "section" header applied to every field that follows it — until the next
// section comment or end of struct. This matches the convention used in
// aether-be/internal/models/*.go where fields are grouped with comments like
// "// Keycloak sync data" or "// Onboarding/Tutorial tracking".
package goextract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Tributary-ai-services/aether-shared/data-models/tools/docgen/internal/ir"
)

// ExtractorVersion identifies the generator that produced a Document; bump
// when the output shape changes so downstream consumers can detect mismatch.
const ExtractorVersion = "go-extract/0.1.0"

// Options configures an extraction run.
type Options struct {
	// Service is the logical service name written into the IR Document
	// (e.g. "aether-be", "audimodal"). Required.
	Service string
	// Dir is the directory of Go files to scan. Non-recursive — callers that
	// want recursion should walk and call Extract per directory.
	Dir string
	// RepoRoot is used to make SourceFile paths repo-relative. If empty,
	// absolute paths are emitted.
	RepoRoot string
	// IncludeUnexported scans private types when true (rare; default false).
	IncludeUnexported bool
}

// Extract parses every .go file in opts.Dir and returns an IR Document
// containing one Model per struct type declaration found.
func Extract(opts Options) (*ir.Document, error) {
	if opts.Service == "" {
		return nil, fmt.Errorf("goextract: Service is required")
	}
	if opts.Dir == "" {
		return nil, fmt.Errorf("goextract: Dir is required")
	}

	entries, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("goextract: read dir %s: %w", opts.Dir, err)
	}

	doc := &ir.Document{
		Service:    opts.Service,
		SourceKind: "go-struct",
		Generator:  ExtractorVersion,
		Generated:  time.Now().UTC().Format(time.RFC3339),
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(opts.Dir, e.Name())
		models, err := extractFile(fset, path, opts)
		if err != nil {
			return nil, fmt.Errorf("goextract: %s: %w", path, err)
		}
		doc.Models = append(doc.Models, models...)
	}

	// Stable ordering: by source file, then by source line.
	sort.SliceStable(doc.Models, func(i, j int) bool {
		if doc.Models[i].SourceFile != doc.Models[j].SourceFile {
			return doc.Models[i].SourceFile < doc.Models[j].SourceFile
		}
		return doc.Models[i].SourceLine < doc.Models[j].SourceLine
	})

	return doc, nil
}

func extractFile(fset *token.FileSet, path string, opts Options) ([]ir.Model, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	relPath := path
	if opts.RepoRoot != "" {
		if r, err := filepath.Rel(opts.RepoRoot, path); err == nil {
			relPath = r
		}
	}

	var models []ir.Model
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		// Hoist the GenDecl-level doc comment so it can attach to a
		// single-spec type declaration: `// Foo bar.\ntype Foo struct{}`.
		genDocComment := commentGroupText(gd.Doc)

		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if !opts.IncludeUnexported && !ts.Name.IsExported() {
				continue
			}
			docComment := commentGroupText(ts.Doc)
			if docComment == "" {
				docComment = genDocComment
			}

			model := ir.Model{
				Name:       ts.Name.Name,
				Kind:       guessKind(ts.Name.Name),
				SourceFile: relPath,
				SourceLine: fset.Position(ts.Pos()).Line,
				DocComment: docComment,
				Fields:     extractFields(fset, src, st),
			}
			models = append(models, model)
		}
	}
	return models, nil
}

func extractFields(fset *token.FileSet, src []byte, st *ast.StructType) []ir.Field {
	if st.Fields == nil {
		return nil
	}

	// Pre-scan: collect free-standing comments inside the struct body that
	// sit on their own line (not attached to a field). These act as section
	// headers for the fields that follow them.
	// We can't get those from the AST directly because the Go parser hangs
	// comments off the next field. Instead, we inspect the bytes between the
	// previous field (or `{`) and each field to find a leading `// section`.
	bodyStart := st.Pos()
	prevEnd := bodyStart

	var fields []ir.Field
	currentSection := ""

	for _, fld := range st.Fields.List {
		// Bytes between prevEnd and the start of this field.
		segStart := fset.Position(prevEnd).Offset
		segEnd := fset.Position(fld.Pos()).Offset
		if segStart < segEnd && segEnd <= len(src) {
			section := findSectionComment(string(src[segStart:segEnd]))
			if section != "" {
				currentSection = section
			}
		}

		typeStr := exprToString(fld.Type)
		tag := parseTag(fld.Tag)
		jsonName, jsonOmit := parseJSONTag(tag["json"])
		validators := parseValidators(tag["validate"])
		required := containsValidator(validators, "required")
		// Optional if: omitempty in json, OR pointer/slice/map type, OR not required.
		optional := jsonOmit || isPointerLike(fld.Type) || !required

		// Strip well-known tags from the generic Tags map so we don't
		// double-report them; keep gorm/neo4j/db tags etc.
		extraTags := map[string]string{}
		for k, v := range tag {
			if k == "json" || k == "validate" {
				continue
			}
			extraTags[k] = v
		}
		if len(extraTags) == 0 {
			extraTags = nil
		}

		fieldDoc := commentGroupText(fld.Doc)
		if fieldDoc == "" {
			fieldDoc = commentGroupText(fld.Comment) // trailing //
		}

		// A field can declare multiple names: `A, B string`. Emit one Field
		// entry per name, sharing type and tags.
		names := fld.Names
		if len(names) == 0 {
			// Embedded field — synthesize the name from the type.
			fields = append(fields, ir.Field{
				Name:       typeStr,
				Type:       typeStr,
				Section:    currentSection,
				DocComment: fieldDoc,
			})
		} else {
			for _, n := range names {
				if !n.IsExported() {
					continue
				}
				fields = append(fields, ir.Field{
					Name:       n.Name,
					JSONName:   jsonName,
					Type:       typeStr,
					Optional:   optional,
					Required:   required,
					Section:    currentSection,
					DocComment: fieldDoc,
					Validators: validators,
					Tags:       extraTags,
				})
			}
		}

		prevEnd = fld.End()
	}
	return fields
}

// findSectionComment scans a chunk of source text (the gap between the
// previous field and the next) for a free-standing `// Foo` comment that
// looks like a group header. Returns the trimmed comment text, or "".
func findSectionComment(chunk string) string {
	// We only treat the LAST such comment in the chunk as the section, since
	// it's the one closest to the field that follows.
	lines := strings.Split(chunk, "\n")
	var found string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "//") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(ln, "//"))
		// Heuristic: section headers are short, sentence-cased, and don't
		// look like full sentences. Skip obvious code-style comments.
		if text == "" || len(text) > 80 {
			continue
		}
		if strings.HasSuffix(text, ".") {
			continue
		}
		found = text
	}
	return found
}

func commentGroupText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var parts []string
	for _, c := range cg.List {
		t := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "/*"))
		t = strings.TrimSpace(strings.TrimSuffix(t, "*/"))
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// parseTag turns a backtick-quoted struct tag literal into a map.
func parseTag(lit *ast.BasicLit) map[string]string {
	out := map[string]string{}
	if lit == nil {
		return out
	}
	raw := strings.Trim(lit.Value, "`")
	t := reflect.StructTag(raw)
	for _, key := range tagKeys(raw) {
		if v, ok := t.Lookup(key); ok {
			out[key] = v
		}
	}
	return out
}

// tagKeys returns every key that appears in a raw struct tag string. We have
// to scan manually because reflect.StructTag has no enumeration method.
func tagKeys(raw string) []string {
	var keys []string
	s := raw
	for len(s) > 0 {
		// Skip leading whitespace.
		for len(s) > 0 && s[0] == ' ' {
			s = s[1:]
		}
		// Find key.
		i := 0
		for i < len(s) && s[i] != ':' && s[i] != '"' && s[i] != ' ' {
			i++
		}
		if i == 0 || i >= len(s) || s[i] != ':' {
			break
		}
		keys = append(keys, s[:i])
		s = s[i+1:]
		if len(s) == 0 || s[0] != '"' {
			break
		}
		s = s[1:]
		j := 0
		// Skip the value, handling escaped quotes.
		for j < len(s) && s[j] != '"' {
			if s[j] == '\\' && j+1 < len(s) {
				j += 2
				continue
			}
			j++
		}
		if j >= len(s) {
			break
		}
		s = s[j+1:]
	}
	return keys
}

func parseJSONTag(v string) (name string, omitempty bool) {
	if v == "" {
		return "", false
	}
	parts := strings.Split(v, ",")
	name = parts[0]
	if name == "-" {
		return "", false
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func parseValidators(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsValidator(vs []string, target string) bool {
	for _, v := range vs {
		if v == target || strings.HasPrefix(v, target+"=") {
			return true
		}
	}
	return false
}

func isPointerLike(e ast.Expr) bool {
	switch e.(type) {
	case *ast.StarExpr, *ast.ArrayType, *ast.MapType, *ast.InterfaceType:
		return true
	}
	return false
}

// exprToString renders a Go type AST back to a human-readable string.
func exprToString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprToString(t.X)
	case *ast.SelectorExpr:
		return exprToString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(t.Elt)
	case *ast.MapType:
		return "map[" + exprToString(t.Key) + "]" + exprToString(t.Value)
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "any"
		}
		return "interface{...}"
	case *ast.StructType:
		return "struct{...}"
	}
	return fmt.Sprintf("%T", e)
}

// guessKind classifies a struct by name suffix. Renderers can use this to
// decide which doc section to place it in (main "node" vs. supporting
// request/response types).
func guessKind(name string) string {
	switch {
	case strings.HasSuffix(name, "Request"):
		return "request"
	case strings.HasSuffix(name, "Response"):
		return "response"
	case strings.HasSuffix(name, "Stats"):
		return "value"
	case strings.HasSuffix(name, "Preferences"):
		return "value"
	}
	return "node"
}
