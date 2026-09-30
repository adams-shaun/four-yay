package searchbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestStateSpecCorpus materialises every dumped spec under
// SEARCHBENCH_SPECS_DIR (files {"row", "turn", "form", "spec"} written by
// the W1 dump script from upstream's reconstruction; never committed),
// checks the staged position against the spec, reaches each item kind's
// decision, builds the canonical options and matches the labels, and logs
// the counts and the rejection reasons.
func TestStateSpecCorpus(t *testing.T) {
	dir := os.Getenv("SEARCHBENCH_SPECS_DIR")
	if dir == "" {
		t.Skip("SEARCHBENCH_SPECS_DIR unset")
	}
	reg := testutil.CorpusRegistry(t)
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no specs in %s (%v)", dir, err)
	}
	sort.Strings(files)
	type stat struct{ candidates, prechecked, materialized, reached, labelled int }
	stats := map[DecisionType]*stat{}
	reasons := map[string]int{}
	details := map[string]int{}
	counts := map[string]int{}
	why := func(kind DecisionType, err error) {
		var r *Refusal
		if errors.As(err, &r) {
			reasons[string(kind)+": "+r.Code]++
			d := r.Detail
			if len(d) > 70 {
				d = d[:70]
			}
			details[string(kind)+": "+r.Code+": "+d]++
			return
		}
		reasons[string(kind)+": error"]++
		details[string(kind)+": error: "+err.Error()]++
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wrap struct {
			Row  int             `json:"row"`
			Turn int             `json:"turn"`
			Form string          `json:"form"`
			Spec json.RawMessage `json:"spec"`
		}
		if err := json.Unmarshal(raw, &wrap); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		spec, err := statespec.Parse(wrap.Spec)
		if err != nil {
			t.Fatalf("%s: strict decode: %v", f, err)
		}
		kinds := []DecisionType{DecisionSpell, DecisionHold, DecisionAttack}
		if wrap.Form == "block" {
			kinds = []DecisionType{DecisionBlock}
		}
		seed := uint64(900000 + wrap.Row)
		for _, kind := range kinds {
			st := stats[kind]
			if st == nil {
				st = &stat{}
				stats[kind] = st
			}
			st.candidates++
			if err := PrecheckKind(spec, kind); err != nil {
				why(kind, err)
				continue
			}
			st.prechecked++
			m, err := Materialize(reg, spec, seed)
			if err != nil {
				why(kind, err)
				continue
			}
			if msg := checkStaged(m); msg != "" {
				t.Errorf("%s: staged position differs: %s", filepath.Base(f), msg)
				continue
			}
			st.materialized++
			opts, err := ItemReachOptions(spec, kind, seed)
			if err != nil {
				why(kind, err)
				continue
			}
			r, err := Reach(m.Engine, kind, opts)
			if err != nil {
				why(kind, err)
				continue
			}
			st.reached++
			counts["bot answers"] += r.BotAnswers
			counts["passes"] += r.Passes
			c, err := BuildCanon(m, r)
			if err != nil {
				why(kind, err)
				continue
			}
			for i := range c.Decision.Options {
				o := c.Decision.Options[i]
				if o.Kind == "activate" || o.Kind == "concede" {
					continue
				}
				if _, _, err := c.Project(m.Engine, singleIntent(c, i)); err != nil && kind != DecisionAttack && kind != DecisionBlock {
					t.Errorf("%s: %s: project option %q: %v", filepath.Base(f), kind, o.Label, err)
				}
			}
			lab, err := LabelItem(m, c)
			if err != nil {
				why(kind, err)
				continue
			}
			st.labelled++
			counts[fmt.Sprintf("%s options", kind)] += len(c.Options)
			if lab.Act {
				counts[fmt.Sprintf("%s act", kind)]++
			}
		}
	}
	for _, kind := range []DecisionType{DecisionSpell, DecisionHold, DecisionAttack, DecisionBlock} {
		if st := stats[kind]; st != nil {
			t.Logf("%-6s candidates %4d  prechecked %4d  materialised %4d  reached %4d  labelled %4d",
				kind, st.candidates, st.prechecked, st.materialized, st.reached, st.labelled)
		}
	}
	logSorted(t, "reason", reasons)
	logSorted(t, "detail", details)
	logSorted(t, "count", counts)
}

