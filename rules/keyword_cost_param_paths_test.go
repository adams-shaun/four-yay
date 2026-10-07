// keyword_cost_param_paths_test.go — end-to-end coverage for the cost-bearing
// keyword readers that hand a KeywordWithCost parameter to ParseCost: the
// cast_begin.go Plot/Miracle cast branches, the replicate/multikicker/squad
// capture sites, the legal_walk_hand.go and legal_plotzone_walk.go Plot OFFER
// walks, and the potential_plan.go planner's Plot/Miracle base costs.
//
// A synthetic line with a trailing colon-field is the only way to make the
// first-field read (cards.Face.KeywordCostParam, Forge's
// KeywordWithCost.parse) differ from KeywordParam's whole remainder. The
// corpus carries no second-colon line for any of these heads today, so every
// test here asserts its own precondition that the two reads really differ --
// reverted to KeywordParam, ParseCost would see the trailing field as Unknown
// and the reader would mis-price or withhold the play.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestKeywordCostParamCapturesStoreFirstField drives the three cast_begin.go
// capture sites through beginCast: the raw keyword parameter a replicated /
// multikicked / squadded cast stores for the later payment re-parse must be
// the FIRST field, not the whole remainder (the ask handler re-parses it and
// would charge a different cost than the offer gate priced).
func TestKeywordCostParamCapturesStoreFirstField(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		head     string
		mode     string
		body     string
		want     string
		captured func(*pendingCast) (string, bool)
	}{
		{"Replicate", "replicated", "3 U:ReduceCost$ X:reminder", "3 U",
			func(pc *pendingCast) (string, bool) { return pc.replicateParam, pc.replicateSet }},
		{"Multikicker", "multikicked", "2 B:ReduceCost$ X:reminder", "2 B",
			func(pc *pendingCast) (string, bool) { return pc.multikickParam, pc.multikickSet }},
		{"Squad", "squadded", "4 R:ReduceCost$ X:reminder", "4 R",
			func(pc *pendingCast) (string, bool) { return pc.squadParam, pc.squadSet }},
	} {
		tc := tc
		t.Run(tc.head, func(t *testing.T) {
			t.Parallel()
			e := handEngine(t, card(t, "Name:Synth "+tc.head+"\nManaCost:0\nTypes:Sorcery\nK:"+tc.head+":"+tc.body+"\nOracle:x\n"))
			id := e.G.Zone(state.ZHand, 0)[0]
			// Precondition: the whole-remainder read differs from the cost
			// field, and handing it to ParseCost really does leak Unknown --
			// so the two reads are observably different here.
			f := e.G.Obj(id).Face()
			whole, ok := f.KeywordParam(tc.head)
			if !ok || whole == tc.want {
				t.Fatalf("precondition: KeywordParam(%q) = %q %v, want the whole remainder (not %q)", tc.head, whole, ok, tc.want)
			}
			if pc := ParseCost(whole); len(pc.Unknown) == 0 {
				t.Fatalf("precondition: ParseCost(%q) runs clean; the trailing field would not be observable", whole)
			}
			// Fund the first-field cost so the cast is payable and the ask is
			// posed (the capture runs before the ask).
			e.G.Players[0].Pool[state.MC] = 5
			e.G.Players[0].Pool[state.MU] = 1
			e.G.Players[0].Pool[state.MB] = 1
			e.G.Players[0].Pool[state.MR] = 1
			castMode(t, e, id, tc.mode)
			if e.cast == nil {
				t.Fatalf("cast did not begin for mode %q; pending=%v", tc.mode, e.Pending())
			}
			got, set := tc.captured(e.cast)
			if !set || got != tc.want {
				t.Fatalf("%s capture = %q set=%v, want %q true (the first field, not the whole remainder %q)",
					tc.head, got, set, tc.want, whole)
			}
			// The ask that re-parses the capture must be the next decision:
			// this proves the capture fed the payment path, not just a field.
			if d := e.Pending(); d == nil || d.Kind != decision.KChoose {
				t.Fatalf("after the %s cast, pending = %+v, want the count ask", tc.head, d)
			}
		})
	}
}

