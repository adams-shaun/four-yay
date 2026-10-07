package main

// The pursuit oracle: an independent, brute-force check of the payment
// planner's per-play verdicts (rules.Engine.PotentialPaymentPlans) on the
// SpellBench workup's own game states.
//
// At a builtin seat's priority decision the oracle takes each potential play
// the planner calls "insufficient" (a PROOF of unpayability, which drops the
// play from the seat's candidates) or leaves unpriced ("unsupported",
// "search_limit"), and searches the manual mana surface on engine clones:
// every "activate" option (every mana ability of every source, whatever its
// cost), every answer to the asks an activation poses (the mana wheel, the
// colour, a tapXType or sacrifice choice), in every order, plus a priority
// pass while the stack holds a trigger an activation caused. The play is
// payable exactly when some node of that search offers it. The search knows
// nothing of the planner's census, tiers or cost composition: it only
// submits intents and reads the offered options, so a disagreement is a real
// planner defect, never a shared one.
//
// A "false unpayable" is a play the planner proved unpayable that the
// oracle pays: a lost legal action. TestPursuitOracleSample asserts none on
// a small sample; SB_ORACLE_GAMES=N widens it to the first N workup games
// (all eight decks) for the lane's audit, and SB_ORACLE_OUT names a file for
// the per-case lines.

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

type oraclePlay struct {
	kind    string
	obj     state.ObjID
	ability int
	mode    string
}

func (k oraclePlay) matches(o *decision.Option) bool {
	return o.Kind == k.kind && o.Obj == k.obj && o.Ability == k.ability && o.Mode == k.mode
}

// oracleResult is one search's answer.
type oracleResult struct {
	payable bool
	nodes   int
	limited bool     // the node budget ran out before the space was exhausted
	path    []string // the payer's answers along the paying line
	board   string   // the payer's board at the root (for a report)
}

// oracleSearch reports whether player p, at root's pending priority
// decision, can make play k offered by activating mana abilities alone (any
// sources, any answers, any order) within the current step.
func oracleSearch(root *rules.Engine, p state.PlayerID, k oraclePlay, budget int) oracleResult {
	var res oracleResult
	seen := map[string]bool{} // lookup only
	turn, step, rootStack := root.G.Turn, root.G.Step, len(root.G.Stack)
	var path []string
	label := func(d *decision.Decision, in decision.Intent) string {
		var ls []string
		for _, c := range in.Choices {
			for _, o := range d.Options {
				if o.Index == c {
					ls = append(ls, o.Label)
				}
			}
		}
		return strings.Join(ls, "+")
	}
	var visit func(c *rules.Engine, inActivation bool) bool
	visit = func(c *rules.Engine, inActivation bool) bool {
		if res.nodes >= budget {
			res.limited = true
			return false
		}
		res.nodes++
		// Answer every decision that is not the payer's choice with a
		// minimal answer (an opponent passing, a trigger order).
		for guard := 0; guard < 64; guard++ {
			d := c.Pending()
			if d == nil || c.G.Over || c.G.Turn != turn || c.G.Step != step {
				return false
			}
			if d.Player == p && (d.Kind == decision.KPriority || inActivation) {
				break
			}
			in := oracleMinimal(d)
			if c.Submit(in) != nil {
				return false
			}
		}
		d := c.Pending()
		if d == nil || d.Player != p {
			return false
		}
		if d.Kind != decision.KPriority {
			// An ask the activation poses: branch over its answers.
			for _, in := range oracleAnswers(d) {
				child := c.Clone()
				if child.Submit(in) != nil {
					continue
				}
				path = append(path, label(d, in))
				if visit(child, true) {
					return true
				}
				path = path[:len(path)-1]
			}
			return false
		}
		for i := range d.Options {
			if k.matches(&d.Options[i]) {
				res.payable = true
				res.path = append([]string(nil), path...)
				return true
			}
		}
		key := oracleStateKey(c, p)
		if seen[key] {
			return false
		}
		seen[key] = true
		for i := range d.Options {
			o := &d.Options[i]
			if o.Kind != "activate" {
				continue
			}
			child := c.Clone()
			in := decision.Intent{Seq: d.Seq, Player: p, Choices: []int{o.Index}}
			if child.Submit(in) != nil {
				continue
			}
			path = append(path, label(d, in))
			if visit(child, true) {
				return true
			}
			path = path[:len(path)-1]
		}
		if len(c.G.Stack) > rootStack {
			// A trigger an activation caused (never an object already on
			// the stack at the decision: letting that resolve is a later
			// decision's business): let it resolve, the pool floats
			// (payexec's stack wait).
			for i := range d.Options {
				if d.Options[i].Kind != "pass" {
					continue
				}
				child := c.Clone()
				if child.Submit(decision.Intent{Seq: d.Seq, Player: p, Choices: []int{d.Options[i].Index}}) != nil {
					continue
				}
				path = append(path, "(pass: resolve)")
				if visit(child, false) {
					return true
				}
				path = path[:len(path)-1]
			}
		}
		return false
	}
	visit(root.Clone(), false)
	var b strings.Builder
	fmt.Fprintf(&b, "life %d pool %v;", root.G.Players[p].Life, root.G.Players[p].Pool)
	for _, id := range root.G.Zone(state.ZBattlefield, p) {
		o := root.G.Obj(id)
		fmt.Fprintf(&b, " %s#%d", o.Face().Name, id)
		if o.Tapped {
			b.WriteString("(T)")
		}
		if o.SummonSick {
			b.WriteString("(sick)")
		}
		if len(o.Counters) > 0 {
			fmt.Fprintf(&b, "%v", o.Counters)
		}
	}
	res.board = b.String()
	return res
}