func singleIntent(c *Canon, i int) decision.Intent {
	return decision.Intent{Seq: c.Decision.Seq, Player: c.Decision.Player, Choices: []int{i}}
}

func logSorted(t *testing.T, what string, m map[string]int) {
	type kv struct {
		k string
		v int
	}
	var rs []kv
	for k, v := range m {
		rs = append(rs, kv{k, v})
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].v > rs[j].v || rs[i].v == rs[j].v && rs[i].k < rs[j].k })
	for i, r := range rs {
		if i >= 40 {
			break
		}
		t.Logf("%s %5d  %s", what, r.v, r.k)
	}
}

// checkStaged compares the staged engine with its spec: turn, active
// player, step, life, and each seat's zone sizes.
func checkStaged(m *Materialized) string {
	e, s := m.Engine, m.Spec
	var bad []string
	if int(e.G.Turn) != s.Turn || e.G.Active != SeatID(s.ActivePlayer) {
		bad = append(bad, fmt.Sprintf("turn %d/%d active %d", e.G.Turn, s.Turn, e.G.Active))
	}
	if step, _ := StepOf(s.Step); e.G.Step != step {
		bad = append(bad, fmt.Sprintf("step %v/%s", e.G.Step, s.Step))
	}
	owned := s.Owned()
	for _, seat := range statespec.Seats {
		p, id := s.Players[seat], SeatID(seat)
		if int(e.G.Players[id].Life) != p.Life {
			bad = append(bad, fmt.Sprintf("%s life %d/%d", seat, e.G.Players[id].Life, p.Life))
		}
		bf := 0
		for _, perm := range p.Battlefield {
			bf += perm.Count
		}
		lib := len(p.Decklist) - len(owned[seat]) - p.HandUnknown + len(p.LibraryTop)
		if p.LibrarySize != nil && *p.LibrarySize < lib {
			lib = *p.LibrarySize
		}
		for _, z := range []struct {
			zone state.Zone
			want int
		}{{state.ZHand, len(p.Hand) + p.HandUnknown}, {state.ZGraveyard, len(p.Graveyard)}, {state.ZExile, len(p.Exile)},
			{state.ZBattlefield, bf}, {state.ZLibrary, lib}} {
			if got := len(e.G.Zone(z.zone, id)); got != z.want {
				bad = append(bad, fmt.Sprintf("%s %v %d/%d", seat, z.zone, got, z.want))
			}
		}
	}
	attacking := map[string]bool{} // lookup only
	for _, a := range s.Attackers {
		attacking[a.Attacker] = true
	}
	for _, seat := range statespec.Seats {
		for i, perm := range s.Players[seat].Battlefield {
			for k, id := range m.Perms[seat][i] {
				o := e.G.Obj(id)
				ref := fmt.Sprintf("%s:%d#%d", seat, i, k+1)
				atk := perm.ID != "" && (attacking[seat+":"+perm.ID] || attacking[fmt.Sprintf("%s:%s#%d", seat, perm.ID, k+1)])
				switch {
				case o == nil || o.Zone != state.ZBattlefield:
					bad = append(bad, ref+" not on the battlefield")
				case o.Controller != SeatID(seat):
					bad = append(bad, ref+" controller")
				case o.SummonSick != perm.Sick:
					bad = append(bad, fmt.Sprintf("%s sick %v", ref, o.SummonSick))
				case !atk && o.Tapped != perm.Tapped:
					bad = append(bad, fmt.Sprintf("%s tapped %v", ref, o.Tapped))
				case atk && !o.IsAttacking:
					bad = append(bad, ref+" not attacking")
				case o.EnteredThisTurn:
					bad = append(bad, ref+" entered this turn")
				case int(o.Damage) != perm.Damage:
					bad = append(bad, fmt.Sprintf("%s damage %d", ref, o.Damage))
				}
				for kind, n := range perm.Counters {
					if int(o.Counter(counterKinds[kind])) != *n {
						bad = append(bad, fmt.Sprintf("%s %s %d/%d", ref, kind, o.Counter(counterKinds[kind]), *n))
					}
				}
			}
		}
	}
	if len(e.G.Stack) != 0 {
		bad = append(bad, "stack not empty")
	}
	return strings.Join(bad, "; ")
}
