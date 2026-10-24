package workload

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// goldTask is one labeled task from the research repo's export_activity_gold.py. The file holds redacted
// real commands and paths, so it is read from SDLC_ACTIVITY_GOLD and never checked in; without the variable
// the test skips.
type goldTask struct {
	UnitID   string `json:"unit_id"`
	Split    string `json:"split"` // dev | control | human
	Complete bool   `json:"complete"`
	Activity string `json:"activity"`
	Calls    []struct {
		Name   string `json:"name"`
		Target string `json:"target"`
	} `json:"calls"`
}

// defaultBookkeeping is the research corpus's bookkeeping list: the backlog file and both of its published pages,
// memory notes and the mirror-sync scripts. It is a tenant's config, here for the measurement only.
const defaultBookkeeping = "BACKLOG.md,BACKLOG.artifact,CLAUDE-PLANS-BACKLOG,artifact/8f9c849f,artifact/JjZKzZLRHP9i8yAkqtdUmx," +
	"/memory/,MEMORY.md,sync-to-repo,refresh-pr-strip"

// TestActivityAgainstGold measures the coarse activity against labeled tasks.
//
// Two numbers matter because the table abstains: coverage (how many tasks it claims) and precision when
// it claims. Agreement is with the labels, not accuracy (dev and control are unaudited reviewer-model
// labels). Control is held out exactly as in TestTurnStartAgainstGold: aggregates only, asked for by
// SDLC_ACTIVITY_SPLITS. SDLC_ACTIVITY_SHOW=1 prints dev misses with their calls, for tuning; it never
// prints control or human rows.
func TestActivityAgainstGold(t *testing.T) {
	path := os.Getenv("SDLC_ACTIVITY_GOLD")
	if path == "" {
		t.Skip("SDLC_ACTIVITY_GOLD not set")
	}
	want := map[string]bool{}
	for _, s := range strings.Split(envOr("SDLC_ACTIVITY_SPLITS", "dev"), ",") {
		want[strings.TrimSpace(s)] = true
	}
	bookkeeping := strings.Split(envOr("SDLC_BOOKKEEPING", defaultBookkeeping), ",")
	show := os.Getenv("SDLC_ACTIVITY_SHOW") == "1"

	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()

	type pr struct{ tp, fp, fn int }
	type tally struct {
		n, claimed, right int
		byConf            map[ActConfidence][2]int // claimed, right
		per               map[string]*pr
		confusion         map[[2]string]int // label, predicted ("-" when abstained)
	}
	bySplit := map[string]*tally{}
	var misses []string

	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	for sc.Scan() {
		var g goldTask
		if err := json.Unmarshal(sc.Bytes(), &g); err != nil {
			t.Fatal(err)
		}
		if !want[g.Split] {
			continue
		}
		in := ActivityInput{Bookkeeping: bookkeeping}
		for _, c := range g.Calls {
			in.Calls = append(in.Calls, ToolCall{Name: c.Name, Target: c.Target})
		}
		res := ExtractActivity(in)
		tl := bySplit[g.Split]
		if tl == nil {
			tl = &tally{byConf: map[ActConfidence][2]int{}, per: map[string]*pr{}, confusion: map[[2]string]int{}}
			bySplit[g.Split] = tl
		}
		tl.n++
		got := string(res.Activity)
		pk := got
		if pk == "" {
			pk = "-"
		}
		tl.confusion[[2]string{g.Activity, pk}]++
		if tl.per[g.Activity] == nil {
			tl.per[g.Activity] = &pr{}
		}
		if got != "" {
			if tl.per[got] == nil {
				tl.per[got] = &pr{}
			}
			tl.claimed++
			bc := tl.byConf[res.Confidence]
			bc[0]++
			if got == g.Activity {
				tl.right++
				bc[1]++
				tl.per[got].tp++
			} else {
				tl.per[got].fp++
				tl.per[g.Activity].fn++
			}
			tl.byConf[res.Confidence] = bc
		} else {
			tl.per[g.Activity].fn++
		}
		if show && g.Split == "dev" && got != g.Activity {
			var cs []string
			for _, c := range g.Calls {
				s := c.Name
				if c.Target != "" {
					s += ": " + c.Target
				}
				if len(s) > 160 {
					s = s[:160]
				}
				cs = append(cs, s)
			}
			misses = append(misses, fmt.Sprintf("%s label=%s got=%s verbs=%v\n      %s", g.UnitID, g.Activity, pk, res.Verbs, strings.Join(cs, "\n      ")))
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
		plo, phi := wilson(tl.right, tl.claimed)
		t.Logf("[%s] tasks=%d claimed=%d (coverage %.0f%%), right when claimed %d (precision %.0f%%, 95%% CI %.0f-%.0f%%), right over all tasks %.0f%%",
			s, tl.n, tl.claimed, 100*float64(tl.claimed)/float64(tl.n), tl.right, 100*float64(tl.right)/float64(max(tl.claimed, 1)), 100*plo, 100*phi,
			100*float64(tl.right)/float64(tl.n))
		for _, c := range []ActConfidence{ConfHigh, ConfMed, ConfLow} {
			bc := tl.byConf[c]
			if bc[0] > 0 {
				t.Logf("    confidence %-4s claimed=%3d precision %.0f%%", c, bc[0], 100*float64(bc[1])/float64(bc[0]))
			}
		}
		var acts []string
		for a := range tl.per {
			acts = append(acts, a)
		}
		sort.Strings(acts)
		for _, a := range acts {
			c := tl.per[a]
			p, r := 0.0, 0.0
			if c.tp+c.fp > 0 {
				p = float64(c.tp) / float64(c.tp+c.fp)
			}
			if c.tp+c.fn > 0 {
				r = float64(c.tp) / float64(c.tp+c.fn)
			}
			t.Logf("    %-16s labeled=%3d predicted=%3d  precision %.2f recall %.2f", a, c.tp+c.fn, c.tp+c.fp, p, r)
		}
		if s == "dev" {
			var keys [][2]string
			for k := range tl.confusion {
				if k[0] != k[1] {
					keys = append(keys, k)
				}
			}
			sort.Slice(keys, func(i, j int) bool { return tl.confusion[keys[i]] > tl.confusion[keys[j]] })
			for _, k := range keys {
				t.Logf("    confused: label %-16s -> %-16s %d", k[0], k[1], tl.confusion[k])
			}
		}
	}
	for _, m := range misses {
		t.Log("  dev miss:", m)
	}
}