// oracleMinimal is the minimal valid answer to a decision the search does
// not branch on: pass at priority, else the clamped empty answer.
func oracleMinimal(d *decision.Decision) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	if d.Kind == decision.KPriority {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return in
			}
		}
	}
	return botpolicy.Clamp(d, in)
}

// oracleAnswers enumerates the answers to an activation's ask: every single
// option (and the empty answer when Min is 0), or for a Min == Max == n
// unit-major allocation every per-unit combination; any other shape gets its
// minimal answer only.
func oracleAnswers(d *decision.Decision) []decision.Intent {
	var out []decision.Intent
	mk := func(ch []int) decision.Intent {
		return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
	}
	switch {
	case d.Max <= 1:
		if d.Min == 0 {
			out = append(out, mk(nil))
		}
		for _, o := range d.Options {
			out = append(out, mk([]int{o.Index}))
		}
	case d.Min == d.Max && d.Min > 1 && len(d.Options)%d.Min == 0:
		n, per := d.Min, len(d.Options)/d.Min
		idx := make([]int, n)
		for {
			ch := make([]int, n)
			for u := 0; u < n; u++ {
				ch[u] = d.Options[u*per+idx[u]].Index
			}
			out = append(out, mk(ch))
			u := n - 1
			for u >= 0 {
				idx[u]++
				if idx[u] < per {
					break
				}
				idx[u] = 0
				u--
			}
			if u < 0 || len(out) > 256 {
				break
			}
		}
	default:
		out = append(out, oracleMinimal(d))
	}
	var valid []decision.Intent
	for _, in := range out {
		if d.Validate(in) == nil {
			valid = append(valid, in)
		}
	}
	return valid
}

// oracleStateKey identifies a payer state for the search's dedup: the pool
// (every unit), each own permanent's tapped bit and counters, the zone
// sizes a sacrifice changes, and the stack height.
func oracleStateKey(c *rules.Engine, p state.PlayerID) string {
	var b strings.Builder
	pl := c.G.Players[p]
	fmt.Fprintf(&b, "%v|%v|%v|", pl.Pool, pl.Snow, pl.ManaUnits())
	for _, id := range c.G.Zone(state.ZBattlefield, p) {
		o := c.G.Obj(id)
		fmt.Fprintf(&b, "%d:%v:%v;", id, o.Tapped, o.Counters)
	}
	fmt.Fprintf(&b, "|g%d|h%d|s%d", len(c.G.Zone(state.ZGraveyard, p)), len(c.G.Zone(state.ZHand, p)), len(c.G.Stack))
	return b.String()
}

// oracleCase is one audited verdict.
type oracleCase struct {
	game, deck, label, verdict string
	res                        oracleResult
}