// TestKeywordCostParamPlotOfferAndCastUseFirstField pins the Plot OFFER walk
// (legal_walk_hand.go) and the Plot CAST branch (cast_begin.go) to the same
// first field: a line carrying a trailing colon-field is offered with the
// first-field pool and the cast charges exactly that field.
func TestKeywordCostParamPlotOfferAndCastUseFirstField(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:Synth Plot\nManaCost:0\nTypes:Sorcery\nK:Plot:2 G:ReduceCost$ X:reminder\nOracle:x\n"))
	id := e.G.Zone(state.ZHand, 0)[0]
	f := e.G.Obj(id).Face()
	// Precondition: offer-whole and offer-first-field really differ.
	whole, ok := f.KeywordParam("Plot")
	if !ok || whole == "2 G" {
		t.Fatalf("precondition: KeywordParam(Plot) = %q %v, want the whole remainder", whole, ok)
	}
	if pc := ParseCost(whole); len(pc.Unknown) == 0 {
		t.Fatalf("precondition: ParseCost(%q) runs clean; the trailing field would not be observable", whole)
	}
	// Fund exactly the FIRST field ({2}{G}): a whole-remainder offer prices
	// {4} plus Unknown and must be withheld.
	e.G.Players[0].Pool[state.MC] = 2
	e.G.Players[0].Pool[state.MG] = 1
	if findMode(e.legalActions(0), "plot") < 0 {
		t.Fatalf("the plot offer was withheld though the first field is payable: %+v", e.legalActions(0))
	}
	// The cast must pay the same first field and exile with the designation.
	castMode(t, e, id, "plot")
	o := e.G.Obj(id)
	if o.Zone != state.ZExile || o.PlottedTurn != e.G.Turn {
		t.Fatalf("plot cast: zone=%s plottedTurn=%d turn=%d, want exile with the designation", o.Zone, o.PlottedTurn, e.G.Turn)
	}
	if p := e.G.Players[0].Pool; p.Total() != 0 {
		t.Fatalf("plot cast pool = %v, want empty (exactly the first-field {2}{G} paid)", p)
	}
}

// TestKeywordCostParamPlotZoneWalkUsesFirstField covers the library-top Plot
// walk (legal_plotzone_walk.go): a face carrying its own Plot line with a
// trailing field, selected by a PlotZone static, must be offered by the first
// field and not the whole remainder.
func TestKeywordCostParamPlotZoneWalkUsesFirstField(t *testing.T) {
	t.Parallel()
	top := card(t, "Name:Synth Zone Bear\nManaCost:1 U\nTypes:Creature Bear\nPT:2/2\nK:Plot:1 U:ReduceCost$ X:reminder\nOracle:x\n")
	e, id := plotZoneEngineForTest(t, true, top)
	if got := e.G.Zone(state.ZLibrary, 0)[0]; got != id {
		t.Fatalf("precondition: library top=%d, want object %d", got, id)
	}
	f := e.G.Obj(id).Face()
	whole, ok := f.KeywordParam("Plot")
	if !ok || whole == "1 U" {
		t.Fatalf("precondition: KeywordParam(Plot) = %q %v, want the whole remainder", whole, ok)
	}
	if pc := ParseCost(whole); len(pc.Unknown) == 0 {
		t.Fatalf("precondition: ParseCost(%q) runs clean; the trailing field would not be observable", whole)
	}
	// The pool already holds {1}{U} (plotZoneEngineForTest), exactly the first
	// field; a whole-remainder read prices {3} plus Unknown and is withheld.
	idx := findMode(e.legalActions(0), "plot")
	if idx < 0 || e.legalActions(0)[idx].Obj != id {
		t.Fatalf("the PlotZone offer was withheld though the first field is payable: %+v", e.legalActions(0))
	}
}

