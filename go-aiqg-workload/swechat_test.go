package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

type seg struct {
	Intent string   `json:"intent"`
	Tools  []string `json:"tools"`
	In     int      `json:"in"`
	Out    int      `json:"out"`
	Resp   int      `json:"resp"`
	Depth  int      `json:"depth"`
}

// expected maps SWE-chat's labelled prompt_intent to the class the seed space
// should produce, per the taxonomy's Axis-1 mapping. Empty means "no class is
// expected" — the documented unseparable cases.
var expected = map[string]string{
	"understand":      "code.discovery",
	"refactor":        "code.modification",
	"create new code": "code.modification", // generation folds into modification
	"test":            "code.execution",    // validation not separable from execution
	"git":             "code.execution",
	"debug":           "", // not separable from conversation
	"other":           "",
	"connect":         "",
	"review":          "",
}

func TestSWEChatConfusionMatrix(t *testing.T) {
	path := os.Getenv("SWECHAT")
	f, err := os.Open(path)
	if err != nil {
		t.Skip("no corpus")
	}
	defer f.Close()
	s := SeedSpace()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)

	matrix := map[string]map[string]int{}
	totals := map[string]int{}
	noTools := map[string]int{}
	for sc.Scan() {
		var g seg
		if json.Unmarshal(sc.Bytes(), &g) != nil {
			continue
		}
		calls := make([]ToolCall, 0, len(g.Tools))
		for _, n := range g.Tools {
			calls = append(calls, ToolCall{Name: n})
		}
		// Condition on what the classifier can possibly see. 44% of labelled
		// segments contain no tool call at all and 40% carry no token counts
		// (the corpus's usage fields are documented as sparse), so an
		// unconditioned number measures the corpus, not the classifier.
		if len(calls) == 0 {
			noTools[g.Intent]++
			continue
		}
		a := s.Assign(Extract(Observation{
			Vantage: VantageSettled, CalledTools: calls, Depth: g.Depth,
			Messages: []Message{{Role: "user"}},
		}))
		if matrix[g.Intent] == nil {
			matrix[g.Intent] = map[string]int{}
		}
		matrix[g.Intent][a.ClassID]++
		totals[g.Intent]++
	}

	intents := make([]string, 0, len(totals))
	for k := range totals {
		intents = append(intents, k)
	}
	sort.Slice(intents, func(i, j int) bool { return totals[intents[i]] > totals[intents[j]] })

	t.Log("SWE-chat prompt_intent  ->  seed class space")
	var agree, judged int
	for _, in := range intents {
		row := matrix[in]
		preds := make([]string, 0, len(row))
		for k := range row {
			preds = append(preds, k)
		}
		sort.Slice(preds, func(i, j int) bool { return row[preds[i]] > row[preds[j]] })
		var parts string
		for i, p := range preds {
			if i == 3 {
				break
			}
			parts += fmt.Sprintf("  %s %.0f%%", p, 100*float64(row[p])/float64(totals[in]))
		}
		want := expected[in]
		mark := "  (no class expected)"
		if want != "" {
			hit := row[want]
			pct := 100 * float64(hit) / float64(totals[in])
			mark = fmt.Sprintf("  → want %s: %.1f%%", want, pct)
			agree += hit
			judged += totals[in]
		}
		t.Logf("%-16s n=%6d %s%s", in, totals[in], parts, mark)
	}
	var skipped int
	for _, v := range noTools {
		skipped += v
	}
	t.Logf("excluded %d segments with no tool calls (%.1f%%) — structurally unclassifiable by a tool-family classifier", skipped, 100*float64(skipped)/float64(skipped+judged+matrix["other"]["x"]))
	t.Logf("agreement on the five judgeable intents, tool-bearing segments only: %.1f%% (%d/%d)",
		100*float64(agree)/float64(judged), agree, judged)
}
