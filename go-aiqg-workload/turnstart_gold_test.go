package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

// goldTurn is one labeled turn from the research repo's export_turn_gold.py. The file holds redacted
// real prompts, so it is read from SDLC_TURN_GOLD and never checked in; without the variable the test skips.
type goldTurn struct {
	EpisodeID     string   `json:"episode_id"`
	Split         string   `json:"split"` // dev | control | human
	LabelSource   string   `json:"label_source"`
	Prompt        string   `json:"prompt"`
	PrevAssistant string   `json:"prev_assistant"`
	TurnActs      []string `json:"turn_acts"`
}

func wilson(k, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.96
	p := float64(k) / float64(n)
	d := 1 + z*z/float64(n)
	c := p + z*z/(2*float64(n))
	h := z * math.Sqrt(p*(1-p)/float64(n)+z*z/(4*float64(n)*float64(n)))
	return (c - h) / d, (c + h) / d
}

// TestTurnStartAgainstGold measures how often the provisional turn act equals the labeled one.
//
// Agreement is with the labels, not accuracy: dev and control labels come from a reviewer model and are
// unaudited; only the 18 human turns were reviewed. Control turns are held out: this test prints only
// aggregate numbers for them (never per-turn rows or text), so the cues can be tuned on dev without
// reading the audit's control group. SDLC_TURN_SPLITS selects splits (default "dev"); asking for control is
// a deliberate act, done once per version, to report the number.
func TestTurnStartAgainstGold(t *testing.T) {
	path := os.Getenv("SDLC_TURN_GOLD")
	if path == "" {
		t.Skip("SDLC_TURN_GOLD not set")
	}
	want := map[string]bool{}
	for _, s := range strings.Split(envOr("SDLC_TURN_SPLITS", "dev"), ",") {
		want[strings.TrimSpace(s)] = true
	}
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()

	type tally struct{ n, exact, anyOverlap, nPrev, exactPrev int }
	bySplit := map[string]*tally{}
	// per-act counts: tp, fp, fn
	type pr struct{ tp, fp, fn int }
	perAct := map[string]map[TurnAct]*pr{}
	var misses []string

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var g goldTurn
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatal(err)
		}
		if !want[g.Split] {
			continue
		}
		got, _ := ProvisionalTurnAct(ExtractTurnStart(TurnStartInput{Prompt: g.Prompt, PrevAssistant: g.PrevAssistant}))
		gs := map[string]bool{}
		for _, a := range g.TurnActs {
			gs[a] = true
		}
		ps := map[string]bool{}
		for _, a := range got {
			ps[string(a)] = true
		}
		tl := bySplit[g.Split]
		if tl == nil {
			tl = &tally{}
			bySplit[g.Split] = tl
			perAct[g.Split] = map[TurnAct]*pr{}
		}
		tl.n++
		same := len(gs) == len(ps)
		overlap := false
		for a := range gs {
			if !ps[a] {
				same = false
			} else {
				overlap = true
			}
		}
		if same {
			tl.exact++
		}
		// The gateway always has the previous agent message after the first turn; the research sheet kept it for
		// only some turns. So the with-prev subset is the one that represents the inline tier.
		if g.PrevAssistant != "" {
			tl.nPrev++
			if same {
				tl.exactPrev++
			}
		}
		if overlap {
			tl.anyOverlap++
		}
		for _, a := range []TurnAct{ActInstruct, ActAskStatus, ActAskInfo, ActCorrect, ActChallenge, ActApprove, ActAnswer} {
			c := perAct[g.Split][a]
			if c == nil {
				c = &pr{}
				perAct[g.Split][a] = c
			}
			switch {
			case gs[string(a)] && ps[string(a)]:
				c.tp++
			case ps[string(a)]:
				c.fp++
			case gs[string(a)]:
				c.fn++
			}
		}
		if !same && g.Split == "dev" {
			misses = append(misses, fmt.Sprintf("%s got=%v want=%v", g.EpisodeID, got, g.TurnActs))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	var splits []string
	for s := range bySplit {
		splits = append(splits, s)
	}
	sort.Strings(splits)
	for _, s := range splits {
		tl := bySplit[s]
		lo, hi := wilson(tl.exact, tl.n)
		t.Logf("[%s] turns=%d exact-set match %d (%.0f%%, 95%% CI %.0f-%.0f%%), at least one act shared %d (%.0f%%)",
			s, tl.n, tl.exact, 100*float64(tl.exact)/float64(tl.n), 100*lo, 100*hi, tl.anyOverlap, 100*float64(tl.anyOverlap)/float64(tl.n))
		plo, phi := wilson(tl.exactPrev, tl.nPrev)
		t.Logf("    with the agent's previous message (%d turns): exact %d (%.0f%%, CI %.0f-%.0f%%); without it (%d turns): exact %d",
			tl.nPrev, tl.exactPrev, 100*float64(tl.exactPrev)/float64(max(tl.nPrev, 1)), 100*plo, 100*phi, tl.n-tl.nPrev, tl.exact-tl.exactPrev)
		for _, a := range []TurnAct{ActInstruct, ActAskStatus, ActAskInfo, ActCorrect, ActChallenge, ActApprove, ActAnswer} {
			c := perAct[s][a]
			if c.tp+c.fp+c.fn == 0 {
				continue
			}
			p, r := 0.0, 0.0
			if c.tp+c.fp > 0 {
				p = float64(c.tp) / float64(c.tp+c.fp)
			}
			if c.tp+c.fn > 0 {
				r = float64(c.tp) / float64(c.tp+c.fn)
			}
			t.Logf("    %-10s labeled=%2d predicted=%2d  precision %.2f recall %.2f", a, c.tp+c.fn, c.tp+c.fp, p, r)
		}
	}
	for _, m := range misses {
		t.Log("  dev miss:", m)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
