package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// End-to-end over real Claude Code transcripts: extract → classify → derive
// outcomes → aggregate by (model, class). This is the shape model_workload_fit
// holds, computed here in memory so the pipeline can be judged before any of
// it is persisted.
//
// Guarded by CLAUDE_TRACES because the corpus is a developer's own machine.

type ccRec struct {
	Type    string `json:"type"`
	Message struct {
		Model   string          `json:"model"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			CacheRead    int `json:"cache_read_input_tokens"`
			CacheCreate  int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type ccBlock struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
	Input     json.RawMessage `json:"input"`
}

type agg struct {
	turns, in, out int
	sig            map[string]struct{ sum, n float64 }
}

func TestFitTableFromRealTraces(t *testing.T) {
	root := os.Getenv("CLAUDE_TRACES")
	if root == "" {
		t.Skip("no traces")
	}
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no transcripts")
	}
	space, adapter := SeedSpace(), CodingAdapter{}
	byClass := map[string]*agg{}
	models := map[string]int{}

	for _, p := range files {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		var turns []TurnRecord
		var meta []ccRec
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		pending := map[string]*ToolCall{}
		for sc.Scan() {
			var r ccRec
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue
			}
			var blocks []ccBlock
			var text string
			if json.Unmarshal(r.Message.Content, &blocks) != nil {
				_ = json.Unmarshal(r.Message.Content, &text)
			}
			switch r.Type {
			case "assistant":
				tr := TurnRecord{Role: "assistant"}
				for _, b := range blocks {
					if b.Type != "tool_use" {
						continue
					}
					var in struct {
						FilePath string `json:"file_path"`
						Command  string `json:"command"`
					}
					_ = json.Unmarshal(b.Input, &in)
					target := in.FilePath
					if target == "" {
						target = in.Command
					}
					tr.Calls = append(tr.Calls, ToolCall{Name: b.Name, Target: target})
					pending[b.ID] = &tr.Calls[len(tr.Calls)-1]
				}
				turns = append(turns, tr)
				meta = append(meta, r)
			case "user":
				resolved := false
				for _, b := range blocks {
					if b.Type != "tool_result" {
						continue
					}
					resolved = true
					if c, ok := pending[b.ToolUseID]; ok {
						c.Resolved, c.Failed = true, b.IsError
					}
				}
				if !resolved {
					turns = append(turns, TurnRecord{Role: "user", Text: text})
					meta = append(meta, ccRec{})
				}
			}
		}
		f.Close()

		for i := range turns {
			if turns[i].Role != "assistant" {
				continue
			}
			m := meta[i]
			models[m.Message.Model]++
			a := space.Assign(Extract(Observation{
				Vantage: VantageSettled, CalledTools: turns[i].Calls, Depth: i,
				InputTokens:  m.Message.Usage.InputTokens + m.Message.Usage.CacheRead,
				OutputTokens: m.Message.Usage.OutputTokens,
				Messages:     []Message{{Role: "user"}},
			}))
			g := byClass[a.ClassID]
			if g == nil {
				g = &agg{sig: map[string]struct{ sum, n float64 }{}}
				byClass[a.ClassID] = g
			}
			g.turns++
			g.in += m.Message.Usage.InputTokens + m.Message.Usage.CacheRead
			g.out += m.Message.Usage.OutputTokens
			for _, s := range adapter.Signals(turns, i) {
				if !s.Observed {
					continue
				}
				e := g.sig[s.Name]
				e.sum += s.Value
				e.n++
				g.sig[s.Name] = e
			}
		}
	}

	ids := make([]string, 0, len(byClass))
	for k := range byClass {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool { return byClass[ids[i]].out > byClass[ids[j]].out })

	var totOut int
	for _, g := range byClass {
		totOut += g.out
	}
	t.Logf("%-20s %8s %10s %8s   %s", "class", "turns", "out tok", "% out", "outcome rates (coverage)")
	for _, id := range ids {
		g := byClass[id]
		var rates string
		for _, n := range []string{SignalEditApplied, SignalCommandExit, SignalRepairLoop, SignalUserCorrection} {
			if e, ok := g.sig[n]; ok && e.n > 0 {
				rates += fmt.Sprintf("  %s %.2f (n=%.0f)", n[5:], e.sum/e.n, e.n)
			}
		}
		if rates == "" {
			rates = "  — none observed"
		}
		t.Logf("%-20s %8d %10d %7.1f%%   %s", id, g.turns, g.out, 100*float64(g.out)/float64(totOut), rates)
	}
}