// oracleAudit plays the sb workup schedule's first nGames games (the four
// builtin policies, all eight pauper-kernel decks) and audits a sample of the
// planner's verdicts at the builtin seats' priority decisions: every
// sampleEvery-th insufficient or unpriced play.
func oracleAudit(t *testing.T, nGames, sampleEvery, budget int, allUnpriced bool) []oracleCase {
	reg := testutil.CorpusRegistry(t)
	setTacticalRegistry(reg)
	bots := []string{"sb-uniform", "sb-heuristic", "sb-uniform-planned", "sb-heuristic-planned"}
	sched := sbSchedule(bots, spellbench.BenchmarkPool, 8, 20260926)
	// Interleave the schedule so a short run still covers every deck and
	// matchup: take game i*stride for a stride coprime with the schedule.
	stride := 97
	var cases []oracleCase
	seq := 0
	for n := 0; n < nGames && n < len(sched); n++ {
		g := sched[(n*stride)%len(sched)]
		deck, err := spellbench.Deck(reg, spellbench.PauperKernel, g.deck)
		if err != nil {
			t.Fatal(err)
		}
		seats := make([]seat.Seat, 2)
		for s := 0; s < 2; s++ {
			s0, err := registry.Build(g.seats[s], g.seed^uint64(s+1))
			if err != nil {
				t.Fatal(err)
			}
			seats[s] = s0
		}
		var res sbResult
		inner := sbSubmitWithFallback(seats, &res)
		cfg := rules.Config{Seed: g.seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens, NameUniverse: reg.AllCards()}
		hooks := gbench.Hooks{
			Setup: func(e *rules.Engine) {
				for _, st := range seats {
					if b, ok := registry.UnwrapSeat(st).(*builtins.Seat); ok {
						b.SetPlanner(e)
					}
				}
			},
			Submit: func(e *rules.Engine, seatIdx int, d *decision.Decision, in decision.Intent) (bool, error) {
				if d.Kind == decision.KPriority {
					for _, pp := range e.PotentialPaymentPlans(d.Player) {
						if pp.Reason == "" || pp.Reason == "ambiguous" || pp.Action.Kind == "play_land" {
							continue
						}
						if !(allUnpriced && (pp.Reason == "unsupported" || pp.Reason == "search_limit")) {
							seq++
							if seq%sampleEvery != 0 {
								continue
							}
						}
						a := pp.Action
						r := oracleSearch(e, d.Player, oraclePlay{a.Kind, a.Obj, a.Ability, a.Mode}, budget)
						verdict := pp.Reason + ":" + pp.Detail
						if pp.Reason == "unsupported" || pp.Reason == "search_limit" {
							// The seat's exact fallback decides these when
							// chosen: audit its verdict instead.
							_, exact := e.PotentialPlayScript(d.Player, a, 2000)
							verdict = "exact:" + exact + " (" + verdict + ")"
						}
						cases = append(cases, oracleCase{game: g.id, deck: g.deck, label: a.Label, verdict: verdict, res: r})
					}
				}
				return inner(e, seatIdx, d, in)
			},
		}
		if _, _, err := gbench.PlayGame(cfg, seats, 0, 20000, hooks); err != nil {
			t.Fatalf("%s: %v", g.id, err)
		}
	}
	return cases
}

// TestPursuitOracleSample: no play the planner proves unpayable is payable
// on the manual surface.
func TestPursuitOracleSample(t *testing.T) {
	nGames, sampleEvery, budget := 4, 7, 4000
	if s := os.Getenv("SB_ORACLE_GAMES"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		nGames, sampleEvery = n, 11
		if s := os.Getenv("SB_ORACLE_EVERY"); s != "" {
			if sampleEvery, err = strconv.Atoi(s); err != nil || sampleEvery < 1 {
				t.Fatalf("SB_ORACLE_EVERY=%q", s)
			}
		}
	} else if testing.Short() {
		t.Skip("oracle audit plays full games")
	}
	// SB_ORACLE_UNPRICED=1 audits every unpriced verdict (the seat's exact
	// fallback decides those), not a sample.
	cases := oracleAudit(t, nGames, sampleEvery, budget, os.Getenv("SB_ORACLE_UNPRICED") != "")
	type agg struct{ n, payable, unpayable, limited int }
	byVerdict := map[string]*agg{} // lookup only; printed sorted
	byDeck := map[string]*agg{}    // lookup only; printed sorted
	var lines []string
	var falseUnpayable []string
	for _, c := range cases {
		for _, kv := range [][2]any{{byVerdict, c.verdict}, {byDeck, c.deck}} {
			m, key := kv[0].(map[string]*agg), kv[1].(string)
			a := m[key]
			if a == nil {
				a = &agg{}
				m[key] = a
			}
			a.n++
			switch {
			case c.res.payable:
				a.payable++
			case c.res.limited:
				a.limited++
			default:
				a.unpayable++
			}
		}
		outcome := "unpayable"
		if c.res.payable {
			outcome = "PAYABLE"
		} else if c.res.limited {
			outcome = "limited"
		}
		lines = append(lines, fmt.Sprintf("%s %-8s %-40s %-9s nodes=%d %q", c.game, c.deck, c.verdict, outcome, c.res.nodes, c.label))
		if c.res.payable && (strings.HasPrefix(c.verdict, "insufficient") || strings.HasPrefix(c.verdict, "exact:insufficient")) {
			falseUnpayable = append(falseUnpayable, lines[len(lines)-1]+"\n    board: "+c.res.board+"\n    paying line: "+strings.Join(c.res.path, " | "))
		}
	}
	report := func(title string, m map[string]*agg) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Logf("%s", title)
		for _, k := range keys {
			a := m[k]
			t.Logf("  %-44s cases %5d  payable %5d  unpayable %5d  limited %4d", k, a.n, a.payable, a.unpayable, a.limited)
		}
	}
	t.Logf("oracle: %d cases", len(cases))
	report("by verdict:", byVerdict)
	report("by deck:", byDeck)
	if out := os.Getenv("SB_ORACLE_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	shown := 0
	for _, c := range cases {
		if strings.HasPrefix(c.verdict, "exact:search_limit") && shown < 12 {
			shown++
			t.Logf("exact search over budget: %s %s %q oracle payable=%v nodes=%d\n    board: %s\n    paying line: %s",
				c.game, c.deck, c.label, c.res.payable, c.res.nodes, c.res.board, strings.Join(c.res.path, " | "))
		}
	}
	for _, l := range falseUnpayable {
		t.Errorf("false unpayable: %s", l)
	}
}
