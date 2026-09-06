// Package workload derives the universal feature vector that AIQG workload
// classification is built on, and it is deliberately the only place that does.
//
// # Why a shared module rather than a copy per service
//
// Three callers need the same answer: the gateway computes features pre-route
// (to match an experiment cohort), aiqg-dashboard-be recomputes them post-hoc
// from the event history (to measure), and aiqg-import derives them from
// imported traces. A copy per service is not a style problem — two services
// disagreeing about what counts as a modification would produce two different
// fit tables and no way to tell which is right. The debt already exists once:
// aiqg-import/workflow/classifier.go says "the canonical classifier should
// move to a shared module both repos import. Keep the cue regexes in sync
// until then." This is that module.
//
// # Structural, not semantic
//
// Every feature here is a count, a ratio, a bucket or a flag. Text is READ to
// compute those — a retrieval-marker count needs to look at the prompt — but
// no text is ever retained in a Features value, and nothing here calls an
// embedding model. That is what makes the same extractor safe over proprietary
// source, regulated documents and customer PII alike, and it is why the vector
// can be pooled across tenants (as centroids) without pooling their traffic.
//
// A semantic tier exists in the design and is opt-in, off by default, and
// consumer-side only. It is not in this package.
//
// # Cheap enough for the request path
//
// The gateway computes these before routing, so the extractor allocates little
// and does no I/O, no reflection and no inference. Anything that cannot hold
// that bar belongs in the settled vantage, computed off the hot path.
package workload

import (
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
)

// ExtractorVersion stamps every Features value.
//
// Bump it when a feature's MEANING changes, not when one is added: consumers
// key stored rows on this, so a bump means "recompute", and a bump that
// changed nothing costs a full re-derivation of every event. Adding a field
// that older rows simply lack is not a meaning change.
const ExtractorVersion = "fx-1"

// Vantage records where a Features value was computed, because the two
// vantages can see different things and must never be silently compared.
//
// Inline runs before the response exists — it cannot know output tokens, the
// finish reason, or which tools were actually called, only which were offered.
// Settled runs after and can see all of it. A drift monitor compares them on
// purpose; an aggregate that mixes them is a bug.
type Vantage string

const (
	VantageInline  Vantage = "inline"
	VantageSettled Vantage = "settled"
)

// ToolFamily is a coarse behavioural class for a tool, derived from its name.
//
// Families rather than names, for two reasons. Names are customer data — an
// "acme_billing_export" tool identifies a customer, so a name may not travel
// into pooled aggregates while a family may. And names are unstable: every
// coding agent spells its file editor differently, but they all read, write or
// execute, and it is the verb that predicts how a model will behave.
type ToolFamily string

const (
	FamilyRead     ToolFamily = "read"     // read a file, fetch a page, list a directory
	FamilySearch   ToolFamily = "search"   // grep, glob, vector search, query
	FamilyEdit     ToolFamily = "edit"     // write, patch, apply a diff
	FamilyExec     ToolFamily = "exec"     // run a command, a test, a build
	FamilyDelegate ToolFamily = "delegate" // spawn a subagent, plan, track work
	FamilyOther    ToolFamily = "other"    // recognised as a tool, unrecognised as a verb
)

// familyTokens map a NAME TOKEN to a family.
//
// Token equality, not substring containment. Substrings look tempting and are
// a trap: "frobnicate" contains "cat", "TodoWrite" contains "write", and both
// would be classified confidently wrong. A tool name is a compound of words —
// str_replace_editor, run_terminal_cmd, NotebookRead — so splitting it into
// words and matching those exactly is both stricter and closer to how the
// names are actually built.
var familyTokens = map[string]ToolFamily{
	// delegate first in the resolution order below, because a name like
	// TodoWrite carries two verbs and the outer one is what it does.
	"task": FamilyDelegate, "todo": FamilyDelegate, "agent": FamilyDelegate,
	"plan": FamilyDelegate, "delegate": FamilyDelegate, "spawn": FamilyDelegate,
	"subagent": FamilyDelegate,

	"grep": FamilySearch, "glob": FamilySearch, "search": FamilySearch,
	"query": FamilySearch, "retrieve": FamilySearch, "retrieval": FamilySearch,
	"find": FamilySearch, "lookup": FamilySearch,

	"edit": FamilyEdit, "write": FamilyEdit, "patch": FamilyEdit,
	"apply": FamilyEdit, "replace": FamilyEdit, "editor": FamilyEdit,
	"insert": FamilyEdit, "update": FamilyEdit,

	"exec": FamilyExec, "bash": FamilyExec, "shell": FamilyExec,
	"run": FamilyExec, "command": FamilyExec, "cmd": FamilyExec,
	"terminal": FamilyExec, "test": FamilyExec, "build": FamilyExec,

	"read": FamilyRead, "fetch": FamilyRead, "list": FamilyRead,
	"open": FamilyRead, "view": FamilyRead, "cat": FamilyRead,
	"notebook": FamilyRead, "ls": FamilyRead,
}