// TestKeywordCostParamPlannerPlotAndMiracleUseFirstField drives
// potential_plan.go's potentialModeBaseCost directly (the planner's own base
// cost for the plot and miracle modes) and asserts it composes the first
// field, never the whole remainder.
func TestKeywordCostParamPlannerPlotAndMiracleUseFirstField(t *testing.T) {
	t.Parallel()
	green := func(n int32) state.Mana {
		var m state.Mana
		m[state.MG] = n
		return m
	}
	white := func(n int32) state.Mana {
		var m state.Mana
		m[state.MW] = n
		return m
	}
	for _, tc := range []struct {
		head string
		mode string
		body string
		want Cost
	}{
		{"Plot", "plot", "2 G:ReduceCost$ X:reminder", Cost{Generic: 2, Colored: green(1)}},
		{"Miracle", "miracle", "1 W:ReduceCost$ X:reminder", Cost{Generic: 1, Colored: white(1)}},
	} {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()
			e := handEngine(t, card(t, "Name:Synth Plan\nManaCost:0\nTypes:Sorcery\nK:"+tc.head+":"+tc.body+"\nOracle:x\n"))
			id := e.G.Zone(state.ZHand, 0)[0]
			f := e.G.Obj(id).Face()
			whole, ok := f.KeywordParam(tc.head)
			if !ok || whole == cards.KeywordCostField(whole) {
				t.Fatalf("precondition: KeywordParam(%q) = %q %v, want the whole remainder", tc.head, whole, ok)
			}
			if pc := ParseCost(whole); len(pc.Unknown) == 0 {
				t.Fatalf("precondition: ParseCost(%q) runs clean; the trailing field would not be observable", whole)
			}
			got, ok := e.potentialModeBaseCost(0, id, f, decision.Option{Kind: "cast", Obj: id, Mode: tc.mode})
			if !ok {
				t.Fatalf("potentialModeBaseCost(mode=%q) ok = false, want the first-field cost", tc.mode)
			}
			if len(got.Unknown) != 0 {
				t.Fatalf("potentialModeBaseCost(mode=%q) carries Unknown %v -- the whole remainder reached ParseCost", tc.mode, got.Unknown)
			}
			if got.Generic != tc.want.Generic || got.Colored != tc.want.Colored {
				t.Fatalf("potentialModeBaseCost(mode=%q) = generic %d colored %v, want the first-field cost", tc.mode, got.Generic, got.Colored)
			}
		})
	}
}

// TestKeywordCostParamMiracleCastUsesFirstField is the Miracle cast branch
// (cast_begin.go) end to end: a first-draw Miracle offer whose line carries a
// trailing colon-field accepts for the first-field cost and puts the spell on
// the stack flagged as a Miracle cast, leaving the pool empty.
func TestKeywordCostParamMiracleCastUsesFirstField(t *testing.T) {
	t.Parallel()
	spell := "Name:Synth Miracle\nManaCost:4 W W\nTypes:Sorcery\nK:Miracle:1 W:ReduceCost$ X:reminder\n" +
		"A:SP$ GainLife | Life$ 1\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 88101, spell)
	moveToLibraryTop(t, e, id)
	f := e.G.Obj(id).Face()
	whole, ok := f.KeywordParam("Miracle")
	if !ok || whole == "1 W" {
		t.Fatalf("precondition: KeywordParam(Miracle) = %q %v, want the whole remainder", whole, ok)
	}
	if pc := ParseCost(whole); len(pc.Unknown) == 0 {
		t.Fatalf("precondition: ParseCost(%q) runs clean; the trailing field would not be observable", whole)
	}
	// Fund exactly the first field ({1}{W}).
	addMana(t, e, 0, "WW")
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	if len(e.pendingTriggers) != 1 || !e.pendingTriggers[0].Miracle {
		t.Fatalf("no miracle offer queued: %+v", e.pendingTriggers)
	}
	e.pending = nil
	e.Advance()
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional || d.Player != 0 {
		t.Fatalf("miracle offer decision = %+v, want a seat-0 trigger-optional ask", d)
	}
	submitChoices(t, e, 0) // accept
	o := e.G.Obj(id)
	if o.Zone != state.ZStack || o.CastFlags&state.FlagMiracle == 0 {
		t.Fatalf("miracle cast: zone=%s flags=%d pool=%d, want a flagged Miracle spell on the stack",
			o.Zone, o.CastFlags, e.G.Players[0].Pool.Total())
	}
	if p := e.G.Players[0].Pool; p.Total() != 0 {
		t.Fatalf("miracle cast pool = %v, want empty (exactly the first-field {1}{W} paid)", p)
	}
	replayCheck(t, e, cfg)
}
