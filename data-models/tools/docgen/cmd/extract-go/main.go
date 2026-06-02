// extract-go scans a directory of Go source files and prints the normalized
// IR JSON to stdout. The renderer consumes this directly or via a pipeline.
//
// Usage:
//
//	extract-go -service aether-be -dir /path/to/aether-be/internal/models \
//	  -repo-root /path/to/repo > models.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Tributary-ai-services/aether-shared/data-models/tools/docgen/internal/goextract"
)

func main() {
	var (
		service  = flag.String("service", "", "Logical service name written into IR (required)")
		dir      = flag.String("dir", "", "Directory of .go files to scan (required)")
		repoRoot = flag.String("repo-root", "", "Optional repo root to make SourceFile paths relative")
		pretty   = flag.Bool("pretty", true, "Pretty-print JSON output")
	)
	flag.Parse()

	if *service == "" || *dir == "" {
		flag.Usage()
		os.Exit(2)
	}

	doc, err := goextract.Extract(goextract.Options{
		Service:  *service,
		Dir:      *dir,
		RepoRoot: *repoRoot,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(doc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