// familyOrder resolves a name carrying more than one verb. TodoWrite is a
// delegation that happens to write; run_tests is an execution that happens to
// test. The earlier family wins, and the order is fixed so two replicas cannot
// disagree.
var familyOrder = []ToolFamily{FamilyDelegate, FamilyEdit, FamilyExec, FamilySearch, FamilyRead}

// tokenize splits a tool name into lowercase words, handling snake_case,
// kebab-case, dotted namespaces and CamelCase alike.
func tokenize(name string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ' || r == '/' || r == ':':
			flush()
		case r >= 'A' && r <= 'Z':
			// A capital starts a new word unless it is inside a run of them
			// (HTTPFetch → http, fetch), which is why the next rune matters.
			if i > 0 && (runes[i-1] < 'A' || runes[i-1] > 'Z') {
				flush()
			} else if i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z' {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

// FamilyOf classifies one tool name. Unknown names are FamilyOther rather than
// a guess: a wrong family is worse than an unknown one, because it moves a
// request into a class it does not belong to and nothing downstream can tell.
func FamilyOf(name string) ToolFamily {
	toks := tokenize(strings.TrimSpace(name))
	if len(toks) == 0 {
		return FamilyOther
	}
	seen := map[ToolFamily]bool{}
	for _, t := range toks {
		if fam, ok := familyTokens[t]; ok {
			seen[fam] = true
		}
	}
	for _, fam := range familyOrder {
		if seen[fam] {
			return fam
		}
	}
	return FamilyOther
}

// retrievalMarker matches the separator convention that RAG-shaped prompts use
// to fence injected context. Kept identical to the cue the existing
// classifiers already use, so this module does not quietly redefine "rag"
// while claiming to unify the definition.
var retrievalMarker = regexp.MustCompile(`(?i)\b(document|context|source|chunk|passage|reference)(\s*#?\s*\d+)?\s*:`)

// Message is one turn's role and text. Text is read and never retained.
type Message struct {
	Role string
	Text string
}

// ToolCall is a tool the model actually invoked (settled vantage only).
// Failed records whether its result came back an error, which is the raw
// material for the coding outcome adapters downstream.
type ToolCall struct {
	Name   string
	Failed bool
}

// Observation is the neutral input every caller can populate.
//
// Neutral on purpose: taking the gateway's ChatRequest would couple this module
// to one service's wire types and make the importer's job a translation
// exercise. Callers fill what they can see and leave the rest zero — the
// vantage records which "rest" that was.
type Observation struct {
	Vantage Vantage

	Messages     []Message
	SystemPrompt string
	Depth        int // conversation turn index; 0 for a first turn

	// Offered tools (inline: definitions passed in) and called tools
	// (settled: what was actually invoked). Inline callers fill OfferedTools;
	// settled callers fill both.
	OfferedTools []string
	CalledTools  []ToolCall

	JSONSchemaOut bool
	Streaming     bool
	Multimodal    bool
	Attachments   int

	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int

	FinishReason string
	Retried      bool
}

// Features is the vector. Every field is a count, a ratio, a bucket or a flag.
type Features struct {
	Vantage          Vantage `json:"vantage"`
	ExtractorVersion string  `json:"extractor_version"`

	// Shape
	MessageCount      int     `json:"message_count"`
	Depth             int     `json:"depth"`
	SystemBytesBucket int     `json:"system_bytes_bucket"` // 0 none · 1 <1KiB · 2 <8KiB · 3 ≥8KiB
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	InOutRatio        float64 `json:"in_out_ratio"` // 0 when output is unknown, never a divide-by-zero

	// Capability
	HasTools      bool               `json:"has_tools"`
	ToolCount     int                `json:"tool_count"`
	ToolSignature uint64             `json:"tool_signature"` // FNV-1a over sorted names; never the names
	FamilyMix     map[ToolFamily]int `json:"family_mix"`     // called tools where known, else offered
	JSONSchemaOut bool               `json:"json_schema_out"`
	Streaming     bool               `json:"streaming"`
	Multimodal    bool               `json:"multimodal"`

	// Context
	RetrievalMarkers  int `json:"retrieval_markers"`
	CacheReadTokens   int `json:"cache_read_tokens"`
	CacheCreateTokens int `json:"cache_creation_tokens"`
	Attachments       int `json:"attachments"`

	// Behaviour — settled only; zero at the inline vantage by definition
	FinishReason string `json:"finish_reason,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
	Retried      bool   `json:"retried,omitempty"`
	ToolFailures int    `json:"tool_failures,omitempty"`
}

// Extract computes the vector. Pure: no I/O, no inference, no retained text.
func Extract(o Observation) Features {
	f := Features{
		Vantage:           o.Vantage,
		ExtractorVersion:  ExtractorVersion,
		MessageCount:      len(o.Messages),
		Depth:             o.Depth,
		SystemBytesBucket: bytesBucket(len(o.SystemPrompt)),
		InputTokens:       o.InputTokens,
		OutputTokens:      o.OutputTokens,
		JSONSchemaOut:     o.JSONSchemaOut,
		Streaming:         o.Streaming,
		Multimodal:        o.Multimodal,
		CacheReadTokens:   o.CacheReadTokens,
		CacheCreateTokens: o.CacheCreationTokens,
		Attachments:       o.Attachments,
		FinishReason:      o.FinishReason,
		Retried:           o.Retried,
		FamilyMix:         map[ToolFamily]int{},
	}
	if o.OutputTokens > 0 {
		f.InOutRatio = float64(o.InputTokens) / float64(o.OutputTokens)
	}
	f.Truncated = o.FinishReason == "length"

	// Called tools are evidence; offered tools are only opportunity. Prefer
	// what happened, fall back to what was available — which is all the inline
	// vantage can ever know.
	names := make([]string, 0, len(o.CalledTools)+len(o.OfferedTools))
	if len(o.CalledTools) > 0 {
		for _, c := range o.CalledTools {
			names = append(names, c.Name)
			f.FamilyMix[FamilyOf(c.Name)]++
			if c.Failed {
				f.ToolFailures++
			}
		}
	} else {
		for _, n := range o.OfferedTools {
			names = append(names, n)
			f.FamilyMix[FamilyOf(n)]++
		}
	}
	f.ToolCount = len(names)
	f.HasTools = f.ToolCount > 0
	f.ToolSignature = signature(names)

	for _, m := range o.Messages {
		f.RetrievalMarkers += len(retrievalMarker.FindAllStringIndex(m.Text, -1))
	}
	f.RetrievalMarkers += len(retrievalMarker.FindAllStringIndex(o.SystemPrompt, -1))

	return f
}

// signature hashes the SORTED, lowercased tool set.
//
// Sorted so the same tools in a different order hash the same — otherwise the
// signature would fragment identical workloads across arbitrarily many values.
// Hashed so it can travel into pooled aggregates without carrying customer
// tool names with it.
func signature(names []string) uint64 {
	if len(names) == 0 {
		return 0
	}
	norm := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		l := strings.ToLower(strings.TrimSpace(n))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		norm = append(norm, l)
	}
	sort.Strings(norm)
	h := fnv.New64a()
	for _, n := range norm {
		_, _ = h.Write([]byte(n))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// bytesBucket coarsens a size so a system prompt's LENGTH can inform a class
// without its content doing so, and so a one-token edit does not move a
// request between classes.
func bytesBucket(n int) int {
	switch {
	case n == 0:
		return 0
	case n < 1024:
		return 1
	case n < 8192:
		return 2
	default:
		return 3
	}
}

// DominantFamily returns the most-used tool family and its share.
//
// Share matters as much as the winner: a turn that is 90% edit calls is a
// different workload from one that edits once among nine reads, and a bare
// argmax cannot tell them apart. Returns ("", 0) when no tools are involved.
func (f Features) DominantFamily() (ToolFamily, float64) {
	if f.ToolCount == 0 || len(f.FamilyMix) == 0 {
		return "", 0
	}
	var best ToolFamily
	bestN := -1
	// Iterate a fixed order, not the map's: ties must resolve identically on
	// every host, or the same request classifies differently across replicas.
	for _, fam := range []ToolFamily{FamilyEdit, FamilyExec, FamilySearch, FamilyRead, FamilyDelegate, FamilyOther} {
		if n := f.FamilyMix[fam]; n > bestN {
			best, bestN = fam, n
		}
	}
	if bestN <= 0 {
		return "", 0
	}
	return best, float64(bestN) / float64(f.ToolCount)
}
